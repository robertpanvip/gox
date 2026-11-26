package main

// GLES 2.0 版的 CPU raster 显示端：像素缓冲上传为纹理，全屏四边形 blit 到
// EGL 默认 framebuffer。与桌面端 blit.go（GL 3.3 core，gl_VertexID 无 VBO
// 技巧）语义相同，差异仅 ES 2.0 的 shader 语法、显式 VBO，以及 x/mobile 的
// gl.Context 调用形态（GL 函数全部走 context 实例）。

import (
	"fmt"
	"unsafe"

	"golang.org/x/mobile/gl"
)

type esBlit struct {
	ctx     gl.Context
	program gl.Program
	vbo     gl.Buffer
	tex     gl.Texture
	texW    int
	texH    int
}

const blitVertES = `attribute vec2 aPos;
varying vec2 vUV;
void main() {
    vUV = aPos * 0.5 + 0.5;
    gl_Position = vec4(aPos, 0.0, 1.0);
}
`

const blitFragES = `precision mediump float;
varying vec2 vUV;
uniform sampler2D uTex;
void main() {
    // CPU raster 左上角原点，GL 采样需翻转 y。
    gl_FragColor = texture2D(uTex, vec2(vUV.x, 1.0 - vUV.y));
}
`

// 全屏四边形（两个三角形，NDC 坐标）。
var quadVerts = []float32{
	-1, -1, 1, -1, -1, 1,
	-1, 1, 1, -1, 1, 1,
}

func compileShaderES(ctx gl.Context, kind gl.Enum, src string) (gl.Shader, error) {
	s := ctx.CreateShader(kind)
	ctx.ShaderSource(s, src)
	ctx.CompileShader(s)
	if log := ctx.GetShaderInfoLog(s); log != "" {
		ctx.DeleteShader(s)
		return gl.Shader{}, fmt.Errorf("shader 编译失败: %s", log)
	}
	return s, nil
}

func newESBlit(ctx gl.Context) (*esBlit, error) {
	vs, err := compileShaderES(ctx, gl.VERTEX_SHADER, blitVertES)
	if err != nil {
		return nil, err
	}
	fs, err := compileShaderES(ctx, gl.FRAGMENT_SHADER, blitFragES)
	if err != nil {
		ctx.DeleteShader(vs)
		return nil, err
	}
	p := ctx.CreateProgram()
	ctx.AttachShader(p, vs)
	ctx.AttachShader(p, fs)
	ctx.LinkProgram(p)
	ctx.DeleteShader(vs)
	ctx.DeleteShader(fs)
	if ctx.GetProgrami(p, gl.LINK_STATUS) == 0 {
		return nil, fmt.Errorf("blit program 链接失败: %s", ctx.GetProgramInfoLog(p))
	}

	vbo := ctx.CreateBuffer()
	ctx.BindBuffer(gl.ARRAY_BUFFER, vbo)
	ctx.BufferData(gl.ARRAY_BUFFER, f32Bytes(quadVerts), gl.STATIC_DRAW)

	tex := ctx.CreateTexture()
	ctx.BindTexture(gl.TEXTURE_2D, tex)
	ctx.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	ctx.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	ctx.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	ctx.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)

	return &esBlit{ctx: ctx, program: p, vbo: vbo, tex: tex}, nil
}

func f32Bytes(fs []float32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&fs[0])), len(fs)*4)
}

func (b *esBlit) resize(w, h int) {
	if b.texW == w && b.texH == h {
		return
	}
	b.texW, b.texH = w, h
	b.ctx.BindTexture(gl.TEXTURE_2D, b.tex)
	b.ctx.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, w, h, gl.RGBA, gl.UNSIGNED_BYTE, nil)
}

// upload 上传 RGBA8888 像素（[]uint32，每 word 低字节为 R，与桌面端同格式）。
func (b *esBlit) upload(w, h int, pix []uint32) {
	b.resize(w, h)
	b.ctx.BindTexture(gl.TEXTURE_2D, b.tex)
	if len(pix) > 0 {
		data := unsafe.Slice((*byte)(unsafe.Pointer(&pix[0])), len(pix)*4)
		b.ctx.TexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, w, h, gl.RGBA, gl.UNSIGNED_BYTE, data)
	}
}

func (b *esBlit) blit() {
	b.ctx.Disable(gl.BLEND)
	b.ctx.Viewport(0, 0, b.texW, b.texH)
	b.ctx.UseProgram(b.program)
	b.ctx.BindBuffer(gl.ARRAY_BUFFER, b.vbo)
	loc := b.ctx.GetAttribLocation(b.program, "aPos")
	b.ctx.EnableVertexAttribArray(loc)
	b.ctx.VertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0)
	b.ctx.ActiveTexture(gl.TEXTURE0)
	b.ctx.BindTexture(gl.TEXTURE_2D, b.tex)
	uloc := b.ctx.GetUniformLocation(b.program, "uTex")
	b.ctx.Uniform1i(uloc, 0)
	b.ctx.DrawArrays(gl.TRIANGLES, 0, 6)
	b.ctx.Enable(gl.BLEND)
}
