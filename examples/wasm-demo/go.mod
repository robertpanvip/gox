module github.com/robertpanvip/gox-wasm-demo

go 1.27

require (
	github.com/robertpanvip/gox-core v0.0.0
	github.com/robertpanvip/gox-backend-web v0.0.0
)

replace github.com/robertpanvip/gox-core => ../../../gox-core

replace github.com/robertpanvip/gox-backend-web => ../../backend/web
