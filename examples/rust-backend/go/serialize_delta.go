package rustbackend

import (
	"encoding/binary"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 增量动作协议 GRX2：只传变化字段 / 结构动作，Rust 侧按 nodeID 打补丁到本地镜像树。
//
// 帧布局：
//
//	┌──────────┬───────────┬────────────┬──────────────┐
//	│Header 16 │ Action[]  │ StringPool │ SubtreeBlobs │
//	└──────────┴───────────┴────────────┴──────────────┘
//
// 结构动作携带的 *Node 子树用「内嵌迷你 GRX1 帧」序列化后追加到 SubtreeBlobs 区。
// blob 区长度 = len(buf) - (pool_off + pool_len)，Rust 侧按此推导，Header 无需新增字段。
const (
	deltaMagic  = 0x32585247 // "GRX2"
	deltaHeader = 16
	actionSize  = 24

	actSetAttr        = 0
	actSetText        = 1
	actSetHandle      = 2
	actInsertBefore   = 3 // nodeID=父, childID=blob off, beforeID=锚点ID, val=blob len
	actRemoveChild    = 4 // nodeID=父, childID=被移除ID
	actMoveChild      = 5 // nodeID=父, childID=移动ID, beforeID=锚点ID
	actReplaceChildren = 6 // nodeID=父, childID=blob off, val=blob len（整棵子树重挂）
)

// action 是单个动作的中间表示。所有字符串进 pool，blob 是内嵌子树帧。
type deltaAction struct {
	kind                                   byte
	nodeID, childID, beforeID, key, val    uint32
	blob                                   []byte // 仅结构动作；非 nil 时追加到 blob 区并回填 childID=off, val=len
}

// findNodeByID 在当前场景树中按稳定 id 查找节点（供 ReplaceChildren/InsertBefore
// 在 Change.Value 为 nil 时从最新树回填子树内容）。
func findNodeByID(root *gxscene.Node, id string) *gxscene.Node {
	if root == nil {
		return nil
	}
	if root.ID == id {
		return root
	}
	for _, c := range root.Children {
		if found := findNodeByID(c, id); found != nil {
			return found
		}
	}
	return nil
}

// subtreeOf 取 Change 携带的子树：优先用 Value(*Node)，为空则从最新 root 按 ChildID 查。
func subtreeOf(c gxscene.Change, root *gxscene.Node) *gxscene.Node {
	if sub, ok := c.Value.(*gxscene.Node); ok && sub != nil {
		return sub
	}
	return findNodeByID(root, c.ChildID)
}

// serializeDelta 把 changes 序列化成 GRX2 动作帧。
// 返回 (bytes, ok)：ok=false 表示存在无法编码的动作，需回退 GRX1 全量快照。
func serializeDelta(changes []gxscene.Change, root *gxscene.Node, width, height uint32) ([]byte, bool) {
	if len(changes) == 0 {
		return nil, true // 无变化
	}
	pool := newStrPool()
	var acts []deltaAction
	var blobs []byte

	addBlob := func(sub *gxscene.Node) (off, length uint32, ok bool) {
		if sub == nil {
			return 0, 0, false
		}
		buf, err := serializeFrameBin(sub, width, height)
		if err != nil || len(buf) == 0 {
			return 0, 0, false
		}
		off = uint32(len(blobs))
		blobs = append(blobs, buf...)
		length = uint32(len(buf))
		return off, length, true
	}

	for _, c := range changes {
		switch c.Kind {
		case gxscene.ChangeSetAttr:
			acts = append(acts, deltaAction{
				kind: actSetAttr, nodeID: pool.add(c.NodeID),
				key: pool.add(c.Key), val: pool.add(binPropValue(c.Value)),
			})
		case gxscene.ChangeSetText:
			s, _ := c.Value.(string)
			acts = append(acts, deltaAction{
				kind: actSetText, nodeID: pool.add(c.NodeID), val: pool.add(s),
			})
		case gxscene.ChangeSetHandler:
			acts = append(acts, deltaAction{
				kind: actSetHandle, nodeID: pool.add(c.NodeID), key: pool.add(c.Key),
			})
		case gxscene.ChangeInsertBefore:
			// Value 是完整子树；BeforeID 为空表示追加末尾。
			// 注意：insert 后子树是新对象，root 中已包含它，故 Value=nil 时可按 ChildID 查。
			off, n, ok := addBlob(subtreeOf(c, root))
			if !ok {
				return nil, false
			}
			acts = append(acts, deltaAction{
				kind: actInsertBefore, nodeID: pool.add(c.NodeID),
				childID: off, beforeID: pool.add(c.BeforeID), val: n,
			})
		case gxscene.ChangeReplaceChildren:
			// host 通常传 nil（"后端按 NodeID 重读 children"）；从最新 root 取该节点整棵子树。
			sub := findNodeByID(root, c.NodeID)
			off, n, ok := addBlob(sub)
			if !ok {
				return nil, false
			}
			acts = append(acts, deltaAction{
				kind: actReplaceChildren, nodeID: pool.add(c.NodeID),
				childID: off, val: n,
			})
		case gxscene.ChangeRemoveChild:
			acts = append(acts, deltaAction{
				kind: actRemoveChild, nodeID: pool.add(c.NodeID),
				childID: pool.add(c.ChildID),
			})
		case gxscene.ChangeMoveChild:
			acts = append(acts, deltaAction{
				kind: actMoveChild, nodeID: pool.add(c.NodeID),
				childID: pool.add(c.ChildID), beforeID: pool.add(c.BeforeID),
			})
		default:
			return nil, false
		}
	}

	total := deltaHeader + len(acts)*actionSize + len(pool.buf) + len(blobs)
	buf := make([]byte, total)
	le := binary.LittleEndian
	le.PutUint32(buf[0:4], deltaMagic)
	le.PutUint32(buf[4:8], width)
	le.PutUint32(buf[8:12], uint32(len(acts)))
	le.PutUint32(buf[12:16], uint32(len(pool.buf)))

	off := deltaHeader
	for _, a := range acts {
		buf[off] = a.kind
		le.PutUint32(buf[off+4:off+8], a.nodeID)
		le.PutUint32(buf[off+8:off+12], a.childID)
		le.PutUint32(buf[off+12:off+16], a.beforeID)
		le.PutUint32(buf[off+16:off+20], a.key)
		le.PutUint32(buf[off+20:off+24], a.val)
		off += actionSize
	}
	copy(buf[off:], pool.buf)
	off += len(pool.buf)
	copy(buf[off:], blobs)
	return buf, true
}
