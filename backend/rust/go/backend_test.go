package rustbackend

import (
	"testing"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
)

// Backend 实现接入接口形状（gxapp.Backend），可注入 gxapp.Use。
func TestBackendImplementsGxappBackend(t *testing.T) {
	var _ gxapp.Backend = Backend{}
	b := NewBackend()
	if b.Name() != "rust" {
		t.Fatalf("Name 应为 rust, got %q", b.Name())
	}
	w, err := b.NewWindow(gxapp.WindowConfig{Title: "x"})
	if err != nil || w == nil {
		t.Fatalf("NewWindow: err=%v win=%v", err, w)
	}
}

// 可选能力段：Describe / Capabilities / InitBackend 按 gxapp 可选接口实现，
// Host 装配时探测注入。
func TestBackendOptionalCapabilities(t *testing.T) {
	var b gxapp.Backend = Backend{}
	ini, ok := b.(gxapp.BackendInitializer)
	if !ok {
		t.Fatal("应实现 BackendInitializer")
	}
	if err := ini.InitBackend(); err != nil {
		t.Fatalf("InitBackend（C ABI 预探测）: %v", err)
	}
	ini.ShutdownBackend() // 幂等空实现

	desc, ok := b.(gxapp.BackendDescriptor)
	if !ok {
		t.Fatal("应实现 BackendDescriptor")
	}
	info := desc.Describe()
	if info.Name != "rust" || info.Version == "" || info.Protocol != "GRX3" {
		t.Fatalf("Describe 内容不完整: %+v", info)
	}

	caps, ok := b.(gxapp.BackendCapabilities)
	if !ok {
		t.Fatal("应实现 BackendCapabilities")
	}
	c := caps.Capabilities()
	if !c.Incremental || !c.Transparent {
		t.Fatalf("Incremental/Transparent 应为 true: %+v", c)
	}
	if c.MultiWindow {
		t.Fatal("当前每窗独立 EventLoop，MultiWindow 应为 false（P4 目标态）")
	}
}
