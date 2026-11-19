// Package main 提供 GX 转译器的 C-ABI 共享库，供 IDEA 插件经 JNA 调用。
//
// 构建：go build -buildmode=c-shared -o libgox.so ./bindings/cabi
// 约定：所有返回的字符串由 Go 的 C.CString 分配，调用方必须用 gx_free 释放。
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"unsafe"

	"github.com/robertpanvip/gox/pkg/compiler"
	"github.com/robertpanvip/gox/pkg/sourcemap"
)

//export gx_version
func gx_version() *C.char {
	return C.CString("gox-cabi 0.1.0")
}

type translateResult struct {
	GoSource  string `json:"goSource"`
	SourceMap string `json:"sourceMap"`
	Error     string `json:"error,omitempty"`
}

//export gx_translate
func gx_translate(source *C.char) *C.char {
	src := C.GoString(source)
	res, err := compiler.Compile("input.gx", src)
	out := translateResult{}
	if err != nil {
		out.Error = err.Error()
	} else {
		out.GoSource = res.GoSource
		if sm, e := json.Marshal(res.SourceMap); e == nil {
			out.SourceMap = string(sm)
		}
	}
	data, _ := json.Marshal(out)
	return C.CString(string(data))
}

//export gx_validate
func gx_validate(goSource *C.char) *C.char {
	src := C.GoString(goSource)
	res := &compiler.Result{File: "check.go", GoSource: src}
	err := res.Validate()
	out := map[string]any{"valid": err == nil}
	if err != nil {
		out["error"] = err.Error()
	}
	data, _ := json.Marshal(out)
	return C.CString(string(data))
}

//export gx_map_lookup
func gx_map_lookup(mapJSON *C.char, line C.int, col C.int) *C.char {
	var sm sourcemap.SourceMap
	out := map[string]any{}
	if err := json.Unmarshal([]byte(C.GoString(mapJSON)), &sm); err != nil {
		out["error"] = err.Error()
	} else if pos, ok := sm.GoToGx(int(line), int(col)); ok {
		out["line"] = pos.Line
		out["col"] = pos.Column
		out["ok"] = true
	} else {
		out["ok"] = false
	}
	data, _ := json.Marshal(out)
	return C.CString(string(data))
}

//export gx_free
func gx_free(p unsafe.Pointer) {
	C.free(p)
}

func main() {}
