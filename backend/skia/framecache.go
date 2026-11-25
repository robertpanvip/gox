package skia

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	sk "github.com/robertpanvip/gox-skia/compat"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 本文件实现帧间缓存 + 脏矩形检测，让"没变化就不重算、只重画变化区域"落在后端。
//
// 背景：Host 是变化驱动的（signal→effect→Draw），但一旦 Draw 被调用，旧实现每帧都会
// 全量 computeLayout（重建 Yoga 树 + CalculateLayout）并全量 paintTree（清屏重画整棵
// 树）。即使只有一处文本变化，也要重算/重画所有节点——滚动场景下开销尤其明显。
//
// 设计（additive，不改 Host 协议、不改 Window 接口、两条绘制路径 Draw/PaintTo 通用）：
//   1. 每帧给整棵树拍一份"内容快照"（nodeStamp），只记录影响布局/绘制的字段；
//   2. 快照与上一帧完全一致 → 整帧复用（不重布局、不清屏、不重画，直接返回上帧 log/hits）；
//   3. 有变化 → 全量重布局（flex 布局局部变化会影响兄弟，必须整体算），但用新旧
//      layoutCache 的差分算出"脏矩形"，只清+画这些区域。
//
// 为什么快照而非依赖 Host 的 Change 流：Skia 后端走 Window.Draw 兼容路径（测试也直接
// 调 Draw），拿不到 Frame.Changes；且 Changes 用 NodeID(string) 定位、缓存用 *Node 指针，
// 需要额外索引。内容快照对调用方完全透明，且对 Draw/PaintTo 两条路径都生效。
//
// 正确性：Node 是原地更新、根节点指针跨帧稳定（见 scene.Instance.Update），因此
// *Node 可作缓存 key；nodeStamp 捕获所有影响 Yoga 布局与 Skia 绘制的属性，任一变化
// 都会让快照不等，触发重算。func 值不计入签名——它求值后的结果已被写回 Props/Text。

// nodeStamp 是一个节点在某帧的"内容快照"：只含影响布局与绘制的字段。
type nodeStamp struct {
	typ      gxscene.NodeType
	kind     string
	text     string
	id       gxscene.NodeID
	propsSig string           // props 的确定性签名（不含纯事件 handler）
	childIDs []gxscene.NodeID // 子节点 ID 序列：结构变化（增删/重排）也会被捕获
}

// snapshotTree 给整棵树拍照，返回按 *Node 索引的快照表 + 整棵树的结构签名。
// 结构签名是"遍历顺序上每个节点 stamp 的拼接"，用于 O(1) 判断整帧是否完全没变。
func snapshotTree(root *gxscene.Node) (map[*gxscene.Node]nodeStamp, string) {
	stamps := make(map[*gxscene.Node]nodeStamp)
	var sb strings.Builder
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n == nil {
			return
		}
		ids := make([]gxscene.NodeID, len(n.Children))
		for i, c := range n.Children {
			ids[i] = c.ID
		}
		st := nodeStamp{
			typ: n.Type, kind: n.Kind, text: n.Text, id: n.ID,
			propsSig: stablePropsSig(n.Props), childIDs: ids,
		}
		stamps[n] = st
		fmt.Fprintf(&sb, "%d|%s|%s|%d|%s|%v;", st.typ, st.kind, st.text, st.id, st.propsSig, ids)
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return stamps, sb.String()
}

// stablePropsSig 生成 props 的确定性签名。跳过 handler（不影响布局/绘制），
// 对 func 值用稳定占位（其求值结果已写回其它 props 字段）。map 按键排序保证确定性。
func stablePropsSig(props map[string]any) string {
	if len(props) == 0 {
		return ""
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		if isHandlerKey(k) {
			continue
		}
		v := props[k]
		fmt.Fprintf(&sb, "%s=%s;", k, stableValueSig(v))
	}
	return sb.String()
}

func isHandlerKey(k string) bool {
	return strings.HasPrefix(k, "on") && len(k) > 2 && k[2] >= 'A' && k[2] <= 'Z'
}

// stableValueSig 把任意值转成确定性字符串。func/chan/unsafe 用类型占位（内容不影响绘制）。
func stableValueSig(v any) string {
	if v == nil {
		return "<nil>"
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return "<opaque:" + rv.Type().String() + ">"
	case reflect.Ptr:
		if rv.IsNil() {
			return "<nil>"
		}
		// 子树指针：用 ID 标识（若指向 Node），避免把内容展开导致递归/抖动。
		if n, ok := v.(*gxscene.Node); ok {
			return fmt.Sprintf("*Node:%d", n.ID)
		}
		if inst, ok := v.(*gxscene.Instance); ok {
			return fmt.Sprintf("*Instance:%d", inst.Root().ID)
		}
		return fmt.Sprintf("%s:%p", rv.Type(), v)
	case reflect.Map:
		// 递归排序，保证 map 序列化稳定。
		keys := make([]string, 0, rv.Len())
		for _, k := range rv.MapKeys() {
			keys = append(keys, stableValueSig(k.Interface()))
		}
		sort.Strings(keys)
		var sb strings.Builder
		sb.WriteString("map{")
		for _, k := range keys {
			sb.WriteString(k)
			sb.WriteByte(':')
		}
		sb.WriteByte('}')
		return sb.String()
	case reflect.Slice, reflect.Array:
		var sb strings.Builder
		sb.WriteByte('[')
		for i := 0; i < rv.Len(); i++ {
			sb.WriteString(stableValueSig(rv.Index(i).Interface()))
			sb.WriteByte(',')
		}
		sb.WriteByte(']')
		return sb.String()
	case reflect.Struct:
		// 复合样式结构全由标量组成，%#v 确定性且完整。
		return fmt.Sprintf("%#v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// FrameCache 是跨帧持有的缓存。挂在 skia.Window 上；PaintTo 的带缓存版本由调用方持有。
type FrameCache struct {
	valid     bool
	root      *gxscene.Node
	w, h      float32
	stamps    map[*gxscene.Node]nodeStamp
	structSig string      // 整棵树的快照拼接，用于 O(1) 无变化判断
	layout    layoutCache // 上一帧布局结果
	log       DrawLog     // 上一帧绘制指令（整帧复用时返回）
	hits      []HitBox    // 上一帧命中盒（整帧复用时返回）
	paints    int         // 实际发生重绘的帧数（观测指标）
	reuses    int         // 整帧复用的帧数（观测指标）

	// lastMeasured 是最近一次真实布局导出的尺寸回流（node ID → W/H）。
	// 由 renderCached 在布局后填入，Window.Draw 在释放 canvas 锁后投递给 Host，
	// 避免 PublishMeasured 回调写 signal → 重入 Draw 造成锁互等。
	lastMeasured []gxscene.Measured
}

// shouldReuse 判断本帧能否整体复用上一帧：根/尺寸不变且内容快照一致。
func (c *FrameCache) shouldReuse(root *gxscene.Node, w, h float32, structSig string) bool {
	return c.valid && c.root == root && c.w == w && c.h == h && c.structSig == structSig
}

// canReuseFast 判断"增量帧快速复用"：调用方已确认本帧无 Host Change、无动画、
// 且非 FullSync。此时场景树内容必然未变（所有画面变化都经 Change 流到达，
// 见 change.go；命令式写回也会 record），无需再 O(N) 拍快照比对——根指针与
// 尺寸不变即可直接复用上一帧的 log/hits 与画布。
//
// 这是 skia 接入 FramePresenter 的核心收益：无变化的帧从 O(N) snapshotTree
// 降到 O(1)。老 Draw 路径不传 changeHint，仍走全量快照，行为完全不变。
func (c *FrameCache) canReuseFast(root *gxscene.Node, w, h float32) bool {
	return c.valid && c.root == root && c.w == w && c.h == h
}

// reuseFast 执行一次快速复用：只更新统计、返回上一帧的 log/hits 副本。画布不动。
func (c *FrameCache) reuseFast() (log DrawLog, hits []HitBox) {
	c.reuses++
	log = append(DrawLog(nil), c.log...)
	hits = append([]HitBox(nil), c.hits...)
	return
}

// dirtyRect 是一块需要重绘的屏幕矩形（已含阴影/描边外扩）。
type dirtyRect struct{ x, y, w, h float32 }

// union 返回包含两个矩形的最小矩形。
func (r dirtyRect) union(o dirtyRect) dirtyRect {
	x0 := minF(r.x, o.x)
	y0 := minF(r.y, o.y)
	x1 := maxF(r.x+r.w, o.x+o.w)
	y1 := maxF(r.y+r.h, o.y+o.h)
	return dirtyRect{x0, y0, x1 - x0, y1 - y0}
}

func (r dirtyRect) intersects(o dirtyRect) bool {
	return r.x < o.x+o.w && o.x < r.x+r.w && r.y < o.y+o.h && o.y < r.y+r.h
}

// diffLayout 对比新旧布局，返回需要重绘的脏矩形列表。
//
// 覆盖的变化来源：
//   - 节点新增/删除：新矩形 / 旧矩形（删除的用上一帧矩形擦除）
//   - 节点矩形变化（X/Y/W/H）：旧矩形 ∪ 新矩形（防止残影）
//   - 裁剪矩形变化（滚动 scrollTop）：裁剪区与子节点区都要纳入
//   - 阴影/描边外扩：矩形按 shadow blur+spread+offset 扩大，避免边缘残影
//
// 由于节点是原地更新、指针稳定，未变化节点的 layoutBox 在新旧 cache 里完全相同，
// 直接跳过——这正是脏矩形能只重画一小条的关键。
func diffLayout(prev, next layoutCache, prevRoot, nextRoot *gxscene.Node) []dirtyRect {
	if !prev.valid() {
		// 无上一帧布局：整屏脏。用 next 全部节点的包围盒。
		return []dirtyRect{boundsOf(next)}
	}
	var rects []dirtyRect
	// 新树里所有节点：只要矩形/裁剪与上一帧不同就纳入。
	if nextRoot != nil {
		var walk func(*gxscene.Node)
		walk = func(n *gxscene.Node) {
			if n == nil || isOverlay(n) {
				return // 浮层由 paintOverlays 单独处理，后面统一兜底
			}
			nb, nok := next[n]
			ob, ook := prev[n]
			if !nok {
				for _, c := range n.Children {
					walk(c)
				}
				return
			}
			if !ook || boxChanged(ob, nb) {
				rects = append(rects, boxDirtyRect(n, ob, nb))
			}
			for _, c := range n.Children {
				walk(c)
			}
		}
		walk(nextRoot)
	}
	// 被删除的节点：上一帧有、这一帧没有 → 用旧矩形擦除。
	for n, ob := range prev {
		if _, ok := next[n]; !ok {
			rects = append(rects, boxDirtyRect(n, ob, layoutBox{}))
		}
	}
	// 浮层（popover/scrim/modal/portal）：数量少，任一变化就整屏兜底，避免层级错误。
	if overlayChanged(prev, next) {
		rects = append(rects, boundsOf(next))
	}
	return mergeRects(rects)
}

func (c layoutCache) valid() bool { return len(c) > 0 }

// boxChanged 判断布局盒的几何是否变化（含裁剪区）。
func boxChanged(a, b layoutBox) bool {
	return a.X != b.X || a.Y != b.Y || a.W != b.W || a.H != b.H ||
		a.HasClip != b.HasClip || a.ClipX != b.ClipX || a.ClipY != b.ClipY ||
		a.ClipW != b.ClipW || a.ClipH != b.ClipH
}

// boxDirtyRect 取节点新旧矩形的并集，并按阴影/描边外扩。
func boxDirtyRect(n *gxscene.Node, old, cur layoutBox) dirtyRect {
	or := boxToRect(old)
	cr := boxToRect(cur)
	var d dirtyRect
	if old.HasClip || cur.HasClip {
		// 有裁剪的容器：脏区至少覆盖裁剪矩形本身（滚动条带进出）。
		cb := cur
		if !cb.HasClip {
			cb = old
		}
		if cb.HasClip {
			d = dirtyRect{cb.ClipX, cb.ClipY, cb.ClipW, cb.ClipH}
		}
	}
	if cur.W > 0 && cur.H > 0 {
		d = d.union(or).union(cr)
	} else {
		d = d.union(or)
	}
	// 阴影/描边外扩：阴影可能画在矩形之外，扩大脏区避免残影。
	if n != nil {
		s := parseStyle(n.Props)
		if s.hasShadow {
			pad := s.shadow.blur + s.shadow.spread + absF(s.shadow.offsetX) + absF(s.shadow.offsetY) + 4
			d = dirtyRect{d.x - pad, d.y - pad, d.w + 2*pad, d.h + 2*pad}
		}
		if s.hasBorder && s.borderWidth > 0 {
			pad := s.borderWidth + 1
			d = dirtyRect{d.x - pad, d.y - pad, d.w + 2*pad, d.h + 2*pad}
		}
	}
	return d
}

func boxToRect(b layoutBox) dirtyRect {
	if b.W <= 0 || b.H <= 0 {
		return dirtyRect{}
	}
	return dirtyRect{b.X, b.Y, b.W, b.H}
}

func absF(a float32) float32 {
	if a < 0 {
		return -a
	}
	return a
}

func boundsOf(c layoutCache) dirtyRect {
	var d dirtyRect
	first := true
	for _, b := range c {
		if b.W <= 0 || b.H <= 0 {
			continue
		}
		r := boxToRect(b)
		if first {
			d = r
			first = false
		} else {
			d = d.union(r)
		}
	}
	return d
}

// overlayChanged 用浮层节点数量与 ID 判断浮层集合是否变化（变化时整屏兜底）。
func overlayChanged(prev, next layoutCache) bool {
	var pn, nn int
	var pid, nid gxscene.NodeID
	for n := range prev {
		if isOverlay(n) {
			pn++
			pid += n.ID
		}
	}
	for n := range next {
		if isOverlay(n) {
			nn++
			nid += n.ID
		}
	}
	return pn != nn || pid != nid
}

// mergeRects 合并相交/相邻的脏矩形，减少重绘遍数。合并次数有上限，避免退化。
func mergeRects(in []dirtyRect) []dirtyRect {
	if len(in) == 0 {
		return nil
	}
	// 去掉零面积。
	rs := in[:0]
	for _, r := range in {
		if r.w > 0 && r.h > 0 {
			rs = append(rs, r)
		}
	}
	// 迭代合并相交矩形。
	for changed := true; changed; {
		changed = false
		for i := 0; i < len(rs) && !changed; i++ {
			for j := i + 1; j < len(rs); j++ {
				if rs[i].intersects(rs[j]) {
					rs[i] = rs[i].union(rs[j])
					rs = append(rs[:j], rs[j+1:]...)
					changed = true
					break
				}
			}
		}
	}
	return rs
}

// renderCached 是帧缓存的核心编排：判断整帧复用、算脏区、按脏区局部重绘、更新缓存。
// canvas 的清屏/裁剪由本函数管理；log/hits 始终是全树结果（绘制原语被 clip 限制，
// 但 log 记录与命中记录不受影响），因此对调用方与测试完全透明。
func renderCached(c *FrameCache, canvas sk.Canvas, root *gxscene.Node, w, h float32, fonts map[int]sk.Font) (log DrawLog, hits []HitBox) {
	return renderCachedInner(c, canvas, root, w, h, fonts, false)
}

// renderCachedKnownChange 是"调用方已确认本帧有变化"的快通道，供 FramePresenter
// 路径（桌面 glWindow）使用。宿主已用 Frame.Changes 判断了有无变化，无需再 O(N)
// 拍快照签名去猜——跳过 snapshotTree 与整帧复用判定，直接布局+算脏区+绘制。
// 省下的快照开销在纯文本变化（脏区为 0、仍全量重画）场景下尤其关键，让这条
// 最高频路径不比无缓存全量绘制慢。
//
// 因未拍快照，缓存的 stamps/structSig 不更新并置为恒不匹配，避免后续误命中
// 整帧复用；桌面的无变化帧由 Present 的帧级短路拦截，不依赖这里的复用判定。
func renderCachedKnownChange(c *FrameCache, canvas sk.Canvas, root *gxscene.Node, w, h float32, fonts map[int]sk.Font) (log DrawLog, hits []HitBox) {
	return renderCachedInner(c, canvas, root, w, h, fonts, true)
}

// renderCachedInner 是帧缓存编排的实际实现。knownChange=true 时跳过快照与复用判定。
func renderCachedInner(c *FrameCache, canvas sk.Canvas, root *gxscene.Node, w, h float32, fonts map[int]sk.Font, knownChange bool) (log DrawLog, hits []HitBox) {
	var stamps map[*gxscene.Node]nodeStamp
	var sig string
	if !knownChange {
		stamps, sig = snapshotTree(root)
		if c.shouldReuse(root, w, h, sig) {
			c.reuses++
			// 整帧复用：画布不动，返回上一帧的 log/hits 副本。
			log = append(DrawLog(nil), c.log...)
			hits = append([]HitBox(nil), c.hits...)
			return
		}
	}

	// 有变化：全量重算布局（flex 局部变化会影响兄弟，无法只算子树）。
	newLayout := computeLayout(root, w, h, skiaFontMeasurer())
	// 导出本帧实测尺寸供组件回流（整帧复用帧沿用上一帧值，无需重发）。
	c.lastMeasured = measuredFromLayout(newLayout)

	p := &painter{canvas: canvas, width: w, height: h, fonts: fonts, log: &log, hits: &hits, layout: newLayout}

	dirty := diffLayout(c.layout, newLayout, c.root, root)
	if len(dirty) == 0 {
		// 内容变了但几何没变（如纯颜色）：仍要重画整屏（绘制内容不同）。
		canvas.Clear(bgWhite())
		p.paintWithLayout(root)
	} else if dirtyCoversMost(dirty, w, h) || len(dirty) > 4 || !c.layout.valid() {
		// 首帧、脏区较大或碎片化（>4 块）：多遍遍历的固定开销超过局部收益，
		// 退化全量清屏重画。只有小而集中的脏区才走局部路径。
		canvas.Clear(bgWhite())
		p.paintWithLayout(root)
	} else {
		// 局部重绘：逐脏区裁剪 + 背景色覆盖 + 只画与脏区相交的节点。
		bgPaint := sk.NewPaint()
		bgPaint.SetColor(bgWhite())
		bgPaint.SetAntiAlias(false)
		for i := range dirty {
			d := dirty[i]
			r := sk.RectMakeXYWH(d.x, d.y, d.w, d.h)
			canvas.Save()
			canvas.ClipRect(r, sk.ClipOpIntersect, false)
			canvas.DrawRect(r, bgPaint) // 用背景色覆盖脏区（模拟局部 Clear）
			p.clip = &dirty[i]
			if i == 0 {
				p.paintWithLayout(root) // 第一个脏区：记录完整 log/hits
			} else {
				// 后续脏区：丢弃 log/hits（首遍已记录完整），只做像素绘制。
				var discardLog DrawLog
				var discardHits []HitBox
				savedLog, savedHits := p.log, p.hits
				p.log, p.hits = &discardLog, &discardHits
				p.paintWithLayout(root)
				p.log, p.hits = savedLog, savedHits
			}
			p.clip = nil
			canvas.Restore()
		}
		bgPaint.Delete()
	}

	c.valid = true
	c.root = root
	c.w, c.h = w, h
	c.layout = newLayout
	c.log = append(DrawLog(nil), log...)
	c.hits = append([]HitBox(nil), hits...)
	c.paints++
	if !knownChange {
		c.stamps = stamps
		c.structSig = sig
	}
	// knownChange 时保留旧 stamps/sig（本帧未拍，不能覆盖）。旧签名与当前树可能
	// 已不匹配，若后续有普通路径调用会重新拍快照比对，不会错误复用。
	return
}

// dirtyCoversMost 判断脏区总面积是否超过画布 25%——此时多段 clip 的固定开销
// （逐区遍历整棵树 + 背景填充）已接近或超过全量重画，退化全量更划算。
func dirtyCoversMost(rs []dirtyRect, w, h float32) bool {
	var area float32
	for _, r := range rs {
		area += maxF(0, r.w) * maxF(0, r.h)
	}
	return area > w*h*0.25
}

func bgWhite() sk.Color { return sk.ColorSetARGB(255, 255, 255, 255) }

// CacheStats 返回缓存命中统计（观测面）。
func (c *FrameCache) CacheStats() (reuses, paints int) {
	return c.reuses, c.paints
}
