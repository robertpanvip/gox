package compiler

// 编译期跨线程检测：在 sig 语法糖改写之前，扫描 `go func(){...}` 的函数体，
// 找出对 sig 变量的裸赋值。这类赋值在后台 goroutine 内读写响应式 signal，
// 与 UI 线程的依赖图重建并发，触发 fatal error: concurrent map writes。
//
// 这是「静态发现」而非运行时守卫：它覆盖最常见的手写形态
//
//	go func() { count = 5 }()          // 报错：应经Signal.safe投递
//	go func() { Signal.safe(func(){ count = 5 }) }()  // 放行
//
// 局限：无法跨函数追踪（go func 里调用一个内部写 sig 的普通函数），
// 那种情形交给 -tags gxthreadcheck 的运行时守卫兜底。

// checkCrossThreadSigWrite 扫描 go func 内对 sig 的裸写，返回诊断。
// 必须在 transformSignals 之前调用（此时 sig 仍是标识符、赋值仍是 =）。
func checkCrossThreadSigWrite(toks []Token, sigs map[string]bool) []GXDiagnostic {
	var out []GXDiagnostic
	if len(sigs) == 0 {
		return out
	}

	// 找每个 go func 的函数体范围 [body, bodyEnd)。
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("go") {
			continue
		}
		f := nextSig(toks, i+1)
		if f < 0 || !toks[f].is("func") {
			continue
		}
		// func 参数括号
		p := nextSig(toks, f+1)
		if p < 0 || toks[p].Text != "(" {
			continue
		}
		c := matchDelim(toks, p, "(", ")")
		if c <= p {
			continue
		}
		b := findFuncBodyBrace(toks, c)
		if b < 0 {
			continue
		}
		e := matchDelim(toks, b, "{", "}")
		if e <= b {
			continue
		}

		// safe/dispatch 回调的参数括号区间：落在其中的 sig 写是合法投递，放行。
		type span struct{ from, to int }
		var safeSpans []span
		for j := b + 1; j < e; j++ {
			t := toks[j]
			if t.Kind != Ident || (t.Text != "safe" && t.Text != "dispatch" &&
				t.Text != "Safe" && t.Text != "Dispatch") {
				continue
			}
			lp := nextSig(toks, j+1)
			if lp < 0 || toks[lp].Text != "(" {
				continue
			}
			rp := matchDelim(toks, lp, "(", ")")
			if rp > lp {
				safeSpans = append(safeSpans, span{lp, rp})
			}
		}
		inSafe := func(idx int) bool {
			for _, s := range safeSpans {
				if idx > s.from && idx < s.to {
					return true
				}
			}
			return false
		}

		// 扫描函数体内对 sig 的赋值
		for k := b + 1; k < e; k++ {
			t := toks[k]
			if t.Kind != Ident || !sigs[t.Text] || isGoKeyword(t.Text) {
				continue
			}
			op := nextSig(toks, k+1)
			if op < 0 {
				continue
			}
			write := isAssignOp(toks[op].Text) || compoundAssignOps[toks[op].Text] ||
				toks[op].Text == "++" || toks[op].Text == "--"
			if !write || inSafe(k) {
				continue
			}
			out = append(out, GXDiagnostic{
				Pos:  t.Pos,
				Code: "signal-cross-thread",
				Message: "后台 goroutine 直接写 sig " + t.Text +
					" 会与 UI 线程并发重建依赖图（可致 concurrent map writes）；请用 Signal.safe(() => { " +
					t.Text + " = ... }) 投递到 UI Runtime",
			})
			k = op // 跳过运算符，避免同一语句重复命中
		}
		i = e // 跳过整个函数体
	}
	return out
}
