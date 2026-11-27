package rustbackend

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
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
