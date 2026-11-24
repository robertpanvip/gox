//! GX 的 Rust 渲染后端 —— C ABI 入口。
//!
//! Go 侧通过 cgo 调用：
//!   - gx_window_new(w, h, title)        创建窗口句柄
//!   - gx_window_draw(handle, buf, len)  传入二进制场景帧（零拷贝解析）
//!   - gx_window_run(handle)             跑 winit 事件循环（阻塞）
//!   - gx_window_close(handle)           关闭
//!   - gx_window_set_event_cb(cb, ctx)   注册事件回调（click/IME/backspace）
//!
//! 线程模型：wgpu surface 与事件循环绑定在 gx_window_run() 所在线程。
//! 该线程由 Go 侧 goroutine 经 cgo 进入，并非进程主线程，故 Linux 下显式开启
//! winit 的 any_thread（官方嵌入宿主 API）；Windows 无主线程限制。事件循环线程
//! 由本后端自管理，Go 侧不再用 LockOSThread 试图绑定（评审 P0）。
//!
//! 帧传输与生命周期（评审 P0）：
//!   - draw() 把帧 push 进 FIFO 队列并通过 EventLoopProxy 唤醒事件循环；事件循环
//!     每次 redraw 排空队列、按顺序 apply，杜绝单槽覆盖导致的增量丢失。
//!   - Backend 用 Arc 引用计数管理：run 持有强引用直到事件循环退出；close 只发
//!     退出请求，不释放内存；内存由最后离开的引用（gx_window_destroy / run 结束）
//!     释放，彻底消除 Run/Close 竞争下的 use-after-free。
//!   - 重绘改为事件驱动：新帧/尺寸/交互状态/动画/视频才请求重绘，静止时停止。

mod binprotocol;
mod canvas;
mod image;
mod protocol;
mod render;
mod scene;
mod style;
mod video;

use render::Renderer;
use std::collections::VecDeque;
use std::ffi::CString;
use std::os::raw::{c_char, c_int, c_uchar, c_void};
use std::sync::{Arc, Mutex};
use winit::{
    dpi::LogicalSize,
    event::{ElementState, Ime, MouseButton, WindowEvent},
    event_loop::{EventLoop, EventLoopProxy},
    keyboard::{Key, NamedKey},
    window::Window,
};

/// 从任意线程投递给事件循环的用户事件（评审 P0/P1）。
/// Wake：有新帧或需要重绘；Close：请求事件循环退出。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum GxEvent {
    Wake,
    Close,
}

/// 队列积压告警阈值（评审 P1：FIFO 保序不等于无限队列安全）。约等于 60fps 下
/// 2 秒的积压。超过阈值**不丢帧**（增量帧丢弃会永久失步），只计数并上报过载，
/// 由 Go Host 侧的 FullSync 机制在适当时机合并成完整状态。
const MAX_PENDING_FRAMES: usize = 120;

/// 共享帧状态：FIFO 队列保证增量不丢；proxy 用于跨线程唤醒；shutdown 用于优雅退出。
struct FrameQueue {
    /// 待消费帧队列（FIFO）。draw push_back，redraw drain 全部按序 apply。
    pending: VecDeque<Vec<u8>>,
    /// 事件循环代理（run 启动后注入）。draw/close 经它唤醒事件循环。
    proxy: Option<EventLoopProxy<GxEvent>>,
    /// 已收到退出请求。
    shutdown: bool,
    /// 是否已有一个 Wake 在途，避免高频 draw 堆积大量 wake 消息。
    wake_in_flight: bool,
    /// 已成功 apply 的帧数（诊断用）。
    applied: u64,
    // ---- 队列积压 / 延迟指标（评审 P1）----
    /// 累计提交帧数。
    submitted: u64,
    /// 历史最大积压帧数。
    peak_pending: usize,
    /// 提交时队列已达到阈值的次数（过载计数）。
    overload_events: u64,
    /// 提交到消费的最大延迟（毫秒）。
    max_latency_ms: u64,
    /// 当前批次最早一帧的入队时间（用于计算提交→消费延迟）。
    first_enqueue: Option<std::time::Instant>,
}

impl FrameQueue {
    fn new() -> Self {
        FrameQueue {
            pending: VecDeque::new(),
            proxy: None,
            shutdown: false,
            wake_in_flight: false,
            applied: 0,
            submitted: 0,
            peak_pending: 0,
            overload_events: 0,
            max_latency_ms: 0,
            first_enqueue: None,
        }
    }

    /// 入队一帧并记录积压 / 延迟指标。过载只计数不丢帧——增量帧必须保序。
    fn enqueue(&mut self, frame: Vec<u8>) {
        if self.pending.is_empty() {
            self.first_enqueue = Some(std::time::Instant::now());
        }
        if self.pending.len() >= MAX_PENDING_FRAMES {
            self.overload_events += 1;
        }
        self.pending.push_back(frame);
        self.submitted += 1;
        if self.pending.len() > self.peak_pending {
            self.peak_pending = self.pending.len();
        }
    }

    /// 排空全部待消费帧，并结算本批次的提交→消费延迟。
    fn drain_all(&mut self) -> Vec<Vec<u8>> {
        if let Some(t0) = self.first_enqueue.take() {
            let ms = t0.elapsed().as_secs_f64() * 1000.0;
            let ms = ms.min(u64::MAX as f64) as u64;
            if ms > self.max_latency_ms {
                self.max_latency_ms = ms;
            }
        }
        self.pending.drain(..).collect()
    }

    /// 诊断快照：[submitted, applied, peak_pending, overload_events, max_latency_ms]。
    fn stats(&self) -> [u64; 5] {
        [
            self.submitted,
            self.applied,
            self.peak_pending as u64,
            self.overload_events,
            self.max_latency_ms,
        ]
    }

    /// 唤醒事件循环请求一次重绘。Wake 消息合并：已有在途则不再发。
    fn wake(&mut self, ev: GxEvent) {
        let need = match ev {
            GxEvent::Wake => !self.wake_in_flight,
            GxEvent::Close => true, // Close 必须每次都发，确保能退出
        };
        if !need {
            return;
        }
        if let Some(p) = &self.proxy {
            if p.send_event(ev).is_ok() && ev == GxEvent::Wake {
                self.wake_in_flight = true;
            }
        }
    }
}

pub type EventCallback = Option<extern "C" fn(*const c_char, *mut c_void)>;

/// 窗口外观配置（经 C ABI 由 Go 侧 WindowConfig 翻译而来）。
/// decorated 用 i32 三态：-1=未设置(用默认 true)，0=无边框，1=有边框，
/// 以兼容"未设置"与"明确 false"的区分（Go 侧 Decorated *bool）。
#[derive(Clone, Copy)]
struct WindowStyle {
    decorated: i32,
    transparent: bool,
    always_on_top: bool,
    opacity: f32,
}

impl Default for WindowStyle {
    fn default() -> Self {
        WindowStyle { decorated: -1, transparent: false, always_on_top: false, opacity: 0.0 }
    }
}

struct Backend {
    /// 共享帧队列（FIFO）+ 事件循环唤醒 + 退出标志
    queue: Arc<Mutex<FrameQueue>>,
    cb: Arc<Mutex<EventCallback>>,
    cb_ctx: Arc<Mutex<*mut c_void>>,
    width: u32,
    height: u32,
    title: String,
    style: WindowStyle,
}

unsafe impl Send for Backend {}
unsafe impl Sync for Backend {}

#[no_mangle]
pub extern "C" fn gx_window_new(width: c_int, height: c_int, title: *const c_char) -> *mut c_void {
    gx_window_new_ex(width, height, title, -1, 0, 0, 0.0)
}

unsafe fn cstr(s: *const c_char) -> String {
    if s.is_null() {
        return String::new();
    }
    std::ffi::CStr::from_ptr(s).to_string_lossy().into_owned()
}

/// 扩展版窗口创建：携带无边框/透明/置顶/不透明度配置。
/// 各布尔参数为 0/1；decorated 为 -1(默认)/0/1；opacity<=0 或 >1 表示不设置。
#[no_mangle]
pub extern "C" fn gx_window_new_ex(
    width: c_int,
    height: c_int,
    title: *const c_char,
    decorated: c_int,
    transparent: c_int,
    always_on_top: c_int,
    opacity: f32,
) -> *mut c_void {
    let title_str = unsafe { cstr(title) };
    let backend = Backend {
        queue: Arc::new(Mutex::new(FrameQueue::new())),
        cb: Arc::new(Mutex::new(None)),
        cb_ctx: Arc::new(Mutex::new(std::ptr::null_mut())),
        width: width.max(1) as u32,
        height: height.max(1) as u32,
        title: title_str,
        style: WindowStyle {
            decorated,
            transparent: transparent != 0,
            always_on_top: always_on_top != 0,
            opacity,
        },
    };
    // Arc::into_raw：handle 是一份强引用的原始指针。run 期间借用（不改计数），
    // destroy 时 Arc::from_raw 释放。close 不释放，避免 run 仍在使用时 UAF。
    Arc::into_raw(Arc::new(backend)) as *mut c_void
}

/// 传入二进制帧。一次 memcpy 入 FIFO 队列，并唤醒事件循环请求重绘。
/// 返回 0=ok, 1=参数错, 2=窗口已请求关闭（调用方应停止提交）。
///
/// 队列保序（评审 P0）：每帧都入队，redraw 时 drain 全部按序 apply。这样即便
/// Go Host 连续提交多个增量帧、事件循环尚未消费，也不会因单槽覆盖丢失中间帧。
#[no_mangle]
pub extern "C" fn gx_window_draw(handle: *mut c_void, buf: *const c_uchar, len: c_int) -> c_int {
    if handle.is_null() || buf.is_null() || len <= 0 {
        return 1;
    }
    let slice = unsafe { std::slice::from_raw_parts(buf, len as usize) };
    let owned = slice.to_vec(); // 唯一一次拷贝
    let backend = unsafe { &*(handle as *const Backend) };
    let mut q = backend.queue.lock().unwrap();
    if q.shutdown {
        return 2;
    }
    q.enqueue(owned);
    q.wake(GxEvent::Wake); // 合并 wake，避免高频帧堆积
    0
}

/// 帧队列诊断指标（评审 P1）：写入 out[0..5] =
/// [submitted, applied, peak_pending, overload_events, max_latency_ms]。
/// out 必须指向至少 5 个 u64 的可写内存。返回 0 表示成功。
#[no_mangle]
pub extern "C" fn gx_window_frame_stats(handle: *mut c_void, out: *mut u64) -> c_int {
    if handle.is_null() || out.is_null() {
        return 1;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    let q = backend.queue.lock().unwrap();
    let s = q.stats();
    unsafe {
        for (i, v) in s.iter().enumerate() {
            *out.add(i) = *v;
        }
    }
    0
}

#[no_mangle]
pub extern "C" fn gx_window_set_event_cb(handle: *mut c_void, cb: EventCallback, ctx: *mut c_void) {
    if handle.is_null() {
        return;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    *backend.cb.lock().unwrap() = cb;
    *backend.cb_ctx.lock().unwrap() = ctx;
}

#[no_mangle]
pub extern "C" fn gx_window_run(handle: *mut c_void) -> c_int {
    if handle.is_null() {
        return 1;
    }
    // 保活（评审 P0 生命周期）：handle 是调用方持有的 1 份强引用（into_raw 产生）。
    // run 期间先用 increment_strong_count 加 1 份，再从 raw 造出 Arc 借用这份新增引用；
    // 这样即便 Go 侧并发调用 destroy 释放原始那份，run 仍有自己的引用，不会 UAF。
    // run 返回时 backend drop（-1），原始那份仍归调用方。
    unsafe { Arc::increment_strong_count(handle as *const Backend) };
    let backend: Arc<Backend> = unsafe { Arc::from_raw(handle as *const Backend) };

    let (w, h) = (backend.width, backend.height);
    let style = backend.style;
    let title = backend.title.clone();
    // 启动时间探针：仅当 GX_STARTUP_PROBE 设置时开启，默认零开销。
    let probe = std::env::var_os("GX_STARTUP_PROBE").is_some();
    let t_run_start = if probe { Some(std::time::Instant::now()) } else { None };
    let result = std::panic::catch_unwind(|| {
        let el = new_event_loop();
        // 注入 proxy：draw/close 可跨线程唤醒这个事件循环。
        {
            let mut q = backend.queue.lock().unwrap();
            q.proxy = Some(el.create_proxy());
            if q.shutdown {
                // close 早于 run：直接退出，不创建窗口。
                return;
            }
            // draw 早于 run（首帧在 run 前提交）：补一次 wake。
            if !q.pending.is_empty() {
                q.wake(GxEvent::Wake);
            }
        }
        let mut state: Option<Renderer> = None;
        el.run_app(&mut WinitApp {
            state: &mut state,
            width: w,
            height: h,
            title,
            style,
            queue: backend.queue.clone(),
            cb: backend.cb.clone(),
            cb_ctx: backend.cb_ctx.clone(),
            probe,
            t_run_start,
            t_resumed_done: None,
            first_frame_done: false,
        })
        .expect("run_app");
    });
    // backend 在此 drop（-1）。原始那份由 gx_window_destroy 释放。
    if result.is_ok() { 0 } else { 2 }
}

/// 构造事件循环。Linux 下事件循环跑在 Go goroutine 经 cgo 进入的线程（非主线程），
/// 必须显式开启 any_thread；X11/Wayland 两条后端都要开，避免运行环境选 Wayland 时踩坑。
fn new_event_loop() -> EventLoop<GxEvent> {
    #[cfg(target_os = "linux")]
    {
        use winit::event_loop::EventLoopBuilder;
        use winit::platform::wayland::EventLoopBuilderExtWayland;
        use winit::platform::x11::EventLoopBuilderExtX11;
        let mut builder: EventLoopBuilder<GxEvent> = EventLoop::<GxEvent>::with_user_event();
        // 两条后端都开，完全限定调用避免 with_any_thread 歧义。
        EventLoopBuilderExtX11::with_any_thread(&mut builder, true);
        EventLoopBuilderExtWayland::with_any_thread(&mut builder, true);
        return builder.build().expect("event loop");
    }
    #[cfg(not(target_os = "linux"))]
    {
        return EventLoop::<GxEvent>::with_user_event()
            .build()
            .expect("event loop");
    }
}

struct WinitApp<'a> {
    state: &'a mut Option<Renderer>,
    width: u32,
    height: u32,
    title: String,
    style: WindowStyle,
    queue: Arc<Mutex<FrameQueue>>,
    cb: Arc<Mutex<EventCallback>>,
    cb_ctx: Arc<Mutex<*mut c_void>>,
    // 启动探针状态（GX_STARTUP_PROBE 门控，默认 false/None）
    probe: bool,
    t_run_start: Option<std::time::Instant>,
    t_resumed_done: Option<std::time::Instant>,
    first_frame_done: bool,
}

impl winit::application::ApplicationHandler<GxEvent> for WinitApp<'_> {
    fn resumed(&mut self, el: &winit::event_loop::ActiveEventLoop) {
        if self.state.is_some() {
            return;
        }
        let title = if self.title.is_empty() {
            "GX Rust Backend".to_string()
        } else {
            self.title.clone()
        };
        let mut attrs = Window::default_attributes()
            .with_inner_size(LogicalSize::new(self.width as f64, self.height as f64))
            .with_title(title)
            .with_resizable(false);
        // 外观：Decorated=nil(-1) 用默认；0 无边框；1 有边框。
        if self.style.decorated >= 0 {
            attrs = attrs.with_decorations(self.style.decorated != 0);
        }
        if self.style.transparent {
            attrs = attrs.with_transparent(true);
        }
        if self.style.always_on_top {
            attrs = attrs.with_window_level(winit::window::WindowLevel::AlwaysOnTop);
        }
        let window = Arc::new(el.create_window(attrs).expect("create window"));
        window.set_ime_allowed(true);
        // opacity: winit 0.30 无跨平台窗口级 opacity API（各平台扩展不一），
        // 透明效果请用 with_transparent + 场景节点的半透明 bg；此处不设置。
        let _ = self.style.opacity;
        *self.state = Some(pollster::block_on(Renderer::new(window)));
        // 首帧主动请求一次重绘：绘制首帧/应用 run 前已提交的帧。之后靠事件驱动。
        self.state.as_ref().unwrap().window().request_redraw();
        if self.probe {
            self.t_resumed_done = Some(std::time::Instant::now());
        }
    }

    /// 跨线程 wake：draw/close 经 EventLoopProxy 投递。
    fn user_event(&mut self, el: &winit::event_loop::ActiveEventLoop, event: GxEvent) {
        match event {
            GxEvent::Close => el.exit(),
            GxEvent::Wake => {
                // 清掉 wake_in_flight，允许后续 draw 再次投递（合并语义）。
                {
                    let mut q = self.queue.lock().unwrap();
                    q.wake_in_flight = false;
                }
                if let Some(renderer) = self.state.as_mut() {
                    renderer.window().request_redraw();
                }
            }
        }
    }

    fn window_event(
        &mut self,
        el: &winit::event_loop::ActiveEventLoop,
        _id: winit::window::WindowId,
        event: WindowEvent,
    ) {
        let Some(renderer) = self.state.as_mut() else { return; };
        match event {
            WindowEvent::Resized(size) => {
                renderer.resize(size.width.max(1), size.height.max(1));
                renderer.window().request_redraw();
            }
            WindowEvent::RedrawRequested => {
                // 排空 FIFO 队列，按顺序 apply 每一帧（评审 P0：增量不丢）。
                // 若队列积压很多（事件循环落后于提交速率），逐帧 apply 会把镜像追到最新；
                // 每帧都成功时 applied 递增，任一增量因无基线失败会由 Go 侧全量重发兜底。
                let batch: Vec<Vec<u8>> = {
                    let mut q = self.queue.lock().unwrap();
                    q.drain_all()
                };
                for buf in &batch {
                    if renderer.apply_frame(buf) {
                        self.queue.lock().unwrap().applied += 1;
                    }
                }
                let t_pre_render = if self.probe { Some(std::time::Instant::now()) } else { None };
                renderer.render();
                // 回流实测尺寸：把本帧布局结果经事件回调送回 Host，供虚拟列表按
                // node_id 订阅并修正 offsets/锚定。text 承载紧凑 JSON 数组。
                let measured = renderer.take_measured();
                if !measured.is_empty() {
                    let ev = protocol::InputEvent {
                        kind: "measured".into(),
                        node_id: String::new(),
                        event: String::new(),
                        text: serde_json::to_string(&measured).unwrap_or_default(),
                        x: 0.0,
                        y: 0.0,
                    };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
                // video 事件（state/meta/time）：解码线程产出，经事件回调回传 Host。
                for ve in video::take_events() {
                    let ev = protocol::InputEvent {
                        kind: ve.kind,
                        node_id: ve.node_id,
                        event: String::new(),
                        text: ve.text,
                        x: 0.0,
                        y: 0.0,
                    };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
                if self.probe && !self.first_frame_done {
                    self.first_frame_done = true;
                    let t_first = std::time::Instant::now();
                    let ms = |a: std::time::Instant, b: std::time::Instant| -> f64 {
                        b.duration_since(a).as_secs_f64() * 1000.0
                    };
                    let run_start = self.t_run_start.unwrap_or(t_first);
                    let resumed = self.t_resumed_done.unwrap_or(t_first);
                    let new_ms = ms(run_start, resumed); // event loop + wgpu 设备 + 字体系统初始化
                    let frame_ms = t_pre_render.map(|t| ms(t, t_first)).unwrap_or(0.0); // 首帧布局+光栅化
                    let text = format!(
                        "init_ms={:.3};first_frame_ms={:.3}",
                        new_ms, frame_ms
                    );
                    let ev = protocol::InputEvent {
                        kind: "startup_first_frame".into(),
                        node_id: String::new(),
                        event: String::new(),
                        text,
                        x: 0.0,
                        y: 0.0,
                    };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
                // 按需重绘（评审 P1）：只有仍有动画/视频/滚动条过渡时才请求下一帧；
                // 静止场景停止 request_redraw，避免空转占用 CPU/GPU。新帧到达时由
                // gx_window_draw 经 proxy 发 Wake 重新驱动一次重绘。
                if renderer.needs_continuous_redraw() {
                    renderer.window().request_redraw();
                }
            }
            WindowEvent::CloseRequested => el.exit(),
            WindowEvent::CursorMoved { position, .. } => {
                let (x, y) = (position.x as f32, position.y as f32);
                renderer.set_cursor(x, y);
                // 伪状态 hover：命中节点变化时才 request_redraw（边沿检测，避免每帧重绘）。
                let hit = renderer.hit_node_id(x, y);
                if renderer.set_hover(hit) {
                    renderer.window().request_redraw();
                }
            }
            WindowEvent::MouseInput {
                state: ElementState::Pressed,
                button: MouseButton::Left,
                ..
            } => {
                let (x, y) = renderer.cursor();
                // active 伪状态：按下命中的节点进入 active（active 优先于 hover）。
                let hit = renderer.hit_node_id(x, y);
                if renderer.set_active(hit) {
                    renderer.window().request_redraw();
                }
                // focus 伪状态：点击输入框 → 该节点聚焦；点击其他区域 → 失焦（同浏览器）。
                // 这与 host 侧 Input 的 focusToken signal 互补：后者驱动 IME/光标，
                // 这里驱动 & 选择器的视觉样式；点击场景下两者指向同一节点。
                // 边沿检测：仅焦点变化才重绘。
                let input_ev = renderer.hit_input(x, y);
                let focus_id = input_ev.as_ref().map(|e| e.node_id.clone());
                if renderer.set_focused(focus_id) {
                    renderer.window().request_redraw();
                }
                if let Some(ev) = renderer.hit_click(x, y) {
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                } else if let Some(ev) = input_ev {
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
            }
            WindowEvent::MouseInput {
                state: ElementState::Released,
                button: MouseButton::Left,
                ..
            } => {
                // 松开：退出 active，回到仅 hover 状态。
                if renderer.set_active(None) {
                    renderer.window().request_redraw();
                }
            }
            WindowEvent::CursorLeft { .. } => {
                // 光标离开窗口：退出 hover（否则会停在最后的 hover 态）。
                if renderer.set_hover(None) {
                    renderer.window().request_redraw();
                }
            }
            WindowEvent::Ime(ime) => match ime {
                Ime::Enabled => fire(&self.cb, &self.cb_ctx, ev_json("ime_enabled")),
                Ime::Disabled => {}
                Ime::Preedit(s, _) => {
                    let ev = protocol::InputEvent { kind: "ime_preedit".into(), node_id: String::new(), event: String::new(), text: s, x: 0.0, y: 0.0 };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
                Ime::Commit(s) => {
                    let ev = protocol::InputEvent { kind: "ime_commit".into(), node_id: String::new(), event: String::new(), text: s, x: 0.0, y: 0.0 };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
            },
            WindowEvent::KeyboardInput {
                event: winit::event::KeyEvent { state: ElementState::Pressed, logical_key, .. },
                ..
            } => {
                if logical_key == Key::Named(NamedKey::Backspace) {
                    fire(&self.cb, &self.cb_ctx, ev_json("backspace"));
                } else if logical_key == Key::Named(NamedKey::Escape) {
                    el.exit();
                } else if let Some(text) = logical_key.to_text() {
                    // IME 未激活时（如纯 ASCII / XTEST 注入），字符走 KeyboardInput 而非 Ime::Commit。
                    // 过滤控制字符，其余按 commit 回写文本框。
                    if !text.chars().any(|c| c.is_control()) {
                        let ev = protocol::InputEvent {
                            kind: "ime_commit".into(), node_id: String::new(),
                            event: String::new(), text: text.to_string(), x: 0.0, y: 0.0,
                        };
                        fire(&self.cb, &self.cb_ctx, &ev.to_json());
                    }
                }
            }
            _ => {}
        }
    }
}

fn ev_json(kind: &str) -> &'static str {
    // 仅用于无文本事件；有文本走 InputEvent::to_json
    match kind {
        "ime_enabled" => r#"{"kind":"ime_enabled","node_id":"","event":"","text":"","x":0,"y":0}"#,
        _ => r#"{"kind":"backspace","node_id":"","event":"","text":"","x":0,"y":0}"#,
    }
}

fn fire(cb: &Arc<Mutex<EventCallback>>, ctx: &Arc<Mutex<*mut c_void>>, json: &str) {
    if let Some(f) = *cb.lock().unwrap() {
        let c = *ctx.lock().unwrap();
        if let Ok(s) = CString::new(json) {
            f(s.as_ptr(), c);
        }
    }
}

/// 请求关闭：设置 shutdown 并通过 proxy 唤醒事件循环退出。
/// 注意：这里【不释放】Backend 内存——run 可能仍在使用它（评审 P0 生命周期竞争）。
/// 内存由 Go 侧在事件循环退出后调用 gx_window_destroy 释放。
#[no_mangle]
pub extern "C" fn gx_window_close(handle: *mut c_void) {
    if handle.is_null() {
        return;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    {
        let mut q = backend.queue.lock().unwrap();
        q.shutdown = true;
        q.wake(GxEvent::Close);
    }
}

/// 真正释放 Backend（Arc::from_raw 归还调用方持有的那份强引用）。
/// Go 侧必须在 gx_window_run 返回之后调用，确保 run 已 drop 它借用的那份。
#[no_mangle]
pub extern "C" fn gx_window_destroy(handle: *mut c_void) {
    if !handle.is_null() {
        unsafe { drop(Arc::from_raw(handle as *const Backend)); }
    }
}

#[no_mangle]
pub extern "C" fn gx_window_version() -> *const c_char {
    b"gxbackend-rust 0.2.0 (binary protocol)\0".as_ptr() as *const c_char
}

/// 纯 CPU 管线基准：buf 是 GRX1 帧，重复 iters 次「解析→建树→Taffy 布局→指令生成」。
/// 返回累计纳秒；解析失败返回 0。不含任何 GPU/窗口工作，供 cross-backend benchmark 调用。
#[no_mangle]
pub extern "C" fn gx_bench_frame(
    buf: *const c_uchar, len: c_int, width: f32, height: f32, iters: c_int,
) -> u64 {
    if buf.is_null() || len <= 0 || iters <= 0 {
        return 0;
    }
    let slice = unsafe { std::slice::from_raw_parts(buf, len as usize) };
    render::bench_frame(slice, width, height, iters as u32).unwrap_or(0)
}

// ---- 镜像一致性探针（纯 CPU，不依赖窗口/GPU）----
//
// 供 Go 侧集成测试验证「Go Scene → 二进制帧 → Rust 镜像」端到端一致性：
// 复用与 Renderer::apply_frame 完全相同的 GRX1/GRX2 消费逻辑，喂一串帧后
// 可查询镜像节点数与规范化摘要，从而对 1000 节点增删/条件切换做断言。

struct MirrorSession {
    scene: Option<crate::scene::SceneNode>,
    applied: u64,
    failed: u64,
}

static MIRROR: std::sync::OnceLock<Mutex<MirrorSession>> = std::sync::OnceLock::new();

fn mirror() -> &'static Mutex<MirrorSession> {
    MIRROR.get_or_init(|| {
        Mutex::new(MirrorSession { scene: None, applied: 0, failed: 0 })
    })
}

/// 重置镜像会话（清空基线树与计数）。
#[no_mangle]
pub extern "C" fn gx_mirror_reset() {
    let mut m = mirror().lock().unwrap();
    m.scene = None;
    m.applied = 0;
    m.failed = 0;
}

/// 喂一帧到镜像：GRX1 全量重建，GRX2 增量打补丁（与 Renderer::apply_frame 同源）。
/// 返回 0=成功应用，1=无基线时收到增量，2=无法解析。
#[no_mangle]
pub extern "C" fn gx_mirror_apply(buf: *const c_uchar, len: c_int) -> c_int {
    if buf.is_null() || len <= 0 {
        return 2;
    }
    let slice = unsafe { std::slice::from_raw_parts(buf, len as usize) };
    let mut m = mirror().lock().unwrap();
    if let Some(view) = crate::binprotocol::FrameView::parse(slice) {
        if let Some(root) = crate::scene::SceneNode::from_frame(&view) {
            m.scene = Some(root);
            m.applied += 1;
            return 0;
        }
        m.failed += 1;
        return 2;
    }
    if let Some(delta) = crate::scene::DeltaView::parse(slice) {
        if let Some(root) = m.scene.as_mut() {
            delta.apply(root);
            m.applied += 1;
            return 0;
        }
        // 无基线收到增量：与真实 Renderer 一致，拒绝（应由 FullSync 全量恢复）。
        m.failed += 1;
        return 1;
    }
    m.failed += 1;
    2
}

/// 返回镜像节点总数（含 root）。无镜像返回 0。
#[no_mangle]
pub extern "C" fn gx_mirror_node_count() -> c_int {
    fn count(n: &crate::scene::SceneNode) -> i32 {
        1 + n.children.iter().map(count).sum::<i32>()
    }
    let m = mirror().lock().unwrap();
    m.scene.as_ref().map(count).unwrap_or(0)
}

/// 把镜像扁平化为 "id:text\n..." 写入调用方缓冲，返回写入字节数（不含结尾 0）。
/// 缓冲不足时返回 -1。供 Go 侧做内容级一致性断言。
#[no_mangle]
pub extern "C" fn gx_mirror_dump(out: *mut c_char, cap: c_int) -> c_int {
    if out.is_null() || cap <= 0 {
        return -1;
    }
    let m = mirror().lock().unwrap();
    let mut s = String::new();
    fn walk(n: &crate::scene::SceneNode, s: &mut String) {
        s.push_str(&n.id);
        s.push(':');
        s.push_str(&n.text);
        s.push('\n');
        for c in &n.children {
            walk(c, s);
        }
    }
    if let Some(root) = m.scene.as_ref() {
        walk(root, &mut s);
    }
    let bytes = s.as_bytes();
    if bytes.len() + 1 > cap as usize {
        return -1;
    }
    unsafe {
        std::ptr::copy_nonoverlapping(bytes.as_ptr(), out as *mut u8, bytes.len());
        *out.add(bytes.len()) = 0;
    }
    bytes.len() as c_int
}

/// 返回成功/失败应用次数（编码为 applied<<32 | failed），供测试断言恢复路径。
#[no_mangle]
pub extern "C" fn gx_mirror_counts() -> u64 {
    let m = mirror().lock().unwrap();
    ((m.applied as u64) << 32) | (m.failed & 0xFFFF_FFFF)
}

#[cfg(test)]
mod frame_queue_tests {
    use super::*;

    #[test]
    fn queue_records_peak_and_overload_without_dropping_frames() {
        let mut q = FrameQueue::new();
        let total = MAX_PENDING_FRAMES + 7;
        for i in 0..total {
            q.enqueue(vec![i as u8]);
        }
        // 过载只计数，不丢帧（增量帧丢弃会永久失步）。
        assert_eq!(q.submitted, total as u64);
        assert_eq!(q.pending.len(), total);
        assert_eq!(q.peak_pending, total);
        assert_eq!(q.overload_events, 7);
        assert_eq!(q.stats()[0], total as u64);
        assert_eq!(q.stats()[3], 7);

        let batch = q.drain_all();
        assert_eq!(batch.len(), total);
        assert!(q.pending.is_empty());
        assert!(q.first_enqueue.is_none());
    }

    #[test]
    fn queue_stats_reset_latency_on_new_batch() {
        let mut q = FrameQueue::new();
        q.enqueue(vec![1, 2, 3]);
        let _ = q.drain_all();
        q.enqueue(vec![4, 5, 6]);
        assert!(q.first_enqueue.is_some());
        let _ = q.drain_all();
        assert!(q.first_enqueue.is_none());
        assert_eq!(q.submitted, 2);
        assert_eq!(q.applied, 0);
    }
}
