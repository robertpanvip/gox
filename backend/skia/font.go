package skia

import (
	"sync"

	sk "github.com/pekim/go-skia"
)

// cjkFamilies 是跨平台的中日韩（CJK）无衬线字体族候选，按优先级排列。
// go-skia 不做自动字体回退（TextBlob/DrawString 遇到缺字形直接画方块），
// 因此后端必须自己为每个字符挑选能覆盖它的字体。这里列出主流系统常见族名，
// resolveFont 会逐个 MatchFamilyStyle，跳过本机不存在的，选第一个覆盖目标字符的。
//
// 顺序原则：先无衬线（与 sans-serif 主字体观感一致），再衬线兜底。
// 新增平台/字体只需在此追加族名，无需改动绘制与测量逻辑。
var cjkFamilies = []string{
	// Linux（fontconfig）
	"Noto Sans CJK SC", "Noto Sans CJK JP", "Source Han Sans SC", "Source Han Sans CN",
	"WenQuanYi Micro Hei", "WenQuanYi Zen Hei", "AR PL UMing CN", "Droid Sans Fallback",
	// macOS
	"PingFang SC", "Hiragino Sans GB", "Heiti SC", "STHeiti",
	// Windows
	"Microsoft YaHei", "SimHei", "SimSun", "Microsoft JhengHei",
	// 通用衬线兜底
	"Noto Serif CJK SC", "AR PL UKai CN",
}

// fontKey 唯一标识一个字体实例：族名 + 字号 + 字重。
// family 为空表示主字体（sans-serif）。
type fontKey struct {
	family string
	size   int
	bold   bool
}

// fontCache 是进程级字体缓存。字体是只读资源，跨 Window 实例、跨帧可安全复用；
// 候选族名 × 字号 × 字重的组合数量有界，随进程生命周期存活不会无界增长。
// 使用进程级缓存的好处：不侵入 painter.fonts / Window.fonts 的持有与释放语义，
// 也不要求 demo 侧的 glwindow.go 感知 fallback 字体的存在。
var fontCache sync.Map // map[fontKey]sk.Font

// getFont 取（或惰性构造）指定族名/字号/字重的字体。
// family 为空时用 sans-serif。
func getFont(family string, size int, bold bool) sk.Font {
	key := fontKey{family: family, size: size, bold: bold}
	if v, ok := fontCache.Load(key); ok {
		return v.(sk.Font)
	}
	style := sk.FontStyleNormal()
	if bold {
		style = sk.FontStyleBold()
	}
	name := family
	if name == "" {
		name = "sans-serif"
	}
	tf := sk.FontMgrDefault().MatchFamilyStyle(name, style)
	if tf.IsNil() {
		return sk.Font{}
	}
	f := sk.NewFontTypefaceSize(tf, float32(size))
	actual, _ := fontCache.LoadOrStore(key, f)
	if actual != (sk.Font{}) {
		// 另一个 goroutine 已构造同款字体，丢弃本次副本。
		return actual.(sk.Font)
	}
	return f
}

// covers 报告字体 f 是否含字符 r 的字形。Skia 用 0 表示缺失字形。
func covers(f sk.Font, r rune) bool {
	return !f.IsNil() && f.UnicharToGlyph(r) != 0
}

// resolveFont 为字符 r 挑选可渲染的字体：主字体优先，缺字则按 cjkFamilies
// 顺序找第一个覆盖 r 的候选；全部缺失时返回主字体（届时显示为 tofu，属环境缺字体）。
func resolveFont(r rune, size int, bold bool) sk.Font {
	primary := getFont("", size, bold)
	if covers(primary, r) {
		return primary
	}
	for _, fam := range cjkFamilies {
		f := getFont(fam, size, bold)
		if covers(f, r) {
			return f
		}
	}
	return primary
}

// run 是一段使用同一字体的连续文本。
type run struct {
	text string
	font sk.Font
}

// splitRuns 把文本按"字体选择"切成若干 run：连续使用同一字体的字符合并为一段，
// 以减少 DrawString 调用次数。绘制与测量共用本函数，保证宽度计算与落笔完全一致。
func splitRuns(text string, size int, bold bool) []run {
	if text == "" {
		return nil
	}
	var runs []run
	var curFont sk.Font
	curStart := 0
	first := true
	for i, r := range text {
		f := resolveFont(r, size, bold)
		if first {
			curFont = f
			curStart = 0
			first = false
		} else if f != curFont {
			runs = append(runs, run{text: text[curStart:i], font: curFont})
			curFont = f
			curStart = i
		}
	}
	runs = append(runs, run{text: text[curStart:], font: curFont})
	return runs
}
