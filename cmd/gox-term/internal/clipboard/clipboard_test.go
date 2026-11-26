package clipboard

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestOSC52(t *testing.T) {
	text := "hello 中文\nworld"
	seq := OSC52(text)
	if !strings.HasPrefix(seq, "\x1b]52;c;") || !strings.HasSuffix(seq, "\x07") {
		t.Fatalf("OSC52 序列格式错误: %q", seq)
	}
	enc := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	if string(decoded) != text {
		t.Errorf("解码 = %q, want %q", decoded, text)
	}
}

func TestWriteOSC52(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteOSC52(&buf, "copied"); err != nil {
		t.Fatal(err)
	}
	if buf.String() != OSC52("copied") {
		t.Errorf("写出内容不符: %q", buf.String())
	}
}
