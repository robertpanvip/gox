module github.com/robertpanvip/gox-idea

go 1.27

require (
	github.com/ahati/yoga-go-binding v0.1.0
	github.com/go-gl/gl v0.0.0-20231021071112-07e5d0ea2e71
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20250301202403-da16c1255728
	github.com/robertpanvip/gox-skia v0.0.0
	github.com/robertpanvip/gox v0.0.0
	github.com/robertpanvip/gox-backend-skia v0.0.0
	github.com/robertpanvip/gox-backend-web v0.0.0
)

replace github.com/robertpanvip/gox => ../..

replace github.com/robertpanvip/gox-backend-skia => ../../backend/skia

replace github.com/robertpanvip/gox-backend-web => ../../backend/web

replace github.com/robertpanvip/gox-skia => ../../third_party/skia

// yoga-go-binding 默认用上游自带的 libyogacore.a（Linux ELF），Linux/macOS 构建
// 直接可用。Windows 交叉编译需用 COFF 版静态库，通过 scripts/build-windows-msys2.sh
// 在本地模块副本里临时加 replace（见 docs/windows-build.md），不要在本文件写死
// 机器相关的绝对路径，否则 Linux/CI 连 go test ./... 都无法 setup。
