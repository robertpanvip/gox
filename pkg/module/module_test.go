package module

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robertpanvip/gox/pkg/compiler"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// 建一个两包项目：main 导入 util；util 导出 pub add（Add）。
func sampleProject(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "main.gx"), `package main
import "example.com/proj/util"
func main() { println(util.add(1, 2)) }
`)
	writeFile(t, filepath.Join(root, "util", "math.gx"), `package util
pub func add(a int, b int) int { return a + b }
`)
	return root
}

func TestLoadProjectGroupsByPackage(t *testing.T) {
	root := sampleProject(t)
	p, err := LoadProject(root, "example.com/proj", PreferGX)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.packages) != 2 {
		t.Fatalf("应有 2 个 GX 包，得到 %d: %v", len(p.packages), p.packages)
	}
	util, ok := p.packages["example.com/proj/util"]
	if !ok {
		t.Fatal("缺少 example.com/proj/util 包")
	}
	if util.Package != "util" || len(util.Files) != 1 {
		t.Errorf("util 包解析异常: pkg=%s files=%v", util.Package, util.Files)
	}
	if util.Exports["add"] != "Add" {
		t.Errorf("util 导出表 add = %q, want Add", util.Exports["add"])
	}
	main, ok := p.packages["example.com/proj"]
	if !ok {
		t.Fatal("根目录 main 包的 importPath 应为 example.com/proj")
	}
	if main.Package != "main" {
		t.Errorf("main 包 package=%s", main.Package)
	}
}

func TestResolveOrigin(t *testing.T) {
	root := sampleProject(t)
	p, err := LoadProject(root, "example.com/proj", PreferGX)
	if err != nil {
		t.Fatal(err)
	}
	info, ok := p.Resolve("example.com/proj/util")
	if !ok || info.Origin != compiler.ImportGX {
		t.Errorf("本地 GX 包应解析为 ImportGX: %+v ok=%v", info, ok)
	}
	if info.Exports["add"] != "Add" {
		t.Errorf("GX 导出表 add = %q", info.Exports["add"])
	}
	info2, ok2 := p.Resolve("fmt")
	if ok2 || info2.Origin != compiler.ImportGo {
		t.Errorf("外部包应解析为 ImportGo: %+v ok=%v", info2, ok2)
	}
	// PreferGo：即便命中本地包也按 Go 来源。
	pg, _ := LoadProject(root, "example.com/proj", PreferGo)
	info3, ok3 := pg.Resolve("example.com/proj/util")
	if ok3 || info3.Origin != compiler.ImportGo {
		t.Errorf("PreferGo 下应按 Go 来源: %+v ok=%v", info3, ok3)
	}
}

func TestOrderDependencyFirst(t *testing.T) {
	root := sampleProject(t)
	p, err := LoadProject(root, "example.com/proj", PreferGX)
	if err != nil {
		t.Fatal(err)
	}
	order, err := p.Order()
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 {
		t.Fatalf("拓扑序应有 2 个包: %v", order)
	}
	if order[0].ImportPath != "example.com/proj/util" || order[1].ImportPath != "example.com/proj" {
		t.Errorf("被依赖方 util 应排在 main 前，得到: %s, %s",
			order[0].ImportPath, order[1].ImportPath)
	}
}

func TestOrderDetectsCycle(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.gx"), `package a
import "example.com/proj/b"
func fa() { b.fb() }
`)
	writeFile(t, filepath.Join(root, "b", "b.gx"), `package b
import "example.com/proj"
func fb() { a.fa() }
`)
	// 注意 main 包 importPath = modulePath = example.com/proj；a.gx 在根的 package a。
	// 二者同目录不同 package → 两个包；b 依赖 example.com/proj（a 所在 importPath）。
	// 但 a.gx 的 importPath 也是 example.com/proj（根），与包名无关。
	p, err := LoadProject(root, "example.com/proj", PreferGX)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Order()
	if err == nil {
		t.Fatal("应检测到循环依赖")
	}
	if !strings.Contains(err.Error(), "循环依赖") || !strings.Contains(err.Error(), "→") {
		t.Errorf("循环诊断应含路径链: %v", err)
	}
}

func TestBuildCrossFile(t *testing.T) {
	root := sampleProject(t)
	p, err := LoadProject(root, "example.com/proj", PreferGX)
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.Build(filepath.Join(root, "main.gx"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("应转译 2 个文件，得到 %d", len(files))
	}
	// util/math.go 应导出 Add（严格来自被导入方 pub）。
	var utilGo, mainGo string
	for _, f := range files {
		if strings.HasSuffix(f.SourcePath, "math.gx") {
			utilGo = f.GoSource
		}
		if strings.HasSuffix(f.SourcePath, "main.gx") {
			mainGo = f.GoSource
		}
	}
	if utilGo == "" || mainGo == "" {
		t.Fatalf("产物不全: util=%q main=%q", utilGo, mainGo)
	}
	if !strings.Contains(utilGo, "func Add(") {
		t.Errorf("util 产物应有 func Add:\n%s", utilGo)
	}
	if !strings.Contains(mainGo, "util.Add(1, 2)") {
		t.Errorf("main 产物应把小写 add 还原为导入方导出名 Add:\n%s", mainGo)
	}
	if strings.Contains(mainGo, "util.add(") {
		t.Errorf("main 产物不应残留小写 add:\n%s", mainGo)
	}
}

func TestBuildWrongCaseIsKept(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.gx"), `package main
import "example.com/proj/util"
func main() { println(util.ADD(1, 2)) }
`)
	writeFile(t, filepath.Join(root, "util", "math.gx"), `package util
pub func add(a int, b int) int { return a + b }
`)
	p, err := LoadProject(root, "example.com/proj", PreferGX)
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.Build(filepath.Join(root, "main.gx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f.SourcePath, "main.gx") {
			if !strings.Contains(f.GoSource, "util.ADD(") || strings.Contains(f.GoSource, "util.Add(") {
				t.Errorf("错误大小写应原样保留、不被猜成 Add:\n%s", f.GoSource)
			}
			return
		}
	}
	t.Fatal("未找到 main 产物")
}

func TestBuildCrossFileStructFields(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.gx"), `package main
import "example.com/proj/model"
func main() { _ = model.box{value: 9, name: "x"} }
`)
	writeFile(t, filepath.Join(root, "model", "box.gx"), `package model
pub type box struct {
    pub value int
    pub name string
    secret bool
}
`)
	p, err := LoadProject(root, "example.com/proj", PreferGX)
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.Build(filepath.Join(root, "main.gx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f.SourcePath, "main.gx") {
			if !strings.Contains(f.GoSource, "model.Box{Value: 9, Name: \"x\"}") {
				t.Errorf("pub 字段应严格还原为导出名:\n%s", f.GoSource)
			}
			return
		}
	}
	t.Fatal("未找到 main 产物")
}

func TestBuildCaseMismatchDiagnostics(t *testing.T) {
	// 跨文件项目：书写 util.ADD / util.box{VALUE}，检查器应直接产出大小写诊断，
	// 且诊断带正确拼写与 GX 坐标——不再依赖 go build 的 undefined/unexported。
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.gx"), `package main
import "example.com/proj/util"
func main() {
    println(util.ADD(1, 2))
    _ = util.box{VALUE: 9}
}
`)
	writeFile(t, filepath.Join(root, "util", "math.gx"), `package util
pub func add(a int, b int) int { return a + b }
pub type box struct { pub value int }
`)
	p, err := LoadProject(root, "example.com/proj", PreferGX)
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.Build(filepath.Join(root, "main.gx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if !strings.HasSuffix(f.SourcePath, "main.gx") {
			continue
		}
		if f.Analysis == nil {
			t.Fatal("CompiledFile 应携带 Analysis 供检查器消费")
		}
		diags := f.Analysis.Diagnostics()
		var codes []string
		for _, d := range diags {
			codes = append(codes, d.Code)
			// 诊断消息应给出正确拼写。
			if d.Code == "import-case" && !strings.Contains(d.Message, "Add") {
				t.Errorf("import-case 应含正确拼写 Add: %s", d.Message)
			}
			if d.Code == "import-field-case" && !strings.Contains(d.Message, "Value") {
				t.Errorf("import-field-case 应含正确拼写 Value: %s", d.Message)
			}
		}
		joined := strings.Join(codes, ",")
		if !strings.Contains(joined, "import-case") || !strings.Contains(joined, "import-field-case") {
			t.Errorf("应同时报大小写符号与字段诊断，得到: %v", codes)
		}
		// 产物保持书写原样（严格不改赌，交 Go 兜底）。
		if !strings.Contains(f.GoSource, "util.ADD(1, 2)") || !strings.Contains(f.GoSource, "util.Box{VALUE: 9}") {
			t.Errorf("写错大小写产物应原样保留:\n%s", f.GoSource)
		}
		return
	}
	t.Fatal("未找到 main 产物")
}

func TestBuildCorrectCaseNoDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.gx"), `package main
import "example.com/proj/util"
func main() { println(util.add(1, 2)); _ = util.box{value: 9} }
`)
	writeFile(t, filepath.Join(root, "util", "math.gx"), `package util
pub func add(a int, b int) int { return a + b }
pub type box struct { pub value int }
`)
	p, err := LoadProject(root, "example.com/proj", PreferGX)
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.Build(filepath.Join(root, "main.gx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f.SourcePath, "main.gx") && len(f.Analysis.Diagnostics()) != 0 {
			t.Errorf("正确拼写不应报大小写诊断: %v", f.Analysis.Diagnostics())
		}
	}
}

// 跨文件项目用了 JSX/sig（会注入 runtime import）时，runtime 路径必须指向
// gox 自己的 module，不得被项目 module 前缀污染，也不得重复注入错误别名。
// 同时验证跨文件 Context 导出还原（小写 .use() → .Use()）。
func TestBuildRuntimeImportUsesGoxModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "theme", "ctx.gx"), `package theme
import "github.com/robertpanvip/gox/widgets"
pub var labelCtx = widgets.NewContext[string]("default")
`)
	writeFile(t, filepath.Join(root, "main.gx"), `package main
import (
    "example.com/proj/theme"
    gxscene "github.com/robertpanvip/gox/runtime/scene"
    "github.com/robertpanvip/gox/runtime/signal"
)
func main() {
    theme.labelCtx.set("shared")
    _ = theme.labelCtx.use()
    view := <div>{theme.labelCtx.value()}</div>
    view.BindReactive(signal.Effect)
    _ = gxscene.Props{}
}
`)
	p, err := LoadProject(root, "example.com/proj", PreferGX)
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.Build(filepath.Join(root, "main.gx"))
	if err != nil {
		t.Fatal(err)
	}
	var mainGo, ctxGo string
	for _, f := range files {
		if strings.HasSuffix(f.SourcePath, "main.gx") {
			mainGo = f.GoSource
		}
		if strings.HasSuffix(f.SourcePath, "ctx.gx") {
			ctxGo = f.GoSource
		}
	}
	if mainGo == "" || ctxGo == "" {
		t.Fatalf("产物不全: main=%q ctx=%q", mainGo, ctxGo)
	}
	// runtime 路径必须是 gox 官方 module，不得出现项目前缀的 runtime 路径
	if strings.Contains(mainGo, "example.com/proj/runtime/") {
		t.Errorf("runtime import 被项目 module 前缀污染:\n%s", mainGo)
	}
	if !strings.Contains(mainGo, `"github.com/robertpanvip/gox/runtime/scene"`) {
		t.Errorf("缺少正确的 gxscene runtime import:\n%s", mainGo)
	}
	// 不得出现重复别名 gxscene2 / Signal2（旧 bug 的症状）
	if strings.Contains(mainGo, "gxscene2") || strings.Contains(mainGo, "Signal2") {
		t.Errorf("出现重复 runtime 别名（错误注入症状）:\n%s", mainGo)
	}
	// 跨文件 Context 导出还原：pub var labelCtx → LabelCtx，小写 .use() → .Use()
	if !strings.Contains(ctxGo, "var LabelCtx") {
		t.Errorf("ctx 产物应导出 LabelCtx:\n%s", ctxGo)
	}
	if !strings.Contains(mainGo, "theme.LabelCtx.Use()") {
		t.Errorf("main 产物应还原为 theme.LabelCtx.Use():\n%s", mainGo)
	}
}
