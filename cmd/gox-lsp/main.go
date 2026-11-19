// gox-lsp 是独立的 GX 语言服务器入口（JSON-RPC over stdio）。
package main

import (
	"fmt"
	"os"

	"github.com/robertpanvip/gox/internal/buildcheck"
	"github.com/robertpanvip/gox/internal/lsp"
)

func main() {
	checker := buildcheck.New(128)
	// 生成的 Go 声明 go 1.27；显式固定 toolchain，避免默认 go 联网下载卡住。
	// 用户可用环境变量覆盖时，仍以这里为准（LSP 诊断需稳定可复现）。
	checker.SetToolchain("go", "GOTOOLCHAIN=go1.27.3")
	srv := lsp.NewServer(os.Stdin, os.Stdout, checker)
	if err := srv.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gox-lsp:", err)
		os.Exit(1)
	}
}
