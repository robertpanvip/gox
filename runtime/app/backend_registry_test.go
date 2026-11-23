package gxapp_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
)

// 注册表是全局状态：测试用唯一名字避免相互干扰，不清理也不影响其他用例。
func TestBackendRegistryOpenBackend(t *testing.T) {
	const name = "test-registry-headless"
	err := gxapp.Register(name, func(cfg gxapp.WindowConfig) (gxapp.Window, error) {
		return &gxapp.HeadlessWindow{Out: os.Stdout}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !gxapp.Registered(name) {
		t.Fatalf("%s 应已注册", name)
	}
	found := false
	for _, n := range gxapp.Backends() {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("Backends() 应包含 %s", name)
	}

	app, err := gxapp.OpenBackend(gxapp.WindowConfig{Backend: name, Title: "t", Width: 100, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if app.Window() == nil {
		t.Fatal("OpenBackend 应持有后端实例")
	}
}

func TestBackendRegistryUnknownAndBadFactory(t *testing.T) {
	_, err := gxapp.OpenBackend(gxapp.WindowConfig{Backend: "definitely-not-registered-xyz"})
	if err == nil || !strings.Contains(err.Error(), "未注册") {
		t.Fatalf("未注册后端应报错, got=%v", err)
	}
	if err := gxapp.Register("", func(gxapp.WindowConfig) (gxapp.Window, error) { return nil, nil }); err == nil {
		t.Fatal("空名应报错")
	}
	if err := gxapp.Register("nil-factory-test", nil); err == nil {
		t.Fatal("nil 工厂应报错")
	}

	// 工厂返回 nil：OpenBackend 应给出清晰错误。
	const nilName = "test-nil-result"
	_ = gxapp.Register(nilName, func(gxapp.WindowConfig) (gxapp.Window, error) { return nil, nil })
	_, err = gxapp.OpenBackend(gxapp.WindowConfig{Backend: nilName})
	if err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("nil 后端应报错, got=%v", err)
	}

	// 工厂返回 error：应透传。
	const errName = "test-err-result"
	sentinel := errors.New("boom")
	_ = gxapp.Register(errName, func(gxapp.WindowConfig) (gxapp.Window, error) { return nil, sentinel })
	_, err = gxapp.OpenBackend(gxapp.WindowConfig{Backend: errName})
	if !errors.Is(err, sentinel) {
		t.Fatalf("应透传工厂错误, got=%v", err)
	}
}

func TestBackendRegistryHeadlessDefault(t *testing.T) {
	// headless / 空串都走内置后端，不查注册表。
	for _, name := range []string{"", "headless"} {
		app, err := gxapp.OpenBackend(gxapp.WindowConfig{Backend: name, Width: 50, Height: 50})
		if err != nil {
			t.Fatalf("backend=%q: %v", name, err)
		}
		if _, ok := app.Window().(*gxapp.HeadlessWindow); !ok {
			t.Fatalf("backend=%q 应为 HeadlessWindow", name)
		}
		app.Close()
	}
}
