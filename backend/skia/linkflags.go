// Package skia 提供基于真实 Skia 的 gxapp.Window 后端。
//
// 设计目标：完全自包含、依赖单向（本包 → host runtime，host 不依赖本包），
// 可整体切到独立仓库。接入点只有 gxapp.Window 接口，核心代码零改动。
package skia

// 链接 flags 现在由 fork 绑定 github.com/robertpanvip/gox-skia 的 cgo 指令
// 提供（单一预编译库 libskia_<os>_<arch>.a + fontconfig/freetype/GL 等），
// 无需本包再注入 --start-group 链接组。
