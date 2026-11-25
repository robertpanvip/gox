package widgets

// 本文件验证 editor.gx 里 rope 版编辑核心与 string 版（公共 API / 原实现）
// 逐字节等价。string 版保留为 oracle，rope 版是实例命令的实际引擎。
// 差分测试覆盖定点用例 + 大量随机操作序列（含 CJK、空行、越界光标）。

import (
	"math/rand"
	"testing"

	Rope "github.com/robertpanvip/gox/runtime/rope"
)

func TestRopeEditCoreMatchesStringOracle(t *testing.T) {
	// 定点用例：插入 / 退格 / 换行 / 删除转发 / 区间删除。
	samples := []string{"", "abc", "ab\ncd", "你好\n世界", "a\n\nb", "\n", "a\nb\nc\nd"}
	for _, s := range samples {
		r := Rope.Of(s)

		// insertText
		for _, col := range []int{0, 1, 99, -1} {
			wantText, wantCol := insertText(s, 0, col, "X")
			nr, gotCol := ropeInsertText(r, 0, col, "X")
			if nr.String() != wantText || gotCol != wantCol {
				t.Fatalf("insertText(%q,col=%d): rope=(%q,%d) want=(%q,%d)", s, col, nr.String(), gotCol, wantText, wantCol)
			}
		}

		// backspaceAt：遍历所有行/列组合。
		for ln := 0; ln < lineCountOf(s); ln++ {
			lineRunes := runeLen(lineAt(s, ln))
			for col := 0; col <= lineRunes+1; col++ {
				wt, wl, wc := backspaceAt(s, ln, col)
				nr, gl, gc := ropeBackspaceAt(r, ln, col)
				if nr.String() != wt || gl != wl || gc != wc {
					t.Fatalf("backspaceAt(%q,ln=%d,col=%d): rope=(%q,%d,%d) want=(%q,%d,%d)",
						s, ln, col, nr.String(), gl, gc, wt, wl, wc)
				}
			}
		}

		// newlineAt
		for ln := 0; ln < lineCountOf(s); ln++ {
			lineRunes := runeLen(lineAt(s, ln))
			for col := 0; col <= lineRunes+1; col++ {
				wt, wl, wc := newlineAt(s, ln, col)
				nr, gl, gc := ropeNewlineAt(r, ln, col)
				if nr.String() != wt || gl != wl || gc != wc {
					t.Fatalf("newlineAt(%q,ln=%d,col=%d): rope=(%q,%d,%d) want=(%q,%d,%d)",
						s, ln, col, nr.String(), gl, gc, wt, wl, wc)
				}
			}
		}

		// deleteForward（手工 oracle：行内删 rune / 行尾并下一行）
		for ln := 0; ln < lineCountOf(s); ln++ {
			lines := splitLines(s)
			lineRunes := runeLen(lines[ln])
			for col := 0; col <= lineRunes+1; col++ {
				want, wantCol := deleteForwardOracle(s, ln, col)
				nr, gotCol := ropeDeleteForward(r, ln, col)
				if nr.String() != want || gotCol != wantCol {
					t.Fatalf("deleteForward(%q,ln=%d,col=%d): rope=(%q,%d) want=(%q,%d)",
						s, ln, col, nr.String(), gotCol, want, wantCol)
				}
			}
		}

		// removeRange：所有行对/列组合。
		lc := lineCountOf(s)
		for fl := 0; fl < lc; fl++ {
			for tl := fl; tl < lc; tl++ {
			for _, fc := range []int{0, 1, 99} {
				for _, tc := range []int{0, 1, 99} {
					if fl == tl && fc > tc { continue } // 合法选区：起点 <= 终点
					want := removeRange(s, fl, fc, tl, tc)
						got := ropeDeleteRange(r, fl, fc, tl, tc).String()
						if got != want {
							t.Fatalf("removeRange(%q,%d,%d,%d,%d): rope=%q want=%q", s, fl, fc, tl, tc, got, want)
						}
					}
				}
			}
		}
	}
}

// TestRandomEditSequence 用随机操作序列对拍：string oracle 与 rope 引擎在
// 同一序列下每步文本必须一致，覆盖长序列下的结构共享正确性。
func TestRandomEditSequence(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	alphabet := []rune("abcXYZ你好\n \t")
	for iter := 0; iter < 200; iter++ {
		s := ""
		r := Rope.Of("")
		ln, col := 0, 0
		for step := 0; step < 60; step++ {
			op := rng.Intn(5)
			switch op {
			case 0: // insert
				insN := 1 + rng.Intn(3)
				ins := ""
				for k := 0; k < insN; k++ {
					ins += string(alphabet[rng.Intn(len(alphabet))])
				}
				ns, nc := insertText(s, ln, col, ins)
				nr, _ := ropeInsertText(r, ln, col, ins)
				s, r, col = ns, nr, nc
			case 1: // backspace
				ns, nl, nc := backspaceAt(s, ln, col)
				nr, rl, rc := ropeBackspaceAt(r, ln, col)
				if rl != nl || rc != nc {
					t.Fatalf("iter=%d step=%d backspace cursor: rope=(%d,%d) want=(%d,%d)", iter, step, rl, rc, nl, nc)
				}
				s, r, ln, col = ns, nr, rl, rc
			case 2: // newline
				ns, nl, nc := newlineAt(s, ln, col)
				nr, rl, rc := ropeNewlineAt(r, ln, col)
				if rl != nl || rc != nc {
					t.Fatalf("iter=%d step=%d newline cursor: rope=(%d,%d) want=(%d,%d)", iter, step, rl, rc, nl, nc)
				}
				s, r, ln, col = ns, nr, rl, rc
			case 3: // delete forward
				lines := splitLines(s)
				if ln < 0 || ln >= len(lines) {
					continue
				}
				rs := []rune(lines[ln])
				if col > len(rs) { col = len(rs) }
				want, wc := deleteForwardOracle(s, ln, col)
				nr, rc := ropeDeleteForward(r, ln, col)
				_ = wc
				s, r, col = want, nr, rc
			case 4: // remove range
				lc := lineCountOf(s)
				if lc == 0 { continue }
				fl := rng.Intn(lc)
				tl := fl + rng.Intn(lc-fl)
				fc := rng.Intn(5)
				tc := rng.Intn(5)
				if fl == tl && fc > tc { fc, tc = tc, fc } // 合法选区：起点<=终点
				s = removeRange(s, fl, fc, tl, tc)
				r = ropeDeleteRange(r, fl, fc, tl, tc)
			}
			if r.String() != s {
				t.Fatalf("iter=%d step=%d op=%d: diverge\nrope=%q\nwant=%q (ln=%d col=%d)", iter, step, op, r.String(), s, ln, col)
			}
		}
	}
}

func lineCountOf(s string) int { return len(splitLines(s)) }
func lineAt(s string, ln int) string {
	ls := splitLines(s)
	if ln < 0 || ln >= len(ls) { return "" }
	return ls[ln]
}

// deleteForwardOracle 是 string 版 DeleteForward 的纯函数 oracle。
func deleteForwardOracle(v string, ln, col int) (string, int) {
	lines := splitLines(v)
	if ln < 0 || ln >= len(lines) { return v, col }
	rs := []rune(lines[ln])
	if col > len(rs) { col = len(rs) }
	if col >= len(rs) {
		if ln >= len(lines)-1 { return v, col }
		merged := string(rs) + lines[ln+1]
		out := []string{}
		for i, l := range lines {
			if i == ln { out = append(out, merged) } else if i != ln+1 { out = append(out, l) }
		}
		return joinLines(out), col
	}
	lines[ln] = string(rs[:col]) + string(rs[col+1:])
	return joinLines(lines), col
}
