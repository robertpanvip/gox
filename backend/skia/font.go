package skia

import (
	"strings"
	"sync"

	sk "github.com/robertpanvip/gox-skia/compat"
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

// monoFamilies 是跨平台等宽字体候选，按优先级排列。当 font-family 栈里
// 声明 monospace 通用名时，Skia 不能像浏览器那样自动选等宽字体（compat 把
// monospace 映射成默认族），因此显式挑一个本机存在的等宽族名。
var monoFamilies = []string{
	"Menlo", "SFMono-Regular", "Consolas", "DejaVu Sans Mono",
	"Noto Sans Mono", "Liberation Mono", "Courier New", "monospace",
}

// pickFontFamily 从 CSS font-family 栈里挑出本机实际可用的族名，作为绘制主字体。
// 规则与浏览器一致：按优先级取第一个命中项；具体族名精确匹配，通用族名
// monospace 走等宽候选列表，其余通用名（sans-serif/serif/system-ui）返回空串
// 表示系统默认。全不匹配返回空串。
func pickFontFamily(stack string) string {
	if stack == "" {
		return ""
	}
	mgr := sk.FontMgrDefault()
	wantMono := false
	for _, raw := range strings.Split(stack, ",") {
		name := strings.TrimSpace(strings.Trim(raw, `"'`))
		if name == "" {
			continue
		}
		lower := strings.ToLower(name)
		switch lower {
		case "monospace", "ui-monospace", "monospace ui", "fixed":
			wantMono = true
			continue
		case "sans-serif", "serif", "system-ui", "system", "ui-sans-serif":
			continue
		}
		if mgr.FamilyExists(name) {
			return name
		}
	}
	if wantMono {
		for _, name := range monoFamilies {
			if name == "monospace" {
				continue // 通用名在 compat 里映射成默认，不作具体匹配
			}
			if mgr.FamilyExists(name) {
				return name
			}
		}
	}
	return ""
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
// family 是节点 font-family 栈的首个可用族名；空串表示默认 sans-serif。
func resolveFont(r rune, size int, bold bool, family string) sk.Font {
	primary := getFont(family, size, bold)
	if covers(primary, r) {
		return primary
	}
	for _, fam := range cjkFamilies {
		f := getFont(fam, size, bold)
		if covers(f, r) {
			return f
		}
	}
	// 主字体族名若来自字体栈，尝试后续候选前先退回 sans-serif，
	// 避免未知族名（MatchFamilyStyle 返回空）导致 tofu。
	if family != "" {
		if fallback := getFont("", size, bold); !fallback.IsNil() && covers(fallback, r) {
			return fallback
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
// family 是节点 font-family 的首个可用族名，空串为默认 sans-serif。
func splitRuns(text string, size int, bold bool, family string) []run {
	if text == "" {
		return nil
	}
	var runs []run
	var curFont sk.Font
	curStart := 0
	first := true
	for i, r := range text {
		f := resolveFont(r, size, bold, family)
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
