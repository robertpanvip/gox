package compiler

import "strings"

// signal.go — GX 响应式 Signal 语法糖降级。
//
// GX 写法：
//
//	signal count = 0                              // 声明源 signal
//	let doubled = Signal.computed(() => count * 2) // 派生 signal（只读）
//	Signal.effect(() => println(count))            // 副作用，依赖变化时重跑
//	count = 5                                      // 写 signal
//
// 降级为（函数式 signal，见 runtime/signal）：
//
//	var count = Signal.New(0)
//	doubled := Signal.Computed(func() int { return count() * 2 })
//	Signal.Effect(func() { println(count()) })
//	count(5)
//
// 核心改写：
//  1. 声明：signal name = expr → var name = Signal.New(expr)；let name = expr → name := expr；
//  2. 登记所有 signal 变量（signal 声明 + Signal.computed/Computed 派生）；
//  3. 读改写：表达式中裸写 signal 变量 name → name()；
//  4. 写改写：name = expr → name(expr)。
//
// 作用域排除（读改写不误伤）：selector 右侧 obj.name、复合字段键 {name: ...}、
// 声明目标、函数参数、已被改写为调用的 name(...)。
// 方法名大小写（computed→Computed、effect→Effect）交给既有 transformExternal
// 的「selector 右侧小写首字母还原」处理；Signal 包名 token 标记 IsGen，由
// ReconcileImports 统一注入 import 并解析别名。

// transformSignals 执行 signal 语法糖降级，返回改写后的 token 流。
// 放在 Analyze 之前：Analyze 看到的是标准 Go（signal/let 已消除、读写已是调用式）。
//
// P1：接收统一绑定表 bindings，遮蔽由 ResolveAt 词法解析（P1-1 根治）。
// E1（表达式 AST 阶段）：signal 声明与引用的识别收敛到 SignalAST —— 节点在原始流
// 上一次构建、直接持有 Binding，主循环按节点 Kind 驱动改写，不再在主循环里重复
// 「名字集合 + 遮蔽 + 形态排除」的扫 token 判定。sigs/isSignalRefAt 保留为对拍参照。
func transformSignals(in []Token, b *Bindings) []Token {
	ast := ParseSignalExprs(in, b)
	if !ast.HasSignal() {
		return in
	}
	return rewriteSignals(in, ast, b)
}

// isSignalRefAt 判断 pos 处对 name 的引用是否解析到 signal 绑定。
// 若解析到参数 / 局部变量 / 普通 var（同名遮蔽），返回 false —— 保持原样。
func isSignalRefAt(b *Bindings, name string, pos Pos) bool {
	if b == nil {
		return true // 兜底：无绑定表时退化为纯名字匹配
	}
	bd := b.ResolveAt(name, pos)
	return bd != nil && (bd.Kind == BkSignal || bd.Kind == BkComputed)
}

// collectSignalVars 收集所有 signal 变量名：
//   - signal name = ... 声明的源 signal；
//   - name := Signal.computed(...) / var name = Signal.Computed(...) 等派生 signal。
func collectSignalVars(toks []Token) map[string]bool {
	sigs := map[string]bool{}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Kind != Ident {
			continue
		}
		// signal name = ...
		if t.is("signal") {
			if j := nextSig(toks, i+1); j >= 0 && toks[j].Kind == Ident && !isGoKeyword(toks[j].Text) {
				sigs[toks[j].Text] = true
			}
			continue
		}
		// name := Signal.computed(...) / name = Signal.Computed(...) / var name = ...
		if t.Text == "computed" || t.Text == "Computed" {
			// 向前找 selector 的接收者必须是 Signal
			if name := computedAssignTarget(toks, i); name != "" {
				sigs[name] = true
			}
		}
	}
	return sigs
}

// computedAssignTarget 若 computed/Computed 调用处于 `name := Signal.computed(...)`
// 或 `var name = Signal.computed(...)` 等赋值右侧，返回被赋值的变量名。
func computedAssignTarget(toks []Token, computedIdx int) string {
	// 确认 computed 是 Signal 的 selector：Signal . computed
	dot := prevSig(toks, computedIdx-1)
	if dot < 0 || toks[dot].Text != "." {
		return ""
	}
	recv := prevSig(toks, dot-1)
	if recv < 0 || !toks[recv].is("Signal") {
		return ""
	}
	// 向左找 `=`/`:=`，再找赋值目标标识符
	op := prevSig(toks, recv-1)
	for op >= 0 && toks[op].Text != "=" && toks[op].Text != ":=" {
		// 跳过类型标注等，最多回退到语句起点
		if toks[op].Kind == Newline || toks[op].Text == ";" || toks[op].Text == "{" {
			return ""
		}
		op = prevSig(toks, op-1)
	}
	if op < 0 || (toks[op].Text != "=" && toks[op].Text != ":=") {
		return ""
	}
	target := prevSig(toks, op-1)
	if target >= 0 && toks[target].Kind == Ident && !isGoKeyword(toks[target].Text) {
		return toks[target].Text
	}
	return ""
}

// rewriteSignals 生成改写后的 token 流。
// E1：读写判定改由 AST 节点 Kind 驱动（ast.ExprAt(Pos.Offset)），主循环不再做
// 「名字集合 + 遮蔽 + 形态排除」的重复扫 token —— 这些已在 ParseSignalExprs 一次完成。
func rewriteSignals(toks []Token, ast *SignalAST, b *Bindings) []Token {
	out := make([]Token, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		t := toks[i]

		// 1) signal 声明：signal name = expr → var name = Signal.New(expr)
		if t.is("signal") {
			if j := nextSig(toks, i+1); j >= 0 && toks[j].Kind == Ident && !isGoKeyword(toks[j].Text) {
				// M-4：必须有显式 `=`。否则 `signal x` 的 nextSig 会指向后续语句，
				// 把下一条语句吞进 Signal.New(...)。无 `=` 时只降级为 `var name`，
				// 不消费后续 token（交给主循环原样输出）。
				eq := nextSig(toks, j+1)
				if eq < 0 || toks[eq].Text != "=" {
					// 无初值：降级为 var name any（合法 Go），不吞后续语句。
					out = append(out, tokIdent("var", t.Pos), tokSpace(" ", t.Pos), tokIdent(toks[j].Text, t.Pos),
						tokSpace(" ", t.Pos), tokIdent("any", t.Pos))
					i = j
					continue
				}
				out = append(out, tokIdent("var", t.Pos), tokSpace(" ", t.Pos), tokIdent(toks[j].Text, t.Pos))
				// 找 `=` 与表达式边界
				end := signalExprEnd(toks, eq+1)
				out = append(out, tokSpace(" ", t.Pos), tokOp("=", t.Pos), tokSpace(" ", t.Pos))
				out = append(out, signalPkgToken(t.Pos), tokOp(".", t.Pos), tokIdent("New", t.Pos), tokOp("(", t.Pos))
				// 跳过表达式起始空白，避免 `New( 0)`
				k := eq + 1
				for k < end && toks[k].Kind == Space {
					k++
				}
				// 右侧表达式里的裸 signal 读取也要改写（H-3）：signal b = a → Signal.New(a())
				out = append(out, rewriteSignalReads(toks[k:end], ast)...)
				out = append(out, tokOp(")", t.Pos))
				i = end - 1
				continue
			}
		}

		// 2) let 声明：let name = expr → name := expr
		if t.is("let") {
			if j := nextSig(toks, i+1); j >= 0 && toks[j].Kind == Ident && !isGoKeyword(toks[j].Text) {
				out = append(out, tokIdent(toks[j].Text, t.Pos), tokSpace(" ", t.Pos))
				eq := nextSig(toks, j+1)
				// 原 `=` token 用 `:=` 替换；若两者之间有空白，跳过
				for k := j + 1; k < eq; k++ {
					if toks[k].Kind == Space {
						continue
					}
					out = append(out, toks[k])
				}
				out = append(out, tokOp(":=", t.Pos))
				i = eq // 表达式余部由后续循环原样输出
				continue
			}
		}

		// 把用户写的 Signal.xxx 的 Signal 标记为生成 token，供 ReconcileImports 识别注入。
		if t.is("Signal") {
			if j := nextSig(toks, i+1); j >= 0 && toks[j].Text == "." {
				c := t
				c.IsGen = true
				out = append(out, c)
				continue
			}
		}

		// 3) signal 变量读写改写：节点由 AST 按 Pos.Offset 命中；遮蔽/形态排除
		// 已在 ParseSignalExprs 完成，这里只按 Kind 生成。
		if t.Kind == Ident {
			node := ast.ExprAt(t.Pos.Offset)
			if node == nil || node.Kind == ExSignalDecl {
				out = append(out, t)
				continue
			}
			switch node.Kind {
			case ExSignalCompound:
				// 复合赋值：name += expr → name(name() + expr)；-=/*= 等类似
				n := nextSig(toks, i+1)
				op := toks[n].Text
				binOp := strings.TrimSuffix(op, "=") // "+=" → "+"
				end := signalExprEnd(toks, n+1)
				out = append(out, t, tokOp("(", t.Pos), t, tokOp("()", t.Pos))
				out = append(out, tokSpace(" ", t.Pos), tokOp(binOp, t.Pos), tokSpace(" ", t.Pos))
				k := n + 1
				for k < end && toks[k].Kind == Space {
					k++
				}
				// 右侧表达式里的裸 signal 读取改写：a += b → a(a() + b())
				out = append(out, rewriteSignalReads(toks[k:end], ast)...)
				out = append(out, tokOp(")", t.Pos))
				i = end - 1
			case ExSignalIncDec:
				// 自增/自减：name++ → name(name() + 1)
				n := nextSig(toks, i+1)
				op := "+"
				if toks[n].Text == "--" {
					op = "-"
				}
				out = append(out, t, tokOp("(", t.Pos), t, tokOp("()", t.Pos))
				out = append(out, tokSpace(" ", t.Pos), tokOp(op, t.Pos), tokSpace(" ", t.Pos))
				out = append(out, tokOp("1", t.Pos), tokOp(")", t.Pos))
				i = n // 消费掉 ++/-- token
			case ExSignalWrite:
				// 写：name = expr → name(expr)
				n := nextSig(toks, i+1)
				end := signalExprEnd(toks, n+1)
				out = append(out, t, tokOp("(", t.Pos))
				// 跳过表达式起始空白，避免 `count( 5)`
				k := n + 1
				for k < end && toks[k].Kind == Space {
					k++
				}
				// 右侧表达式里的裸 signal 读取改写：a = b → a(b())
				out = append(out, rewriteSignalReads(toks[k:end], ast)...)
				out = append(out, tokOp(")", t.Pos))
				i = end - 1
			case ExSignalRead:
				// 读：name → name()
				out = append(out, t, tokOp("(", t.Pos), tokOp(")", t.Pos))
			}
			continue
		}

		out = append(out, t)
	}
	return out
}

// rewriteSignalReads 改写一段表达式 token 里的裸 signal 读取 name → name()。
// 用于被整段拷贝出主循环的右侧表达式（signal 声明 / 简单赋值 / 复合赋值），
// 这些片段不会经过 rewriteSignals 主循环的读改写，需在此补上（H-3）。
// E1：片段 token 保留原始 Pos，直接按 Pos.Offset 查 AST 节点；只有 ExSignalRead
// 类节点改写成调用式，其余（遮蔽/selector/字段键/声明目标/已调用）在 AST 解析
// 阶段已被排除或未归类为 Read。
func rewriteSignalReads(seg []Token, ast *SignalAST) []Token {
	out := make([]Token, 0, len(seg))
	for i := range seg {
		t := seg[i]
		if t.Kind == Ident {
			if node := ast.ExprAt(t.Pos.Offset); node != nil && node.Kind == ExSignalRead {
				out = append(out, t, tokOp("(", t.Pos), tokOp(")", t.Pos))
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// isAssignOp 判断是否为简单赋值运算符。
func isAssignOp(s string) bool { return s == "=" }

// compoundAssignOps 是 signal 写需要展开为 name(name() op expr) 的复合赋值运算符。
// 覆盖 Go 全部复合赋值：算术(+ - * / %)与位运算(& | ^ << >> &^)。
var compoundAssignOps = map[string]bool{
	"+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	"&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true, "&^=": true,
}

// isSignalDeclTarget 判断位置 i 是否是变量声明/短声明的左侧目标。
func isSignalDeclTarget(toks []Token, i int) bool {
	if n := nextSig(toks, i+1); n >= 0 && toks[n].Text == ":=" {
		return true
	}
	if p := prevSig(toks, i-1); p >= 0 {
		if toks[p].is("var") || toks[p].is("signal") || toks[p].is("let") {
			return true
		}
		// var name TYPE = ... （name 后是类型，再后是 =）：name 仍是声明目标
		if n := nextSig(toks, i+1); n >= 0 && toks[n].Kind == Ident && !isGoKeyword(toks[n].Text) {
			if e := nextSig(toks, n+1); e >= 0 && toks[e].Text == "=" {
				if toks[p].is("var") {
					return true
				}
			}
		}
	}
	return false
}

// inDeclZone 判断位置 i 是否处于「名字后跟类型」的声明上下文：
//   - 函数参数：func f(name int) / func(name int)
//   - struct 字段：type T struct { name int }
//
// 判据：从 i 向左跳过空白，若最近的有效 token 是 func 参数左括号 '(' 且
// 其前是 func/函数名，或最近处于 struct{...} 块内，则视为声明区。
// 同时用「i 后紧跟类型标识符」排除误伤（读表达式后通常跟运算符/分隔符）。
func inDeclZone(toks []Token, i int) bool {
	// 名字后紧跟「类型标识符」「逗号」「右括号」才可能是参数声明；
	// 结合左侧括号上下文（func 定义 vs 调用）最终确认。
	// 逗号/右括号是为 `func f(a, b int)` 的首参 a（后跟逗号）放宽（L-6）。
	n := nextSig(toks, i+1)
	if n < 0 {
		return false
	}
	if !(toks[n].Kind == Ident && !isGoKeyword(toks[n].Text)) && toks[n].Text != "," && toks[n].Text != ")" {
		return false
	}
	// 向左找最近的语句边界内的 '(' 或 '{'
	for j := prevSig(toks, i-1); j >= 0; j = prevSig(toks, j-1) {
		t := toks[j]
		switch t.Text {
		case "(":
			// func 参数列表：'(' 前是 func 关键字或函数名（函数名前是 func）
			p := prevSig(toks, j-1)
			if p >= 0 && toks[p].is("func") {
				return true
			}
			if p >= 0 && toks[p].Kind == Ident {
				q := prevSig(toks, p-1)
				if q >= 0 && toks[q].is("func") {
					return true
				}
			}
			return false
		case "{":
			// struct 字段块：'{' 前是 struct 关键字
			if p := prevSig(toks, j-1); p >= 0 && toks[p].is("struct") {
				return true
			}
			return false
		case ")", "}", ";":
			return false
		}
		if t.Kind == Newline {
			return false
		}
	}
	return false
}

// signalExprEnd 返回赋值右侧表达式的结束下标（开区间）。
// 从 start 起按括号深度扫描，遇深度 0 的换行/分号/逗号/闭括号停止。
func signalExprEnd(toks []Token, start int) int {
	depth := 0
	for i := start; i < len(toks); i++ {
		t := toks[i]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth == 0 {
				return i
			}
			depth--
		case ",", ";":
			if depth == 0 {
				return i
			}
		}
		if (t.Kind == Newline || t.Kind == Comment) && depth == 0 {
			return i
		}
		if t.Kind == EOF {
			return i
		}
	}
	return len(toks)
}

// signalPkgToken 生成标记为 IsGen 的 Signal 占位包名 token。
func signalPkgToken(p Pos) Token {
	return Token{Kind: Ident, Text: "Signal", Pos: p, IsGen: true}
}

// signalImportPath 返回 signal 运行时的 import 路径。
func signalImportPath(module string) string {
	if module == "" {
		module = defaultModule
	}
	return module + "/runtime/signal"
}

// trimSignalText 仅用于诊断展示：去掉空白。
func trimSignalText(s string) string { return strings.TrimSpace(s) }

// rewriteJSXInterpSignals 是给 JSX 插值片段专用的 signal 读改写（两参数版）。
//
// 背景：JSX 插值 {count} 在 transformSignals（基于 bindings+AST 的主流程）执行时
// 仍是整体 JSX token，改写穿透不到；scene/vdom 后端重新词法化插值时需要按名字集合
// 补上读调用 count()。这里的片段已脱离原始 token 流、无对应 Binding/AST，故沿用
// 基于 sigs 名字集合的改写逻辑（保留 signal 声明/赋值/复合赋值/自增等完整形态），
// 与上游三参数 rewriteSignals 分工不同、名字不冲突。
func rewriteJSXInterpSignals(toks []Token, sigs map[string]bool) []Token {
	out := make([]Token, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Kind != Ident || !sigs[t.Text] {
			out = append(out, t)
			continue
		}
		if isSignalDeclTarget(toks, i) {
			out = append(out, t)
			continue
		}
		if p := prevSig(toks, i-1); p >= 0 && toks[p].Text == "." {
			out = append(out, t)
			continue
		}
		if n := nextSig(toks, i+1); n >= 0 && toks[n].Text == ":" {
			out = append(out, t)
			continue
		}
		if inDeclZone(toks, i) {
			out = append(out, t)
			continue
		}
		if n := nextSig(toks, i+1); n >= 0 && toks[n].Text == "(" {
			out = append(out, t)
			continue
		}
		if n := nextSig(toks, i+1); n >= 0 && compoundAssignOps[toks[n].Text] {
			op := toks[n].Text
			binOp := strings.TrimSuffix(op, "=")
			end := signalExprEnd(toks, n+1)
			out = append(out, t, tokOp("(", t.Pos), t, tokOp("()", t.Pos))
			out = append(out, tokSpace(" ", t.Pos), tokOp(binOp, t.Pos), tokSpace(" ", t.Pos))
			k := n + 1
			for k < end && toks[k].Kind == Space {
				k++
			}
			for ; k < end; k++ {
				out = append(out, toks[k])
			}
			out = append(out, tokOp(")", t.Pos))
			i = end - 1
			continue
		}
		if n := nextSig(toks, i+1); n >= 0 && (toks[n].Text == "++" || toks[n].Text == "--") {
			op := "+"
			if toks[n].Text == "--" {
				op = "-"
			}
			out = append(out, t, tokOp("(", t.Pos), t, tokOp("()", t.Pos))
			out = append(out, tokSpace(" ", t.Pos), tokOp(op, t.Pos), tokSpace(" ", t.Pos))
			out = append(out, tokOp("1", t.Pos), tokOp(")", t.Pos))
			i = n
			continue
		}
		if n := nextSig(toks, i+1); n >= 0 && isAssignOp(toks[n].Text) {
			end := signalExprEnd(toks, n+1)
			out = append(out, t, tokOp("(", t.Pos))
			k := n + 1
			for k < end && toks[k].Kind == Space {
				k++
			}
			for ; k < end; k++ {
				out = append(out, toks[k])
			}
			out = append(out, tokOp(")", t.Pos))
			i = end - 1
			continue
		}
		out = append(out, t, tokOp("(", t.Pos), tokOp(")", t.Pos))
	}
	return out
}
