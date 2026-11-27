module github.com/robertpanvip/gox-fpsbench

go 1.27.2

require (
	github.com/robertpanvip/gox v0.0.0
	github.com/robertpanvip/gox-backend-rust v0.0.0
)

replace github.com/robertpanvip/gox => ../..

replace github.com/robertpanvip/gox-backend-rust => ../../backend/rust/go
