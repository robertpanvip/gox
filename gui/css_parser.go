package gui

import (
	"strconv"
	"strings"
)

// parseColor 解析 CSS 颜色字符串为 Color
func parseColor(colorStr string) Color {
	if colorStr == "" {
		return ColorTransparent
	}
	
	// 处理 #RRGGBB 格式
	if strings.HasPrefix(colorStr, "#") {
		hex := colorStr[1:]
		if len(hex) == 6 {
			r, _ := strconv.ParseUint(hex[0:2], 16, 8)
			g, _ := strconv.ParseUint(hex[2:4], 16, 8)
			b, _ := strconv.ParseUint(hex[4:6], 16, 8)
			return NewColor(uint8(r), uint8(g), uint8(b), 255)
		}
	}
	
	// 处理 rgb(r, g, b) 格式
	if strings.HasPrefix(colorStr, "rgb(") {
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(colorStr, "rgb("), ")"), ",")
		if len(parts) == 3 {
			r, _ := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 8)
			g, _ := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 8)
			b, _ := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 8)
			return NewColor(uint8(r), uint8(g), uint8(b), 255)
		}
	}
	
	// 预定义颜色名称
	switch colorStr {
	case "red":
		return ColorRed
	case "green":
		return ColorGreen
	case "blue":
		return ColorBlue
	case "black":
		return ColorBlack
	case "white":
		return ColorWhite
	case "transparent":
		return ColorTransparent
	case "orange":
		return ColorOrange
	case "purple":
		return ColorPurple
	case "cyan":
		return ColorCyan
	case "yellow":
		return ColorYellow
	case "pink":
		return ColorPink
	case "gray", "grey":
		return ColorGray
	case "lightgray", "lightgrey":
		return ColorLightGray
	case "darkgray", "darkgrey":
		return ColorDarkGray
	}
	
	return ColorTransparent
}

// parseSize 解析 CSS 尺寸字符串为 int（像素）
func parseSize(sizeStr string) int {
	if sizeStr == "" || sizeStr == "auto" {
		return 0
	}
	
	// 移除 "px" 后缀
	sizeStr = strings.TrimSuffix(sizeStr, "px")
	sizeStr = strings.TrimSpace(sizeStr)
	
	value, err := strconv.Atoi(sizeStr)
	if err != nil {
		return 0
	}
	
	return value
}

// parsePadding 解析 CSS padding 字符串
func parsePadding(paddingStr string) (top, right, bottom, left int) {
	if paddingStr == "" {
		return 0, 0, 0, 0
	}
	
	parts := strings.Fields(paddingStr)
	values := make([]int, len(parts))
	
	for i, part := range parts {
		values[i] = parseSize(part)
	}
	
	switch len(values) {
	case 1:
		return values[0], values[0], values[0], values[0]
	case 2:
		return values[0], values[1], values[0], values[1]
	case 3:
		return values[0], values[1], values[2], values[1]
	case 4:
		return values[0], values[1], values[2], values[3]
	default:
		return 0, 0, 0, 0
	}
}
