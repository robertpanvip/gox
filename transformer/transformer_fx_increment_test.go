package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_FxPostIncrement(t *testing.T) {
	src := `import "github.com/gox-lang/gox/gui"

fx func Counter() {
    let count = 0
    
    return <button text="Increment" onClick={func() {
        count++
    }} />
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// 检查生成的代码是否包含 count++
	if !strings.Contains(result, "count++") {
		t.Error("expected 'count++' in output, got:", result)
	}
	
	// 确保没有错误的格式（如 count++ 被错误转换）
	if strings.Contains(result, "count++++") {
		t.Error("incorrect transformation, got 'count++++':", result)
	}
}

func TestTransformer_FxPostDecrement(t *testing.T) {
	src := `import "github.com/gox-lang/gox/gui"

fx func Counter() {
    let count = 0
    
    return <button text="Decrement" onClick={func() {
        count--
    }} />
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// 检查生成的代码是否包含 count--
	if !strings.Contains(result, "count--") {
		t.Error("expected 'count--' in output, got:", result)
	}
	
	// 确保没有错误的格式
	if strings.Contains(result, "count----") {
		t.Error("incorrect transformation, got 'count----':", result)
	}
}

func TestTransformer_FxPostIncrementAndDecrement(t *testing.T) {
	src := `import "github.com/gox-lang/gox/gui"

fx func Counter() {
    let count = 0
    
    return <div>
        <button text="Increment" onClick={func() {
            count++
        }} />
        <button text="Decrement" onClick={func() {
            count--
        }} />
    </div>
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// 检查生成的代码是否同时包含 count++ 和 count--
	if !strings.Contains(result, "count++") {
		t.Error("expected 'count++' in output, got:", result)
	}
	if !strings.Contains(result, "count--") {
		t.Error("expected 'count--' in output, got:", result)
	}
	
	// 验证生成的代码中状态变量有正确的前缀
	if !strings.Contains(result, "c.Count++") {
		t.Error("expected 'c.Count++' (with state prefix) in output, got:", result)
	}
	if !strings.Contains(result, "c.Count--") {
		t.Error("expected 'c.Count--' (with state prefix) in output, got:", result)
	}
}
