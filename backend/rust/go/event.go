package rustbackend

// inputEvent 是 Rust → Go 的事件 wire 结构（与 Rust protocol::InputEvent 对齐）。
type inputEvent struct {
	Kind   string  `json:"kind"`
	NodeID string  `json:"node_id"`
	Event  string  `json:"event"`
	Text   string  `json:"text"`
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Mods   string  `json:"mods"`
}
