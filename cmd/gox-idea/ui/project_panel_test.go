package ui

import (
	"testing"

	gxicon "github.com/robertpanvip/gox-core/runtime/icon"
	"github.com/robertpanvip/gox-core/runtime/signal"
)

// resetProjectState 把项目树信号恢复到「全展开、无目录选中、回到第 0 个示例」，
// 并在测试结束后再恢复一次，避免全局 signal 在用例间串味。
func resetProjectState(t *testing.T) {
	t.Helper()
	restore := func() {
		signal.Batch(func() {
			ProjectExpanded([]string{"基础", "响应式", "语言特性"})
			ProjectDirSelected("")
		})
	}
	restore()
	t.Cleanup(func() {
		restore()
		LoadSample(0)
	})
}

func TestProjectGroups(t *testing.T) {
	resetProjectState(t)
	got := projectGroups()
	want := []string{"基础", "响应式", "语言特性"}
	if len(got) != len(want) {
		t.Fatalf("分组数不对: got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("分组[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSampleFile(t *testing.T) {
	cases := map[int]string{
		0:  "01_hello.gx",
		4:  "05_computed.gx",
		14: "15_collections.gx",
		-1: "sample.gx",
		99: "sample.gx",
	}
	for i, want := range cases {
		if got := sampleFile(i); got != want {
			t.Errorf("sampleFile(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestProjectRowsFullTree(t *testing.T) {
	resetProjectState(t)
	rows := projectRows()
	dirs, files := 0, 0
	for _, r := range rows {
		if r.IsDir {
			dirs++
			if r.Depth != 1 || r.Sample != -1 {
				t.Errorf("目录行字段不对: %+v", r)
			}
		} else {
			files++
			if r.Depth != 2 || r.Label == "" {
				t.Errorf("文件行字段不对: %+v", r)
			}
		}
	}
	if dirs != 3 || files != len(Samples()) {
		t.Fatalf("展开行数: 目录=%d 文件=%d, want 3/%d", dirs, files, len(Samples()))
	}
	if rows[0].Label != "基础" || !rows[0].Expanded {
		t.Fatalf("首个目录行不对: %+v", rows[0])
	}
	if rows[1].Label != "01_hello.gx" || rows[1].Sample != 0 {
		t.Fatalf("首个文件行不对: %+v", rows[1])
	}
}

func TestToggleGroupCollapseExpand(t *testing.T) {
	resetProjectState(t)

	toggleGroup("基础")
	if containsStr(ProjectExpanded(), "基础") {
		t.Fatal("切换后「基础」仍为展开")
	}
	if got := len(projectRows()); got != 14 { // 3 目录 + (15-4) 文件
		t.Fatalf("折叠「基础」后行数=%d, want 14", got)
	}

	toggleGroup("基础")
	if got := len(projectRows()); got != 18 {
		t.Fatalf("再展开后行数=%d, want 18", got)
	}

	collapseAllGroups()
	if got := len(projectRows()); got != 3 {
		t.Fatalf("全部折叠后行数=%d, want 3（只剩目录）", got)
	}
	expandAllGroups()
	if got := len(projectRows()); got != 18 {
		t.Fatalf("全部展开后行数=%d, want 18", got)
	}
}

func TestOnProjectRowClickFile(t *testing.T) {
	resetProjectState(t)
	onProjectRowClick(ProjectNode{Label: "08_enum.gx", Path: "file:7", Sample: 7, Depth: 2})
	if got := SampleIndex(); got != 7 {
		t.Fatalf("点击文件后示例索引=%d, want 7", got)
	}
	if got := ProjectDirSelected(); got != "" {
		t.Fatalf("点击文件后目录选中=%q, want 空", got)
	}
}

func TestOnProjectRowClickDir(t *testing.T) {
	resetProjectState(t)
	onProjectRowClick(ProjectNode{Label: "基础", Path: "dir:基础", IsDir: true, Sample: -1, Depth: 1, Expanded: true})
	if got := ProjectDirSelected(); got != "dir:基础" {
		t.Fatalf("点击目录后选中=%q, want dir:基础", got)
	}
	if containsStr(ProjectExpanded(), "基础") {
		t.Fatal("点击已展开目录应折叠")
	}
}

// 项目树只使用 runtime/icon 内置矢量图标，不依赖 emoji。
func TestProjectIconsAreBuiltin(t *testing.T) {
	for _, name := range []string{"folder", "doc", "chevron-down", "chevron-right"} {
		if _, ok := gxicon.Lookup(name); !ok {
			t.Errorf("图标 %q 不是内置图标", name)
		}
	}
}

func TestProjectPanelRenders(t *testing.T) {
	resetProjectState(t)
	if projectPanel() == nil {
		t.Fatal("projectPanel() 返回 nil")
	}
	for _, r := range projectRows() {
		if projectRow(r) == nil {
			t.Fatalf("projectRow(%+v) 返回 nil", r)
		}
	}
}
