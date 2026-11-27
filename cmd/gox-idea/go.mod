module github.com/robertpanvip/gox-idea

go 1.27.2

require (
	github.com/robertpanvip/gox-core v0.0.0
	github.com/robertpanvip/gox-backend-rust v0.0.0
)

replace github.com/robertpanvip/gox-core => ../../../gox-core

replace github.com/robertpanvip/gox-backend-rust => ../../backend/rust/go

replace github.com/robertpanvip/gox-backend-web => ../../backend/web

// yoga-go-binding 默认用上游自带的 libyogacore.a（Linux ELF），Linux/macOS 构建
// 直接可用。Windows 交叉编译需用 COFF 版静态库，通过 scripts/build-windows-msys2.sh
// 在本地模块副本里临时加 replace（见 docs/windows-build.md），不要在本文件写死
// 机器相关的绝对路径，否则 Linux/CI 连 go test ./... 都无法 setup。
