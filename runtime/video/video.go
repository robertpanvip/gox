// Package video 定义 <video> 原语的跨端协议。
//
// 设计目标（性能优先、零新增编译依赖）：src 由后端内部读取并解码——后端 spawn
// 一个 ffmpeg 子进程把 src 解码成 RGBA 原始帧，维护双缓冲「当前帧」，绘制时贴到
// video 节点盒子里。新帧到达只请求重绘、不触发布局重算。解码在独立线程进行，
// 不阻塞 UI。
//
// 没有 ffmpeg 时安全降级：画 poster 占位 + 回传 StateError 事件，绝不崩溃。
// 宿主可通过 env FFMPEG_PATH 或 PATH 提供 ffmpeg；应用也可随包分发一个。
//
// 与 canvas/img 一样，video 在场景树里是普通 NodeDiv（Kind="video"），参与
// 布局/定位/裁剪/伪状态。src 与所有播放意图都走 Node.Props 原生透传。
//
// Props（host 写入，后端读取）：
//
//	src          视频源（本地路径 / http(s) URL / data: URI，交给 ffmpeg -i）
//	poster       封面图 src（解码就绪前的占位，后端用 imgutil 画）
//	object-fit   "fill"(默认)/"contain"/"cover"
//	width/height/border-radius/bg/opacity  常规视觉属性
//	autoplay/loop/muted  bool 播放意图
//	playback-id  播放会话版本（变更=重新打开/重启解码）
//	playing      bool，"true"=播放中，"false"=暂停（后端据此暂停/恢复解码进程）
//	seek-to      float，目标时间秒（变化=向解码进程 seek）
//
// 事件（后端 → host，经现有事件通道，Kind 见下，node_id=video 节点）：
//
//	EventStateChange  text=state 常量（playing/paused/ended/error）
//	EventMeta         text=JSON{duration,w,h}（元数据就绪一次）
//	EventTimeUpdate   text=当前秒数（节流，如每 250ms）
package video

import (
	"encoding/json"
	"strconv"
)

// Kind 是 video 节点在场景树里的 Kind 标签。
const Kind = "video"

// Props 键名（host 与后端共享）。
const (
	PropSrc        = "src"
	PropPoster     = "poster"
	PropObjectFit  = "object-fit"
	PropAutoplay   = "autoplay"
	PropLoop       = "loop"
	PropMuted      = "muted"
	PropPlaybackID = "playback-id"
	PropPlaying    = "playing"
	PropSeekTo     = "seek-to"
)

// 播放状态（EventStateChange 的 text 值）。
const (
	StateIdle    = "idle"
	StatePlaying = "playing"
	StatePaused  = "paused"
	StateEnded   = "ended"
	StateError   = "error"
)

// 事件 Kind（后端 → host）。
const (
	EventStateChange = "videostate"
	EventMeta        = "videometa"
	EventTimeUpdate  = "videotime"
)

// Meta 是 EventMeta 的 JSON 载荷。
type Meta struct {
	Duration float64 `json:"duration"`
	W        int     `json:"w"`
	H        int     `json:"h"`
}

// EncodeMeta / DecodeMeta。
func EncodeMeta(m Meta) string {
	b, _ := json.Marshal(m)
	return string(b)
}

func DecodeMeta(s string) (Meta, bool) {
	var m Meta
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return Meta{}, false
	}
	return m, true
}

// BoolProp / NumString 供 host 组件把 Props 值规整成后端可读字符串。
func BoolProp(v bool) string { return strconv.FormatBool(v) }

func NumString(v any) string {
	switch x := v.(type) {
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case string:
		return x
	case func() int:
		return strconv.Itoa(x())
	case func() float64:
		return strconv.FormatFloat(x(), 'f', -1, 64)
	default:
		return ""
	}
}
