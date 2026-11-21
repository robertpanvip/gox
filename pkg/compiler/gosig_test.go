package compiler

import "testing"

// TestGoSigGeneric 证明外部签名查询是通用的（非针对某个 runtime 包特判）：
// 直接解析标准库 net/http 的 NewRequest 签名。
func TestGoSigGeneric(t *testing.T) {
	dir := goListPkgDir("net/http", "")
	if dir == "" {
		t.Skip("go list net/http 不可用")
	}
	sigs := parseFuncSigs(dir)
	params, ok := sigs["NewRequest"]
	if !ok || len(params) < 2 {
		t.Fatalf("未解析到 NewRequest 签名: %+v", sigs["NewRequest"])
	}
	if params[0] != "string" || params[1] != "string" {
		t.Errorf("NewRequest 前两参数应为 string/string, got %v", params[:2])
	}
}

// TestQualifyExternalType 验证包内类型加前缀、已限定类型保留、指针/切片递归处理。
func TestQualifyExternalType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"WindowConfig", "gxapp.WindowConfig"},
		{"*WindowConfig", "*gxapp.WindowConfig"},
		{"gxscene.Instance", "gxscene.Instance"},
		{"[]WindowConfig", "[]gxapp.WindowConfig"},
	}
	for _, c := range cases {
		if got := qualifyExternalType(c.in, "gxapp"); got != c.want {
			t.Errorf("qualifyExternalType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
