// Command gox-stdlib-gen 为 internal/typecheck/embedstd 生成 embed 源码包。
// 在仓库根目录运行：go run ./cmd/gox-stdlib-gen
//
// 闭包来源：go list -deps ./runtime/... ./widgets/... ./pkg/sourcemap。
// 过滤复用 typecheck.DiskLookup（与类型检查同一套 GOOS/GOARCH/build-tag/cgo 规则，
// 不存在两份名单漂移）。输出带 VERSION 锁：与编译工具链不一致时 embedstd 测试报错。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/robertpanvip/gox/internal/typecheck"
)

func main() {
	out := flag.String("out", "internal/typecheck/embedstd/_srcs", "输出目录")
	goos := flag.String("os", "js", "目标 GOOS")
	goarch := flag.String("arch", "wasm", "目标 GOARCH")
	flag.Parse()

	goroot := runtime.GOROOT()
	if goroot == "" {
		b, _ := exec.Command("go", "env", "GOROOT").Output()
		goroot = strings.TrimSpace(string(b))
	}
	listOut, err := exec.Command("go", "list", "-deps", "./runtime/...", "./widgets/...", "./pkg/sourcemap").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "go list -deps 失败:", err)
		os.Exit(1)
	}
	stdLK := typecheck.DiskLookup(filepath.Join(goroot, "src"), *goos, *goarch)
	goxLK := typecheck.DiskLookup(".", *goos, *goarch)

	const modp = "github.com/robertpanvip/gox/"
	total, pkgs, skipped := 0, 0, []string{}
	for _, line := range strings.Fields(string(listOut)) {
		if line == "unsafe" || strings.HasPrefix(line, "vendor/") {
			continue
		}
		var files []typecheck.File
		var rel string
		if r, ok := strings.CutPrefix(line, modp); ok {
			rel = filepath.Join(*out, "gox", filepath.FromSlash(r))
			var ok2 bool
			files, ok2 = goxLK(r)
			if !ok2 {
				skipped = append(skipped, line)
				continue
			}
		} else if strings.Contains(line, ".") {
			skipped = append(skipped, line) // 第三方：playground 世界不存在，防御性跳过
			continue
		} else {
			rel = filepath.Join(*out, "std", filepath.FromSlash(line))
			var ok2 bool
			files, ok2 = stdLK(line)
			if !ok2 {
				skipped = append(skipped, line)
				continue
			}
		}
		if err := os.MkdirAll(rel, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, f := range files {
			b := []byte(f.Src)
			if err := os.WriteFile(filepath.Join(rel, f.Name), b, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			total += len(b)
		}
		pkgs++
	}
	if err := os.WriteFile(filepath.Join(*out, "VERSION"), []byte(runtime.Version()+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("生成完成: %d 包, %.1f MB 源码 (go %s, %s/%s)\n", pkgs, float64(total)/1048576, runtime.Version(), *goos, *goarch)
	if len(skipped) > 0 {
		fmt.Printf("跳过 %d 个: %s\n", len(skipped), strings.Join(skipped[:min(8, len(skipped))], " "))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
