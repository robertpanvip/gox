module github.com/robertpanvip/gox-backend-rust

go 1.27

require (
	github.com/robertpanvip/gox v0.0.0
	github.com/robertpanvip/gox-backend-skia v0.0.0
)

replace github.com/robertpanvip/gox => ../../..
replace github.com/robertpanvip/gox-backend-skia => ../../../backend/skia
