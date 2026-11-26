// Package interp 是 GX 的轻量解释器：在运行时解释用户编辑的 GX 源码，
// 构建 gxscene 场景树。playground 的 Run 按钮用它真正执行编辑后的代码，
// 而不是跑预编译好的 demo。
//
// 复用的部分：pkg/compiler 的词法器（compiler.New(src).AllTokens()）。
// 自己实现的部分：表达式求值、语句执行、JSX 解析与场景树构建。
// 覆盖范围以 15 个 playground demo 用到的语言特性为准。
package interp

import (
	"fmt"
	"strconv"

	"github.com/robertpanvip/gox/runtime/signal"
)

// Value 是解释器的动态值。Go 没有运行时类型系统，这里用 any + 类型 switch。
type Value = any

// Cell 是环境里的一个变量槽。sig 声明用 signal.Signal 承载，裸读自动解引用、
// 赋值自动写回，从而让解释器里的表达式和编译产物语义一致。
type Cell struct {
	isSig bool
	sig   signal.Signal[Value]
	val   Value
}

func newCell(v Value) *Cell  { return &Cell{val: v} }
func newSigCell(v Value) *Cell {
	s := signal.New[Value](v)
	return &Cell{isSig: true, sig: s}
}

// Get 读变量：sig 走无参调用（订阅依赖），普通值直接返回。
func (c *Cell) Get() Value {
	if c == nil {
		return nil
	}
	if c.isSig {
		return c.sig()
	}
	return c.val
}

// Set 写变量：sig 走带参调用（触发响应式更新），普通值直接覆盖。
func (c *Cell) Set(v Value) {
	if c.isSig {
		c.sig(v)
		return
	}
	c.val = v
}

// Env 是链式作用域。函数每次调用创建子 Env，parent 指向闭包环境。
type Env struct {
	vars   map[string]*Cell
	funcs  map[string]*Function
	parent *Env
}

func newEnv(parent *Env) *Env {
	return &Env{vars: make(map[string]*Cell), funcs: make(map[string]*Function), parent: parent}
}

func (e *Env) lookup(name string) *Cell {
	for cur := e; cur != nil; cur = cur.parent {
		if c, ok := cur.vars[name]; ok {
			return c
		}
	}
	return nil
}

func (e *Env) define(name string, v Value, isSig bool) *Cell {
	c := &Cell{val: v}
	if isSig {
		c = newSigCell(v)
	}
	e.vars[name] = c
	return c
}

func (e *Env) assign(name string, v Value) bool {
	for cur := e; cur != nil; cur = cur.parent {
		if c, ok := cur.vars[name]; ok {
			c.Set(v)
			return true
		}
	}
	return false
}

func (e *Env) lookupFunc(name string) *Function {
	for cur := e; cur != nil; cur = cur.parent {
		if f, ok := cur.funcs[name]; ok {
			return f
		}
	}
	return nil
}

// toInt / toFloat / toStr / toBool 把动态值收敛到具体类型。
func toFloat(v Value) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case float64:
		return x
	case bool:
		if x { return 1 }
		return 0
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func toInt(v Value) int { return int(toFloat(v)) }

func toStr(v Value) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		if x == float64(int(x)) { return strconv.Itoa(int(x)) }
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}

func toBool(v Value) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	case nil:
		return false
	}
	return true
}

func toSlice(v Value) []Value {
	switch x := v.(type) {
	case []Value:
		return x
	case []string:
		out := make([]Value, len(x))
		for i, s := range x { out[i] = s }
		return out
	case []int:
		out := make([]Value, len(x))
		for i, n := range x { out[i] = n }
		return out
	case nil:
		return nil
	}
	return nil
}

// Function 是用户函数 / lambda 的统一可调用形态。
type Function struct {
	Params  []Param
	Body    []Stmt
	Closure *Env
	Native  func([]Value) Value // 内建函数（len/append/string 等）走这里
}

type Param struct {
	Name string
	Type string
	// SigField 表示这是 struct sig 字段构造出的信号引用（demo 14）。
	SigField bool
}
