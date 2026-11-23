package rustbackend

import (
	"encoding/binary"
	"fmt"
	"strconv"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
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
