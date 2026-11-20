package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGoldenExamples 用 testdata/*.golden 做回归保护：examples 的转译结果须与 golden 一致。
func TestGoldenExamples(t *testing.T) {
	root := filepath.Join("..", "..")
	cases := []string{"hello", "pub", "enum", "jsx", "collections", "lowerext"}
	for _, name := range cases {
		srcPath := filepath.Join(root, "examples", name+".gx")
		goldenPath := filepath.Join(root, "testdata", name+".go.golden")
		src, err := os.ReadFile(srcPath)
		if err != nil {
			t.Fatalf("读 %s: %v", srcPath, err)
		}
		want, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("读 %s: %v", goldenPath, err)
		}
		res, err := Compile(name+".gx", string(src))
		if err != nil {
			t.Fatalf("转译 %s: %v", name, err)
		}
		if res.GoSource != string(want) {
			t.Errorf("%s 转译结果与 golden 不一致\n--- got ---\n%s\n--- want ---\n%s", name, res.GoSource, want)
		}
	}
}

// TestGoldenSceneJSX 用 CompileScene（opcode 后端）转译 examples/jsxscene.gx，
// 与 testdata/jsxscene.go.golden 比对。
func TestGoldenSceneJSX(t *testing.T) {
	root := filepath.Join("..", "..")
	src, err := os.ReadFile(filepath.Join(root, "examples", "jsxscene.gx"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, "testdata", "jsxscene.go.golden"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := CompileScene("jsxscene.gx", string(src))
	if err != nil {
		t.Fatalf("转译失败: %v", err)
	}
	if res.GoSource != string(want) {
		t.Errorf("scene JSX 转译结果与 golden 不一致\n--- got ---\n%s\n--- want ---\n%s", res.GoSource, want)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成 Go 语法无效: %v\n---\n%s", err, res.GoSource)
	}
}
