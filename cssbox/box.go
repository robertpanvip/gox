// Package cssbox 提供 CSS 盒模型简写值的解析能力，供 web / skia 两个后端共用。
//
// GX 的 Style 把 padding / margin / gap / border-radius 等盒模型属性统一表示为
// 一个 StyleValue 字符串。CSS 允许这些属性用 1~4 个值简写：
//
//	1 值  → 四边相同
//	2 值  → 上下 / 左右
//	3 值  → 上 / 左右 / 下
//	4 值  → 上 / 右 / 下 / 左（顺时针）
//
// 此前两个后端都只认单一数值（strconv.ParseFloat 整串解析），遇到 "7 14" 这类
// 双值直接失败：web 端输出无单位 CSS 被忽略，skia 端回退成 0，导致按钮文字挤压。
// 本包把"简写解析"收敛为一处，两端共享同一套语义。
package cssbox

import (
	"strconv"
	"strings"
)

// Edges 是盒模型的四边值，顺序固定为 [top, right, bottom, left]（顺时针，CSS 语义）。
type Edges [4]string

// Values 把 CSS 简写值解析成四边字符串（已去掉 px 后缀，保留原始数值文本）。
// 支持 1/2/3/4 值；每个分量必须是纯数值或数值+px；失败时返回 ok=false。
// 空串返回零值 + ok=false，由调用方决定是否保留缺省。
func Values(s string) (Edges, bool) {
	parts := strings.Fields(strings.TrimSpace(s))
	if len(parts) == 0 || len(parts) > 4 {
		return Edges{}, false
	}
	nums := make([]string, len(parts))
	for i, p := range parts {
		v := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(p), "px"))
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return Edges{}, false
		}
		nums[i] = v
	}
	var e Edges
	switch len(nums) {
	case 1:
		e = Edges{nums[0], nums[0], nums[0], nums[0]}
	case 2:
		e = Edges{nums[0], nums[1], nums[0], nums[1]}
	case 3:
		e = Edges{nums[0], nums[1], nums[2], nums[1]}
	case 4:
		e = Edges{nums[0], nums[1], nums[2], nums[3]}
	}
	return e, true
}

// Float32s 把简写值解析成四边浮点（像素）。失败时 ok=false。
func Float32s(s string) ([4]float32, bool) {
	e, ok := Values(s)
	if !ok {
		return [4]float32{}, false
	}
	var out [4]float32
	for i, v := range e {
		f, err := strconv.ParseFloat(v, 32)
		if err != nil {
			return [4]float32{}, false
		}
		out[i] = float32(f)
	}
	return out, true
}

// WithUnit 把四边字符串补上 CSS 单位：纯数值（含 0）加 px，已是 px 的保留。
// 用于 web 后端输出合法的多值 CSS（如 "7px 14px"）。
func WithUnit(e Edges) Edges {
	var out Edges
	for i, v := range e {
		if v == "" {
			continue
		}
		if _, err := strconv.ParseFloat(v, 64); err == nil {
			out[i] = v + "px"
		} else {
			out[i] = v
		}
	}
	return out
}

// Shorthand 把四边值重新拼成 CSS 简写串（去冗余：四边相同→1 值，上下/左右对称→2 值）。
func Shorthand(e Edges) string {
	if e[0] == e[1] && e[1] == e[2] && e[2] == e[3] {
		return e[0]
	}
	if e[0] == e[2] && e[1] == e[3] {
		return e[0] + " " + e[1]
	}
	return e[0] + " " + e[1] + " " + e[2] + " " + e[3]
}
