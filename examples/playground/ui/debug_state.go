package ui

import "encoding/json"

// DebugSnapshot 是 Desktop Playground GUI 端到端测试用的状态快照。
//
// 桌面交互（点击定位、拖动选区、滚轮、主题切换、Run）肉眼可见但进程外
// 无法读取；快照把 UI 内部状态序列化，E2E 脚本在每次真实鼠标/键盘操作后
// 读文件做硬断言，从而自动发现「状态没变」或「显示与状态脱节」的 bug。
//
// 仅当设置 GOX_PLAYGROUND_STATE_FILE 时由装配层按帧写入，正常用户零开销。
type DebugSnapshot struct {
	LightTheme   bool    `json:"light_theme"`
	EditorText   string  `json:"editor_text"`
	EditorLine   int     `json:"editor_line"`
	EditorCol    int     `json:"editor_col"`
	HasSelection bool    `json:"has_selection"`
	SelectedText string  `json:"selected_text"`
	EditorStatus string  `json:"editor_status"`
	ScrollTop    float64 `json:"scroll_top"`
	RunInfo      string  `json:"run_info"`
	ConsoleText  string  `json:"console_text"`
	WindowW      int     `json:"window_w"`
	WindowH      int     `json:"window_h"`
}

// DebugStateJSON 生成当前 UI 状态的 JSON 快照。
// 调用方必须保证在 UI 线程（glWindow.Post）调用，避免与编辑器编辑并发。
func DebugStateJSON() []byte {
	s := DebugSnapshot{
		LightTheme:   LightTheme(),
		EditorText:   EditorText(),
		EditorLine:   EditorLine(),
		EditorCol:    EditorCol(),
		HasSelection: EditorHasSelection(),
		SelectedText: EditorSelectedText(),
		EditorStatus: EditorStatus(),
		ScrollTop:    EditorScrollTop(),
		RunInfo:      RunInfo(),
		ConsoleText:  ConsoleTextSnapshot(),
	}
	b, err := json.Marshal(s)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}
