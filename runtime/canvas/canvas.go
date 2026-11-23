// Package canvas 定义 <canvas> 原语的绘制指令协议。
//
// canvas 是命令式绘制面，与 GX 的声明式场景树不同。为保持零协议改动，绘制指令
// 序列化成紧凑 JSON 放在 canvas 节点的 Props["canvas-ops"] 里，两端后端原生层
// 各自解析并绘制。场景树里 <canvas> 仍是一个普通 NodeDiv（参与布局、定位、裁剪），
// 指令只在它的绘制阶段被解释，不产生额外布局节点。
//
// 文本不在本协议内：用声明式 <text> 节点叠加在 canvas 上，跨后端天然一致。
//
// 指令集（两端必须实现一致）：
//   - rect   : x,y,w,h, fill, radius, stroke, stroke-width
//   - circle : cx,cy,r, fill, stroke, stroke-width
//   - line   : x1,y1,x2,y2, stroke, stroke-width
//   - path   : d (SVG path 子集: M/L/H/V/C/Q/Z), fill, stroke, stroke-width
//
// 所有坐标单位是 px，相对 canvas 节点布局盒左上角。颜色与 CSS 颜色同语义
// （#rgb/#rrggbb/#rrggbbaa/rgb()/rgba()/命名色），与 imgutil/STYLE 一致。
package canvas

import "encoding/json"

// OpKind 指令类型。
type OpKind string

const (
	OpRect   OpKind = "rect"
	OpCircle OpKind = "circle"
	OpLine   OpKind = "line"
	OpPath   OpKind = "path"
)

// Op 是一条绘制指令。字段全部可选，按 Kind 使用相关字段；零值表示不设置。
type Op struct {
	Kind        OpKind  `json:"op"`
	X           float32 `json:"x,omitempty"`
	Y           float32 `json:"y,omitempty"`
	W           float32 `json:"w,omitempty"`
	H           float32 `json:"h,omitempty"`
	CX          float32 `json:"cx,omitempty"`
	CY          float32 `json:"cy,omitempty"`
	R           float32 `json:"r,omitempty"`
	X1          float32 `json:"x1,omitempty"`
	Y1          float32 `json:"y1,omitempty"`
	X2          float32 `json:"x2,omitempty"`
	Y2          float32 `json:"y2,omitempty"`
	Radius      float32 `json:"radius,omitempty"`
	D           string  `json:"d,omitempty"`
	Fill        string  `json:"fill,omitempty"`
	Stroke      string  `json:"stroke,omitempty"`
	StrokeWidth float32 `json:"stroke-width,omitempty"`
}

// Encode 把指令列表编码成放进 Props["canvas-ops"] 的字符串。
func Encode(ops []Op) string {
	if len(ops) == 0 {
		return "[]"
	}
	b, err := json.Marshal(ops)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// Decode 解析 Props["canvas-ops"]。空/非法输入返回空列表（安全降级）。
func Decode(s string) []Op {
	if s == "" {
		return nil
	}
	var ops []Op
	if err := json.Unmarshal([]byte(s), &ops); err != nil {
		return nil
	}
	return ops
}

// PropKey 是 canvas 指令在 Node.Props 里的键名。
const PropKey = "canvas-ops"
