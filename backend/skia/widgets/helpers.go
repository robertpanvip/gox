package widgets

import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 这一组小工具把 gxscene.Op 的构造压缩成一行，控件模板因此能集中在 widgets.go
// 里保持可读。它们只是 Op 字面量的语法糖，不改任何 host 协议。

func el(kind string) gxscene.Op { return gxscene.Op{Code: gxscene.OpElem, Arg: kind} }
func closeEl() gxscene.Op       { return gxscene.Op{Code: gxscene.OpClose} }

func attr(k, v string) gxscene.Op { return gxscene.Op{Code: gxscene.OpAttr, Key: k, Arg: v} }

func dynAttr(k string, slot int) gxscene.Op {
	return gxscene.Op{Code: gxscene.OpDynAttr, Key: k, Slot: slot}
}

func dynChild(slot int) gxscene.Op { return gxscene.Op{Code: gxscene.OpDynChild, Slot: slot} }

func event(name string, slot int) gxscene.Op {
	return gxscene.Op{Code: gxscene.OpEvent, Key: name, Slot: slot}
}

// findByID 在当前场景树里按 host 生成的稳定 ID 找回节点。
// 输入焦点用节点 ID 记录而不是裸指针，因此结构插槽重建子树后仍能定位到新节点。
func findByID(n *gxscene.Node, id string) *gxscene.Node {
	if n == nil || id == "" {
		return nil
	}
	if n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if hit := findByID(c, id); hit != nil {
			return hit
		}
	}
	return nil
}
