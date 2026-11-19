// gox 是 GX 语言的命令行工具：转译、单步调试、sourcemap 查询与 LSP 启动。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/robertpanvip/gox/internal/lsp"
	"github.com/robertpanvip/gox/pkg/compiler"
	"github.com/robertpanvip/gox/pkg/sourcemap"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var err error
	switch cmd {
	case "build":
		err = cmdBuild(args)
	case "jsx", "pub", "enum", "collections", "external", "all":
		err = cmdStage(cmd, args)
	case "sourcemap":
		err = cmdSourcemap(args)
	case "map-lookup":
		err = cmdMapLookup(args)
	case "lsp-map":
		err = cmdLspMap(args)
	case "lsp":
		err = lsp.NewServer(os.Stdin, os.Stdout, nil).Run()
	case "test":
		err = cmdSelfTest(args)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`gox — GX 编译到 Go 的工具

用法:
  gox build <file.gx> [-o out.go]   转译为 Go 源码（默认输出到 <file>.go）
  gox jsx <file.gx>                 仅执行 JSX → createElement 转换
  gox pub <file.gx>                 仅执行 pub 可见性转换
  gox collections <file.gx>         仅执行集合链式转换
  gox enum|external|all <file.gx>   其它单步 / 全管线
  gox sourcemap <file.gx>           输出 GX↔Go 位置映射 JSON
  gox map-lookup <map.json> <goLine> <goCol>   查询 Go 位置对应的 GX 位置
  gox lsp-map <file.gx>             输出符号/位置映射摘要
  gox lsp                           启动 LSP server（stdio）
  gox test                          运行内置自检`)
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func cmdBuild(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("build: 需要 <file.gx>")
	}
	src, err := readFile(args[0])
	if err != nil {
		return err
	}
	res, err := compiler.Compile(args[0], src)
	if err != nil {
		return err
	}
	if err := res.Validate(); err != nil {
		return fmt.Errorf("生成的 Go 代码语法无效: %w", err)
	}
	out := args[0] + ".go"
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "-o" {
			out = args[i+1]
		}
	}
	if err := os.WriteFile(out, []byte(res.GoSource), 0644); err != nil {
		return err
	}
	fmt.Printf("已生成 %s （%d 字节，%d 条映射）\n", out, len(res.GoSource), res.SourceMap.Len())
	return nil
}

func cmdStage(stage string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s: 需要 <file.gx>", stage)
	}
	src, err := readFile(args[0])
	if err != nil {
		return err
	}
	out, err := compiler.CompileStage(src, stage)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

func cmdSourcemap(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("sourcemap: 需要 <file.gx>")
	}
	src, err := readFile(args[0])
	if err != nil {
		return err
	}
	res, err := compiler.Compile(args[0], src)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(res.SourceMap, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func cmdMapLookup(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("map-lookup: 需要 <map.json> <goLine> <goCol>")
	}
	data, err := readFile(args[0])
	if err != nil {
		return err
	}
	var sm sourcemap.SourceMap
	if err := json.Unmarshal([]byte(data), &sm); err != nil {
		return err
	}
	line, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	col, err := strconv.Atoi(args[2])
	if err != nil {
		return err
	}
	pos, ok := sm.GoToGx(line, col)
	if !ok {
		return fmt.Errorf("映射为空")
	}
	fmt.Printf("go %d:%d → gx %d:%d\n", line, col, pos.Line, pos.Column)
	return nil
}

func cmdLspMap(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("lsp-map: 需要 <file.gx>")
	}
	src, err := readFile(args[0])
	if err != nil {
		return err
	}
	res, err := compiler.Compile(args[0], src)
	if err != nil {
		return err
	}
	sm := lsp.BuildSymbolMap(src)
	out := map[string]any{
		"file":        args[0],
		"symbols":     sm.Symbols,
		"mapCount":    res.SourceMap.Len(),
		"generatedGo": res.GoSource,
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	return nil
}
