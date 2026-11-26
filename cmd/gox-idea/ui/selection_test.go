package ui

import "testing"

// 选区引擎回归。编辑器状态是包级 sig，测试串行执行（Go 默认同包内顺序运行）。

func setupSel(t *testing.T) {
	t.Helper()
	EditorSetText("alpha beta gamma\nsecond 中文变量 line\n\tlast")
	EditorSetCursor(0, 0)
}

func TestSelectionNoneAfterClick(t *testing.T) {
	setupSel(t)
	if HasSelection() { t.Fatal("普通点击后不应有选区") }
	if EditorSelectedText() != "" { t.Fatal("无选区时 SelectedText 应为空串") }
}

func TestSelectWordEnglish(t *testing.T) {
	setupSel(t)
	EditorSelectWord(0, 7) // beta 内部
	if EditorSelectedText() != "beta" { t.Fatalf("选词=「%s」 want beta", EditorSelectedText()) }
	if EditorLine() != 0 || EditorCol() != 10 { t.Fatalf("光标=(%d,%d) want (0,10)", EditorLine(), EditorCol()) }
	// 词尾边界（beta 后的空格上）双击：优先左侧词，不选下一个词
	EditorSelectWord(0, 10)
	if EditorSelectedText() != "beta" { t.Fatalf("词尾边界选词=「%s」 want beta", EditorSelectedText()) }
}

func TestSelectWordCJK(t *testing.T) {
	setupSel(t)
	EditorSelectWord(1, 10) // "量" 上
	if EditorSelectedText() != "中文变量" { t.Fatalf("CJK 选词=「%s」 want 中文变量", EditorSelectedText()) }
}

func TestDragAndShiftExtend(t *testing.T) {
	setupSel(t)
	EditorSetSelect(0, 5)
	EditorExtendTo(0, 10)
	if EditorSelectedText() != " beta" { t.Fatalf("拖拽选区=「%s」", EditorSelectedText()) }
	EditorClearSelection()
	if HasSelection() { t.Fatal("ClearSelection 后不应有选区") }
	EditorSetCursor(0, 0)
	EditorMoveEx(0, 5, true) // 锚点即当前 (0,0)，右扩 5
	if EditorSelectedText() != "alpha" { t.Fatalf("Shift 右扩=「%s」", EditorSelectedText()) }
	EditorMoveEx(0, -2, true) // 光标回到 col 3，锚点留在 0
	if EditorSelectedText() != "alp" { t.Fatalf("Shift 回拉=「%s」", EditorSelectedText()) }
}

func TestCrossLineSelectionAndDelete(t *testing.T) {
	setupSel(t)
	EditorSetSelect(0, 5)
	EditorExtendTo(1, 6)
	if EditorSelectedText() != " beta gamma\nsecond" { t.Fatalf("跨行选区=「%s」", EditorSelectedText()) }
	EditorDeleteSelection()
	want := "alpha 中文变量 line\n\tlast"
	if EditorText() != want { t.Fatalf("删除跨行选区后=%q want %q", EditorText(), want) }
	if HasSelection() { t.Fatal("删除后应无选区") }
	if EditorLine() != 0 || EditorCol() != 5 { t.Fatalf("删除后光标=(%d,%d) want (0,5)", EditorLine(), EditorCol()) }
}

func TestInsertBackspaceOverwriteSelection(t *testing.T) {
	setupSel(t)
	EditorSetSelect(0, 6)
	EditorExtendTo(0, 10)
	EditorInsert("z") // 替换选区
	if EditorText() != "alpha z gamma\nsecond 中文变量 line\n\tlast" { t.Fatalf("插入替换选区后=%q", EditorText()) }
	if EditorLine() != 0 || EditorCol() != 7 { t.Fatalf("插入后光标=(%d,%d) want (0,7)", EditorLine(), EditorCol()) }
	EditorSetSelect(0, 0)
	EditorExtendTo(0, 1) // 选中 "a"
	EditorBackspace()    // 退格应删选区而不是删光标前字符
	if EditorText() != "lpha z gamma\nsecond 中文变量 line\n\tlast" { t.Fatalf("退格删选区后=%q", EditorText()) }
}

func TestPasteMultilineReplacesSelection(t *testing.T) {
	setupSel(t)
	EditorSetSelect(0, 0)
	EditorExtendTo(1, 6)
	EditorPaste("x\nyy")
	want := "x\nyy 中文变量 line\n\tlast"
	if EditorText() != want { t.Fatalf("多行粘贴后=%q want %q", EditorText(), want) }
	if EditorLine() != 1 || EditorCol() != 2 { t.Fatalf("粘贴后光标=(%d,%d) want (1,2)", EditorLine(), EditorCol()) }
	if HasSelection() { t.Fatal("粘贴后应无选区") }
}

func TestSelectAllSelectLine(t *testing.T) {
	full := "one\ntwo"
	EditorSetText(full)
	EditorSelectAll()
	if EditorSelectedText() != full { t.Fatalf("全选=「%s」 want %q", EditorSelectedText(), full) }
	EditorSetCursor(1, 0)
	EditorSelectLine(0) // 含行尾换行符
	if EditorSelectedText() != "one\n" { t.Fatalf("三击选行=「%s」 want one+换行", EditorSelectedText()) }
	EditorSelectLine(1) // 末行只选内容
	if EditorSelectedText() != "two" { t.Fatalf("末行选行=「%s」 want two", EditorSelectedText()) }
	EditorSetCursor(0, 0)
}

func TestHomeEndDeleteForward(t *testing.T) {
	setupSel(t)
	EditorSetCursor(1, 9)
	EditorKeyEnd(false)
	if EditorCol() != 16 { t.Fatalf("End 后 col=%d want 16", EditorCol()) }
	EditorKeyHome(false)
	if EditorCol() != 0 { t.Fatalf("Home 后 col=%d want 0（本行首字符即非空白）", EditorCol()) }
	EditorSetCursor(2, 3) // "\tlast" 制表符行
	EditorKeyHome(false)
	if EditorCol() != 1 { t.Fatalf("缩进行 Home col=%d want 1（先到首非空白）", EditorCol()) }
	EditorSetCursor(0, 5) // alpha|beta 之间的空格上
	EditorDeleteForward() // 无选区：删光标后字符
	if EditorText() != "alphabeta gamma\nsecond 中文变量 line\n\tlast" { t.Fatalf("Delete 后=%q", EditorText()) }
}

func TestDeleteForwardLineJoin(t *testing.T) {
	EditorSetText("one\ntwo")
	EditorSetCursor(0, 3) // "one|" 行尾
	EditorDeleteForward() // 并入下行
	if EditorText() != "onetwo" { t.Fatalf("行尾 Delete 并线后=%q want onetwo", EditorText()) }
	if EditorLine() != 0 || EditorCol() != 3 { t.Fatalf("并线后光标=(%d,%d) want (0,3)", EditorLine(), EditorCol()) }
}

func TestWordMoveAndCollapse(t *testing.T) {
	setupSel(t)
	EditorSetCursor(0, 0)
	EditorMoveWord(1, false)
	if EditorCol() != 5 { t.Fatalf("Ctrl→ 后 col=%d want 5", EditorCol()) }
	EditorMoveWord(1, true) // 跳过空格再跳词
	if EditorCol() != 10 || EditorSelectedText() != " beta" { t.Fatalf("Ctrl+Shift→ col=%d 选区=「%s」", EditorCol(), EditorSelectedText()) }
	EditorSetCursor(0, 2)
	EditorExtendTo(0, 8)
	EditorMove(0, -1) // 左移先折叠到选区起点再移动
	if HasSelection() { t.Fatal("方向键后应无选区") }
	if EditorCol() != 1 { t.Fatalf("折叠后 col=%d want 1", EditorCol()) }
}

func TestUndoRedoCollapseSelection(t *testing.T) {
	setupSel(t)
	EditorSetSelect(0, 0)
	EditorExtendTo(0, 4)
	EditorInsert("X") // 覆盖式插入
	EditorUndo()      // 回退到插入前：文本恢复、选区折叠
	if EditorText() != "alpha beta gamma\nsecond 中文变量 line\n\tlast" { t.Fatalf("撤销后=%q", EditorText()) }
	if HasSelection() { t.Fatal("撤销后不应残留选区") }
	EditorRedo()
	if EditorText() != "Xa beta gamma\nsecond 中文变量 line\n\tlast" { t.Fatalf("重做后=%q", EditorText()) }
	if HasSelection() { t.Fatal("重做后不应残留选区") }
	EditorSetCursor(0, 0)
}
