package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

// TestTransformEnum 测试枚举转换
func TestTransformEnum(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result string)
	}{
		{
			name: "simple enum with auto values",
			input: `package main
enum Direction {
    Up,
    Down,
    Left,
    Right,
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "type Direction int") {
					t.Errorf("expected Direction type, got: %s", result)
				}
				if !strings.Contains(result, "Up Direction = iota") {
					t.Errorf("expected Up with iota, got: %s", result)
				}
				if !strings.Contains(result, "Down") {
					t.Errorf("expected Down variant, got: %s", result)
				}
				if !strings.Contains(result, "Left") {
					t.Errorf("expected Left variant, got: %s", result)
				}
				if !strings.Contains(result, "Right") {
					t.Errorf("expected Right variant, got: %s", result)
				}
			},
		},
		{
			name: "enum with explicit values",
			input: `package main
enum Status {
    Pending = 1,
    Running = 2,
    Done = 100,
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "type Status int") {
					t.Errorf("expected Status type, got: %s", result)
				}
				if !strings.Contains(result, "Pending Status = 1") {
					t.Errorf("expected Pending = 1, got: %s", result)
				}
				if !strings.Contains(result, "Running Status = 2") {
					t.Errorf("expected Running = 2, got: %s", result)
				}
				if !strings.Contains(result, "Done Status = 100") {
					t.Errorf("expected Done = 100, got: %s", result)
				}
			},
		},
		{
			name: "mixed enum with some explicit values",
			input: `package main
enum Color {
    Red = 10,
    Green,
    Blue,
    Yellow = 20,
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "Red Color = 10") {
					t.Errorf("expected Red = 10, got: %s", result)
				}
				// Green should be auto-incremented from Red
				if !strings.Contains(result, "Green") {
					t.Errorf("expected Green variant, got: %s", result)
				}
				if !strings.Contains(result, "Yellow Color = 20") {
					t.Errorf("expected Yellow = 20, got: %s", result)
				}
			},
		},
		{
			name: "enum usage in function",
			input: `package main
enum Direction {
    Up,
    Down,
}
func test() {
    let d = Direction.Up
    d
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "type Direction int") {
					t.Errorf("expected Direction type, got: %s", result)
				}
				if !strings.Contains(result, "d := Direction.Up") {
					t.Errorf("expected Direction.Up usage, got: %s", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parser.New(tt.input)
			prog := p.ParseProgram()

			if len(p.Errors()) > 0 {
				t.Fatalf("parser errors: %v", p.Errors())
			}

			tfm := New()
			result := tfm.Transform(prog)
			tt.validate(t, result)
		})
	}
}
