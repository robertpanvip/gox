package compiler

import (
	"strconv"
	"strings"
)

// ImportManager 统一管理运行时 import 的别名，解决各 transform 各自注入 import、
// 并硬编码占位包名（list / gxvdom / strconv）导致的别名不一致问题。
//
// 典型故障：源码写 import mylist ".../runtime/list"，而集合提升生成 list.List[T]，
// 结果是 undefined: list + mylist imported and not used。
//
// 方案：
//  1. 扫描源码已有的 import，建立 path→alias 与已占用别名集合；
//  2. 对每个运行时依赖，若路径已导入就复用其别名，否则分配一个无冲突别名并注入；
//  3. 把「生成 token」里作为包名使用的占位别名替换为实际别名——只改 IsGen 的
//     token，绝不碰用户源码里同名的变量/标识符。
type ImportManager struct {
	toks      []Token
	pathAlias map[string]string // import 路径 → 实际别名
	used          map[string]bool   // 已被占用的别名
	module        string
	pendingInject []runtimeImport // 需要新增的 import
}

// runtimeDep 描述一个运行时依赖：生成代码使用的占位别名与 import 路径。
type runtimeDep struct {
	placeholder string
	path        string
}

// newImportManager 扫描 toks 中已有的 import 声明。
func newImportManager(toks []Token, module string) *ImportManager {
	m := &ImportManager{
		toks:      toks,
		pathAlias: map[string]string{},
		used:      map[string]bool{},
		module:    module,
	}
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("import") {
			continue
		}
		// 单行 import：import [alias] "path"
		// 块 import：import ( [alias] "path" ... )
		j := nextSig(toks, i+1)
		if j >= 0 && toks[j].Text == "(" {
			close := matchDelimSkipSpace(toks, j, "(", ")")
			for k := j + 1; k < close && k < len(toks); {
				k = m.recordImport(toks, k, close)
			}
			i = close
		} else {
			m.recordImport(toks, i+1, len(toks))
		}
	}
	return m
}

// recordImport 在 [start,limit) 内识别一条 [alias] "path"，登记并返回下一条的起点。
func (m *ImportManager) recordImport(toks []Token, start, limit int) int {
	k := start
	// 跳过空白/换行/注释
	for k < limit && (toks[k].Kind == Space || toks[k].Kind == Newline || toks[k].Kind == Comment) {
		k++
	}
	if k >= limit {
		return limit
	}
	alias := ""
	if toks[k].Kind == Ident {
		alias = toks[k].Text
		k++
	}
	// 找字符串路径
	for k < limit {
		if toks[k].Kind == String {
			path, err := strconv.Unquote(toks[k].Text)
			if err != nil {
				path = strings.Trim(toks[k].Text, "\"`")
			}
			if alias == "" {
				alias = pkgNameFromPath(toks[k].Text)
			}
			m.pathAlias[path] = alias
			m.used[alias] = true
			return k + 1
		}
		if toks[k].Kind == Newline || toks[k].Text == ";" {
			return k + 1
		}
		k++
	}
	return k
}

// placeholderUsed 判断占位包名是否在 token 流里被真正用作 selector 前缀
// （如 list.List / gxvdom.CreateElement / strconv.Itoa）。只认生成 token，
// 避免把用户源码里的同名变量误当成运行时依赖。
func placeholderUsed(toks []Token, placeholder string) bool {
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if !t.IsGen || t.Kind != Ident || t.Text != placeholder {
			continue
		}
		j := nextSig(toks, i+1)
		if j >= 0 && toks[j].Text == "." {
			return true
		}
	}
	return false
}

// resolve 为一个运行时依赖确定实际别名：
//   - 生成代码未使用该占位名 → 返回 ""（无需注入/替换）
//   - 源码已导入该路径 → 复用其别名
//   - 否则分配无冲突别名并登记待注入
func (m *ImportManager) resolve(dep runtimeDep, used bool) string {
	if !used {
		return ""
	}
	if alias, ok := m.pathAlias[dep.path]; ok {
		return alias // 源码已导入（可能带别名），直接复用
	}
	alias := dep.placeholder
	if m.used[alias] {
		// 占位名被占用，分配一个唯一别名
		for n := 2; ; n++ {
			cand := dep.placeholder + strconv.Itoa(n)
			if !m.used[cand] {
				alias = cand
				break
			}
		}
	}
	m.used[alias] = true
	m.pathAlias[dep.path] = alias
	m.pendingInject = append(m.pendingInject, runtimeImport{alias: alias, path: dep.path})
	return alias
}

type runtimeImport struct{ alias, path string }

// Reconcile 统一处理全部运行时依赖，返回替换别名并注入新 import 后的 token 流。
// 调用时机：所有生成阶段之后、collectScope 之前（确保 scope 看到最终别名）。
func ReconcileImports(in []Token, module string) []Token {
	m := newImportManager(in, module)
	deps := []runtimeDep{
		{placeholder: "list", path: listImportPath(module)},
		{placeholder: "gxvdom", path: "github.com/robertpanvip/gox/runtime/vdom"},
		{placeholder: "gxscene", path: sceneImportPath(module)},
		{placeholder: "strconv", path: "strconv"},
		{placeholder: "fmt", path: "fmt"},
	}
	renames := map[string]string{}
	for _, d := range deps {
		actual := m.resolve(d, placeholderUsed(in, d.placeholder))
		if actual != "" && actual != d.placeholder {
			renames[d.placeholder] = actual
		}
	}

	out := append([]Token(nil), in...)
	// 只替换生成 token 中作为包名（后跟 '.'）使用的占位别名。
	for i := range out {
		t := &out[i]
		if !t.IsGen || t.Kind != Ident {
			continue
		}
		actual, ok := renames[t.Text]
		if !ok {
			continue
		}
		j := nextSig(out, i+1)
		if j >= 0 && out[j].Text == "." {
			t.Text = actual
		}
	}

	// 注入待新增的 import（源码未导入的运行时依赖）
	if len(m.pendingInject) > 0 {
		out = injectImportSpecs(out, m.pendingInject)
	}
	return out
}

// injectImportSpecs 在 package 子句之后插入若干 import 行。
func injectImportSpecs(toks []Token, specs []runtimeImport) []Token {
	insert := 0
	for i, t := range toks {
		if t.is("package") {
			for j := i + 1; j < len(toks); j++ {
				if toks[j].Kind == Newline {
					insert = j + 1
					break
				}
			}
			break
		}
	}
	var imp []Token
	p := toks[0].Pos
	// package 子句后补一个空行，生成的 import 自成一块（符合既有输出格式）
	imp = append(imp, tokNewline(p))
	for _, s := range specs {
		imp = append(imp, tokIdent("import", p), tokSpace(" ", p))
		// 别名与路径默认包名相同时省略显式别名（符合 Go 习惯，兼容既有 golden）
		defaultName := pkgNameFromPath(strconv.Quote(s.path))
		if s.alias != defaultName {
			imp = append(imp, tokIdent(s.alias, p), tokSpace(" ", p))
		}
		imp = append(imp, tokString(strconv.Quote(s.path), p), tokNewline(p))
	}
	out := append([]Token{}, toks[:insert]...)
	out = append(out, imp...)
	out = append(out, toks[insert:]...)
	return out
}
