package cssbox

import "testing"

func TestValues(t *testing.T) {
	tests := []struct {
		in   string
		want Edges
		ok   bool
	}{
		{"8", Edges{"8", "8", "8", "8"}, true},
		{"8px", Edges{"8", "8", "8", "8"}, true},
		{"7 14", Edges{"7", "14", "7", "14"}, true},
		{"0 16 12 16", Edges{"0", "16", "12", "16"}, true},
		{"1 2 3", Edges{"1", "2", "3", "2"}, true},
		{"  6   10  ", Edges{"6", "10", "6", "10"}, true},
		{"", Edges{}, false},
		{"7px 14px", Edges{"7", "14", "7", "14"}, true},
		{"auto", Edges{}, false},
		{"1 2 3 4 5", Edges{}, false},
		{"a b", Edges{}, false},
	}
	for _, tc := range tests {
		got, ok := Values(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Values(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFloat32s(t *testing.T) {
	got, ok := Float32s("7 14")
	if !ok || got != [4]float32{7, 14, 7, 14} {
		t.Fatalf("Float32s(\"7 14\") = %v, %v", got, ok)
	}
}

func TestWithUnit(t *testing.T) {
	e, _ := Values("7 14")
	got := WithUnit(e)
	if got != (Edges{"7px", "14px", "7px", "14px"}) {
		t.Fatalf("WithUnit = %v", got)
	}
}

func TestShorthand(t *testing.T) {
	cases := []struct {
		in   Edges
		want string
	}{
		{Edges{"8", "8", "8", "8"}, "8"},
		{Edges{"7", "14", "7", "14"}, "7 14"},
		{Edges{"1", "2", "3", "2"}, "1 2 3 2"},
	}
	for _, tc := range cases {
		if got := Shorthand(tc.in); got != tc.want {
			t.Errorf("Shorthand(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
