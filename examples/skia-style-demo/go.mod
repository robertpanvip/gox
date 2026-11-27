module github.com/robertpanvip/gox-skia-style-demo

go 1.27.2

require (
	github.com/robertpanvip/gox-core v0.0.0
	github.com/robertpanvip/gox-backend-rust v0.0.0
)

require (
	github.com/ahati/yoga-go-binding v0.1.0 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/richardwilkes/canvas v0.3.2 // indirect
	golang.org/x/image v0.45.0 // indirect
)

replace github.com/robertpanvip/gox-core => ../../../gox-core

replace github.com/robertpanvip/gox-backend-rust => ../../../gox-backend/rust/go
