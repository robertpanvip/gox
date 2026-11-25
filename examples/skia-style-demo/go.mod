module github.com/robertpanvip/gox-skia-style-demo

go 1.27.2

require (
	github.com/go-gl/gl v0.0.0-20231021071112-07e5d0ea2e71
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20250301202403-da16c1255728
	github.com/robertpanvip/gox v0.0.0
	github.com/robertpanvip/gox-backend-skia v0.0.0
	github.com/robertpanvip/gox-skia v0.0.0
)

require (
	github.com/ahati/yoga-go-binding v0.1.0 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/richardwilkes/canvas v0.3.2 // indirect
	golang.org/x/image v0.46.0 // indirect
)

replace github.com/robertpanvip/gox => ../..

replace github.com/robertpanvip/gox-backend-skia => ../../backend/skia

replace github.com/robertpanvip/gox-skia => ../../third_party/skia
