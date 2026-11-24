module github.com/robertpanvip/gox-backend-rust

go 1.27

require (
	github.com/robertpanvip/gox v0.0.0
)

replace github.com/robertpanvip/gox => ../../..

// Rust 后端只依赖 core（runtime/app、runtime/scene、widgets），不依赖任何其它
// 渲染后端。gox-backend-skia 曾被错误地引入以满足 demo/widgets 复用，但 widgets
// 属 core 公共组件层，Rust Adapter 无一行代码 import skia——移除该跨后端依赖，
// 使 gox-backend-rust 可独立成仓（评审 P1）。
