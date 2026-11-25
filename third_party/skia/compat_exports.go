// Copyright (c) 2026 GoX contributors. All rights reserved.
//
// 本文件为 GoX fork 新增的最小桥接导出，补充上游 unison skia 绑定未暴露、
// 但 GoX backend 需要的少量构造能力。作为新增文件，不影响上游 MPL-2.0 文件。
package skia

/*
#include "sk_capi.h"
*/
import "C"

// SamplingOptionsNew 创建一个零值采样选项（nearest 过滤）。
// 上游绑定把 SamplingOptions 定义为 *C.sk_sampling_options_t 但未暴露构造函数，
// CanvasDrawImageRect 需要一个非 nil 值，故在此补充。
func SamplingOptionsNew() SamplingOptions {
	return new(C.sk_sampling_options_t)
}

// BackendRenderTargetClone 返回一个共享底层对象的新 handle。
// 旧 go-skia 的 NewGrBackendRenderTargetCopy 是复制构造；unison 绑定无此 API，
// GoX 的 GL 窗口用它生成第二个 handle（仅用于生命周期管理），这里按引用共享。
func BackendRenderTargetClone(rt BackendRenderTarget) BackendRenderTarget {
	return rt
}
