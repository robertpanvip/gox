package compiler

import (
	"os"
	"strings"
	"testing"
)

// goTokens 把 Go 源码切成有效 token 序列（忽略排版空白与注释），
// 用于验证格式化前后编译产物的语义一致。
func goTokens(src string) string {
	var b strings.Builder
	for _, t := range New(src).AllTokens() {
		switch t.Kind {
		case Space, Newline, Comment, EOF:
			continue
		}
		b.WriteString(t.Kind.String())
		b.WriteString(":")
		b.WriteString(t.Text)
		b.WriteString("|")
	}
	return b.String()
}

func assertFormatSemantics(t *testing.T, name, src string) string {
	t.Helper()
	out := Format(src)
	before, err := Compile("before.gx", src)
	if err != nil {
		t.Fatalf("%s: 格式化前编译失败: %v", name, err)
	}
	after, err := Compile("after.gx", out)
	if err != nil {
		t.Fatalf("%s: 格式化后编译失败:\n%s", name, out)
	}
	if a, b := goTokens(before.GoSource), goTokens(after.GoSource); a != b {
		t.Fatalf("%s: 格式化改变了语义\n--- before ---\n%s\n--- after ---\n%s\n--- src ---\n%s",
			name, before.GoSource, after.GoSource, out)
	}
	if again := Format(out); again != out {
		t.Fatalf("%s: 格式化不是幂等的\n--- once ---\n%s\n--- twice ---\n%s", name, out, again)
	}
	return out
}

func TestFormatSelfClosingAndAttrs(t *testing.T) {
	src := `package ui
func view() any {
    return <input disabled ref={onMount} placeholder="hi"/>
}
`
	out := assertFormatSemantics(t, "self-closing", src)
	if !strings.Contains(out, `return <input disabled ref={onMount} placeholder="hi"/>`) {
		t.Fatalf("自闭合元素格式不符:\n%s", out)
	}
}

func TestFormatNestedChildrenIndented(t *testing.T) {
	src := `package ui
func view() any {
    return <div bg="#1e1e1e"><div flex-direction="column"><span>hi</span>{name}</div></div>
}
`
	out := assertFormatSemantics(t, "nested", src)
	want := `    return <div bg="#1e1e1e">
        <div flex-direction="column">
            <span>hi</span>
            {name}
        </div>
    </div>`
	if !strings.Contains(out, want) {
		t.Fatalf("嵌套缩进不符，期望包含:\n%s\n实际:\n%s", want, out)
	}
}

func TestFormatLongAttrsOnePerLine(t *testing.T) {
	src := `package ui
func view() any {
    return <div bg="#0b1020" padding="20" border-radius="12" flex-direction="column" gap="8" color="#ffffff" width="200"><b>card</b></div>
}
`
	out := assertFormatSemantics(t, "long-attrs", src)
	for _, frag := range []string{
		"    return <div\n",
		"        bg=\"#0b1020\"\n",
		"        width=\"200\"\n",
		"    >\n",
		"        <b>card</b>\n",
		"    </div>",
	} {
		if !strings.Contains(out, frag) {
			t.Fatalf("长属性逐行排版缺少 %q:\n%s", frag, out)
		}
	}
}

func TestFormatForLambdaWithNestedJSX(t *testing.T) {
	src := `package ui
func view() any {
    return <div><For each={todos}>{(t string) => <span>{t()}</span>}</For></div>
}
`
	out := assertFormatSemantics(t, "for-lambda", src)
	if !strings.Contains(out, `<For each={todos}>{(t string) => <span>{t()}</span>}</For>`) {
		t.Fatalf("For lambda 内嵌 JSX 格式不符:\n%s", out)
	}
}

func TestFormatTextWhitespace(t *testing.T) {
	src := `package ui
func view() any {
    return <div>
        Hello, {name}!
    </div>
}
`
	out := assertFormatSemantics(t, "text-ws", src)
	// 多行文本按 normalizeText 规则折叠首尾空白（GX 编译器本身如此），
	// 因此 "Hello, " 的尾空格在原语义里就不存在，不是格式化丢的。
	if !strings.Contains(out, `Hello,{name}!`) {
		t.Fatalf("文本空白折叠后丢内容:\n%s", out)
	}
	if strings.Contains(out, "        \n") {
		t.Fatalf("排版纯空白未清除:\n%s", out)
	}
}

func TestFormatConditionBranches(t *testing.T) {
	src := `package ui
func view() any {
    return <div>if ok { <b>yes</b> } else if other { <i>maybe</i> } else { <u>no</u> }</div>
}
`
	out := assertFormatSemantics(t, "cond", src)
	for _, frag := range []string{
		"        if ok {\n",
		"            <b>yes</b>\n",
		"        } else if other {\n",
		"        } else {\n",
	} {
		if !strings.Contains(out, frag) {
			t.Fatalf("条件分支排版缺少 %q:\n%s", frag, out)
		}
	}
}

func TestFormatStyleObjectIndent(t *testing.T) {
	src := `package ui
func view() any {
    return <div bg="#1e1e1e" padding="20" color="#fff"><span style={{
        background: rowBg(idx),
        fontSize: 12,
        "&:hover": { bg: "#eee" },
    }}>x</span></div>
}
`
	out := assertFormatSemantics(t, "style-indent", src)
	for _, frag := range []string{
		"        style={{\n",
		"            background: rowBg(idx),\n",
		"            fontSize: 12,\n",
		"            \"&:hover\": { bg: \"#eee\" },\n",
		"        }}",
	} {
		if !strings.Contains(out, frag) {
			t.Fatalf("style 对象缩进不符，缺少 %q:\n%s", frag, out)
		}
	}
}

func TestFormatRealPlaygroundFiles(t *testing.T) {
	for _, path := range []string{
		"../../examples/playground/ui/app.gx",
		"../../examples/playground/ui/editor.gx",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s: %v", path, err)
		}
		assertFormatSemantics(t, path, string(b))
	}
}

// TestFormatWideExprChildNoSpin 回归：当一个插值子节点自身格式化后就是多行
// （典型是宽 JSX）时，writeJSXNode 的文本/插值合并循环在 j==i 处不能原地
// i=j 打转。旧实现会反复解析同一表达式直到耗尽内存（表现为
// TestFormatRealPlaygroundFiles 在 editor.gx 上 OOM）。
func TestFormatWideExprChildNoSpin(t *testing.T) {
	src := `package ui
func view() any {
    return <div>{<span bg="#0b1020" padding="20" border-radius="12" flex-direction="column" gap="8" color="#ffffff" width="200">card</span>}tail</div>
}
`
	out := assertFormatSemantics(t, "wide-expr-child", src)
	if !strings.Contains(out, "card") || !strings.Contains(out, "tail") {
		t.Fatalf("输出丢失内容:\n%s", out)
	}
}
