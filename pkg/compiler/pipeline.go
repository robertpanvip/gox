package compiler

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"

	"github.com/robertpanvip/gox/pkg/sourcemap"
)

// Result 是一次完整转译的产物。
type Result struct {
	File      string
	GoSource  string
	SourceMap *sourcemap.SourceMap
	// Analysis 是 desugar 之前 GX 语义层的产物（符号表 / 收窄 / 诊断）。
	// 供 CLI / LSP 消费 GX 原生诊断；仅 Compile 路径填充，CompileStage 为 nil。
	Analysis *Analysis
}

// Validate 用 Go 解析器校验生成源码的语法（不检查类型/符号）。
func (r *Result) Validate() error {
	if r == nil {
		return fmt.Errorf("nil result")
	}
	_, err := parser.ParseFile(token.NewFileSet(), r.File, r.GoSource, parser.AllErrors)
	return err
}

// Compile 执行完整管线：
// lex → stripDefaults → signals(Signal 语法糖：声明消除 + 读写调用式改写)
// → analyze(GX 语义层：符号表 / 收窄 / 诊断) → union(联合类型擦除)
// → typeguard(is 类型守卫，消费 analyze 结果) → enum → pub → jsx
// → collections(集合链式) → scope(作用域符号收集) → restoreRefs(自身 pub 符号)
// → external(外部符号机械还原) → emit。
//
// 对象字面量与对象解构在 desugar 早期处理：对象字面量（transformObjectLits）
// 先展开为 struct 形态，对象解构（expandDestructures）随后把 {a,b} := p 改写为
// 逐字段声明，并借此做「解构字段 ⊆ 构造类型字段」的编译期校验。
//
// Signal transform 在 analyze 之前：消除 signal/let 声明并把 signal 变量读写改为
// 调用式（n / n=v → n() / n(v)），使语义层看到的是标准 Go；Signal 占位包名由
// ReconcileImports 统一注入 import。
// analyze 必须在 union 之前：此时联合类型 A|B 尚未被擦除，is 守卫仍是原貌，
// 语义层才能建立真实的联合类型环境并做收窄。typeguard 消费 analyze 的产物，
// 只负责生成 comma-ok 断言，不再自行判定守卫。
// scope 必须在 pub 之后（pub 名已大写，不会被误收为私有）、
// jsx/collections 之后（它们注入的 import 也要被识别为包名）。
func Compile(file, src string) (*Result, error) {
	return CompileWithModule(file, src, defaultModule)
}

// CompileWithModule 以指定 module 转译。<module>/runtime/list 作为集合运行时 import 路径。
// imports 为 nil（单文件模式），所有 import 按 Go 来源处理，行为与改造前一致。
func CompileWithModule(file, src, module string) (*Result, error) {
	return CompileWithProject(file, src, module, nil)
}

// CompileWithProject 以指定 module 和 import 来源表转译。
// imports 的键是 import 路径（未加引号），值是该 import 的解析结果（来源 + GX 导出表）。
// 仅当某条 import 命中 imports 且来源为 ImportGX 时，selector 右侧才严格查导出表；
// 其余一律维持原 upperFirst 行为。imports 为 nil 等价于 CompileWithModule。
func CompileWithProject(file, src, module string, imports map[string]ImportInfo) (*Result, error) {
	toks := New(src).AllTokens()
	// struct signal 字段语言特性：必须在 Bind 之前降级 `signal name TYPE`
	// → `name Signal.Signal[TYPE]`，否则 `signal` 关键字会被局部 signal 逻辑
	// 误判为 `signal name = expr`（旧行为生成非法 `var name any string`）。
	// 降级后字段成为标准 Go，所有下游 transform 天然享受既有作用域/大小写保护。
	signalDecls := collectSignalFieldDecls(toks)
	signalFields := buildSignalFieldTable(signalDecls)
	toks = downgradeSignalFields(toks, signalDecls)
	// 统一绑定表 + 表达式 AST：lex 之后、任何 transform 之前构建一次，贯穿整条管线。
	// 以 Pos.Offset 为稳定身份，不受后续 desugar 增删 token 的下标漂移影响。
	bindings := Bind(toks)
	templateAST := ParseTemplateExprs(toks)
	// 默认值：声明处先物理剥离（否则 Analyze 会把 `= default` 算进类型），
	// 调用点在 Analyze 之后补全（补全表达式仍流经后续全部 transform）。
	toks, defaultDefs := stripDefaults(toks)
	// Signal 语法糖：signal/let 声明消除 + signal 变量读写改写为调用式。
	// 放在 Analyze 之前，让语义层看到标准 Go（signal/let 已消除、读写已是调用）。
	// P1：传入 bindings，用 ResolveAt 做遮蔽判断（P1-1 根治）。
	toks = transformSignals(toks, bindings)
	analysis := Analyze(toks)    // desugar 之前的 GX 语义层（联合未擦除）
	names := map[string]string{} // 小写原名 → 大写导出名（自身 pub 符号）
	toks = expandDefaults(toks, defaultDefs)
	toks = transformLambdas(toks, analysis, bindings)
	// 对象字面量：期望类型校验失败时返回带 GX 坐标的错误。
	toks, err := transformObjectLits(toks, analysis, bindings)
	if err != nil {
		return nil, err
	}
	// 对象解构：{ name, age } := p → name := p.name; age := p.age
	// 放在对象字面量之后，右值声明类型（含 struct{...}{...} 展开形态）已可见。
	toks, err = expandDestructures(toks, bindings)
	if err != nil {
		return nil, err
	}
	toks = transformTemplates(toks, bindings, templateAST)
	// struct signal 字段：构造位置自动包 Signal.New，selector 读写自动调用化。
	// 放在 signal 局部变量改写与模板展开之后（右侧表达式已处理），external 之前
	// （字段名仍是小写，signalFieldTable 用原始类型名判定）。
	toks = wrapSignalFieldLiterals(toks, bindings, signalFields)
	toks = rewriteSignalFieldRefs(toks, bindings, signalFields)
	toks = runUnion(toks)
	toks = transformTypeGuard(toks, analysis)
	if err := checkStrandedIs(toks); err != nil {
		return nil, err
	}
	toks = transformEnum(toks, names)
	toks = transformPub(toks, names)
	toks = transformJSX(toks, bindings)
	toks = transformCollections(toks, module)
	// 统一 import 别名：源码若已用别名导入 list/vdom/strconv，生成代码的占位包名
	// 需替换为实际别名；否则各 transform 注入的 import 会与生成引用对不上。
	toks = ReconcileImports(toks, module)
	scope := collectScopeWithImports(toks, imports)
	toks = restoreRefs(toks, names, bindings)
	// GX 来源符号的大小写错误诊断落入 analysis.diags，随 Result.Analysis 被
	// check.CheckAll / LSP 消费；单文件模式 imports 为 nil 不产生诊断（零回归）。
	toks = transformExternal(toks, scope, names, &analysis.diags)
	toks = transformCompositeFields(toks, scope, &analysis.diags)
	goSrc, sm := emit(toks, file)
	return &Result{File: file, GoSource: goSrc, SourceMap: sm, Analysis: analysis}, nil
}

// CompileSource 以默认文件名转译源码。
func CompileSource(src string) (*Result, error) { return Compile("input.gx", src) }

// emit 把 token 流拼成源码，同时记录「输出 token 起点 → 输入位置」的映射。
func emit(toks []Token, file string) (string, *sourcemap.SourceMap) {
	var b strings.Builder
	line, col := 1, 1
	var raw []sourcemap.RawMapping
	for _, t := range toks {
		if t.Skip || t.Text == "" {
			continue
		}
		if t.Kind != Space && t.Kind != Newline && t.Pos.valid() {
			raw = append(raw, sourcemap.RawMapping{
				OutLine: line, OutCol: col, InLine: t.Pos.Line, InCol: t.Pos.Col,
			})
		}
		// Go 编译器的列号是 UTF-8 字节列，因此这里按字节递增 col，
		// 与 lexer 的 l.col（同样按字节）保持一致；否则含中文/emoji 的
		// 行会产生 sourcemap 列偏移，导致 buildcheck 诊断回投错位。
		for i := 0; i < len(t.Text); i++ {
			c := t.Text[i]
			b.WriteByte(c)
			if c == '\n' {
				line++
				col = 1
			} else {
				col++
			}
		}
	}
	return b.String(), sourcemap.Build(file, raw)
}

// CompileStage 只执行单个转换阶段，用于 CLI 的 jsx/pub 等子命令与调试。
// stage 取值：lex / union / typeguard / enum / jsx / pub / external / all。
func CompileStage(src, stage string) (string, error) {
	toks := New(src).AllTokens()
	names := map[string]string{}
	switch stage {
	case "lex":
		return dumpTokens(toks), nil
	case "union":
		toks = runUnion(toks)
	case "typeguard":
		toks = transformTypeGuard(toks, Analyze(toks))
	case "enum":
		toks = transformEnum(toks, names)
	case "jsx":
		toks = transformJSX(toks, nil)
	case "pub":
		toks = transformPub(toks, names)
	case "collections":
		toks = transformCollections(toks, defaultModule)
	case "external":
		scope := collectScope(toks)
		toks = transformExternal(toks, scope, names, nil)
		toks = transformCompositeFields(toks, scope, nil)
	case "all":
		bindings := Bind(toks)
		templateAST := ParseTemplateExprs(toks)
		toks, defaultDefs := stripDefaults(toks)
		toks = transformSignals(toks, bindings)
		analysis := Analyze(toks)
		toks = expandDefaults(toks, defaultDefs)
		toks = transformLambdas(toks, analysis, bindings)
		toks, err := transformObjectLits(toks, analysis, bindings)
		if err != nil {
			return "", err
		}
		toks, err = expandDestructures(toks, bindings)
		if err != nil {
			return "", err
		}
		toks = transformTemplates(toks, bindings, templateAST)
		toks = runUnion(toks)
		toks = transformTypeGuard(toks, analysis)
		toks = transformEnum(toks, names)
		toks = transformPub(toks, names)
		toks = transformJSX(toks, bindings)
		toks = transformCollections(toks, defaultModule)
		toks = ReconcileImports(toks, defaultModule)
		scope := collectScope(toks)
		toks = restoreRefs(toks, names, bindings)
		toks = transformExternal(toks, scope, names, nil)
		toks = transformCompositeFields(toks, scope, nil)
		// all 分支内 toks 被 := 重新声明（见上方 stripDefaults），必须在此作用域内
		// 直接 emit 并返回，否则外层 emit 拿到的是未改写的原始 token。
		out, _ := emit(toks, "<stage>")
		return out, nil
	default:
		return "", fmt.Errorf("unknown stage %q (want lex/union/typeguard/enum/jsx/pub/collections/external/all)", stage)
	}
	out, _ := emit(toks, "<stage>")
	return out, nil
}

func dumpTokens(toks []Token) string {
	var b strings.Builder
	for i, t := range toks {
		if t.Kind == EOF {
			break
		}
		fmt.Fprintf(&b, "%3d  %-8s  %-24q  @%d:%d\n", i, t.Kind, t.Text, t.Pos.Line, t.Pos.Col)
	}
	return b.String()
}
