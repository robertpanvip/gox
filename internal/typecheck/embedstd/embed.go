// Package embedstd 提供 js/wasm 视图的 std + gox 闭包 embed 虚拟文件系统。
//
// 数据由 `go run ./cmd/gox-stdlib-gen` 在构建期生成到 srcs/（.gitignore，仓库零增长）：
// srcs/std/** 来自同一 GOROOT 的 std 闭包源码，srcs/gox/** 来自本仓库 runtime/widgets/
// pkg 源码，srcs/VERSION 锁版本——生成与编译必须同工具链，不一致时测试直接报错。
// 未生成数据时 Lookup 返回 nil，goxlsp 类型层整体缺席（零行为变化的安全阀）。
package embedstd

import (
	"embed"
	"io/fs"
	"strings"

	"github.com/robertpanvip/gox/internal/typecheck"
)

//go:embed all:_srcs
var srcs embed.FS

// Version 返回生成数据的 Go 版本；空串表示尚未生成。
func Version() string {
	b, err := fs.ReadFile(srcs, "_srcs/VERSION")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Lookup 返回 js/wasm 视图的虚拟文件系统入口；无数据时返回 nil。
func Lookup() typecheck.Lookup {
	if Version() == "" {
		return nil
	}
	dirFor := func(p string) string {
		if r, ok := strings.CutPrefix(p, "github.com/robertpanvip/gox/"); ok {
			return "_srcs/gox/" + r
		}
		return "_srcs/std/" + p
	}
	return typecheck.FSRoot(srcs, dirFor, "js", "wasm")
}
