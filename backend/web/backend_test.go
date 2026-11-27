package gxweb

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
)

// Backend 实现接入接口形状（gxapp.Backend），可注入 gxapp.Use。
// Web 无头环境无法真开 DOM 窗口，只验证接口满足性与静态描述。
func TestBackendImplementsGxappBackend(t *testing.T) {
	var _ gxapp.Backend = webBackend{}
	b := NewBackend()
	if b.Name() != "web" {
		t.Fatalf("Name 应为 web, got %q", b.Name())
	}
	desc, ok := b.(gxapp.BackendDescriptor)
	if !ok {
		t.Fatal("应实现 BackendDescriptor")
	}
	if info := desc.Describe(); info.Name != "web" || info.Protocol == "" {
		t.Fatalf("Describe 内容不完整: %+v", info)
	}
	caps, ok := b.(gxapp.BackendCapabilities)
	if !ok {
		t.Fatal("应实现 BackendCapabilities")
	}
	if !caps.Capabilities().MultiWindow {
		t.Fatal("Web 后端每窗挂各自 DOM 容器，MultiWindow 应为 true")
	}
}
