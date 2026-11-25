package typecheck

import (
	"runtime"
	"strings"
	"testing"
)

// mapFS 内存版虚拟文件系统：path → 单文件源码。
func mapFS(m map[string]string) Lookup {
	return func(path string) ([]File, bool) {
		src, ok := m[path]
		if !ok {
			return nil, false
		}
		return []File{{Name: "x.go", Src: src}}, true
	}
}

func hasDiag(diags []Diagnostic, substr string) bool {
	for _, d := range diags {
		if strings.Contains(d.Message, substr) {
			return true
		}
	}
	return false
}

func TestDetectsAssignTypeMismatch(t *testing.T) {
	diags, err := Check("package main\n\nfunc f() {\n\tvar a int\n\ta = \"s\"\n}\n", mapFS(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiag(diags, "cannot use") {
		t.Fatalf("应检出赋值类型错误, got %v", diags)
	}
	if diags[0].Line < 2 || diags[0].Col < 1 {
		t.Fatalf("诊断必须带真实行列: %+v", diags[0])
	}
}

func TestDetectsUndefined(t *testing.T) {
	diags, err := Check("package main\n\nfunc f() {\n\tx := y + 1\n}\n", mapFS(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiag(diags, "undefined: y") {
		t.Fatalf("应检出 undefined: y, got %v", diags)
	}
}

func TestVfsImportResolved(t *testing.T) {
	vfs := mapFS(map[string]string{
		"greeter": "package greeter\n\nfunc Hello() string { return \"hi\" }\n",
	})
	diags, err := Check("package main\n\nimport \"greeter\"\n\nfunc f() {\n\tvar n int = greeter.Hello()\n\t_ = n\n}\n", vfs)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiag(diags, "cannot use greeter.Hello()") {
		t.Fatalf("虚拟包 API 未参与类型检查: %v", diags)
	}
}

func TestVfsUnknownImportGraceful(t *testing.T) {
	diags, err := Check("package main\n\nimport \"net/http\"\n\nfunc f() { _ = http.Get }\n", mapFS(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) == 0 {
		t.Fatal("未知包必须产生可见诊断，不得静默")
	}
}

func TestDiskStdFmt(t *testing.T) {
	gr := runtime.GOROOT()
	if gr == "" || !dirExists(gr+"/src/fmt") {
		t.Skip("无 GOROOT/src/fmt")
	}
	lk := DiskLookup(gr+"/src", "linux", "amd64")
	diags, err := Check("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println()\n\tvar s string = 42\n\t_ = s\n}\n", lk)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiag(diags, "cannot use") {
		t.Fatalf("磁盘虚拟 FS 的 fmt 未参与检查: %v", diags)
	}
}

func TestDiskTagFilter(t *testing.T) {
	gr := runtime.GOROOT()
	if gr == "" || !dirExists(gr+"/src/syscall") {
		t.Skip("无 syscall 源码")
	}
	lk := DiskLookup(gr+"/src", "js", "wasm")
	files, ok := lk("syscall")
	if !ok {
		t.Fatal("syscall 应可解析")
	}
	for _, f := range files {
		if strings.Contains(f.Src, "import \"C\"") {
			t.Fatalf("js/wasm 视图混入 cgo 文件: %s", f.Name)
		}
	}
}
