package skia

import (
	"os"
	"testing"
	"time"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	gxvideo "github.com/robertpanvip/gox-core/runtime/video"
)

const testVideo = "/tmp/gx-test-video.mp4"

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if ffmpegPath() == "" {
		t.Skip("ffmpeg 不在 PATH/FFMPEG_PATH，跳过解码测试")
	}
	if _, err := os.Stat(testVideo); err != nil {
		t.Skipf("测试视频不存在: %v", err)
	}
}

// TestVideoDecoderProducesFrames 验证 ffmpeg 子进程真能解码出 RGBA 帧。
func TestVideoDecoderProducesFrames(t *testing.T) {
	requireFFmpeg(t)
	var events []videoEvent
	dec := newVideoDecoder("1", testVideo, "1", true, false, true, 0,
		func(e videoEvent) { events = append(events, e) })
	defer dec.stop()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f, ok := dec.currentFrame(); ok {
			if f.w != 160 || f.h != 120 {
				t.Fatalf("帧尺寸错误: %dx%d", f.w, f.h)
			}
			if len(f.data) != 160*120*4 {
				t.Fatalf("帧数据长度错误: %d", len(f.data))
			}
			// 校验不是全黑（testsrc 有彩色图案）。
			nonBlack := 0
			for i := 0; i < len(f.data) && nonBlack < 10; i += 4 {
				if f.data[i] > 10 || f.data[i+1] > 10 || f.data[i+2] > 10 {
					nonBlack++
				}
			}
			if nonBlack == 0 {
				t.Error("解码帧全黑，可能解码失败")
			}
			// 应收到 meta + playing 事件。
			seenMeta, seenPlaying := false, false
			for _, e := range events {
				switch e.Kind {
				case gxvideo.EventMeta:
					if m, ok := gxvideo.DecodeMeta(e.Text); ok && m.W == 160 && m.H == 120 {
						seenMeta = true
					}
				case gxvideo.EventStateChange:
					if e.Text == gxvideo.StatePlaying {
						seenPlaying = true
					}
				}
			}
			if !seenMeta {
				t.Error("缺少正确的 videometa 事件")
			}
			if !seenPlaying {
				t.Error("缺少 playing 事件")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("3 秒内未解出任何帧")
}

// TestVideoDecoderNoFFmpeg 无 ffmpeg 时进入 error 态、不崩溃。
func TestVideoDecoderNoFFmpeg(t *testing.T) {
	old := ffmpegBin
	ffmpegBin = ""
	defer func() { ffmpegBin = old }()
	var got []string
	dec := newVideoDecoder("1", "x.mp4", "1", true, false, false, 0,
		func(e videoEvent) { got = append(got, e.Kind+":"+e.Text) })
	if _, ok := dec.currentFrame(); ok {
		t.Error("无 ffmpeg 时不应有帧")
	}
	if len(got) == 0 || got[0] != gxvideo.EventStateChange+":"+gxvideo.StateError {
		t.Errorf("无 ffmpeg 应回传 error 事件, got %v", got)
	}
}

// TestVideoPosterFallback 无帧时画 poster 占位（不依赖 ffmpeg，走 imgutil 解码）。
func TestVideoPosterFallback(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 300, Height: 200}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	poster := testDataURI(t, 40, 30) // 复用 image_test 的 data URI 生成
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "video"},
		{Code: gxscene.OpAttr, Key: "poster", Arg: poster},
		{Code: gxscene.OpAttr, Key: "width", Arg: "120"},
		{Code: gxscene.OpAttr, Key: "height", Arg: "80"},
		{Code: gxscene.OpClose},
	}})
	if err := w.Draw(inst.Root()); err != nil {
		t.Fatal(err)
	}
	log := w.DrawLog()
	if log.Find("video", "video") != 1 {
		t.Fatalf("应有 1 条 video 绘制记录（poster 占位）, log=%v", log)
	}
}
