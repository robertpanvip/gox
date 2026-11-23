package rustbackend

import (
	"reflect"
	"testing"

	"github.com/robertpanvip/gox/widgets"
)

func TestRustNativeFileDialogImplementsContract(t *testing.T) {
	var d widgets.FileDialog = NewNativeFileDialog()
	if d == nil {
		t.Fatal("NewNativeFileDialog 返回 nil")
	}
}

func TestSplitDialogPaths(t *testing.T) {
	got := splitDialogPaths("/tmp/a.png\n/tmp/b.png\n")
	want := []string{"/tmp/a.png", "/tmp/b.png"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitDialogPaths = %v, want %v", got, want)
	}
}
