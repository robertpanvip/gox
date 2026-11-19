package compiler

import "testing"

func TestFuncSigParamsUnion(t *testing.T) {
	src := `package main
func f(a int | string, b bool) {}
`
	a := Analyze(New(src).AllTokens())
	sig, ok := a.FuncSig("f")
	if !ok {
		t.Fatal("应收集到 f 的签名")
	}
	if len(sig.Params) != 2 {
		t.Fatalf("参数数 = %d, want 2", len(sig.Params))
	}
	if !sig.Params[0].IsUnion() || sig.Params[0].String() != "int|string" {
		t.Errorf("参数 a 应为 int|string 联合, got %s", sig.Params[0].String())
	}
	if sig.Params[1].String() != "bool" {
		t.Errorf("参数 b 应为 bool, got %s", sig.Params[1].String())
	}
}

func TestFuncSigResultsUnion(t *testing.T) {
	src := `package main
func f() int | string { return 1 }
`
	a := Analyze(New(src).AllTokens())
	sig, ok := a.FuncSig("f")
	if !ok {
		t.Fatal("应收集到 f 的签名")
	}
	if len(sig.Results) != 1 || !sig.Results[0].IsUnion() || sig.Results[0].String() != "int|string" {
		t.Errorf("返回值应为 int|string 联合, got %v", sig.Results)
	}
}

func TestFuncSigNamedResults(t *testing.T) {
	src := `package main
func f() (x int | string, err error) { return }
`
	a := Analyze(New(src).AllTokens())
	sig, ok := a.FuncSig("f")
	if !ok {
		t.Fatal("应收集到 f 的签名")
	}
	if len(sig.Results) != 2 {
		t.Fatalf("返回值数 = %d, want 2: %v", len(sig.Results), sig.Results)
	}
	if !sig.Results[0].IsUnion() || sig.Results[0].String() != "int|string" {
		t.Errorf("命名返回 x 应为 int|string, got %s", sig.Results[0].String())
	}
}

func TestFuncSigMethod(t *testing.T) {
	src := `package main
type T struct{}
func (r T) f(v int | string) {}
`
	a := Analyze(New(src).AllTokens())
	sig, ok := a.FuncSig("f")
	if !ok {
		t.Fatal("应收集到方法 f 的签名")
	}
	if len(sig.Params) != 1 || !sig.Params[0].IsUnion() {
		t.Errorf("方法参数应为联合, got %v", sig.Params)
	}
}

func TestFuncSigAtGx(t *testing.T) {
	src := `package main
func f() int | string {
    return 1
}
func g() {
    return
}
`
	a := Analyze(New(src).AllTokens())
	// 第 3 行 return 应定位到 f
	sig, ok := a.FuncSigAtGx(3, 5)
	if !ok || sig.Name != "f" {
		t.Errorf("GX 3:5 应定位到 f, got %q ok=%v", sig.Name, ok)
	}
	// 第 6 行 return 应定位到 g
	sig, ok = a.FuncSigAtGx(6, 5)
	if !ok || sig.Name != "g" {
		t.Errorf("GX 6:5 应定位到 g, got %q ok=%v", sig.Name, ok)
	}
}
