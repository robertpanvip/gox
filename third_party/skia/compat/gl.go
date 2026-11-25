package compat

import usk "github.com/robertpanvip/gox-skia"

// ---- GL 互操作类型（值类型包装，供真实 GL 窗口使用）----

type GrGLInterface struct{ p usk.GLInterface }
type GrDirectContext struct{ p usk.DirectContext }
type GrRecordingContext = usk.DirectContext
type GrBackendRenderTarget struct{ p usk.BackendRenderTarget }
type Surface struct{ p usk.Surface }
type ColorSpace = usk.ColorSpace

type GrSurfaceOrigin = usk.SurfaceOrigin

const (
	GrSurfaceOriginTopLeft    GrSurfaceOrigin = usk.SurfaceOriginTopLeft
	GrSurfaceOriginBottomLeft GrSurfaceOrigin = usk.SurfaceOriginBottomLeft
)

type GrSyncCpu bool

const (
	GrSyncCpuNo  GrSyncCpu = false
	GrSyncCpuYes GrSyncCpu = true
)

// GrGLFramebufferInfo 值类型，提供 SetFBOID/SetFormat setter。
type GrGLFramebufferInfo struct {
	FBOID  uint32
	Format uint32
}

func (i *GrGLFramebufferInfo) SetFBOID(v uint32)  { i.FBOID = v }
func (i *GrGLFramebufferInfo) SetFormat(v uint32) { i.Format = v }

func GrGLMakeNativeInterface() GrGLInterface {
	return GrGLInterface{usk.GLInterfaceCreateNativeInterface()}
}
func (i GrGLInterface) IsNil() bool { return i.p == nil }

func GrDirectContextsMakeGLInterface(intf GrGLInterface) GrDirectContext {
	return GrDirectContext{usk.ContextMakeGL(intf.p)}
}
func (c GrDirectContext) IsNil() bool { return c.p == nil }
func (c GrDirectContext) AsGrRecordingContext() GrRecordingContext { return c.p }
func (c GrDirectContext) FlushAndSubmit(sync GrSyncCpu) { usk.ContextFlushAndSubmit(c.p, bool(sync)) }

func GrBackendRenderTargetsMakeGL(w, h, samples, stencil int32, info GrGLFramebufferInfo) GrBackendRenderTarget {
	fi := &usk.GLFrameBufferInfo{Fboid: info.FBOID, Format: info.Format}
	return GrBackendRenderTarget{usk.BackendRenderTargetNewGL(int(w), int(h), int(samples), int(stencil), fi)}
}
func NewGrBackendRenderTargetCopy(rt GrBackendRenderTarget) GrBackendRenderTarget {
	return GrBackendRenderTarget{usk.BackendRenderTargetClone(rt.p)}
}
func (rt GrBackendRenderTarget) IsNil() bool { return rt.p == nil }
func (rt GrBackendRenderTarget) Delete() {
	if rt.p != nil {
		usk.BackendRenderTargetDelete(rt.p)
	}
}

func SurfacesWrapBackendRenderTarget(
	ctx GrRecordingContext,
	backend GrBackendRenderTarget,
	origin GrSurfaceOrigin,
	colorType ColorType,
	colorSpace ColorSpace,
	props SurfaceProps,
) Surface {
	return Surface{usk.SurfaceNewBackendRenderTarget(ctx, backend.p, origin, colorType, colorSpace, props.p)}
}
func (s Surface) IsNil() bool  { return s.p == nil }
func (s Surface) Unref()       { if s.p != nil { usk.SurfaceUnref(s.p) } }
func (s Surface) GetCanvas() Canvas {
	return Canvas{c: usk.SurfaceGetCanvas(s.p)}
}
