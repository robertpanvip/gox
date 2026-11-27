package skia

// ffmpeg 子进程解码器：把视频源解码成 RGBA 原始帧。
//
// 性能要点：
//   - 解码在独立 goroutine，不阻塞 UI 绘制线程；
//   - 双缓冲：解码线程写 back buffer，绘制读 front buffer，帧完整才切换（无撕裂）；
//   - 帧按 src 缓存到 manager，src/playbackID/seek 变化才重开进程；
//   - 暂停 = 继续抽帧但丢弃（不更新 front），跨平台、无需进程信号；
//   - 无 ffmpeg 时进入 error 态，调用方画 poster 降级。
//
// ffmpeg 可执行文件按 FFMPEG_PATH → PATH 探测；缺失时安全降级，绝不崩溃。

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	gxvideo "github.com/robertpanvip/gox-core/runtime/video"
)

// ffmpegPath 按 FFMPEG_PATH → PATH 探测 ffmpeg，结果缓存一次。
var (
	ffmpegOnce sync.Once
	ffmpegBin  string
)

func ffmpegPath() string {
	ffmpegOnce.Do(func() {
		if p := strings.TrimSpace(os.Getenv("FFMPEG_PATH")); p != "" {
			if _, err := os.Stat(p); err == nil {
				ffmpegBin = p
				return
			}
		}
		if p, err := exec.LookPath("ffmpeg"); err == nil {
			ffmpegBin = p
		}
	})
	return ffmpegBin
}

// videoFrame 是一帧解码结果（RGBA8888）。
type videoFrame struct {
	data  []byte
	w, h  int
	pts   float64
}

// videoEvent 是解码器回传给宿主的事件。
type videoEvent struct {
	NodeID string
	Kind   string // gxvideo.EventStateChange / EventMeta / EventTimeUpdate
	Text   string
}

// onVideoFrame 在新帧就绪时由解码 goroutine 调用，通知宿主请求重绘。
// 全局钩子：宿主在装配 video 时注册（通常是 dispatcher.RequestFrame）。
var (
	videoFrameMu sync.RWMutex
	onVideoFrame func()
)

// SetVideoFrameHook 注册"新帧就绪"回调（宿主据此请求重绘）。传 nil 清空。
// 回调在解码 goroutine 调用，宿主实现应只做非阻塞投递（如 RequestFrame）。
func SetVideoFrameHook(fn func()) {
	videoFrameMu.Lock()
	onVideoFrame = fn
	videoFrameMu.Unlock()
}

func notifyVideoFrame() {
	videoFrameMu.RLock()
	fn := onVideoFrame
	videoFrameMu.RUnlock()
	if fn != nil {
		fn()
	}
}

// videoDecoder 管理单个 src 的解码进程与双缓冲当前帧。
type videoDecoder struct {
	nodeID, src, playbackID string
	bin                     string
	loop, muted             bool
	seek                    float64

	mu     sync.RWMutex
	front  *videoFrame
	paused bool
	stopped bool

	cmd    *exec.Cmd
	cancel chan struct{}
	done   chan struct{}

	onEvent func(videoEvent)
}

// newVideoDecoder 启动解码；bin 为空（无 ffmpeg）时进入 error 态。
func newVideoDecoder(nodeID, src, playbackID string, autoplay, loop, muted bool, seek float64, onEvent func(videoEvent)) *videoDecoder {
	d := &videoDecoder{
		nodeID: nodeID, src: src, playbackID: playbackID, bin: ffmpegPath(),
		loop: loop, muted: muted, seek: seek, onEvent: onEvent,
		paused: !autoplay, cancel: make(chan struct{}), done: make(chan struct{}),
	}
	if d.bin == "" || strings.TrimSpace(src) == "" {
		d.emit(gxvideo.EventStateChange, gxvideo.StateError)
		close(d.done)
		return d
	}
	go d.run()
	return d
}

// probe 读取宽高与时长（用 ffprobe；失败则返回 0 让调用方用默认值）。
func (d *videoDecoder) probe() (w, h int, duration float64) {
	probe := strings.Replace(d.bin, "ffmpeg", "ffprobe", 1)
	out, err := exec.Command(probe, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height", "-of", "json", d.src).Output()
	if err == nil {
		var res struct {
			Streams []struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"streams"`
		}
		if json.Unmarshal(out, &res) == nil && len(res.Streams) > 0 {
			w, h = res.Streams[0].Width, res.Streams[0].Height
		}
	}
	out, err = exec.Command(probe, "-v", "error", "-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1", d.src).Output()
	if err == nil {
		duration, _ = strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	}
	return
}

// buildArgs 组装 ffmpeg 解码命令：输出 RGBA 原始帧到 stdout。
func (d *videoDecoder) buildArgs(w, h int) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if d.loop {
		args = append(args, "-stream_loop", "-1")
	}
	if d.seek > 0 {
		args = append(args, "-ss", strconv.FormatFloat(d.seek, 'f', 3, 64))
	}
	args = append(args, "-i", d.src)
	if d.muted {
		args = append(args, "-an")
	}
	// 统一缩放到探测尺寸（保证帧大小稳定，ReadFull 不会错位）。
	if w > 0 && h > 0 {
		args = append(args, "-vf", "scale="+strconv.Itoa(w)+":"+strconv.Itoa(h))
	}
	args = append(args,
		"-f", "rawvideo", "-pix_fmt", "rgba", "-vsync", "0", "pipe:1",
	)
	return args
}

// run 解码主循环。
func (d *videoDecoder) run() {
	defer close(d.done)

	w, h, duration := d.probe()
	meta := gxvideo.Meta{Duration: duration, W: w, H: h}
	d.emit(gxvideo.EventMeta, gxvideo.EncodeMeta(meta))
	if w <= 0 || h <= 0 {
		w, h = 640, 360
	}

	cmd := exec.Command(d.bin, d.buildArgs(w, h)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		d.emit(gxvideo.EventStateChange, gxvideo.StateError)
		return
	}
	if err := cmd.Start(); err != nil {
		d.emit(gxvideo.EventStateChange, gxvideo.StateError)
		return
	}
	d.mu.Lock()
	d.cmd = cmd
	d.mu.Unlock()

	if d.paused {
		d.emit(gxvideo.EventStateChange, gxvideo.StatePaused)
	} else {
		d.emit(gxvideo.EventStateChange, gxvideo.StatePlaying)
	}

	frameSize := w * h * 4
	buf := make([]byte, frameSize)
	frameIdx := 0
	lastTime := time.Time{}
	for {
		select {
		case <-d.cancel:
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return
		default:
		}
		if _, err := io.ReadFull(stdout, buf); err != nil {
			break
		}
		pts := float64(frameIdx) / 30.0
		frameIdx++

		d.mu.Lock()
		paused := d.paused
		if !paused {
			// 双缓冲切换：整帧写入 front（deep copy，解码 buffer 可复用）。
			d.front = &videoFrame{data: append([]byte(nil), buf...), w: w, h: h, pts: pts}
		}
		stopped := d.stopped
		d.mu.Unlock()
		if stopped {
			break
		}
		// 新帧就绪：通知宿主请求重绘（非阻塞钩子）。
		notifyVideoFrame()
		if time.Since(lastTime) > 250*time.Millisecond {
			lastTime = time.Now()
			d.emit(gxvideo.EventTimeUpdate, strconv.FormatFloat(pts, 'f', 2, 64))
		}
	}
	_ = cmd.Wait()
	d.emit(gxvideo.EventStateChange, gxvideo.StateEnded)
}

// currentFrame 返回当前帧（绘制线程安全）。
func (d *videoDecoder) currentFrame() (videoFrame, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.front == nil {
		return videoFrame{}, false
	}
	return *d.front, true
}

// setPaused 暂停/恢复（仅切标志，不碰进程）。
func (d *videoDecoder) setPaused(paused bool) {
	d.mu.Lock()
	if d.paused == paused {
		d.mu.Unlock()
		return
	}
	d.paused = paused
	d.mu.Unlock()
	if paused {
		d.emit(gxvideo.EventStateChange, gxvideo.StatePaused)
	} else {
		d.emit(gxvideo.EventStateChange, gxvideo.StatePlaying)
	}
}

// stop 终止解码进程并等待 goroutine 退出。
func (d *videoDecoder) stop() {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.stopped = true
	close(d.cancel)
	d.mu.Unlock()
	select {
	case <-d.done:
	case <-time.After(1 * time.Second):
	}
}

func (d *videoDecoder) emit(kind, text string) {
	if d.onEvent != nil {
		d.onEvent(videoEvent{NodeID: d.nodeID, Kind: kind, Text: text})
	}
}
