package main

import (
	"fmt"
	"strings"

	"github.com/robertpanvip/gox/pkg/compiler"
)

// cmdSelfTest 运行内联断言，覆盖转译核心特性。无需外部文件。
func cmdSelfTest(_ []string) error {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "pub + 引用还原",
			src:  "package main\n\npub func greet() {}\nfunc main() { greet() }",
			want: []string{"func Greet()", "Greet()"},
		},
		{
			name: "enum",
			src:  "package main\n\npub enum color { red, green, blue }",
			want: []string{"type Color int", "Red Color = iota", "Green", "Blue"},
		},
		{
			name: "JSX → createElement",
			src:  "package main\n\npub func view() any { return <div class=\"box\">hi</div> }",
			want: []string{"gxvdom.CreateElement", `"div"`, `"class"`, `"box"`, `"hi"`},
		},
		{
			name: "外部符号还原",
			src:  "package main\n\nimport \"fmt\"\npub func f() { fmt.println(\"x\") }",
			want: []string{"fmt.Println"},
		},
		{
			name: "集合链式 + 提升",
			src:  "package main\n\npub func f() { xs := []int{1,2}; ys := xs.map(fn).filter(g) }\nfunc fn(x int) int{return x}\nfunc g(x int) bool{return true}",
			want: []string{"gxcollections.List[int]", "xs.Map(fn).Filter(g)"},
		},
	}

	failed := 0
	for _, tt := range tests {
		res, err := compiler.Compile(tt.name+".gx", tt.src)
		if err != nil {
			fmt.Printf("FAIL %-20s 转译错误: %v\n", tt.name, err)
			failed++
			continue
		}
		missing := []string{}
		for _, w := range tt.want {
			if !strings.Contains(res.GoSource, w) {
				missing = append(missing, w)
			}
		}
		if len(missing) > 0 {
			fmt.Printf("FAIL %-20s 缺少: %v\n---\n%s\n---\n", tt.name, missing, res.GoSource)
			failed++
		} else {
			fmt.Printf("PASS %-20s\n", tt.name)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d/%d 自检失败", failed, len(tests))
	}
	fmt.Printf("全部 %d 项自检通过\n", len(tests))
	return nil
}
