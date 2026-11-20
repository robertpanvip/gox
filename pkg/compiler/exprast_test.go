package compiler

import (
	"fmt"
	"testing"
)

// signalASTOf 从源码构建原始流 + 绑定表 + SignalAST。
func signalASTOf(src string) ([]Token, *Bindings, *SignalAST) {
	toks := New(src).AllTokens()
	b := Bind(toks)
	return toks, b, ParseSignalExprs(toks, b)
}

// countKind 统计某类节点数量。
func countKind(a *SignalAST, k ExprKind) int {
	n := 0
	for _, e := range a.Nodes() {
		if e.Kind == k {
			n++
		}
	}
	return n
}

func TestExprASTSignalDeclNode(t *testing.T) {
	src := `package main
signal count = 0
signal total
func main() {}
`
	_, _, a := signalASTOf(src)
	if !a.HasSignal() {
		t.Fatal("应识别到 signal 声明")
	}
	if got := countKind(a, ExSignalDecl); got != 2 {
		t.Fatalf("signal 声明节点应为 2，got %d: %v", got, a.Nodes())
	}
	// 声明节点应持有 BkSignal 绑定，且带初值的记录 RHS 区间。
	var withInit *GXExpr
	for _, e := range a.Nodes() {
		if e.Kind != ExSignalDecl {
			continue
		}
		if e.Binding == nil || e.Binding.Kind != BkSignal {
			t.Errorf("声明节点 %v 应持有 BkSignal 绑定", e)
		}
		if e.Name == "count" {
			withInit = e
		}
	}
	if withInit == nil || withInit.RhsFrom < 0 {
		t.Fatal("count 声明应记录右值区间")
	}
	// 无初值声明的 RHS 为 -1。
	for _, e := range a.Nodes() {
		if e.Kind == ExSignalDecl && e.Name == "total" && e.RhsFrom != -1 {
			t.Errorf("无初值声明 total 的 RhsFrom 应为 -1，got %d", e.RhsFrom)
		}
	}
}

func TestExprASTReadWriteClassify(t *testing.T) {
	src := `package main
signal count = 0
func main() {
    println(count)   // read
    count = 5        // write
    count += 2       // compound
    count++          // incdec
}
`
	_, _, a := signalASTOf(src)
	checks := []struct {
		kind ExprKind
		want int
	}{
		{ExSignalDecl, 1},
		{ExSignalRead, 1}, // println(count) 里的 count
		{ExSignalWrite, 1},
		{ExSignalCompound, 1},
		{ExSignalIncDec, 1},
	}
	for _, c := range checks {
		if got := countKind(a, c.kind); got != c.want {
			t.Errorf("%s 节点应为 %d，got %d（全部节点：%v）", c.kind, c.want, got, a.Nodes())
		}
	}
	// 每个引用节点都应直接持有 signal 绑定。
	for _, e := range a.Nodes() {
		if e.Kind == ExSignalDecl {
			continue
		}
		if e.Binding == nil || e.Binding.Kind != BkSignal {
			t.Errorf("引用节点 %v 应持有 BkSignal 绑定", e)
		}
	}
}

func TestExprASTShadowing(t *testing.T) {
	// 参数遮蔽 signal 名：参数 count 不是 signal 引用，不应产生引用节点。
	src := `package main
signal count = 0
func f(count int) int { return count + 1 }
func main() {
    println(count)
    {
        count := 9
        println(count)
    }
}
`
	_, _, a := signalASTOf(src)
	// 引用节点只有 main 里 println(count) 一处；f 的参数与块内 := 都被遮蔽。
	if got := countKind(a, ExSignalRead); got != 1 {
		t.Fatalf("遮蔽后 signal 读应仅 1 处，got %d（全部节点：%v）", got, a.Nodes())
	}
}

// TestExprASTParityWithLegacy 对拍：AST 认定「哪些 offset 是 signal 引用」应与
// 旧判定（collectSignalVars + isSignalRefAt + 形态排除）完全一致。E1 切换的等价性
// 由 golden 全量兜底，这里先做节点级对拍。
func TestExprASTParityWithLegacy(t *testing.T) {
	src := `package main
signal count = 0
signal total = 0
func f(count int) int {
    total = count
    total += count
    count++
    return count
}
func main() {
    println(count, total)
    let doubled = Signal.computed(() => count * 2)
    println(doubled)
    obj := struct{ count int }{count: 1}
    _ = obj.count
}
`
	toks, b, a := signalASTOf(src)

	// 旧逻辑：sigs 集合 + 每处 isSignalRefAt + 形态排除，得出应改写的 offset 集合。
	sigs := collectSignalVars(toks)
	legacy := map[int]bool{}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Kind != Ident || !sigs[t.Text] {
			continue
		}
		if !isSignalRefAt(b, t.Text, t.Pos) {
			continue
		}
		if isSignalDeclTarget(toks, i) || inDeclZone(toks, i) {
			continue
		}
		if p := prevSig(toks, i-1); p >= 0 && toks[p].Text == "." {
			continue
		}
		if n := nextSig(toks, i+1); n >= 0 && (toks[n].Text == ":" || toks[n].Text == "(") {
			continue
		}
		legacy[t.Pos.Offset] = true
	}

	// AST 侧：所有引用类节点（排除声明节点）。
	astRefs := map[int]bool{}
	for _, e := range a.Nodes() {
		if e.Kind == ExSignalDecl {
			continue
		}
		astRefs[e.Pos.Offset] = true
	}

	if len(legacy) != len(astRefs) {
		t.Errorf("signal 引用数不一致：legacy=%d ast=%d\nlegacy=%v\nast=%v",
			len(legacy), len(astRefs), fmt.Sprint(legacy), fmt.Sprint(astRefs))
	}
	for off := range legacy {
		if !astRefs[off] {
			t.Errorf("legacy 认定 off=%d 是 signal 引用但 AST 未收录", off)
		}
	}
	for off := range astRefs {
		if !legacy[off] {
			t.Errorf("AST 认定 off=%d 是 signal 引用但 legacy 未认定", off)
		}
	}
	// 声明名集合也应对拍。
	if len(sigs) != len(a.SignalNames()) {
		t.Errorf("signal 名集合不一致：legacy=%v ast=%v", sigs, a.SignalNames())
	}
	for name := range sigs {
		if !a.SignalNames()[name] {
			t.Errorf("legacy signal 名 %q 未在 AST 中", name)
		}
	}
}

// templateASTOf 从源码构建原始流 + TemplateAST。
func templateASTOf(src string) ([]Token, *TemplateAST) {
	toks := New(src).AllTokens()
	return toks, ParseTemplateExprs(toks)
}

func TestExprASTTemplateNodes(t *testing.T) {
	src := "package main\nfunc main() {\n a := 1\n s := `a=${a} b=${a+1}`\n p := `plain`\n println(s, p)\n}\n"
	toks, ast := templateASTOf(src)
	nodes := ast.Nodes()
	// 只有含 ${ 的反引号串是 Template token；`plain` 是原生 raw string。
	if len(nodes) != 1 {
		t.Fatalf("模板节点应为 1（plain 是原生 raw string），got %d: %v", len(nodes), nodes)
	}
	n := nodes[0]
	if n.Kind != ExTemplate || n.TplBody == "" {
		t.Errorf("节点应为 ExTemplate 且持模板原文: %v", n)
	}
	// 节点 Pos.Offset 应命中原始流里对应 Template token。
	hit := false
	for _, tk := range toks {
		if tk.Kind == Template && tk.Pos.Offset == n.Pos.Offset {
			hit = true
		}
	}
	if !hit {
		t.Errorf("模板节点 offset=%d 未命中原始 Template token", n.Pos.Offset)
	}
}

// TestExprASTTemplateOffsetThroughPipeline 固化 E2 关键前提：desugar 到
// transformTemplates 之前，主流 Template token 的 Pos.Offset 仍能命中原始 AST。
// 覆盖 signal/lambda/对象字面量/解构混合（这些 transform 会增删 token 造成下标漂移）。
func TestExprASTTemplateOffsetThroughPipeline(t *testing.T) {
	sources := []string{
		"package main\nsignal n = 0\nfunc main() { s := `hi ${n}`; println(s) }\n",
		"package main\nfunc f(x int) int { return x }\nfunc main() { a := 1; s := `a=${a} f=${f(a)}`; println(s) }\n",
		"package main\ntype P struct{ name string }\nfunc main() { p := P{name: \"z\"}; { name, age := p.name, 1; s := `n=${name} a=${age}`; println(s) } }\n",
	}
	for si, src := range sources {
		toks := New(src).AllTokens()
		ast := ParseTemplateExprs(toks)
		b := Bind(toks)
		toks, defs := stripDefaults(toks)
		toks = transformSignals(toks, b)
		an := Analyze(toks)
		toks = expandDefaults(toks, defs)
		toks = transformLambdas(toks, an, b)
		var err error
		toks, err = transformObjectLits(toks, an, b)
		if err != nil {
			t.Fatalf("src%d objectlits: %v", si, err)
		}
		toks, err = expandDestructures(toks, b)
		if err != nil {
			t.Fatalf("src%d destruct: %v", si, err)
		}
		total, hit := 0, 0
		for _, tk := range toks {
			if tk.Kind != Template {
				continue
			}
			total++
			if ast.TemplateAt(tk.Pos.Offset) != nil {
				hit++
			}
		}
		if total == 0 || hit != total {
			t.Errorf("src%d: 主流模板 offset 命中 %d/%d", si, hit, total)
		}
	}
}
