package editor

import (
	"bytes"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// 编码常量：Buffer.Encoding 取值。
const (
	EncUTF8 = "UTF-8"
	EncGBK  = "GBK"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Decode 把磁盘原始字节解码为内部 UTF-8 文本，并返回检测到的编码名。
// 规则：
//   - 合法 UTF-8（含 ASCII）→ 原样，UTF-8；
//   - 否则尝试 GBK 解码（中文 Windows 下最常见的非 UTF-8 编码），
//     非法字节替换为 U+FFFD，标记 GBK。
// 打开 GBK 文件时若不做这一步，中文注释会变成乱码——"打开字符混乱"的
// 另一个来源。
func Decode(raw []byte) (text, encoding string) {
	raw = bytes.TrimPrefix(raw, utf8BOM)
	if utf8.Valid(raw) {
		return string(raw), EncUTF8
	}
	dec := simplifiedchinese.GBK.NewDecoder()
	out, err := dec.Bytes(raw)
	if err != nil || len(out) == 0 {
		// 解码失败：按 UTF-8 容错回显（非法字节逐个替换），不标记 GBK。
		return replaceInvalidUTF8(raw), EncUTF8
	}
	return string(out), EncGBK
}

// Encode 把内部 UTF-8 文本按缓冲的编码还原为磁盘字节。
// GBK 缓冲保存时写回 GBK，避免把用户的 GBK 文件悄悄改成 UTF-8。
func Encode(text, encoding string) []byte {
	if encoding != EncGBK {
		return []byte(text)
	}
	enc := simplifiedchinese.GBK.NewEncoder()
	out, err := enc.Bytes([]byte(text))
	if err != nil {
		// 存在 GBK 无法表示的字符（用户粘贴的 emoji 等）：退回 UTF-8 写盘，
		// 宁可改编码也不能丢数据。
		return []byte(text)
	}
	return out
}

// replaceInvalidUTF8 把非法 UTF-8 字节逐个替换为 U+FFFD，保证渲染层
// 拿到的字符串永远是合法 UTF-8（Go 的 range/[]rune 会把非法字节当
// U+FFFD 处理，但直接截断/宽度计算可能出偏差）。
func replaceInvalidUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb bytes.Buffer
	for len(b) > 0 {
		r, sz := utf8.DecodeRune(b)
		sb.WriteRune(r) // 非法字节 DecodeRune 返回 RuneError
		b = b[sz:]
	}
	return sb.String()
}
