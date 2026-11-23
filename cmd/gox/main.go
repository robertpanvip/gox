// gox 是 GX 语言的命令行工具：转译、单步调试、sourcemap 查询与 LSP 启动。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/robertpanvip/gox/internal/buildcheck"
	"github.com/robertpanvip/gox/internal/check"
	"github.com/robertpanvip/gox/internal/lsp"
	"github.com/robertpanvip/gox/pkg/compiler"
	"github.com/robertpanvip/gox/pkg/module"
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
  gox build <file.gx> [-o out.go] [-module path] [-check] [-replace old=new] [-root dir] [-prefer gx|go]
       -module   集合/JSX 运行时的 module 路径（默认 github.com/robertpanvip/gox）
       -check    转译后执行真实 go build，并把错误回投到 .gx 位置
       -replace  go.mod replace 指令，可重复（module=本地目录）
       -root     项目根目录（默认取入口文件所在目录）；启用跨文件 .gx 模块模式
       -prefer   本地 .gx 与 Go 包同名时的来源裁决：gx（默认，本地优先）/ go
  gox union|typeguard|jsx|jsxscene|pub|enum|collections|external|all <file.gx>   单步 / 全管线调试
  gox check <file.gx> [-root dir] [-prefer gx|go]        GX 语义层类型检查（AST + side table，含跨文件大小写诊断）
       -root   项目根目录；启用跨文件 GX 模块检查（大小写错误由检查器直接报出）
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
	check    bool
	replaces []string
	root     string // 项目根目录；非空则启用跨文件模块模式
	prefer   string // gx / go
}

func parseBuildFlags(args []string) (*buildFlags, error) {
	f := &buildFlags{module: "github.com/robertpanvip/gox", prefer: "gx"}
	if len(args) == 0 {
		return nil, fmt.Errorf("build: 需要 <file.gx>")
	}
	f.file = args[0]
	for i := 1; i < len(args); i++ {
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
		case "-root":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("-root 缺少参数")
			}
			i++
			f.root = args[i]
		case "-prefer":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("-prefer 缺少参数")
			}
			i++
			f.prefer = args[i]
		default:
			return nil, fmt.Errorf("未知参数 %q", args[i])
		}
	}
	if f.prefer != "gx" && f.prefer != "go" {
		return nil, fmt.Errorf("-prefer 只支持 gx 或 go，得到 %q", f.prefer)
	}
	if f.out == "" {
		f.out = f.file + ".go"
	}
	return f, nil
}

// readProjectModule 从 go.mod 的 `module xxx` 行读取项目 module 路径。
// 找不到 go.mod 或无 module 行时返回空串，调用方回退到 -module。
func readProjectModule(root string) string {
	b, err := os.ReadFile(root + string(os.PathSeparator) + "go.mod")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "module"))
			rest = strings.Trim(rest, "\"`")
			if rest != "" {
				return rest
			}
		}
	}
	return ""
}

func cmdBuild(args []string) error {
	f, err := parseBuildFlags(args)
	if err != nil {
		return err
	}

	// 跨文件项目模式：-root 指定项目根，扫描全部 .gx、按来源还原大小写、逐文件落盘。
	if f.root != "" {
		return cmdBuildProject(f)
	}

	// 单文件模式（默认，零回归）。
	src, err := readFile(f.file)
	if err != nil {
		return err
	}
	res, err := compiler.CompileWithModule(f.file, src, f.module)
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

	// 第十三轮 P2-B：单文件模式此前从不消费 Analysis 诊断，导致同一份 .gx
	// 「单文件构建通过、项目模式阻断」的结论分叉。与 cmdBuildProject 对齐：
	// 语义诊断（signal-field-index-unknown / for-not-list / 大小写等）统一打印，
	// 非空即阻断。诊断已是 GX 坐标，直接复用项目模式的打印格式。
	var gxDiags []gxFileDiagnostic
	if res.Analysis != nil {
		for _, d := range res.Analysis.Diagnostics() {
			gxDiags = append(gxDiags, gxFileDiagnostic{
				File: filepath.ToSlash(f.file), Line: d.Pos.Line, Col: d.Pos.Col,
				Code: d.Code, Message: d.Message,
			})
		}
	}
	if len(gxDiags) > 0 {
		printGxDiagnostics(gxDiags)
		return fmt.Errorf("GX 语义检查未通过")
	}

	if f.check {
		return runCheck(res.GoSource, res.SourceMap, f.replaces)
	}
	return nil
}

// cmdBuildProject 在跨文件项目模式下转译整个 GX 项目并落盘。
func cmdBuildProject(f *buildFlags) error {
	projModule := readProjectModule(f.root)
	if projModule == "" {
		projModule = f.module
	}
	var prefer module.Prefer
	if f.prefer == "go" {
		prefer = module.PreferGo
	} else {
		prefer = module.PreferGX
	}
	proj, err := module.LoadProject(f.root, projModule, prefer)
	if err != nil {
		return err
	}
	// runtime 包（runtime/scene 等）永远在 gox module 下，与项目 module 无关。
	// -module 是用户覆盖 runtime module 的入口（如 fork gox），默认已是 gox 官方路径。
	proj.RuntimeModule = f.module
	files, err := proj.Build(f.file)
	if err != nil {
		return err
	}
	var srcFiles []buildcheck.SourceFile
	smByGoPath := map[string]*sourcemap.SourceMap{}
	var gxDiags []gxFileDiagnostic
	for _, cf := range files {
		if err := os.WriteFile(cf.GoPath, []byte(cf.GoSource), 0644); err != nil {
			return err
		}
		relGo, _ := filepath.Rel(proj.Root, cf.GoPath)
		relSrc, _ := filepath.Rel(proj.Root, cf.SourcePath)
		fmt.Printf("已生成 %s （%d 字节，%d 条映射）\n", filepath.ToSlash(relGo), len(cf.GoSource), cf.SourceMap.Len())
		srcFiles = append(srcFiles, buildcheck.SourceFile{Name: filepath.ToSlash(relGo), Source: cf.GoSource})
		smByGoPath[filepath.ToSlash(relGo)] = cf.SourceMap
		// GX 语义层诊断（含跨文件大小写错误）已是 GX 坐标，按源文件归集。
		if cf.Analysis != nil {
			for _, d := range cf.Analysis.Diagnostics() {
				gxDiags = append(gxDiags, gxFileDiagnostic{
					File: filepath.ToSlash(relSrc), Line: d.Pos.Line, Col: d.Pos.Col,
					Code: d.Code, Message: d.Message,
				})
			}
		}
	}

	if f.check {
		return runCheckProject(srcFiles, smByGoPath, gxDiags, f.replaces, projModule, proj.Root)
	}
	// 未开 -check 时，GX 语义诊断（大小写等）仍应可见——这是检查器的直接产出。
	if len(gxDiags) > 0 {
		printGxDiagnostics(gxDiags)
		return fmt.Errorf("GX 语义检查未通过")
	}
	return nil
}

// gxFileDiagnostic 是一条带源文件相对路径的 GX 语义诊断（已是 GX 坐标）。
type gxFileDiagnostic struct {
	File, Code, Message string
	Line, Col           int
}

func printGxDiagnostics(diags []gxFileDiagnostic) {
	fmt.Fprintf(os.Stderr, "GX 语义检查发现 %d 条问题:\n", len(diags))
	for _, d := range diags {
		fmt.Fprintf(os.Stderr, "  %s:%d:%d: %s: %s\n", d.File, d.Line, d.Col, d.Code, d.Message)
	}
}

// runCheckProject 对多文件产物执行 go build，并把诊断按 File 回投到对应 .gx sourcemap。
// gxDiags 是 GX 语义层（大小写等）诊断，已是 GX 坐标，与 go build 诊断一并输出。
func runCheckProject(srcFiles []buildcheck.SourceFile, smByPath map[string]*sourcemap.SourceMap, gxDiags []gxFileDiagnostic, replaces []string, projectModule, root string) error {
	checker := buildcheck.New(64)
	checker.SetToolchain("go", "GOTOOLCHAIN=go1.27.3")
	checker.SetModulePath(projectModule)
	for _, r := range replaces {
		parts := strings.SplitN(r, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("-replace 格式应为 module=dir，得到 %q", r)
		}
		checker.AddReplace(parts[0], parts[1])
	}
	diags, err := checker.CheckFiles(context.Background(), srcFiles)
	if err != nil {
		return fmt.Errorf("编译检查失败: %w", err)
	}
	if len(diags) == 0 && len(gxDiags) == 0 {
		fmt.Println("编译检查通过：无错误")
		return nil
	}
	if len(gxDiags) > 0 {
		printGxDiagnostics(gxDiags)
	}
	if len(diags) > 0 {
		fmt.Fprintf(os.Stderr, "Go 编译检查发现 %d 条错误（已回投到 .gx）:\n", len(diags))
		for _, d := range diags {
			sm := smByPath[d.File]
			if sm != nil {
				if pos, ok := sm.GoToGx(d.Line, d.Col); ok {
					// 用相对项目根的路径，避免多个包有同名 .gx 时产生歧义。
					rel := sm.File
					if r, err := filepath.Rel(root, sm.File); err == nil {
						rel = filepath.ToSlash(r)
					}
					fmt.Fprintf(os.Stderr, "  %s:%d:%d: %s\n", rel, pos.Line, pos.Column, d.Message)
					continue
				}
			}
			fmt.Fprintf(os.Stderr, "  %s:%d:%d: %s\n", d.File, d.Line, d.Col, d.Message)
		}
	}
	return fmt.Errorf("编译检查未通过")
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
// 带 -root 时走跨文件项目模式，额外纳入大小写等跨文件语义诊断。
func cmdCheck(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("check: 需要 <file.gx>")
	}
	file := args[0]
	var root, prefer string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-root":
			if i+1 >= len(args) {
				return fmt.Errorf("-root 缺少参数")
			}
			i++
			root = args[i]
		case "-prefer":
			if i+1 >= len(args) {
				return fmt.Errorf("-prefer 缺少参数")
			}
			i++
			prefer = args[i]
		default:
			return fmt.Errorf("check: 未知参数 %q", args[i])
		}
	}

	// 项目模式：加载整个项目，逐文件跑 GX 语义检查（含跨文件大小写诊断）。
	if root != "" {
		return cmdCheckProject(file, root, prefer)
	}

	src, err := readFile(file)
	if err != nil {
		return err
	}
	res, err := compiler.Compile(file, src)
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

// cmdCheckProject 在项目模式下逐文件跑 GX 语义检查，诊断按源文件相对路径输出。
func cmdCheckProject(file, root, prefer string) error {
	projModule := readProjectModule(root)
	if projModule == "" {
		projModule = "github.com/robertpanvip/gox"
	}
	p := module.PreferGX
	if prefer == "go" {
		p = module.PreferGo
	}
	proj, err := module.LoadProject(root, projModule, p)
	if err != nil {
		return err
	}
	files, err := proj.Build(file)
	if err != nil {
		return err
	}
	var all []gxFileDiagnostic
	for _, cf := range files {
		if cf.Analysis == nil {
			continue
		}
		// CheckAll 合并 AST 规则与 Analysis 诊断（含大小写），统一为 GX 坐标。
		c := check.New(cf.SourceMap, cf.Analysis)
		diags, err := c.CheckAll(cf.GoSource)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(proj.Root, cf.SourcePath)
		for _, d := range diags {
			all = append(all, gxFileDiagnostic{
				File: filepath.ToSlash(rel), Line: d.Line, Col: d.Col,
				Code: d.Code, Message: d.Message,
			})
		}
	}
	if len(all) == 0 {
		fmt.Println("GX 语义检查通过：无错误")
		return nil
	}
	printGxDiagnostics(all)
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
