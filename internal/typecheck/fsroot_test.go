package typecheck

import (
	"os"
	"runtime"
	"testing"
)

// TestFSRootParityWithDisk FSRoot(os.DirFS) 与 DiskLookup 对同一包给出一致结果。
func TestFSRootParityWithDisk(t *testing.T) {
	gr := runtime.GOROOT()
	if gr == "" || !dirExists(gr+"/src/strings") {
		t.Skip("无 GOROOT/src/strings")
	}
	a, okA := FSRoot(os.DirFS(gr+"/src"), nil, "linux", "amd64")("strings")
	b, okB := DiskLookup(gr+"/src", "linux", "amd64")("strings")
	if okA != okB || len(a) != len(b) {
		t.Fatalf("parity 失败: okA=%v lenA=%d okB=%v lenB=%d", okA, len(a), okB, len(b))
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			t.Fatalf("文件顺序/名称不一致: %s vs %s", a[i].Name, b[i].Name)
		}
	}
}
