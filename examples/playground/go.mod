module github.com/robertpanvip/gox-playground

go 1.27.2

require (
	github.com/robertpanvip/gox v0.0.0
	github.com/robertpanvip/gox-backend-android v0.0.0
	github.com/robertpanvip/gox-backend-rust v0.0.0
	github.com/robertpanvip/gox-backend-skia v0.0.0
	github.com/robertpanvip/gox-backend-web v0.0.0
	golang.org/x/mobile v0.0.0-20261117202452-47699113848a
)

require (
	github.com/ahati/yoga-go-binding v0.1.0 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/richardwilkes/canvas v0.3.2 // indirect
	github.com/robertpanvip/gox-skia v0.0.0 // indirect
	golang.org/x/exp/shiny v0.0.0-20261113113407-e3970619a1a4 // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
)

replace github.com/robertpanvip/gox => ../..

replace github.com/robertpanvip/gox-backend-rust => ../../backend/rust/go

replace github.com/robertpanvip/gox-backend-android => ../../backend/rust/android

replace github.com/robertpanvip/gox-backend-skia => ../../backend/skia

replace github.com/robertpanvip/gox-backend-web => ../../backend/web

replace github.com/robertpanvip/gox-skia => ../../third_party/skia

replace github.com/ahati/yoga-go-binding => /tmp/yoga-android-binding-amd64
