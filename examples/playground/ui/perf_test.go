package ui

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

const benchLine = `    return <button onClick={gxscene.Handler(func(ctx gxscene.EventContext) { inc() })}>{count()}</button>`

// BenchmarkHighlightLine 度量单行分词开销。
func BenchmarkHighlightLine(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		toks := Highlight(benchLine)
		if len(toks) == 0 {
			b.Fatal("空 token 列表")
		}
	}
}

// BenchmarkRichTextHTML 度量富文本序列化开销；span 数直接决定 innerHTML 长度与浏览器解析成本。
func BenchmarkRichTextHTML(b *testing.B) {
	rt := gxscene.RichText{}
	for _, t := range Highlight(benchLine) {
		rt = append(rt, gxscene.RichSpan{Text: t.Text, Color: t.Color})
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if len(rt.HTML()) == 0 {
			b.Fatal("空 HTML")
		}
	}
}

// BenchmarkSetSourceOneChar 度量一次按键路径：整篇源码 diff + token 缓存刷新 + 行版本写。
func BenchmarkSetSourceOneChar(b *testing.B) {
	a := "line0\nsig count = 0\nline2\nline3"
	c := "line0\nsig count = 01\nline2\nline3"
	EditorSetText(a)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			setSource(c)
		} else {
			setSource(a)
		}
	}
}
