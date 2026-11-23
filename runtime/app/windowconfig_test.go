package gxapp_test

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
)

func TestWindowConfigAppearanceDefaults(t *testing.T) {
	// 未设置外观字段时保持零值默认：Decorated=nil（后端用默认），其余 false/空。
	cfg := gxapp.DefaultConfig()
	if cfg.Decorated != nil {
		t.Error("DefaultConfig 不应预设 Decorated（交给后端默认）")
	}
	if cfg.Transparent || cfg.AlwaysOnTop || cfg.Opacity != 0 || cfg.IconPath != "" {
		t.Error("外观字段默认值应为未设置状态")
	}
}

func TestWindowConfigDecorationsHelpers(t *testing.T) {
	off := gxapp.NoDecorations()
	if off == nil || *off {
		t.Error("NoDecorations 应返回 *false")
	}
	on := gxapp.WithDecorations()
	if on == nil || !*on {
		t.Error("WithDecorations 应返回 *true")
	}
	// 显式 false 与未设置(nil) 必须可区分——这是用指针的核心目的。
	if off == nil {
		t.Fatal("无边框标记不应为 nil")
	}
	cfg := gxapp.WindowConfig{Decorated: off}
	if cfg.Decorated == nil || *cfg.Decorated {
		t.Error("Decorated=NoDecorations() 应表达明确的无边框")
	}
}
