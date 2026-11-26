package embedstd

import (
	"runtime"
	"testing"

	"github.com/robertpanvip/gox/internal/typecheck"
)

// TestEmbeddedStdResolves 生成数据存在时：fmt 必须可从 embed 虚拟 FS 解析，
// 且完成一次真实类型检查；VERSION 与当前工具链不一致时报错（版本锁）。
func TestEmbeddedStdResolves(t *testing.T) {
	lk := Lookup()
	if lk == nil {
		t.Skip("未生成 embed 数据：go run ./cmd/gox-stdlib-gen")
	}
	if v := Version(); v != runtime.Version() {
		t.Fatalf("源码版本 %s 与工具链 %s 不一致，请重跑生成器", v, runtime.Version())
	}
	files, ok := lk("fmt")
	if !ok || len(files) == 0 {
		t.Fatal("embed fmt 应可解析")
	}
	diags, err := typecheck.Check("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tvar s string = 42\n\t_ = s\n\tfmt.Println(\"ok\")\n}\n", lk)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range diags {
		if d.Line == 6 && d.Col > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("embed 虚拟 FS 类型检查失败: %v", diags)
	}
}

// TestPackagesList 内嵌 import 路径清单：含 std 与 gox 闭包，不得混入 internal。
func TestPackagesList(t *testing.T) {
	if Version() == "" {
		t.Skip("embedstd 未生成")
	}
	var hasFmt, hasStr, hasGox bool
	for _, p := range Packages() {
		if p == "fmt" {
			hasFmt = true
		}
		if p == "strings" {
			hasStr = true
		}
		if p == "github.com/robertpanvip/gox/widgets" {
			hasGox = true
		}
		if len(p) >= 8 && p[:8] == "internal" {
			t.Fatalf("internal 不应出现在可导入清单: %s", p)
		}
	}
	if !hasFmt || !hasStr || !hasGox {
		t.Fatalf("fmt=%v strings=%v gox-widgets=%v", hasFmt, hasStr, hasGox)
	}
}
