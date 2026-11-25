module github.com/robertpanvip/gox-backend-skia

go 1.27

replace github.com/robertpanvip/gox => ../..

require (
	github.com/ahati/yoga-go-binding v0.1.0
	github.com/robertpanvip/gox v0.0.0-00010101000000-000000000000
	github.com/robertpanvip/gox-skia v0.0.0
)

replace github.com/robertpanvip/gox-skia => ../../third_party/skia
