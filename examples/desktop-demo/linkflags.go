package main

// #cgo linux LDFLAGS: -L${SRCDIR}/../../.demolibs
// #cgo linux LDFLAGS: -lX11 -lXrandr -lXxf86vm -lXi -lXcursor -lXinerama -lGL -lfontconfig -lfreetype -lpthread -ldl -lm
import "C"

// 上面的 cgo 指令提供 GLFW 所需的 X11 系列开发库（沙箱里只有运行时 .so.1，
// 仓库 .demolibs 提供对应 .so 开发软链）。Skia 静态库的链接 flags 由 fork 绑定
// github.com/robertpanvip/gox-skia 的 cgo 指令提供。这些是构建期链接配置，
// 与 host 协议和业务逻辑无关。
