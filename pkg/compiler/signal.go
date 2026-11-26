package compiler

import (
	"fmt"
	"strings"
	"unicode"
)

// signal.go — GX 响应式 Signal 语法糖降级。
//
// GX 写法：
//
//	sig count = 0                              // 声明源 sig
//	let doubled = Signal.computed(() => count * 2) // 派生 sig（只读）
//	Signal.effect(() => println(count))            // 副作用，依赖变化时重跑
//	count = 5                                      // 写 sig
//
// 降级为（函数式 sig，见 runtime/signal）：
//
//	var count = Signal.New(0)
//	doubled := Signal.Computed(func() int { return count() * 2 })
//	Signal.Effect(func() { println(count()) })
//	count(5)
//
// 核心改写：
//  1. 声明：sig name = expr → var name = Signal.New(expr)；let name = expr → name := expr；
//  2. 登记所有 sig 变量（sig 声明 + Signal.computed/Computed 派生）；
//  3. 读改写：表达式中裸写 sig 变量 name → name()；
//  4. 写改写：name = expr → name(expr)。
//
// 作用域排除（读改写不误伤）：selector 右侧 obj.name、复合字段键 {name: ...}、
// 声明目标、函数参数、已被改写为调用的 name(...)。
// 方法名大小写（computed→Computed、effect→Effect）交给既有 transformExternal
// 的「selector 右侧小写首字母还原」处理；Signal 包名 token 标记 IsGen，由
// ReconcileImports 统一注入 import 并解析别名。

// checkNoRawSignalAPI 禁止用户直接书写底层 Signal 运行时构造器（大写 API）。
//
// GX 只暴露语法糖：源信号用 `sig n = v`、派生用 `Signal.computed(...)`、
// 副作用用 `Signal.effect(...)`。底层 `Signal.New/Computed/Effect/...`（首字母
// 大写）是编译器内部实现细节，用户手写会绕过编译器导致：binding 认成普通变量
// 失去透明改写、占位包名被重命名触发 Signal2 未定义、被 sig 字段二次包装
// 类型错等一系列问题——这些正是第六、七轮反复打补丁的根因。禁止手写即可归零。
//
// 判据：`pkg . <大写开头标识符>` 形态，且 pkg 是「signal runtime 的包名引用」
// 而非用户同名变量。包名按「import 身份」判定而非字面 "Signal"（方案 B）：
//   - ident 解析到 BkImport 绑定且 ImportPath 指向 signal runtime → 任意别名
//     （import Signal / 无别名 signal / 自定义 s）都在覆盖范围内——早期版本
//     只认字面 "Signal"，且 BkImport 命中即放行，恰好是别名绕过的后门；
//   - 无绑定但名为 Signal → 隐式占位包名（编译器 ReconcileImports 时注入
//     import），同样是包名引用；
//   - 解析到其他绑定（局部/参数/字段，如 `Signal := Holder{New:"v"}`）→
//     合法的同名字段访问，放行；
//   - 首字母小写成员（computed/effect/createRoot 等）是语法糖，放行；
//   - 类型位置放行：Signal.Signal[T] / Signal.Owner 等是 signal 包的「类型名」
//     而非底层构造器（泛型组件签名需要），判据是成员名在类型白名单且其后
//     不是调用括号 '('；
//   - 只认用户手写的 token（IsGen==false）——编译器生成的 Signal.New/Computed/
//     Signal[T] 都带 IsGen，避免误报自身产物（第九轮 P2-A）。
//
// 调用时机：必须在 transformSignals 之后（Bindings 可用）、transformExternal 之前
// （小写语法糖尚未还原成大写）。详见 pipeline.go 调用点注释。
func checkNoRawSignalAPI(toks []Token, b *Bindings) error {
	for i := 0; i < len(toks); i++ {
		if toks[i].Kind != Ident || toks[i].IsGen || isGoKeyword(toks[i].Text) {
			continue // 编译器生成物 / 非标识符 / 关键字放行
		}
		dot := nextSig(toks, i+1)
		if dot < 0 || toks[dot].Text != "." {
			continue
		}
		name := nextSig(toks, dot+1)
		if name < 0 || toks[name].Kind != Ident || !startsUpper(toks[name].Text) {
			continue
		}
		// 包名身份判定（见函数注释）。
		pkgRef := false
		if b != nil {
			if bd := b.ResolveAt(toks[i].Text, toks[i].Pos); bd != nil {
				if bd.Kind != BkImport || !isSignalImportPath(bd.ImportPath) {
					continue // 用户同名变量/其他包的 import：合法访问，放行
				}
				pkgRef = true // signal runtime 的 import（任意别名）
			} else if toks[i].is("Signal") {
				pkgRef = true // 隐式占位包名，编译器后续注入 import
			}
		} else if toks[i].is("Signal") {
			pkgRef = true
		}
		if !pkgRef {
			continue
		}
		// 类型白名单且非调用 → 类型位置引用，放行；其余大写成员一律拦截。
		if isSignalTypeName(toks[name].Text) && !isCallParen(toks, name+1) {
			continue
		}
		// Effect 函数值接线放行：view.BindReactive(signal.Effect) 是官方启动
		// 接线用法（jsxspp-context 等示例），传的是运行时回调函数本身、不构造
		// 任何信号，不绕过编译器改写。调用形态 signal.Effect(...) 仍拦截——
		// 创建 effect 必须走 Signal.effect 语法糖。
		if toks[name].Text == "Effect" && !isCallParen(toks, name+1) {
			continue
		}
		return fmt.Errorf("%d:%d: 不允许直接调用底层 API %s.%s；源信号请用 `sig n = v`，派生用 `%s.computed(() => expr)`，副作用用 `%s.effect(() => {...})`",
			toks[name].Pos.Line, toks[name].Pos.Col, toks[i].Text, toks[name].Text, toks[i].Text, toks[i].Text)
	}
	return nil
}

// isSignalImportPath 判断 import 路径是否指向 signal runtime。
// 用后缀而非完整路径判定：module 名可由编译入口覆盖（CompileWithModule），
// 但 runtime/signal 的子路径固定。
func isSignalImportPath(path string) bool {
	return strings.HasSuffix(path, "/runtime/signal")
}

// isSignalTypeName 判断标识符是否为 runtime/signal 包的导出类型名。
// 这些标识符出现在类型位置（Signal.Signal[T]、Signal.Owner、Signal.Vec[T]）时
// 是合法的类型引用，不应被当作底层构造器 API 拦截。
func isSignalTypeName(name string) bool {
	switch name {
	case "Signal", "Owner", "Vec", "Computation", "Runtime", "TaskPoster", "GlobalLease":
		return true
	}
	return false
}

// isCallParen 判断 from 之后第一个有效 token 是否为调用括号 '('。
func isCallParen(toks []Token, from int) bool {
	k := nextSig(toks, from)
	return k >= 0 && toks[k].Text == "("
}

// startsUpper 判断标识符是否以大写字母开头，用于区分底层 API（New/Computed/...）
// 与小写语法糖（computed/effect/...）。非 ASCII 或空串按非大写处理（放行）。
func startsUpper(s string) bool {
	for _, r := range s {
		return unicode.IsUpper(r)
	}
	return false
}

// checkNoExplicitSigCall 禁止用户显式调用 sig（如 count()、s.name()）。
//
// GX 的 sig 是「透明响应式变量/字段」：读写都按普通变量书写（count + 1、
// s.name = v），编译器统一降级为函数式 Signal 的调用式（count()、s.name()）。
// 用户手写 () 是把实现细节当语法用——它虽被「已是调用」形态排除而原样保留、
// 侥幸能跑，但语义脆弱（JSX 插值里绕过订阅登记、与隐式读混用易错），且误导
// 读者以为 GX 需要 () 读信号。故统一报错，强制惯用写法。
//
// 覆盖两类：
//  1. sig 变量：标识符解析到 BkSignal/BkComputed 绑定且后跟 '('；
//  2. sig 字段：`recv.field(...)` 中 field 是 struct 的 sig 字段（由 signalFieldTable
//     判定），后跟 '(' 即报错，强制写 `recv.field` 让编译器补 ()。
//
// 形态排除：复合字段键 ({name: ...})、声明目标、声明区不报。selector 右侧不再是
// 无条件排除——它正是 sig 字段的所在，交由 isSignalSelector 精确判定（普通方法
// 如 s.HasSelection() 不会被误判，因为 HasSelection 不是 sig 字段）。
//
// 不误伤：context getter（scoreCtx.use() 后的 score()）是 BkLocal 且非 sig 字段；
// 编译器生成物带 IsGen，跳过；sig 声明的初值、赋值目标均非「后跟 (」。
//
// 调用时机：与 checkNoRawSignalAPI 同处——Bind 之后、transformSignals 之前。
func checkNoExplicitSigCall(toks []Token, b *Bindings, tbl signalFieldTable) error {
	if b == nil {
		return nil
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Kind != Ident || t.IsGen {
			continue
		}
		// 形态排除：复合字段键 / 声明目标 / 声明区。
		if nk := nextSig(toks, i+1); nk >= 0 && toks[nk].Text == ":" {
			continue
		}
		if isSignalDeclTarget(toks, i) || inDeclZone(toks, i) {
			continue
		}
		// 类别 1：sig 变量。标识符后紧跟 '(' 且解析到 sig/computed 绑定。
		// selector 右侧（obj.name）的 name 跳过——那是 sig 字段的领地，交类别 2。
		if n := nextSig(toks, i+1); n >= 0 && toks[n].Text == "(" {
			bd := bindingAt(b, t.Text, t.Pos)
			if bd != nil && (bd.Kind == BkSignal || bd.Kind == BkComputed) {
				if p := prevSig(toks, i-1); p >= 0 && toks[p].Text == "." {
					continue
				}
				return fmt.Errorf("%d:%d: sig 变量 %s 应像普通变量一样读写（写 %s 而非 %s()）；GX 会编译期自动补全信号读写",
					t.Pos.Line, t.Pos.Col, t.Text, t.Text, t.Text)
			}
		}
		// 类别 2：sig 字段链。把 i 当链首，复用编译器改写所用的 resolveSignalChainEx
		// ——它沿 '.' 逐段传播 receiver 类型，覆盖简单链 s.x、多级链 s.a.b.x、
		// 索引链 m["k"].x、调用结果链 getP().x。skipCalls=true 让中间段已显式写的
		// 括号（m.leaf().v）也能走过、收集到链尾所有 sig 段。
		// 对收集到的每个 sig 段，若其后紧跟 '('（用户手写的函数式读写）即报错。
		// 裸读（s.x）、普通方法（s.Method()）无 sig 段后跟括号，不受影响。
		if chain, ok := resolveSignalChainEx(toks, i, t.Pos, b, tbl, 0, true); ok {
			for _, seg := range chain.segs {
				if c := nextSig(toks, seg+1); c >= 0 && toks[c].Text == "(" {
					field := toks[seg].Text
					return fmt.Errorf("%d:%d: sig 字段 %s 应像普通字段一样读写（写 recv.%s 而非 recv.%s()）；GX 会编译期自动补全信号字段读写",
						toks[seg].Pos.Line, toks[seg].Pos.Col, field, field, field)
				}
			}
		}
	}
	return nil
}

// transformLetDecls 把 GX 的 let 声明降级为标准 Go（语言规范 §11：let 即短声明，
// 无新语义）。两种形态：
//
//	let name = expr   → name := expr
//	let name T = expr → var  name T = expr
//
// 必须无条件执行、不依赖 sig 是否存在（旧实现耦合在 transformSignals 的
// HasSignal 短路后面，导致纯 let 文件透传 `let` 生成非法 Go——遗留缺陷）。
// 无 `=` 的 `let name` 不消费后续 token，原样放行（交给下游报错，避免吞语句）。
func transformLetDecls(in []Token) []Token {
	changed := false
	for _, t := range in {
		if t.is("let") {
			changed = true
			break
		}
	}
	if !changed {
		return in
	}
	out := make([]Token, 0, len(in)+8)
	for i := 0; i < len(in); i++ {
		t := in[i]
		if !t.is("let") {
			out = append(out, t)
			continue
		}
		j := nextSig(in, i+1)
		if j < 0 || in[j].Kind != Ident || isGoKeyword(in[j].Text) {
			out = append(out, t) // 非声明形态（let 后无标识符）：放行，不吞 token
			continue
		}
		// 在本语句范围内找 `=`：中间可能是类型标注（let x int = 42 的 `int`），
		// 也可能根本没有（let x := ... 的非法写法）。越过换行/分号/块边界仍未找到
		// 就视为非声明形态，放行不吞 token。
		eq := -1
		for k := nextSig(in, j+1); k >= 0 && k < len(in); k = nextSig(in, k+1) {
			switch in[k].Text {
			case "=":
				eq = k
			case ";", "{", "}", ":=", ",", "(":
				k = -1 // 用 -1 终止：这些符号说明 let 后面不是 `name [= type] =` 声明
			}
			if eq >= 0 || k < 0 {
				break
			}
			if in[k].Kind == Newline {
				break
			}
		}
		if eq < 0 {
			out = append(out, t) // 无显式 `=`：放行
			continue
		}
		// j+1..eq 之间是空白或类型标注 token。
		hasType := false
		for k := j + 1; k < eq; k++ {
			if in[k].Kind != Space && in[k].Kind != Newline {
				hasType = true
				break
			}
		}
		if !hasType {
			// let name = expr → name := expr
			out = append(out, tokIdent(in[j].Text, in[j].Pos), tokSpace(" ", t.Pos), tokOp(":=", t.Pos))
			i = eq // 表达式余部由后续循环原样输出
			continue
		}
		// let name T = expr → var name T = expr
		out = append(out, tokIdent("var", t.Pos), tokSpace(" ", t.Pos), tokIdent(in[j].Text, in[j].Pos))
		for k := j + 1; k <= eq; k++ {
			out = append(out, in[k]) // 类型标注与 `=` 原样保留
		}
		i = eq
	}
	return out
}

// transformSignals 执行 sig 语法糖降级，返回改写后的 token 流。
// 放在 Analyze 之前：Analyze 看到的是标准 Go（sig/let 已消除、读写已是调用式）。
//
// P1：接收统一绑定表 bindings，遮蔽由 ResolveAt 词法解析（P1-1 根治）。
// E1（表达式 AST 阶段）：sig 声明与引用的识别收敛到 SignalAST —— 节点在原始流
// 上一次构建、直接持有 Binding，主循环按节点 Kind 驱动改写，不再在主循环里重复
// 「名字集合 + 遮蔽 + 形态排除」的扫 token 判定。sigs/isSignalRefAt 保留为对拍参照。
func transformSignals(in []Token, b *Bindings) []Token {
	// let 是与 signal 无关的短声明语法糖：无条件降级（遗留缺陷修复——此前
	// 耦合在 HasSignal 短路后面，纯 let 文件的 let 会透传成非法 Go）。
	in = transformLetDecls(in)
	ast := ParseSignalExprs(in, b)
	if !ast.HasSignal() {
		return in
	}
	return rewriteSignals(in, ast, b)
}

// isSignalRefAt 判断 pos 处对 name 的引用是否解析到 sig 绑定。
// 若解析到参数 / 局部变量 / 普通 var（同名遮蔽），返回 false —— 保持原样。
func isSignalRefAt(b *Bindings, name string, pos Pos) bool {
	if b == nil {
		return true // 兜底：无绑定表时退化为纯名字匹配
	}
	bd := b.ResolveAt(name, pos)
	return bd != nil && (bd.Kind == BkSignal || bd.Kind == BkComputed)
}

// collectSignalVars 收集所有 sig 变量名：
//   - sig name = ... 声明的源 sig；
//   - name := Signal.computed(...) / var name = Signal.Computed(...) 等派生 sig。
func collectSignalVars(toks []Token) map[string]bool {
	sigs := map[string]bool{}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Kind != Ident {
			continue
		}
		// sig name = ...
		if t.is("sig") {
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

// collectSignalTypes 在 sig 改写前，从原始 token 流收集 sig/let 变量的
// 声明类型（如 sig users = []User{{...}} → {"users":"[]User"}）。
// 改写后初值被 Signal.New(...) 包裹，字面量类型无法从声明直接推断，
// 因此本函数必须在 transformSignals 之前调用。
// 只收集能静态推断出类型的变量（布尔/字符串/数字/切片等），无法判定则不入表。
func collectSignalTypes(toks []Token) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("sig") && !toks[i].is("let") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 || j >= len(toks) || toks[j].Kind != Ident || isGoKeyword(toks[j].Text) {
			continue
		}
		eq := nextSig(toks, j+1)
		if eq < 0 || eq >= len(toks) || toks[eq].Text != "=" {
			continue
		}
		end := signalExprEnd(toks, eq+1)
		exprFrom := eq + 1
		for exprFrom < end && toks[exprFrom].Kind == Space {
			exprFrom++
		}
		if exprFrom >= end {
			continue
		}
		if typ := inferExprLitType(toks, exprFrom, end); typ != "" {
			out[toks[j].Text] = typ
		}
	}
	return out
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

		// 1) sig 声明：sig name = expr → var name = Signal.New(expr)
		if t.is("sig") {
			if j := nextSig(toks, i+1); j >= 0 && toks[j].Kind == Ident && !isGoKeyword(toks[j].Text) {
				// M-4：必须有显式 `=`。否则 `sig x` 的 nextSig 会指向后续语句，
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
				// 右侧表达式里的裸 sig 读取也要改写（H-3）：sig b = a → Signal.New(a())
				out = append(out, rewriteSignalReads(toks[k:end], ast)...)
				out = append(out, tokOp(")", t.Pos))
				i = end - 1
				continue
			}
		}

		// 2) let 声明由 transformLetDecls 无条件处理（见下），不再依赖 ast.HasSignal()。

		// 把用户写的 Signal.xxx 的 Signal 标记为生成 token，供 ReconcileImports 识别注入。
		// 第八轮 P1-4：标记前必须查 Bindings——若 Signal 在该位置解析到用户自己的
		// 局部/参数/类型绑定（如 `Signal := Holder{...}; Signal.val`），它是一次普通
		// 字段访问，不是手写运行时包名。误标 IsGen 会让 ReconcileImports 在别名冲突时
		// 把它连同 import 一起替换成 Signal2.val，静默篡改语义。
		if t.is("Signal") {
			if j := nextSig(toks, i+1); j >= 0 && toks[j].Text == "." {
				bd := b.ResolveAt(t.Text, t.Pos)
				if bd == nil || bd.Kind == BkImport {
					c := t
					c.IsGen = true
					out = append(out, c)
					continue
				}
			}
		}

		// 3) sig 变量读写改写：节点由 AST 按 Pos.Offset 命中；遮蔽/形态排除
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
				// 右侧表达式里的裸 sig 读取改写：a += b → a(a() + b())
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
				// 右侧表达式里的裸 sig 读取改写：a = b → a(b())
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

// rewriteSignalReads 改写一段表达式 token 里的裸 sig 读取 name → name()。
// 用于被整段拷贝出主循环的右侧表达式（sig 声明 / 简单赋值 / 复合赋值），
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

// rewriteSignalReadsInTemplateExpr 对模板插值片段内的 sig 裸读补 name → name()。
//
// 背景：sig 读写改写（transformSignals）先于模板展开（transformTemplates）执行，
// 彼时 ${...} 还是整块 Template token、内部标识符未被识别，导致模板里的 sig
// 读取漏改（实测 `v=${a}` 生成 fmt.Sprint(a)，输出函数地址而非值 0x…）。此处在
// 插值展开后按绑定表补一次改写。
//
// 插值 token 由子串重新词法分析得到、Pos 被重置为模板起始位置，无法用 ast.ExprAt
// 命中原始流节点；改用 ResolveAt(name, at) 按名字 + 模板位置解析绑定（at 为模板
// token 的原始 Pos，offset 语义让局部遮蔽天然生效）。形态排除与 ParseSignalExprs
// 一致：selector 右侧 / 复合字段键 / 声明目标 / 声明区 / 已是调用都不改。
func rewriteSignalReadsInTemplateExpr(seg []Token, b *Bindings, at Pos) []Token {
	if b == nil {
		return seg // 无绑定表时无法判定 sig，保持原样（生产路径 b 始终非 nil）
	}
	out := make([]Token, 0, len(seg))
	for i := range seg {
		t := seg[i]
		if t.Kind == Ident && isSignalRefAt(b, t.Text, at) {
			skip := false
			if p := prevSig(seg, i-1); p >= 0 && seg[p].Text == "." {
				skip = true // selector 右侧 obj.name
			}
			if n := nextSig(seg, i+1); n >= 0 && seg[n].Text == ":" {
				skip = true // 复合字段键 {name: ...}
			}
			if isSignalDeclTarget(seg, i) || inDeclZone(seg, i) {
				skip = true // 声明目标 / 函数参数 / struct 字段
			}
			if n := nextSig(seg, i+1); n >= 0 && seg[n].Text == "(" {
				skip = true // 已是调用 name(...)
			}
			if !skip {
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

// compoundAssignOps 是 sig 写需要展开为 name(name() op expr) 的复合赋值运算符。
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
		if toks[p].is("var") || toks[p].is("sig") || toks[p].is("let") {
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

// signalImportPath 返回 sig 运行时的 import 路径。
func signalImportPath(module string) string {
	if module == "" {
		module = defaultModule
	}
	return module + "/runtime/signal"
}

// trimSignalText 仅用于诊断展示：去掉空白。
func trimSignalText(s string) string { return strings.TrimSpace(s) }

// rewriteJSXInterpSignals 是给 JSX 插值片段专用的 sig 读改写（两参数版）。
//
// 背景：JSX 插值 {count} 在 transformSignals（基于 bindings+AST 的主流程）执行时
// 仍是整体 JSX token，改写穿透不到；scene/vdom 后端重新词法化插值时需要按名字集合
// 补上读调用 count()。这里的片段已脱离原始 token 流、无对应 Binding/AST，故沿用
// 基于 sigs 名字集合的改写逻辑（保留 sig 声明/赋值/复合赋值/自增等完整形态），
// 与上游三参数 rewriteSignals 分工不同、名字不冲突。
func rewriteJSXInterpSignals(toks []Token, sigs map[string]bool, errSink *error, allowCall string) []Token {
	// errSink：JSX 插值里的 sig 显式调用 count() 应报错（与主 token 流的
	// checkNoExplicitSigCall 同源）。插值 token 已脱离原始流、无 IsGen，按 sigs
	// 名字集合判定；遮蔽由调用方传入的 effective 集合 + 本函数形态排除处理。
	// allowCall：For item getter 参数名——它虽在 sigs 集合里，但 item 是 func() T，
	// 必须显式 t() 读取（写 t 会拿到函数地址），故排除报错。
	sink := errSink
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
			if sink != nil && *sink == nil && t.Text != allowCall {
				*sink = fmt.Errorf("%d:%d: sig 变量 %s 应像普通变量一样读写（写 %s 而非 %s()）；GX 会编译期自动补全信号读写",
					t.Pos.Line, t.Pos.Col, t.Text, t.Text, t.Text)
			}
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
			// 复合赋值 RHS 必须递归改写裸 sig 读（H-3），与顶层闭包一致：
			// load = load + 1 → load(load() + 1)，漏改会生成 load(load + 1)
			// 导致 Signal[int] 与 int 相加的类型错误。形态排除（selector 右侧 /
			// 字段键 / 声明目标 / 已是调用 / 嵌套复合赋值）由本函数递归统一处理。
			out = append(out, rewriteJSXInterpSignals(toks[k:end], sigs, sink, "")...)
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
			// 简单赋值 RHS 同样递归补读调用：a = b → a(b())。
			out = append(out, rewriteJSXInterpSignals(toks[k:end], sigs, sink, "")...)
			out = append(out, tokOp(")", t.Pos))
			i = end - 1
			continue
		}
		out = append(out, t, tokOp("(", t.Pos), tokOp(")", t.Pos))
	}
	return out
}
