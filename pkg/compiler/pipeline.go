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
	Analysis  *Analysis
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
// lex → analyze(GX 语义层：符号表 / 收窄 / 诊断) → union(联合类型擦除)
// → typeguard(is 类型守卫，消费 analyze 结果) → enum → pub → jsx
// → collections(集合链式) → scope(作用域符号收集) → restoreRefs(自身 pub 符号)
// → external(外部符号机械还原) → emit。
//
// analyze 必须在 union 之前：此时联合类型 A|B 尚未被擦除，is 守卫仍是原貌，
// 语义层才能建立真实的联合类型环境并做收窄。typeguard 消费 analyze 的产物，
// 只负责生成 comma-ok 断言，不再自行判定守卫。
// scope 必须在 pub 之后（pub 名已大写，不会被误收为私有）、
// jsx/collections 之后（它们注入的 import 也要被识别为包名）。
func Compile(file, src string) (*Result, error) {
	return CompileWithModule(file, src, defaultModule)
}

// CompileWithModule 以指定 module 转译。<module>/runtime/list 作为集合运行时 import 路径。
func CompileWithModule(file, src, module string) (*Result, error) {
	toks := New(src).AllTokens()
	analysis := Analyze(toks) // desugar 之前的 GX 语义层（联合未擦除）
	names := map[string]string{} // 小写原名 → 大写导出名（自身 pub 符号）
	toks = runUnion(toks)
	toks = transformTypeGuard(toks, analysis)
	if err := checkStrandedIs(toks); err != nil {
		return nil, err
	}
	toks = transformEnum(toks, names)
	toks = transformPub(toks, names)
	toks = transformJSX(toks)
	toks = transformCollections(toks, module)
	// 统一 import 别名：源码若已用别名导入 list/vdom/strconv，生成代码的占位包名
	// 需替换为实际别名；否则各 transform 注入的 import 会与生成引用对不上。
	toks = ReconcileImports(toks, module)
	scope := collectScope(toks)
	toks = restoreRefs(toks, names)
	toks = transformExternal(toks, scope, names)
	toks = transformCompositeFields(toks, scope)
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
		toks = transformJSX(toks)
	case "pub":
		toks = transformPub(toks, names)
	case "collections":
		toks = transformCollections(toks, defaultModule)
	case "external":
		scope := collectScope(toks)
		toks = transformExternal(toks, scope, names)
		toks = transformCompositeFields(toks, scope)
	case "all":
		analysis := Analyze(toks)
		toks = runUnion(toks)
		toks = transformTypeGuard(toks, analysis)
		toks = transformEnum(toks, names)
		toks = transformPub(toks, names)
		toks = transformJSX(toks)
		toks = transformCollections(toks, defaultModule)
		toks = ReconcileImports(toks, defaultModule)
		scope := collectScope(toks)
		toks = restoreRefs(toks, names)
		toks = transformExternal(toks, scope, names)
		toks = transformCompositeFields(toks, scope)
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
