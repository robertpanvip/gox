package main

// #cgo linux LDFLAGS: -Wl,--start-group -lskia -lskshaper -lsvg -lskunicode_core -lskunicode_icu -lskia -Wl,--end-group
// #cgo linux LDFLAGS: -L${SRCDIR}/../../../.demolibs
// #cgo linux LDFLAGS: -lX11 -lXrandr -lXxf86vm -lXi -lXcursor -lXinerama -lGL -lfontconfig -lfreetype -lpthread -ldl -lm
import "C"

// 上面的 cgo 指令修复两件事：
//  1. 上游 go-skia v0.1.0 的 libsvg.a 引用 libskia.a 里的 SkParse::*，GNU ld 需要 --start-group 循环解析；
//  2. GLFW 需要 X11 系列开发库，沙箱里只有运行时 .so.1，仓库 .demolibs 提供对应 .so 开发软链。
// 这些是构建期链接配置，与 host 协议和业务逻辑无关。
