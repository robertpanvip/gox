module github.com/robertpanvip/gox-backend-android

go 1.27.2

require (
	github.com/robertpanvip/gox-backend-rust v0.0.0
	github.com/robertpanvip/gox-backend-skia v0.0.0
	github.com/robertpanvip/gox-core v0.0.0
	github.com/robertpanvip/gox-skia v0.0.0
	golang.org/x/mobile v0.0.0-20261117202452-47699113848a
)

require (
	github.com/ahati/yoga-go-binding v0.1.0 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/richardwilkes/canvas v0.3.2 // indirect
	golang.org/x/exp/shiny v0.0.0-20261113113407-e3970619a1a4 // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
)

replace github.com/robertpanvip/gox-core => ../../../../gox-core

replace github.com/robertpanvip/gox-backend-rust => ../go

replace github.com/robertpanvip/gox-backend-skia => ../../skia

replace github.com/robertpanvip/gox-skia => ../../../third_party/skia

// Android NDK 下的 yoga-go-binding 本地 stub（与 playground 同款）。
