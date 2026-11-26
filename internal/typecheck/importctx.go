package typecheck

// import 路径补全上下文判定（纯语法，不需要编译成功——正在敲 import 的源码
// 往往本就无法编译）。line 0 基，col 1 基指向最后输入字符。

import (
	"regexp"
	"strings"
)

var (
	reImportLine = regexp.MustCompile(`^\s*import\s+(?:[\w.]+\s+)?"([^"]*)$`)
	reImportItem = regexp.MustCompile(`^\s+(?:[\w.]+\s+)?"([^"]*)$`)
)

// ImportContext 判定光标是否落在 import 路径字符串内（单行式或括号块内），
// 是则返回已输入前缀。
func ImportContext(src string, line, col int) (string, bool) {
	ls := strings.Split(src, "\n")
	if line < 0 || line >= len(ls) || col <= 0 || col > len(ls[line]) {
		return "", false
	}
	seg := ls[line][:col]
	if m := reImportLine.FindStringSubmatch(seg); m != nil {
		return m[1], true
	}
	if m := reImportItem.FindStringSubmatch(seg); m != nil && inImportBlock(ls, line) {
		return m[1], true
	}
	return "", false
}

// inImportBlock 向上找最近一个非空行归属：`import (` 之内、遇到 ')' 或其他
// 代码行即终止。
func inImportBlock(ls []string, line int) bool {
	for i := line - 1; i >= 0; i-- {
		t := strings.TrimSpace(ls[i])
		switch {
		case t == "" || strings.HasPrefix(t, "\""):
			continue
		case strings.HasSuffix(t, "(") && strings.HasPrefix(t, "import"):
			return true
		default:
			return false
		}
	}
	return false
}
