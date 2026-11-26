package lsp

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"
)

// workspace/willRenameFiles + didRenameFiles 红测试：
// gox 文件包语义各自独立，重命名不产生跨文件引用编辑——价值点在服务器状态
// 正确跟随（文档迁移 + 诊断换发），供 gox-ide/编辑器直接调用。

func TestWillRenameFilesDesktop(t *testing.T) {
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	s.docs["file:///old.gx"] = &document{Text: "package main\n\nfunc main() {}\n"}
	res, e := s.willRenameFiles([]byte(`{"files":[{"oldUri":"file:///old.gx","newUri":"file:///new.gx"}]}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	if _, ok := res.(WorkspaceEdit); !ok {
		t.Fatalf("类型: %T", res)
	}
}

func TestDidRenameFilesDesktop(t *testing.T) {
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	s.docs["file:///a.gx"] = &document{Text: "package main\n\nfunc a() {}\n"}
	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	s.handleNotification(&RequestMessage{Method: "workspace/didRenameFiles"},
		[]byte(`{"files":[{"oldUri":"file:///a.gx","newUri":"file:///b.gx"}]}`), bw)
	bw.Flush()
	if _, ok := s.docs["file:///a.gx"]; ok {
		t.Fatal("旧 URI 应移除")
	}
	d := s.docs["file:///b.gx"]
	if d == nil || !strings.Contains(d.Text, "func a()") {
		t.Fatalf("文档未迁移: %+v", d)
	}
	if !strings.Contains(buf.String(), "file:///b.gx") {
		t.Fatalf("未按新 URI 换发诊断: %.200s", buf.String())
	}
}
