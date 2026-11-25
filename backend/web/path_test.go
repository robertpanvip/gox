package gxweb

import (
	"reflect"
	"testing"
)

// TestTokenizePathCommands 锁定 SVG path 命令字母与数字粘连的解析——
// 内置矢量图标（play / chevron 等）的路径是 "M4.375 2.5 L12.5 7.5 Z"
// 形式，旧实现按空白分词得到 "M4.375"，匹配不到命令导致图标全空。
func TestTokenizePathCommands(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "紧凑命令与小数（play 图标）",
			path: "M4.375 2.5 L12.5 7.5 L4.375 12.5 Z",
			want: []string{"M", "4.375", "2.5", "L", "12.5", "7.5", "L", "4.375", "12.5", "Z"},
		},
		{
			name: "带逗号与负号",
			path: "M-1.5,2 L3,-4.25 Z",
			want: []string{"M", "-1.5", "2", "L", "3", "-4.25", "Z"},
		},
		{
			name: "命令与数字完全无空格（chevron）",
			path: "M15 5L8 12L15 19",
			want: []string{"M", "15", "5", "L", "8", "12", "L", "15", "19"},
		},
		{
			name: "指数记数法",
			path: "M1e2 2.5e-1 L3 4",
			want: []string{"M", "1e2", "2.5e-1", "L", "3", "4"},
		},
		{
			name: "曲线命令",
			path: "M3 9 Q5 4 7 9 C8 10 9 11 10 12 Z",
			want: []string{"M", "3", "9", "Q", "5", "4", "7", "9", "C", "8", "10", "9", "11", "10", "12", "Z"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tokenizePath(tc.path)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tokenizePath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
