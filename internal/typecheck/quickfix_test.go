package typecheck

import (
	"strings"
	"testing"
)

// QuickFixes：从诊断文本产出可执行修复——补 import / 删未使用 import。

func TestQuickFixAddImport(t *testing.T) {
	src := "package main\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", 1)\n\t_ = s\n}\n"
	fixes := QuickFixes(src, []RawDiag{{Line: 4, Msg: "undefined: fmt"}}, []string{"fmt", "strings"})
	if len(fixes) != 1 {
		t.Fatalf("应产出 1 条修复: %+v", fixes)
	}
	f := fixes[0]
	if !strings.Contains(f.Title, "fmt") || f.DeleteLine != 0 {
		t.Fatalf("title/形态错误: %+v", f)
	}
	if f.InsertLine != 2 || !strings.Contains(f.InsertText, `import "fmt"`) {
		t.Fatalf("插入位置/文本错误: %+v", f)
	}
}

func TestQuickFixAddImportIntoBlock(t *testing.T) {
	src := "package main\n\nimport (\n\t\"strings\"\n)\n\nfunc main() {\n\t_ = strings\n\tfmt.Println()\n}\n"
	fixes := QuickFixes(src, []RawDiag{{Line: 8, Msg: "undefined: fmt"}}, []string{"fmt", "strings"})
	if len(fixes) != 1 || fixes[0].InsertLine != 4 || fixes[0].InsertText != "\t\"fmt\"\n" {
		t.Fatalf("括号块内插入错误: %+v", fixes)
	}
}

func TestQuickFixUnusedImport(t *testing.T) {
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n}\n"
	fixes := QuickFixes(src, []RawDiag{{Line: 3, Msg: `"fmt" imported and not used`}}, nil)
	if len(fixes) != 1 || fixes[0].DeleteLine != 3 || !strings.Contains(fixes[0].Title, "fmt") {
		t.Fatalf("删除修复错误: %+v", fixes)
	}
}
