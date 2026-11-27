//! video 原语：ffmpeg 子进程解码器（Rust 侧）。
//!
//! 与 Skia 后端同构：读取节点 src，spawn ffmpeg 解码成 RGBA 原始帧，
//! 双缓冲维护"当前帧"，绘制时贴到 video 节点盒子。无 ffmpeg 时安全降级。
//!
//! 不引入 ffmpeg-next crate：其链接需要 libav*.pc/头文件（本机缺失），
//! 静态库分发更与 cgo 冲突。用系统 ffmpeg 可执行文件做子进程，零编译依赖。
//!
//! 线程模型：每个视频一个解码线程写 Arc<RwLock<VideoFrame>>，UI 渲染线程读。
//! 新帧到达时置全局 FRAME_SEQ，render() 据此判断是否需要重绘（frame_dirty）。

use std::collections::HashMap;
use std::io::Read;
use std::process::{Child, Command, Stdio};
use std::sync::{Arc, Mutex, RwLock};
use std::time::{Duration, Instant};

/// 一帧 RGBA8888 解码结果。
#[derive(Clone)]
pub struct VideoFrame {
    pub data: Vec<u8>,
    pub w: u32,
    pub h: u32,
    pub pts: f64,
}

/// 解码器回传给宿主的事件。
#[derive(Clone, Debug)]
pub struct VideoEvent {
    pub node_id: String,
    pub kind: String, // videostate / videometa / videotime
    pub text: String,
}

/// 解码参数（来自节点 Props）。
#[derive(Clone, Debug, PartialEq)]
pub struct VideoSpec {
    pub src: String,
    pub playback_id: String,
    pub seek: String,
    pub playing: bool,
    pub autoplay: bool,
    pub loop_: bool,
    pub muted: bool,
}

/// 单个视频的解码器句柄。
struct Decoder {
    spec: VideoSpec,
    frame: Arc<RwLock<Option<VideoFrame>>>,
    paused: Arc<RwLock<bool>>,
    stop: Arc<RwLock<bool>>,
    events: Arc<Mutex<Vec<VideoEvent>>>,
    child: Arc<Mutex<Option<Child>>>,
}

/// 判断两个 spec 是否需要重启解码进程（playing 变化只需暂停/恢复）。
fn needs_restart(a: &VideoSpec, b: &VideoSpec) -> bool {
    a.src != b.src
        || a.playback_id != b.playback_id
        || a.seek != b.seek
        || a.autoplay != b.autoplay
        || a.loop_ != b.loop_
        || a.muted != b.muted
}

impl Decoder {
    fn stop(&mut self) {
        *self.stop.write().unwrap() = true;
        if let Some(c) = self.child.lock().unwrap().as_mut() {
            let _ = c.kill();
            let _ = c.wait();
        }
    }

    fn set_paused(&self, paused: bool) {
        *self.paused.write().unwrap() = paused;
    }
}

/// 全局解码管理器。
struct Manager {
    decoders: HashMap<String, Decoder>, // key = node_id
    events: Vec<VideoEvent>,
    frame_seq: u64,
    /// ffmpeg 路径缓存（None=未探测，Some("")=缺失）。
    bin: Option<String>,
}

fn manager() -> &'static Mutex<Manager> {
    static M: std::sync::OnceLock<Mutex<Manager>> = std::sync::OnceLock::new();
    M.get_or_init(|| {
        Mutex::new(Manager {
            decoders: HashMap::new(),
            events: Vec::new(),
            frame_seq: 0,
            bin: None,
        })
    })
}

fn ffmpeg_bin() -> String {
    let mut m = manager().lock().unwrap();
    if let Some(ref b) = m.bin {
        return b.clone();
    }
    let bin = std::env::var("FFMPEG_PATH")
        .ok()
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty() && std::path::Path::new(s).exists())
        .or_else(|| which("ffmpeg"))
        .unwrap_or_default();
    m.bin = Some(bin.clone());
    bin
}

/// 最小 which：在 PATH 里找可执行文件。
fn which(name: &str) -> Option<String> {
    let path = std::env::var("PATH").ok()?;
    for dir in path.split(':') {
        let p = std::path::Path::new(dir).join(name);
        if p.is_file() {
            return Some(p.to_string_lossy().into_owned());
        }
    }
    None
}

/// 同步解码器集合：对每个 node_id 确保 spec 对应的解码器；清理消失节点。
/// 由 render() 在 DisplayList 构建后调用。
pub fn sync(specs: &[(String, VideoSpec)]) {
    let bin = ffmpeg_bin();
    let seen: std::collections::HashSet<String> = specs.iter().map(|(id, _)| id.clone()).collect();
    // 需要停止的旧解码器先摘出来，在释放 manager 锁后再 stop/kill——
    // 解码线程的 bump_seq 会锁 manager，持锁 stop 存在死锁风险。
    let mut to_stop: Vec<Decoder> = Vec::new();
    let mut to_spawn: Vec<(String, VideoSpec)> = Vec::new();

    {
        let mut m = manager().lock().unwrap();
        for (id, spec) in specs {
            let need_restart = match m.decoders.get(id) {
                None => true,
                Some(d) => needs_restart(&d.spec, spec),
            };
            // playing 变化：不重启，只切暂停标志。
            if !need_restart {
                if let Some(d) = m.decoders.get_mut(id) {
                    d.spec.playing = spec.playing;
                    let want_paused = !spec.playing;
                    if *d.paused.read().unwrap() != want_paused {
                        d.set_paused(want_paused);
                    }
                }
                continue;
            }
            if let Some(old) = m.decoders.remove(id) {
                to_stop.push(old);
            }
            to_spawn.push((id.clone(), spec.clone()));
        }
        // 清理消失节点。
        let gone: Vec<String> = m
            .decoders
            .keys()
            .filter(|k| !seen.contains(*k))
            .cloned()
            .collect();
        for id in gone {
            if let Some(d) = m.decoders.remove(&id) {
                to_stop.push(d);
            }
        }
    }

    // 锁外停止旧解码器。
    for mut d in to_stop {
        d.stop();
    }
    // 锁外启动新解码器，再写回表。
    for (id, spec) in to_spawn {
        let frame = Arc::new(RwLock::new(None::<VideoFrame>));
        let paused = Arc::new(RwLock::new(!spec.autoplay && !spec.playing));
        let stop = Arc::new(RwLock::new(false));
        let events = Arc::new(Mutex::new(Vec::new()));
        let child: Arc<Mutex<Option<Child>>> = Arc::new(Mutex::new(None));
        let decoder = Decoder {
            spec: spec.clone(),
            frame: frame.clone(),
            paused: paused.clone(),
            stop: stop.clone(),
            events: events.clone(),
            child: child.clone(),
        };
        if bin.is_empty() || spec.src.trim().is_empty() {
            events.lock().unwrap().push(VideoEvent {
                node_id: id.clone(),
                kind: "videostate".into(),
                text: "error".into(),
            });
        } else {
            spawn_decode(
                bin.clone(),
                id.clone(),
                spec.clone(),
                frame,
                paused,
                stop,
                events,
                child,
            );
        }
        manager().lock().unwrap().decoders.insert(id, decoder);
    }
}

/// 当前帧（渲染线程读）。
pub fn frame(id: &str) -> Option<VideoFrame> {
    let m = manager().lock().unwrap();
    m.decoders
        .get(id)
        .and_then(|d| d.frame.read().unwrap().clone())
}

/// drain 事件（render 后由 lib.rs 经 eventCb 回传 Go）。
pub fn take_events() -> Vec<VideoEvent> {
    let mut m = manager().lock().unwrap();
    let mut out = std::mem::take(&mut m.events);
    // 同时收集各解码器线程写入的事件。
    for d in m.decoders.values() {
        let mut evs = d.events.lock().unwrap();
        out.append(&mut evs);
    }
    out
}

/// 当前帧序号（每次新帧 +1）。render() 比较它判断是否需要重绘。
pub fn frame_seq() -> u64 {
    manager().lock().unwrap().frame_seq
}

/// 是否存在正在播放的视频解码器。事件驱动重绘用它判断是否需要持续调度帧：
/// 有视频在播放时不能停，否则新帧虽写入 frame_seq 却无人请求重绘而卡住画面。
/// 保守返回 true——宁可多绘几帧，也不能让播放中的视频停在旧画面。
pub fn has_active() -> bool {
    let m = manager().lock().unwrap();
    m.decoders
        .values()
        .any(|d| !*d.stop.read().unwrap() && d.spec.playing && !*d.paused.read().unwrap())
}

fn bump_seq() {
    let mut m = manager().lock().unwrap();
    m.frame_seq = m.frame_seq.wrapping_add(1);
}

/// 启动一个 ffmpeg 解码线程。
fn spawn_decode(
    bin: String,
    node_id: String,
    spec: VideoSpec,
    frame: Arc<RwLock<Option<VideoFrame>>>,
    paused: Arc<RwLock<bool>>,
    stop: Arc<RwLock<bool>>,
    events: Arc<Mutex<Vec<VideoEvent>>>,
    child_slot: Arc<Mutex<Option<Child>>>,
) {
    std::thread::spawn(move || {
        let mut args: Vec<String> = vec![
            "-hide_banner".into(),
            "-loglevel".into(),
            "error".into(),
            "-nostdin".into(),
        ];
        if spec.loop_ {
            args.push("-stream_loop".into());
            args.push("-1".into());
        }
        if let Ok(seek) = spec.seek.parse::<f64>() {
            if seek > 0.0 {
                args.push("-ss".into());
                args.push(seek.to_string());
            }
        }
        args.push("-i".into());
        args.push(spec.src.clone());
        if spec.muted {
            args.push("-an".into());
        }
        // 尺寸由 probe 得到；失败用默认。
        let (w, h, duration) = probe(&bin, &spec.src);
        if let Some(meta) = serde_json::to_string(&serde_json::json!({
            "duration": duration, "w": w, "h": h,
        }))
        .ok()
        {
            events.lock().unwrap().push(VideoEvent {
                node_id: node_id.clone(),
                kind: "videometa".into(),
                text: meta,
            });
        }
        let (w, h) = if w > 0 && h > 0 { (w, h) } else { (640, 360) };
        args.push("-vf".into());
        args.push(format!("scale={}:{}", w, h));
        args.push("-f".into());
        args.push("rawvideo".into());
        args.push("-pix_fmt".into());
        args.push("rgba".into());
        args.push("-vsync".into());
        args.push("0".into());
        args.push("pipe:1".into());

        let result = Command::new(&bin)
            .args(&args)
            .stdin(Stdio::null())
            .stderr(Stdio::null())
            .stdout(Stdio::piped())
            .spawn();

        let mut child = match result {
            Ok(c) => c,
            Err(_) => {
                events.lock().unwrap().push(VideoEvent {
                    node_id: node_id.clone(),
                    kind: "videostate".into(),
                    text: "error".into(),
                });
                return;
            }
        };
        events.lock().unwrap().push(VideoEvent {
            node_id: node_id.clone(),
            kind: "videostate".into(),
            text: if *paused.read().unwrap() {
                "paused".into()
            } else {
                "playing".into()
            },
        });

        let mut stdout = match child.stdout.take() {
            Some(s) => s,
            None => return,
        };
        // 把 Child 放回共享槽（stdout 已取走），stop() 可随时 kill 解除阻塞读。
        *child_slot.lock().unwrap() = Some(child);
        let frame_size = (w * h * 4) as usize;
        let mut buf = vec![0u8; frame_size];
        let mut idx = 0u64;
        let mut last_time = Instant::now();
        loop {
            if *stop.read().unwrap() {
                break;
            }
            let mut filled = 0usize;
            while filled < frame_size {
                match stdout.read(&mut buf[filled..]) {
                    Ok(0) => break,
                    Ok(n) => filled += n,
                    Err(_) => break,
                }
            }
            if filled < frame_size {
                break;
            }
            let pts = idx as f64 / 30.0;
            idx += 1;
            if !*paused.read().unwrap() {
                *frame.write().unwrap() = Some(VideoFrame {
                    data: buf.clone(),
                    w,
                    h,
                    pts,
                });
                bump_seq();
            }
            if last_time.elapsed() >= Duration::from_millis(250) {
                last_time = Instant::now();
                events.lock().unwrap().push(VideoEvent {
                    node_id: node_id.clone(),
                    kind: "videotime".into(),
                    text: format!("{:.2}", pts),
                });
            }
        }
        if let Some(c) = child_slot.lock().unwrap().as_mut() {
            let _ = c.kill();
            let _ = c.wait();
        }
    });
}

/// probe 视频宽高与时长（ffprobe）。
fn probe(bin: &str, src: &str) -> (u32, u32, f64) {
    let probe_bin = bin.replacen("ffmpeg", "ffprobe", 1);
    let out = Command::new(&probe_bin)
        .args([
            "-v",
            "error",
            "-select_streams",
            "v:0",
            "-show_entries",
            "stream=width,height",
            "-of",
            "json",
            src,
        ])
        .output();
    let (w, h) = match out {
        Ok(o) if o.status.success() => {
            let txt = String::from_utf8_lossy(&o.stdout);
            // 极简 JSON 提取（避免依赖 serde_json::Value 之外的解析）。
            let width = extract_num(&txt, "width");
            let height = extract_num(&txt, "height");
            (width, height)
        }
        _ => (0, 0),
    };
    let duration = Command::new(&probe_bin)
        .args([
            "-v",
            "error",
            "-show_entries",
            "format=duration",
            "-of",
            "default=nw=1:nk=1",
            src,
        ])
        .output()
        .ok()
        .and_then(|o| {
            String::from_utf8_lossy(&o.stdout)
                .trim()
                .parse::<f64>()
                .ok()
        })
        .unwrap_or(0.0);
    (w, h, duration)
}

fn extract_num(json: &str, key: &str) -> u32 {
    let needle = format!("\"{}\":", key);
    if let Some(i) = json.find(&needle) {
        let rest = &json[i + needle.len()..];
        let digits: String = rest
            .chars()
            .skip_while(|c| c.is_whitespace())
            .take_while(|c| c.is_ascii_digit())
            .collect();
        return digits.parse().unwrap_or(0);
    }
    0
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn extract_num_basic() {
        assert_eq!(
            extract_num(r#"{"width": 160, "height": 120}"#, "width"),
            160
        );
        assert_eq!(extract_num(r#"{"width":160,"height":120}"#, "height"), 120);
        assert_eq!(extract_num("no json", "width"), 0);
    }

    #[test]
    fn no_ffmpeg_degrades_to_error_event() {
        // 用不存在的 src 触发解码失败；不应 panic。
        let spec = VideoSpec {
            src: "/nonexistent/video.mp4".into(),
            playback_id: "1".into(),
            seek: "0".into(),
            playing: true,
            autoplay: true,
            loop_: false,
            muted: true,
        };
        sync(&[("n1".into(), spec)]);
        // 清理
        sync(&[]);
    }
}
