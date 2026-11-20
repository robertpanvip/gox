// gox 是 GX 语言的命令行工具：转译、单步调试、sourcemap 查询与 LSP 启动。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/robertpanvip/gox/internal/buildcheck"
	"github.com/robertpanvip/gox/internal/check"
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
	case "check":
		err = cmdCheck(args)
	case "union", "typeguard", "jsx", "pub", "enum", "collections", "external", "all":
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
	fmt.Println(`gox — GX（编译到 Go）命令行工具

用法:
  gox build <file.gx> [-o out.go] [-module path] [-jsx vdom|scene] [-check] [-replace old=new]
       -module   集合/JSX 运行时的 module 路径（默认 github.com/robertpanvip/gox）
       -jsx      JSX 后端：vdom=CreateElement+Diff（默认）；scene=opcode 常驻场景树+细粒度更新
       -check    转译后执行真实 go build，并把错误回投到 .gx 位置
       -replace  go.mod replace 指令，可重复（module=本地目录）
  gox union|typeguard|jsx|jsxscene|pub|enum|collections|external|all <file.gx>   单步 / 全管线调试
  gox check <file.gx>                                   GX 语义层类型检查（AST + side table，GX 坐标诊断）
  gox sourcemap <file.gx> [-o map.json]                 输出 GX↔Go 位置映射 JSON
  gox map-lookup [-to-go] <map.json> <line> <col>        位置反查（默认 Go→GX）
  gox lsp-map <dir-or-file>                              项目符号映射 JSON
  gox lsp                                                启动 LSP server（stdio）
  gox test                                               运行内置转译自检`)
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// buildFlags 解析 build 子命令参数。
type buildFlags struct {
	file     string
	out      string
	module   string
	jsx      string // "vdom"（默认）或 "scene"（opcode 常驻场景树）
	check    bool
	replaces []string
}

func parseBuildFlags(args []string) (*buildFlags, error) {
	f := &buildFlags{module: "github.com/robertpanvip/gox"}
	if len(args) == 0 {
		return nil, fmt.Errorf("build: 需要 <file.gx>")
	}
	f.file = args[0]
	for i := 1; i < len(args); i++ {
		// 支持 -key=value 形式（与空格分隔等价）
		if k, v, ok := strings.Cut(args[i], "="); ok {
			switch k {
			case "-jsx":
				f.jsx = v
				if f.jsx != "vdom" && f.jsx != "scene" {
					return nil, fmt.Errorf("-jsx 只能是 vdom 或 scene，得到 %q", f.jsx)
				}
				continue
			}
		}
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("-o 缺少参数")
			}
			i++
			f.out = args[i]
		case "-module":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("-module 缺少参数")
			}
			i++
			f.module = args[i]
		case "-check":
			f.check = true
		case "-replace":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("-replace 缺少参数")
			}
			i++
			f.replaces = append(f.replaces, args[i])
		case "-jsx":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("-jsx 缺少参数")
			}
			i++
			f.jsx = args[i]
			if f.jsx != "vdom" && f.jsx != "scene" {
				return nil, fmt.Errorf("-jsx 只能是 vdom 或 scene，得到 %q", f.jsx)
			}
		default:
			return nil, fmt.Errorf("未知参数 %q", args[i])
		}
	}
	if f.out == "" {
		f.out = f.file + ".go"
	}
	return f, nil
}

func cmdBuild(args []string) error {
	f, err := parseBuildFlags(args)
	if err != nil {
		return err
	}
	src, err := readFile(f.file)
	if err != nil {
		return err
	}
	var res *compiler.Result
	if f.jsx == "scene" {
		res, err = compiler.CompileSceneWithModule(f.file, src, f.module)
	} else {
		res, err = compiler.CompileWithModule(f.file, src, f.module)
	}
	if err != nil {
		return err
	}
	if err := res.Validate(); err != nil {
		return fmt.Errorf("生成的 Go 代码语法无效: %w", err)
	}
	if err := os.WriteFile(f.out, []byte(res.GoSource), 0644); err != nil {
		return err
	}
	fmt.Printf("已生成 %s （%d 字节，%d 条映射）\n", f.out, len(res.GoSource), res.SourceMap.Len())

	if f.check {
		return runCheck(res.GoSource, res.SourceMap, f.replaces)
	}
	return nil
}

// runCheck 执行真实 go build，并把诊断从 Go 位置经 sourcemap 回投到 .gx。
func runCheck(goSrc string, sm *sourcemap.SourceMap, replaces []string) error {
	checker := buildcheck.New(64)
	checker.SetToolchain("go", "GOTOOLCHAIN=go1.27.3")
	for _, r := range replaces {
		parts := strings.SplitN(r, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("-replace 格式应为 module=dir，得到 %q", r)
		}
		checker.AddReplace(parts[0], parts[1])
	}

	diags, err := checker.Check(context.Background(), goSrc)
	if err != nil {
		return fmt.Errorf("编译检查失败: %w", err)
	}
	if len(diags) == 0 {
		fmt.Println("编译检查通过：无错误")
		return nil
	}
	fmt.Fprintf(os.Stderr, "编译检查发现 %d 条错误（已回投到 .gx）:\n", len(diags))
	for _, d := range diags {
		pos, ok := sm.GoToGx(d.Line, d.Col)
		if !ok {
			fmt.Fprintf(os.Stderr, "  %d:%d: %s\n", d.Line, d.Col, d.Message)
			continue
		}
		fmt.Fprintf(os.Stderr, "  %d:%d: %s\n", pos.Line, pos.Column, d.Message)
	}
	return fmt.Errorf("编译检查未通过")
}

// cmdCheck 用 GX 语义层检查器分析 .gx：降级为 Go 后用 go/parser 拿 AST，
// 坐标映射回 GX 后查 side table，产出 GX 原生诊断（联合赋值等规则）。
func cmdCheck(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("check: 需要 <file.gx>")
	}
	src, err := readFile(args[0])
	if err != nil {
		return err
	}
	res, err := compiler.Compile(args[0], src)
	if err != nil {
		return err
	}
	c := check.New(res.SourceMap, res.Analysis)
	diags, err := c.CheckAll(res.GoSource)
	if err != nil {
		return err
	}
	if len(diags) == 0 {
		fmt.Println("GX 语义检查通过：无错误")
		return nil
	}
	fmt.Fprintf(os.Stderr, "GX 语义检查发现 %d 条问题:\n", len(diags))
	for _, d := range diags {
		fmt.Fprintf(os.Stderr, "  %d:%d: %s: %s\n", d.Line, d.Col, d.Code, d.Message)
	}
	return fmt.Errorf("GX 语义检查未通过")
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
	var outPath string
	var fileArgs []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			i++
			outPath = args[i]
		} else {
			fileArgs = append(fileArgs, args[i])
		}
	}
	if len(fileArgs) == 0 {
		return fmt.Errorf("sourcemap: 需要 <file.gx>")
	}
	src, err := readFile(fileArgs[0])
	if err != nil {
		return err
	}
	res, err := compiler.Compile(fileArgs[0], src)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(res.SourceMap, "", "  ")
	if err != nil {
		return err
	}
	if outPath != "" {
		if err := os.WriteFile(outPath, data, 0644); err != nil {
			return err
		}
		fmt.Printf("已生成 %s\n", outPath)
		return nil
	}
	fmt.Println(string(data))
	return nil
}

func cmdMapLookup(args []string) error {
	toGo := false
	if len(args) > 0 && args[0] == "-to-go" {
		toGo = true
		args = args[1:]
	}
	if len(args) < 3 {
		return fmt.Errorf("map-lookup: 需要 [-to-go] <map.json> <line> <col>")
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
	if toGo {
		pos, ok := sm.GxToGo(line, col)
		if !ok {
			return fmt.Errorf("映射为空")
		}
		fmt.Printf("gx %d:%d → go %d:%d\n", line, col, pos.Line, pos.Column)
		return nil
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
		return fmt.Errorf("lsp-map: 需要 <file.gx> 或 <dir>")
	}
	// 目录：合并目录下所有 .gx 的符号
	info, err := os.Stat(args[0])
	if err != nil {
		return err
	}
	all := map[string]any{}
	if info.IsDir() {
		entries, err := os.ReadDir(args[0])
		if err != nil {
			return err
		}
		files := map[string]any{}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".gx") {
				continue
			}
			p := args[0] + "/" + e.Name()
			src, err := readFile(p)
			if err != nil {
				continue
			}
			sm := lsp.BuildSymbolMap(src)
			files[p] = sm.Symbols
		}
		all["files"] = files
	} else {
		src, err := readFile(args[0])
		if err != nil {
			return err
		}
		res, err := compiler.Compile(args[0], src)
		if err != nil {
			return err
		}
		sm := lsp.BuildSymbolMap(src)
		all["file"] = args[0]
		all["symbols"] = sm.Symbols
		all["mapCount"] = res.SourceMap.Len()
	}
	data, _ := json.MarshalIndent(all, "", "  ")
	fmt.Println(string(data))
	return nil
}
