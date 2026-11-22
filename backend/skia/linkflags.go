// Package skia 提供基于真实 Skia（pekim/go-skia CGO 绑定）的 gxapp.Window 后端。
//
// 设计目标：完全自包含、依赖单向（本包 → host runtime，host 不依赖本包），
// 可整体切到独立仓库。接入点只有 gxapp.Window 接口，核心代码零改动。
package skia

// 上游 pekim/go-skia v0.1.0 的 api.go 里 LDFLAGS 顺序为 -lskia -lskshaper -lsvg
// -lskunicode_core -lskunicode_icu。静态库解析从左到右单向：libsvg.a 引用
// libskia.a 里的 SkParse::* 符号，但 -lskia 在 -lsvg 之前，链接时报
// "undefined reference to SkParse::FindScalar"。这是上游缺陷，与本后端无关。
//
// 用 --start-group/--end-group 把 Skia 静态库包成循环解析组即可修复，只影响
// 本模块最终链接，不改上游包、不改 host。仅 Linux 需要（macOS 二进制无此问题）。
// 指令必须紧邻 import "C"，故说明置于此。

// #cgo LDFLAGS: -Wl,--start-group -lskia -lskshaper -lsvg -lskunicode_core -lskunicode_icu -lskia -Wl,--end-group
import "C"
