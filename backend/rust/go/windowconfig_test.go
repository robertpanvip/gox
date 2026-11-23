package rustbackend

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
)

func TestDecoratedC(t *testing.T) {
	tests := []struct {
		name string
		in   *bool
		want int
	}{
		{"unset", nil, -1},
		{"no-decorations", gxapp.NoDecorations(), 0},
		{"with-decorations", gxapp.WithDecorations(), 1},
	}
	for _, tt := range tests {
		if got := decoratedC(tt.in); got != tt.want {
			t.Errorf("%s: decoratedC = %d, want %d", tt.name, got, tt.want)
		}
	}
}
