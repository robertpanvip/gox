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
