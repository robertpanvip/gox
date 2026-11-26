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

// 上游 yoga-go-binding v0.1.0 自带的 libyogacore.a 是 Linux ELF 格式，
// Windows 链接器不识别；此处替换为本地拷贝，lib 内是 mingw 编译的 COFF 静态库
// （源码：facebook/yoga v3.2.1，头文件与上游 binding 完全一致）。
replace github.com/ahati/yoga-go-binding => E:/AI-workspace/tools/yoga-go-binding-win
