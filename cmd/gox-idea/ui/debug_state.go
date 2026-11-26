package ui

import "encoding/json"

// DebugSnapshot 是 GUI 端到端测试用的状态快照。
//
// 桌面交互（拖分隔条、滚轮、选区、主题切换、Run）肉眼可见但进程外无法读取，
// 单靠截图比对抓不到「状态确实变了但界面没跟上」这类 bug。此快照把 UI 内部
// 状态序列化，E2E 脚本在每次真实鼠标/键盘操作后读文件做硬断言。
//
// 仅当设置 GOX_IDEA_STATE_FILE 时由装配层按帧写入，正常用户运行时零开销。
type DebugSnapshot struct {
	ProjectW            int      `json:"project_w"`
	TerminalH           int      `json:"terminal_h"`
	SplitActive         bool     `json:"split_active"`
	ProjectPanelVisible bool     `json:"project_panel_visible"`
	LightTheme          bool     `json:"light_theme"`
	Menu                string   `json:"menu"`
	BottomTab           string   `json:"bottom_tab"`
	ActiveTab           string   `json:"active_tab"`
	Tabs                []string `json:"tabs"`
	EditorText          string   `json:"editor_text"`
	EditorLine          int      `json:"editor_line"`
	EditorCol           int      `json:"editor_col"`
	HasSelection        bool     `json:"has_selection"`
	SelectedText        string   `json:"selected_text"`
	EditorStatus        string   `json:"editor_status"`
	ScrollTop           float64  `json:"scroll_top"`
	RunInfo             string   `json:"run_info"`
	WindowW             int      `json:"window_w"`
	WindowH             int      `json:"window_h"`
	ProjectVisibleRows  int      `json:"project_visible_rows"`
	ProjectExpandedCnt  int      `json:"project_expanded_count"`
}

// DebugScrollTop 供装配层诊断滚轮链路：区分「回调没到」「转发没生效」「被 effect 回写」。
func DebugScrollTop() float64 { return editorState.ScrollTop() }

// DebugStateJSON 生成当前 UI 状态的 JSON 快照。
// 调用方必须保证在 UI 线程（glWindow.Post）调用，避免与 UI 编辑并发。
func DebugStateJSON() []byte {
	tabs := TabNames()
	if tabs == nil {
		tabs = []string{}
	}
	s := DebugSnapshot{
		ProjectW:            LayoutProjectW(),
		TerminalH:           LayoutTerminalH(),
		SplitActive:         LayoutSplitActive(),
		ProjectPanelVisible: projectPanelVisible(),
		LightTheme:          LightTheme(),
		Menu:                MenuName(),
		BottomTab:           BottomTab(),
		ActiveTab:           ActiveTabName(),
		Tabs:                tabs,
		EditorText:          EditorText(),
		EditorLine:          EditorLine(),
		EditorCol:           EditorCol(),
		HasSelection:        HasSelection(),
		SelectedText:        EditorSelectedText(),
		EditorStatus:        EditorStatus(),
		ScrollTop:           editorState.ScrollTop(),
		RunInfo:             RunInfo(),
		ProjectVisibleRows:  ProjectVisibleRows(),
		ProjectExpandedCnt:  ProjectExpandedCount(),
	}
	b, err := json.Marshal(s)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}
