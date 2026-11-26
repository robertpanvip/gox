package main

import (
	"fmt"
	"unsafe"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// rasterBlit 把 CPU raster 像素缓冲上传为 GL 纹理，并用全屏三角形 blit 到默认 framebuffer。
// 这是「纯 Go canvas CPU 绘制 + GL 纹理显示」架构的显示端。
type rasterBlit struct {
	program uint32
	vao     uint32
	tex     uint32
	texW    int32
	texH    int32
}

const blitVertSrc = `#version 330 core
out vec2 vUV;
void main() {
    // 无 VBO：用 gl_VertexID 生成一个覆盖全屏的三角形。
    vec2 p = vec2((gl_VertexID << 1) & 2, gl_VertexID & 2);
    vUV = p;
    gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);
}
`

const blitFragSrc = `#version 330 core
in vec2 vUV;
uniform sampler2D uTex;
out vec4 fragColor;
void main() {
    // CPU raster 采用左上角原点，GL 采样 vUV.y 需翻转。
    vec2 uv = vec2(vUV.x, 1.0 - vUV.y);
    fragColor = texture(uTex, uv);
}
`

func compileShader(kind uint32, src string) (uint32, error) {
	s := gl.CreateShader(kind)
	cs, free := gl.Strs(src + "\x00")
	defer free()
	gl.ShaderSource(s, 1, cs, nil)
	gl.CompileShader(s)
	var ok int32
	gl.GetShaderiv(s, gl.COMPILE_STATUS, &ok)
	if ok == 0 {
		var buf [512]byte
		gl.GetShaderInfoLog(s, int32(len(buf)), nil, &buf[0])
		gl.DeleteShader(s)
		return 0, fmt.Errorf("shader 编译失败: %s", string(buf[:]))
	}
	return s, nil
}

func newRasterBlit() (*rasterBlit, error) {
	vs, err := compileShader(gl.VERTEX_SHADER, blitVertSrc)
	if err != nil {
		return nil, err
	}
	fs, err := compileShader(gl.FRAGMENT_SHADER, blitFragSrc)
	if err != nil {
		gl.DeleteShader(vs)
		return nil, err
	}
	p := gl.CreateProgram()
	gl.AttachShader(p, vs)
	gl.AttachShader(p, fs)
	gl.LinkProgram(p)
	gl.DeleteShader(vs)
	gl.DeleteShader(fs)
	var ok int32
	gl.GetProgramiv(p, gl.LINK_STATUS, &ok)
	if ok == 0 {
		return nil, fmt.Errorf("blit program 链接失败")
	}

	var vao, tex uint32
	gl.GenVertexArrays(1, &vao)
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)

	return &rasterBlit{program: p, vao: vao, tex: tex}, nil
}

// resize 确保纹理尺寸匹配 framebuffer（必要时重新分配）。
func (b *rasterBlit) resize(w, h int32) {
	if b.texW == w && b.texH == h {
		return
	}
	b.texW, b.texH = w, h
	gl.BindTexture(gl.TEXTURE_2D, b.tex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, w, h, 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
}

// upload 上传 RGBA8888 像素（[]uint32，每 word 低字节为 R）。
func (b *rasterBlit) upload(w, h int32, pix []uint32) {
	b.resize(w, h)
	gl.BindTexture(gl.TEXTURE_2D, b.tex)
	if len(pix) > 0 {
		gl.TexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, w, h, gl.RGBA, gl.UNSIGNED_BYTE, unsafe.Pointer(&pix[0]))
	}
}

// blit 把纹理绘制到当前 framebuffer。
func (b *rasterBlit) blit() {
	gl.Disable(gl.BLEND)
	gl.Viewport(0, 0, b.texW, b.texH)
	gl.UseProgram(b.program)
	gl.BindVertexArray(b.vao)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, b.tex)
	loc := gl.GetUniformLocation(b.program, gl.Str("uTex\x00"))
	gl.Uniform1i(loc, 0)
	gl.DrawArrays(gl.TRIANGLES, 0, 3)
	gl.Enable(gl.BLEND)
}

func (b *rasterBlit) destroy() {
	if b == nil {
		return
	}
	gl.DeleteTextures(1, &b.tex)
	gl.DeleteVertexArrays(1, &b.vao)
	gl.DeleteProgram(b.program)
}
