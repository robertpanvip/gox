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
func transformSignals(in []Token) []Token {
	sigs := collectSignalVars(in)
	if len(sigs) == 0 {
		return in
	}
	return rewriteSignals(in, sigs)
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
func rewriteSignals(toks []Token, sigs map[string]bool) []Token {
	out := make([]Token, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		t := toks[i]

		// 1) signal 声明：signal name = expr → var name = Signal.New(expr)
		if t.is("signal") {
			if j := nextSig(toks, i+1); j >= 0 && toks[j].Kind == Ident && !isGoKeyword(toks[j].Text) {
				out = append(out, tokIdent("var", t.Pos), tokSpace(" ", t.Pos), tokIdent(toks[j].Text, t.Pos))
				// 找 `=` 与表达式边界
				eq := nextSig(toks, j+1)
				end := signalExprEnd(toks, eq+1)
				out = append(out, tokSpace(" ", t.Pos), tokOp("=", t.Pos), tokSpace(" ", t.Pos))
				out = append(out, signalPkgToken(t.Pos), tokOp(".", t.Pos), tokIdent("New", t.Pos), tokOp("(", t.Pos))
				// 跳过表达式起始空白，避免 `New( 0)`
				k := eq + 1
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

		// 3) signal 变量读写改写
		if t.Kind == Ident && sigs[t.Text] {
			// 声明目标不改写（var name = ... / name := ...）
			if isSignalDeclTarget(toks, i) {
				out = append(out, t)
				continue
			}
			// selector 右侧 obj.name 不改写
			if p := prevSig(toks, i-1); p >= 0 && toks[p].Text == "." {
				out = append(out, t)
				continue
			}
			// 复合字段键 {name: ...} 不改写
			if n := nextSig(toks, i+1); n >= 0 && toks[n].Text == ":" {
				out = append(out, t)
				continue
			}
			// 函数参数 / struct 字段声明不改写
			if inDeclZone(toks, i) {
				out = append(out, t)
				continue
			}
			// 已是调用式 name(...) 不重复改写
			if n := nextSig(toks, i+1); n >= 0 && toks[n].Text == "(" {
				out = append(out, t)
				continue
			}
			// 复合赋值：name += expr → name(name() + expr)；-=/*= 等类似
			if n := nextSig(toks, i+1); n >= 0 && compoundAssignOps[toks[n].Text] {
				op := toks[n].Text
				binOp := strings.TrimSuffix(op, "=") // "+=" → "+"
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
			// 自增/自减：name++ → name(name() + 1)
			if n := nextSig(toks, i+1); n >= 0 && (toks[n].Text == "++" || toks[n].Text == "--") {
				op := "+"
				if toks[n].Text == "--" {
					op = "-"
				}
				out = append(out, t, tokOp("(", t.Pos), t, tokOp("()", t.Pos))
				out = append(out, tokSpace(" ", t.Pos), tokOp(op, t.Pos), tokSpace(" ", t.Pos))
				out = append(out, tokOp("1", t.Pos), tokOp(")", t.Pos))
				i = n // 消费掉 ++/-- token
				continue
			}
			// 写：name = expr → name(expr)
			if n := nextSig(toks, i+1); n >= 0 && isAssignOp(toks[n].Text) {
				end := signalExprEnd(toks, n+1)
				out = append(out, t, tokOp("(", t.Pos))
				// 跳过表达式起始空白，避免 `count( 5)`
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
			// 读：name → name()
			out = append(out, t, tokOp("(", t.Pos), tokOp(")", t.Pos))
			continue
		}

		out = append(out, t)
	}
	return out
}

// isAssignOp 判断是否为简单赋值运算符。
func isAssignOp(s string) bool { return s == "=" }

// compoundAssignOps 是 signal 写需要展开为 name(name() op expr) 的复合赋值运算符。
var compoundAssignOps = map[string]bool{
	"+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
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
	// 名字后紧跟类型标识符 → 可能是声明；结合左侧上下文确认
	n := nextSig(toks, i+1)
	if n < 0 || toks[n].Kind != Ident || isGoKeyword(toks[n].Text) {
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
		if t.Kind == Newline && depth == 0 {
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
