package rustbackend

import (
	"encoding/binary"
	"fmt"
	"strconv"

	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// 二进制协议常量（与 PROTOCOL.md / Rust binprotocol.rs 对齐）
const (
	binMagic    = 0x31585247 // "GRX1"
	headerSize  = 24
	nodeSize    = 32
	propSize    = 12
	eventSize   = 12
	typeFrag    = 0
	typeDiv     = 1
	typeText    = 2
)

// binFrame 是序列化过程中的中间结构。
type binFrame struct {
	nodes  []binNode
	props  []binProp
	events []binEvent
	pool   *strPool
}

type binNode struct {
	typ        byte
	flags      byte
	kindOff    uint32
	idOff      uint32
	textOff    uint32
	propStart  uint32
	propCount  uint16
	childCount uint16
	parent     uint32
	nodeID     uint64 // GRX3 直传（GRX1 编码不使用）
}

type binProp struct{ keyOff, valOff uint32 }
type binEvent struct{ nodeIndex, nameOff uint32 }

// strPool 字符串池：去重 + u16 长度前缀。
type strPool struct {
	buf   []byte
	index map[string]uint32
}

func newStrPool() *strPool {
	return &strPool{index: map[string]uint32{}}
}

func (p *strPool) add(s string) uint32 {
	if off, ok := p.index[s]; ok {
		return off
	}
	off := uint32(len(p.buf))
	var lenBuf [2]byte
	binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(s)))
	p.buf = append(p.buf, lenBuf[:]...)
	p.buf = append(p.buf, s...)
	p.index[s] = off
	return off
}

func binPropValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprint(v)
	}
}

// serializeFrameBin 把场景树序列化成二进制帧。
func serializeFrameBin(root *gxscene.Node, width, height uint32) ([]byte, error) {
	pool := newStrPool()
	bf := &binFrame{pool: pool}

	var build func(n *gxscene.Node, parent uint32) int
	build = func(n *gxscene.Node, parent uint32) int {
		if n == nil {
			return -1
		}
		idx := len(bf.nodes)
		bn := binNode{parent: parent}

		switch n.Type {
		case gxscene.NodeText:
			bn.typ = typeText
			bn.textOff = pool.add(n.Text)
		case gxscene.NodeDiv:
			bn.typ = typeDiv
			bn.kindOff = pool.add(n.Kind)
		default:
			bn.typ = typeFrag
			bn.kindOff = pool.add(n.Kind)
		}
		bn.idOff = pool.add(n.ID.String())

		// 属性：字符串池去重
		bn.propStart = uint32(len(bf.props))
		count := 0
		for k, v := range n.Props {
			bf.props = append(bf.props, binProp{
				keyOff: pool.add(k),
				valOff: pool.add(binPropValue(v)),
			})
			count++
		}
		bn.propCount = uint16(count)

		// 事件
		if len(n.Handlers) > 0 {
			bn.flags |= 0x1
			for k := range n.Handlers {
				bf.events = append(bf.events, binEvent{
					nodeIndex: uint32(idx),
					nameOff:   pool.add(k),
				})
			}
		}

		// 先占位，递归后回填 child_count
		bf.nodes = append(bf.nodes, bn)
		childCount := 0
		for _, c := range n.Children {
			if c != nil {
				build(c, uint32(idx))
				childCount++
			}
		}
		bf.nodes[idx].childCount = uint16(childCount)
		return idx
	}
	build(root, 0xFFFFFFFF)

	// 组装 buffer
	total := headerSize + len(bf.nodes)*nodeSize + len(bf.props)*propSize +
		len(bf.events)*eventSize + len(pool.buf)
	buf := make([]byte, total)
	le := binary.LittleEndian
	le.PutUint32(buf[0:4], binMagic)
	le.PutUint32(buf[4:8], width)
	le.PutUint32(buf[8:12], height)
	le.PutUint32(buf[12:16], uint32(len(bf.nodes)))
	le.PutUint32(buf[16:20], uint32(len(bf.props)))
	le.PutUint32(buf[20:24], uint32(len(pool.buf)))

	off := headerSize
	for _, n := range bf.nodes {
		buf[off] = n.typ
		buf[off+1] = n.flags
		le.PutUint16(buf[off+2:off+4], 0)
		le.PutUint32(buf[off+4:off+8], n.kindOff)
		le.PutUint32(buf[off+8:off+12], n.idOff)
		le.PutUint32(buf[off+12:off+16], n.textOff)
		le.PutUint32(buf[off+16:off+20], n.propStart)
		le.PutUint16(buf[off+20:off+22], n.propCount)
		le.PutUint16(buf[off+22:off+24], n.childCount)
		le.PutUint32(buf[off+24:off+28], n.parent)
		le.PutUint32(buf[off+28:off+32], 0)
		off += nodeSize
	}
	for _, p := range bf.props {
		le.PutUint32(buf[off:off+4], p.keyOff)
		le.PutUint32(buf[off+4:off+8], p.valOff)
		le.PutUint32(buf[off+8:off+12], 0)
		off += propSize
	}
	for _, e := range bf.events {
		le.PutUint32(buf[off:off+4], e.nodeIndex)
		le.PutUint32(buf[off+4:off+8], e.nameOff)
		le.PutUint32(buf[off+8:off+12], 0)
		off += eventSize
	}
	copy(buf[off:], pool.buf)
	return buf, nil
}

// ---------- GRX3：NodeID uint64 直传 ----------
//
// GRX3（magic 0x33585247）：节点结构 40 字节，尾部新增 8 字节 node_id u64
// 直传——Go runtime 的 Node.ID 本就是 uint64（gxscene.NodeID），旧协议把
// 它字符串化进池纯属传输负担。GRX3 帧不再携带 id 字符串（id 池槽指向空串），
// Rust 侧 SceneNode.nid 直接取整数身份，id 兜底匹配仍可用（空串跳过）。

const (
	binMagicV3  = 0x33585247 // "GRX3"
	nodeSizeV3  = 40
)

// serializeFrameBinV3 把场景树序列化成 GRX3 二进制帧（节点 40 字节 + u64 node_id）。
func serializeFrameBinV3(root *gxscene.Node, width, height uint32) ([]byte, error) {
	pool := newStrPool()
	bf := &binFrame{pool: pool}

	var build func(n *gxscene.Node, parent uint32) int
	build = func(n *gxscene.Node, parent uint32) int {
		if n == nil {
			return -1
		}
		idx := len(bf.nodes)
		bn := binNode{parent: parent}

		switch n.Type {
		case gxscene.NodeText:
			bn.typ = typeText
			bn.textOff = pool.add(n.Text)
		case gxscene.NodeDiv:
			bn.typ = typeDiv
			bn.kindOff = pool.add(n.Kind)
		default:
			bn.typ = typeFrag
			bn.kindOff = pool.add(n.Kind)
		}
		// GRX3：id 槽指向空串（整数身份在尾部 u64 字段直传）。
		bn.idOff = pool.add("")
		bn.nodeID = uint64(n.ID)

		bn.propStart = uint32(len(bf.props))
		count := 0
		for k, v := range n.Props {
			bf.props = append(bf.props, binProp{
				keyOff: pool.add(k),
				valOff: pool.add(binPropValue(v)),
			})
			count++
		}
		bn.propCount = uint16(count)

		if len(n.Handlers) > 0 {
			bn.flags |= 0x1
			for k := range n.Handlers {
				bf.events = append(bf.events, binEvent{
					nodeIndex: uint32(idx),
					nameOff:   pool.add(k),
				})
			}
		}

		bf.nodes = append(bf.nodes, bn)
		childCount := 0
		for _, c := range n.Children {
			if c != nil {
				build(c, uint32(idx))
				childCount++
			}
		}
		bf.nodes[idx].childCount = uint16(childCount)
		return idx
	}
	build(root, 0xFFFFFFFF)

	total := headerSize + len(bf.nodes)*nodeSizeV3 + len(bf.props)*propSize +
		len(bf.events)*eventSize + len(pool.buf)
	buf := make([]byte, total)
	le := binary.LittleEndian
	le.PutUint32(buf[0:4], binMagicV3)
	le.PutUint32(buf[4:8], width)
	le.PutUint32(buf[8:12], height)
	le.PutUint32(buf[12:16], uint32(len(bf.nodes)))
	le.PutUint32(buf[16:20], uint32(len(bf.props)))
	le.PutUint32(buf[20:24], uint32(len(pool.buf)))

	off := headerSize
	for _, n := range bf.nodes {
		buf[off] = n.typ
		buf[off+1] = n.flags
		le.PutUint16(buf[off+2:off+4], 0)
		le.PutUint32(buf[off+4:off+8], n.kindOff)
		le.PutUint32(buf[off+8:off+12], n.idOff)
		le.PutUint32(buf[off+12:off+16], n.textOff)
		le.PutUint32(buf[off+16:off+20], n.propStart)
		le.PutUint16(buf[off+20:off+22], n.propCount)
		le.PutUint16(buf[off+22:off+24], n.childCount)
		le.PutUint32(buf[off+24:off+28], n.parent)
		le.PutUint32(buf[off+28:off+32], 0)
		// GRX3 新增：尾部 8 字节 node_id 直传。
		le.PutUint64(buf[off+32:off+40], n.nodeID)
		off += nodeSizeV3
	}
	for _, p := range bf.props {
		le.PutUint32(buf[off:off+4], p.keyOff)
		le.PutUint32(buf[off+4:off+8], p.valOff)
		le.PutUint32(buf[off+8:off+12], 0)
		off += propSize
	}
	for _, e := range bf.events {
		le.PutUint32(buf[off:off+4], e.nodeIndex)
		le.PutUint32(buf[off+4:off+8], e.nameOff)
		le.PutUint32(buf[off+8:off+12], 0)
		off += eventSize
	}
	copy(buf[off:], pool.buf)
	return buf, nil
}
