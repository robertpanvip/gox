package compiler

import "testing"

func TestTemplateBasic(t *testing.T) {
	src := `package main
func main() {
	name := "gx"
	msg := ` + "`hello ${name}`" + `
	println(msg)
}
`
	got := compileOK(t, "tpl.gx", src)
	containsAll(t, got,
		`"hello "+fmt.Sprint(name)`,
		`import "fmt"`,
	)
}

func TestTemplateMultipleInterp(t *testing.T) {
	src := `package main
func main() {
	a := 1
	b := 2
	msg := ` + "`a=${a} b=${b} done`" + `
	println(msg)
}
`
	got := compileOK(t, "tpl2.gx", src)
	containsAll(t, got,
		`"a="+fmt.Sprint(a)+" b="+fmt.Sprint(b)+" done"`,
	)
}

func TestTemplateOnlyInterp(t *testing.T) {
	src := `package main
func main() {
	x := 42
	msg := ` + "`${x}`" + `
	println(msg)
}
`
	got := compileOK(t, "tpl3.gx", src)
	containsAll(t, got, `fmt.Sprint(x)`)
}

func TestTemplateReuseExistingFmtImport(t *testing.T) {
	src := `package main
import "fmt"
func main() {
	x := 1
	msg := ` + "`v=${x}`" + `
	fmt.Println(msg)
}
`
	got := compileOK(t, "tpl4.gx", src)
	containsAll(t, got, `fmt.Sprint(x)`)
	// 只应有一个 fmt import（复用而非重复注入）。
	count := 0
	for i := 0; i+len(`import "fmt"`) <= len(got); i++ {
		if got[i:i+len(`import "fmt"`)] == `import "fmt"` {
			count++
		}
	}
	if count != 1 {
		t.Errorf("fmt import 应恰好 1 处，实际 %d\n---\n%s", count, got)
	}
}

func TestTemplateInterpWithExternalCall(t *testing.T) {
	// 插值内的外部符号小写调用应被还原为大写。
	src := `package main
import "strconv"
func main() {
	x := 12
	msg := ` + "`n=${strconv.itoa(x)}`" + `
	println(msg)
}
`
	got := compileOK(t, "tpl5.gx", src)
	containsAll(t, got, `fmt.Sprint(strconv.Itoa(x))`)
}

func TestTemplateEscapes(t *testing.T) {
	// 静态段的引号须被 strconv.Quote 转义为合法 Go 字符串字面量。
	src := `package main
func main() {
	x := 1
	msg := ` + "`say \"hi\" ${x}`" + `
	println(msg)
}
`
	got := compileOK(t, "tpl6.gx", src)
	containsAll(t, got, `"say \"hi\" "+fmt.Sprint(x)`)
}

func TestTemplateInterpWithQuotedString(t *testing.T) {
	// 插值内含字符串字面量：parseTemplateParts 须跳过其中的引号与花括号。
	src := `package main
func main() {
	msg := ` + "`a${\"x\"}b`" + `
	println(msg)
}
`
	got := compileOK(t, "tpl7.gx", src)
	containsAll(t, got, `"a"+fmt.Sprint("x")+"b"`)
}
