package skia

import (
	"reflect"
	"testing"

	"github.com/robertpanvip/gox-core/widgets"
)

func TestNativeFileDialogImplementsContract(t *testing.T) {
	var d widgets.FileDialog = NewNativeFileDialog()
	if d == nil {
		t.Fatal("NewNativeFileDialog 返回 nil")
	}
}

func TestSplitPaths(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"/tmp/a.png\n/tmp/b.png\n", []string{"/tmp/a.png", "/tmp/b.png"}},
		{"/tmp/a.jpg, /tmp/b.jpg", []string{"/tmp/a.jpg", "/tmp/b.jpg"}},
		{"  \n", nil},
		{"", nil},
	} {
		got := splitPaths(tc.in)
		if len(got) == 0 && len(tc.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitPaths(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
