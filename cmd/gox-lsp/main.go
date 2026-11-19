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
	// 尊重外部注入的 GOTOOLCHAIN（如 go1.27.3）；默认工具链为 go。
	checker.SetToolchain("go")
	srv := lsp.NewServer(os.Stdin, os.Stdout, checker)
	if err := srv.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gox-lsp:", err)
		os.Exit(1)
	}
}
