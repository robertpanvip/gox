package compat

// GL 互操作类型。
//
// 迁移到纯 Go canvas 后，桌面窗口改为「CPU raster 绘制 → 上传纹理 → blit」架构：
// canvas 负责 CPU 光栅化，GL 纹理/blit 由窗口层（glwindow）用 go-gl 完成。
// 因此这里保留旧 API 的名字/形状让 backend/skia 与窗口代码兼容，但内部退化为
// CPU raster surface——不再需要 Skia GL context / backend render target。

type GrGLInterface struct{}

func GrGLMakeNativeInterface() GrGLInterface { return GrGLInterface{} }
func (i GrGLInterface) IsNil() bool          { return false }

type GrDirectContext struct{}

func GrDirectContextsMakeGLInterface(_ GrGLInterface) GrDirectContext { return GrDirectContext{} }
func (c GrDirectContext) IsNil() bool                                { return false }
func (c GrDirectContext) AsGrRecordingContext() GrRecordingContext   { return c }

// FlushAndSubmit 在 CPU raster 模式下为空操作（绘制直接落在像素缓冲里）。
func (c GrDirectContext) FlushAndSubmit(_ GrSyncCpu) {}

type GrRecordingContext = GrDirectContext

type GrSurfaceOrigin int32

const (
	GrSurfaceOriginTopLeft    GrSurfaceOrigin = 0
	GrSurfaceOriginBottomLeft GrSurfaceOrigin = 1
)

type GrSyncCpu bool

const (
	GrSyncCpuNo  GrSyncCpu = false
	GrSyncCpuYes GrSyncCpu = true
)

// GrGLFramebufferInfo 值类型，保留 setter 兼容旧调用窗口代码。
type GrGLFramebufferInfo struct {
	FBOID  uint32
	Format uint32
}

func (i *GrGLFramebufferInfo) SetFBOID(v uint32)  { i.FBOID = v }
func (i *GrGLFramebufferInfo) SetFormat(v uint32) { i.Format = v }

// GrBackendRenderTarget 在 CPU raster 模式下只保存尺寸信息。
type GrBackendRenderTarget struct {
	W, H           int32
	Samples, Stencil int32
	Info           GrGLFramebufferInfo
}

func GrBackendRenderTargetsMakeGL(w, h, samples, stencil int32, info GrGLFramebufferInfo) GrBackendRenderTarget {
	return GrBackendRenderTarget{W: w, H: h, Samples: samples, Stencil: stencil, Info: info}
}
func NewGrBackendRenderTargetCopy(rt GrBackendRenderTarget) GrBackendRenderTarget { return rt }
func (rt GrBackendRenderTarget) IsNil() bool                                     { return rt.W <= 0 || rt.H <= 0 }
func (rt GrBackendRenderTarget) Delete()                                         {}

// ColorSpace 占位。
type ColorSpace = int32

// Surface 在 CPU raster 模式下持有 raster canvas（值类型、可比较）。
type Surface struct {
	c Canvas
}

func (s Surface) IsNil() bool { return s.c.IsNil() }
func (s Surface) Unref()      { s.c.Delete() }
func (s Surface) GetCanvas() Canvas { return s.c }

// SurfacesWrapBackendRenderTarget 依据 backend 尺寸创建 CPU raster surface。
func SurfacesWrapBackendRenderTarget(
	_ GrRecordingContext,
	backend GrBackendRenderTarget,
	_ GrSurfaceOrigin,
	_ ColorType,
	_ ColorSpace,
	_ SurfaceProps,
) Surface {
	return Surface{c: NewCanvasWithDimensions(backend.W, backend.H, SurfaceProps{})}
}
