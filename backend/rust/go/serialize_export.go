package rustbackend

import gxscene "github.com/robertpanvip/gox-core/runtime/scene"

// 导出序列化器供其他后端宿主复用（如 Android 原生渲染接入：Go 侧只做
// GRX3 编码，绘制提交走平台专属 ABI）。与桌面 Present 内部使用同一份编码。
func SerializeFrameBin(root *gxscene.Node, width, height uint32) ([]byte, error) {
	return serializeFrameBin(root, width, height)
}

func SerializeFrameBinV3(root *gxscene.Node, width, height uint32) ([]byte, error) {
	return serializeFrameBinV3(root, width, height)
}
