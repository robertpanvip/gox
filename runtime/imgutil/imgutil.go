// Package imgutil 提供后端无关的图片来源解析：把 <img src> 的字符串统一解析为
// 标准库 image.Image。skia 后端与 Rust 后端的 Go adapter 都依赖核心 gox，因此
// 可共用本包，保证两端对图片来源的语义完全一致。
//
// 支持的 src 形式：
//   - data URI:  data:image/png;base64,xxxx   /  data:image/jpeg;base64,xxxx
//     （也支持非 base64 的 data URI，按原文 byte 解码）
//   - 本地路径:  ./logo.png  /  /abs/path.png  /  C:\...（由 os.Open 处理）
//   - http(s):   https://example.com/a.png（有超时保护，默认 5s）
//
// 本包只依赖 Go 标准库，不依赖任何后端绘制代码，也不引入新 module。
package imgutil

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	// 注册 PNG/JPEG/GIF/WebP 解码器（空白导入触发 init）
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultHTTPTimeout 是远程图片拉取的默认超时，防止后端绘制被慢网络卡住。
var DefaultHTTPTimeout = 5 * time.Second

// Resolve 把 src 解析为 image.Image。
// 对 data URI 与本地文件优先；http(s) 走带超时的 GET。
func Resolve(src string) (image.Image, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("imgutil: 空 src")
	}
	var (
		reader io.Reader
		closer io.Closer
	)
	switch {
	case strings.HasPrefix(src, "data:"):
		data, err := decodeDataURI(src)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	case strings.HasPrefix(src, "http://"), strings.HasPrefix(src, "https://"):
		client := &http.Client{Timeout: DefaultHTTPTimeout}
		resp, err := client.Get(src)
		if err != nil {
			return nil, fmt.Errorf("imgutil: 拉取 %q: %w", src, err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("imgutil: %q 返回 HTTP %d", src, resp.StatusCode)
		}
		reader, closer = resp.Body, resp.Body
	default:
		f, err := os.Open(src)
		if err != nil {
			return nil, fmt.Errorf("imgutil: 打开 %q: %w", src, err)
		}
		reader, closer = f, f
	}
	if closer != nil {
		defer closer.Close()
	}
	img, _, err := image.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("imgutil: 解码 %q: %w", trunc(src, 60), err)
	}
	return img, nil
}

// decodeDataURI 解析 data:[<mediatype>][;base64],<data>。
func decodeDataURI(uri string) ([]byte, error) {
	comma := strings.IndexByte(uri, ',')
	if comma < 0 {
		return nil, fmt.Errorf("imgutil: 非法 data URI（缺逗号）")
	}
	meta, payload := uri[5:comma], uri[comma+1:]
	if strings.HasSuffix(strings.ToLower(meta), ";base64") {
		dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
		if err != nil {
			return nil, fmt.Errorf("imgutil: base64 解码: %w", err)
		}
		return dec, nil
	}
	// 非 base64：URL 解码百分号转义。
	return []byte(unescape(payload)), nil
}

// unescape 做最小百分号解码（%XX → byte），避免引入 net/url 的额外行为。
func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			var hi, lo byte
			if hex(s[i+1], &hi) && hex(s[i+2], &lo) {
				b.WriteByte(hi<<4 | lo)
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func hex(c byte, out *byte) bool {
	switch {
	case c >= '0' && c <= '9':
		*out = c - '0'
	case c >= 'a' && c <= 'f':
		*out = c - 'a' + 10
	case c >= 'A' && c <= 'F':
		*out = c - 'A' + 10
	default:
		return false
	}
	return true
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
