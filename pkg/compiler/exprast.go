package compiler

// exprast.go — GX 扩展表达式 AST（E0 地基层）。
//
// 背景：各 GX 扩展表达式（signal / lambda / 对象字面量 / 解构 / 模板）此前各自
// 在 token 流上做「扫 token 找边界 + 消歧」的模式匹配，判定逻辑重复、坐标基准
// 不一（token 下标 vs Pos）。符号表方案里反复出现的 P4/P5 障碍，根因就是没有
// 一个「节点直接持有 Binding」的语义载体，只能靠 Pos 跨流映射。
//
// 本文件引入轻量 AST 层：节点在原始流（lex 后、Bind 后）上构建一次，以
// Pos.Offset 作稳定锚点，并直接持有解析到的 *Binding——这是符号表方案里
// 「AST 节点直接持有 Binding，无需 Pos 跨流映射」的落点。
//
// 边界（与方案 B 一致）：只表达 GX 扩展语法相关节点；普通 Go 表达式仍交给
// 官方工具链，不重复造 parser。
//
// 阶段：
//   - E0（本文件）：只建层 + 单元测试对拍，不改任何 transform 行为。
//   - E1：signal 的引用判定切到本层（transformSignals 消费 SignalAST）。
//   - E2：其余 detect 逐个切到本层，删除重复的扫 token 补丁函数。

import "fmt"

// ExprKind 是 GX 扩展表达式 AST 节点的语义类别。
type ExprKind uint8

const (
	ExSignalDecl     ExprKind = iota // signal name = expr（声明改写点）
	ExSignalRead                     // 裸读：name → name()
	ExSignalWrite                    // 简单写：name = expr → name(expr)
	ExSignalCompound                 // 复合写：name += expr → name(name()+expr)
	ExSignalIncDec                   // 自增自减：name++ → name(name()+1)
)

func (k ExprKind) String() string {
	switch k {
	case ExSignalDecl:
		return "signal-decl"
	case ExSignalRead:
		return "signal-read"
	case ExSignalWrite:
		return "signal-write"
	case ExSignalCompound:
		return "signal-compound"
	case ExSignalIncDec:
		return "signal-incdec"
	}
	return "?"
}

// GXExpr 是一个 GX 扩展表达式 AST 节点。
//   - Pos 是稳定锚点（Pos.Offset 单调，不受后续 desugar 增删 token 影响）；
//   - Binding 是解析到的声明，节点持有它即可判定 signal/computed/遮蔽，
//     无需再靠 token 下标跨流映射。
type GXExpr struct {
	Kind    ExprKind
	Name    string
	Idx     int      // 标识符/关键字在原始流的下标（调试用，不参与跨流定位）
	Pos     Pos      // 稳定锚点
	Binding *Binding // 解析到的 signal/computed 声明；声明节点为自身绑定
	RhsFrom int      // 右侧表达式起点（Decl/Write/Compound）；其余为 -1
	RhsTo   int      // 右侧表达式终点（开区间）
}

func (e *GXExpr) String() string {
	return fmt.Sprintf("%s(%s)@off=%d", e.Kind, e.Name, e.Pos.Offset)
}

// SignalAST 是一份源码里全部 signal 相关 AST 节点的集合。
// byOffset 以 Pos.Offset 为键，O(1) 查询且不受 token 下标漂移影响。
type SignalAST struct {
	byOffset    map[int]*GXExpr // 标识符/声明点 Pos.Offset → 节点
	nodes       []*GXExpr       // 稳定顺序的全体节点
	signalNames map[string]bool // 声明为 signal/computed 的名字（对齐 collectSignalVars）
	hasSignal   bool
}

// ParseSignalExprs 在原始 token 流上解析全部 signal 相关表达式节点。
//
// 判定顺序与现有 signal.go 完全对齐（E1 接入靠 golden 对拍兜底等价）：
//  1. signal 声明 → ExSignalDecl；
//  2. 标识符经 ResolveAt 解析，仅 BkSignal/BkComputed 进入引用判定（遮蔽天然排除）；
//  3. 形态排除：selector 右侧 / 复合字段键 / 声明目标 / 声明区 / 已是调用；
//  4. 按后继 token 分类为 Read/Write/Compound/IncDec。
//
// b 为 nil 时退化为不产生节点（生产路径始终非 nil；绑定表已由管线构建）。
func ParseSignalExprs(toks []Token, b *Bindings) *SignalAST {
	a := &SignalAST{
		byOffset:    map[int]*GXExpr{},
		signalNames: map[string]bool{},
	}
	add := func(n *GXExpr) {
		a.nodes = append(a.nodes, n)
		a.byOffset[n.Pos.Offset] = n
	}

	for i := 0; i < len(toks); i++ {
		t := toks[i]

		// 1) signal 声明：signal name [= expr]
		if t.is("signal") {
			if j := nextSig(toks, i+1); j >= 0 && toks[j].Kind == Ident && !isGoKeyword(toks[j].Text) {
				n := &GXExpr{
					Kind:    ExSignalDecl,
					Name:    toks[j].Text,
					Idx:     j,
					Pos:     t.Pos,
					Binding: bindingAt(b, toks[j].Text, toks[j].Pos),
					RhsFrom: -1,
					RhsTo:   -1,
				}
				if eq := nextSig(toks, j+1); eq >= 0 && toks[eq].Text == "=" {
					n.RhsFrom, n.RhsTo = eq+1, signalExprEnd(toks, eq+1)
				}
				add(n)
				a.signalNames[n.Name] = true
				a.hasSignal = true
			}
			continue
		}

		if t.Kind != Ident {
			continue
		}

		// 2) 绑定判定：仅 signal/computed 声明的引用进入；同名参数/局部变量的
		// ResolveAt 落在 BkParam/BkLocal 上，天然被排除（等价于旧 isSignalRefAt）。
		bd := bindingAt(b, t.Text, t.Pos)
		if bd == nil || (bd.Kind != BkSignal && bd.Kind != BkComputed) {
			continue
		}
		// 记录声明名（computed 派生的目标在声明区被排除，但仍是 signal 名字）。
		a.signalNames[bd.Name] = true
		a.hasSignal = true

		// 3) 形态排除
		if p := prevSig(toks, i-1); p >= 0 && toks[p].Text == "." {
			continue // selector 右侧 obj.name
		}
		if n := nextSig(toks, i+1); n >= 0 && toks[n].Text == ":" {
			continue // 复合字段键 {name: ...}
		}
		if isSignalDeclTarget(toks, i) {
			continue // var/signal/let/:= 声明目标
		}
		if inDeclZone(toks, i) {
			continue // 函数参数 / struct 字段声明
		}
		if n := nextSig(toks, i+1); n >= 0 && toks[n].Text == "(" {
			continue // 已是调用式 name(...)
		}

		// 4) 读写分类
		n := &GXExpr{Name: t.Text, Idx: i, Pos: t.Pos, Binding: bd, RhsFrom: -1, RhsTo: -1}
		k := nextSig(toks, i+1)
		switch {
		case k >= 0 && compoundAssignOps[toks[k].Text]:
			n.Kind = ExSignalCompound
			n.RhsFrom, n.RhsTo = k+1, signalExprEnd(toks, k+1)
		case k >= 0 && (toks[k].Text == "++" || toks[k].Text == "--"):
			n.Kind = ExSignalIncDec
		case k >= 0 && toks[k].Text == "=":
			n.Kind = ExSignalWrite
			n.RhsFrom, n.RhsTo = k+1, signalExprEnd(toks, k+1)
		default:
			n.Kind = ExSignalRead
		}
		add(n)
	}
	return a
}

// bindingAt 是 nil 安全的 ResolveAt 包装。
func bindingAt(b *Bindings, name string, pos Pos) *Binding {
	if b == nil {
		return nil
	}
	return b.ResolveAt(name, pos)
}

// ExprAt 返回 Pos.Offset 为 off 处的节点，无则 nil。
func (a *SignalAST) ExprAt(off int) *GXExpr {
	if a == nil {
		return nil
	}
	return a.byOffset[off]
}

// HasSignal 表示源码里存在 signal/computed 声明（对齐 transformSignals 的早退条件）。
func (a *SignalAST) HasSignal() bool { return a != nil && a.hasSignal }

// Nodes 返回全体节点（稳定顺序）。
func (a *SignalAST) Nodes() []*GXExpr {
	if a == nil {
		return nil
	}
	return a.nodes
}

// SignalNames 返回声明为 signal/computed 的名字集合（对拍用）。
func (a *SignalAST) SignalNames() map[string]bool { return a.signalNames }
