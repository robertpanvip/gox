package icon

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/robertpanvip/gox/runtime/canvas"
)

// NumString 把属性里常见的数值写法（含 signal getter）统一成字符串，
// 供组件从 Props 读取 size/stroke-width 等数值属性。与 video.NumString 同语义。
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
	case func() any:
		return NumString(x())
	default:
		return ""
	}
}

// Resolve 把 name 或 src 解析成 SVG 文本。识别顺序：
//  1. 内置图标名（含别名）→ 内置 SVG；
//  2. 完整 SVG 文本（含 <svg）→ 原样；
//  3. data:image/svg+xml[,...] URI（base64 或 URL 编码）→ 解码；
//  4. 以 .svg 结尾的本地路径 → 读文件；
//  5. http(s) URL → 走 imgutil 的通用取流。
func Resolve(name, src string) (string, error) {
	if svg, ok := Lookup(strings.TrimSpace(name)); ok {
		return svg, nil
	}
	s := strings.TrimSpace(src)
	if s == "" {
		if name != "" {
			return "", fmt.Errorf("icon: 未知内置图标 %q", name)
		}
		return "", fmt.Errorf("icon: 未提供 name 或 src")
	}
	if strings.Contains(s, "<svg") {
		return s, nil
	}
	if strings.HasPrefix(strings.ToLower(s), "data:image/svg+xml") {
		return decodeSVGDataURI(s)
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(s)
		if err != nil {
			return "", fmt.Errorf("icon: 拉取 %q: %w", s, err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("icon: 读取 %q: %w", s, err)
		}
		return string(b), nil
	}
	if strings.HasSuffix(strings.ToLower(s), ".svg") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") {
		b, err := os.ReadFile(s)
		if err != nil {
			return "", fmt.Errorf("icon: 读取 %q: %w", s, err)
		}
		return string(b), nil
	}
	return "", fmt.Errorf("icon: 无法识别的 src %q", s)
}

func decodeSVGDataURI(uri string) (string, error) {
	comma := strings.IndexByte(uri, ',')
	if comma < 0 {
		return "", fmt.Errorf("icon: 非法 data URI（缺逗号）")
	}
	meta, payload := uri[5:comma], uri[comma+1:]
	if strings.HasSuffix(strings.ToLower(meta), ";base64") {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
		if err != nil {
			return "", fmt.Errorf("icon: base64 解码: %w", err)
		}
		return string(b), nil
	}
	dec, err := url.QueryUnescape(payload)
	if err != nil {
		return "", fmt.Errorf("icon: URL 解码: %w", err)
	}
	return dec, nil
}

// IconOps 是组件层的主入口：name/src + 选项 → 可编码进 Props["canvas-ops"] 的矢量指令。
func IconOps(name, src string, opt ParseOptions) ([]canvas.Op, error) {
	svg, err := Resolve(name, src)
	if err != nil {
		return nil, err
	}
	return Parse(svg, opt)
}
