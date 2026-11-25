module github.com/robertpanvip/gox-widget-showcase

go 1.27

require (
	github.com/robertpanvip/gox-skia v0.0.0
	github.com/robertpanvip/gox v0.0.0
	github.com/robertpanvip/gox-backend-skia v0.0.0
)

require github.com/ahati/yoga-go-binding v0.1.0 // indirect

replace github.com/robertpanvip/gox => ../..

replace github.com/robertpanvip/gox-backend-skia => ../../backend/skia

replace github.com/robertpanvip/gox-skia => ../../third_party/skia
