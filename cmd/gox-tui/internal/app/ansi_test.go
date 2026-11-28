// ANSI 清洗辅助：跨平台共享（e2e_pty_test.go 有 //go:build !windows，
// ansiRe 不能定义在那儿，否则 Windows 下其他测试文件引用会 build failed）。
package app

import "regexp"

var ansiRe = regexp.MustCompile(`\x1b(?:\[[0-9;?<>=]*[a-zA-Z]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
