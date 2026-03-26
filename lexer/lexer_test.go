package lexer

import (
	"testing"

	"github.com/gox-lang/gox/token"
)

func TestLexer_Ident(t *testing.T) {
	l := New("hello world")
	tokens := l.Tokens()

	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens, got %d", len(tokens))
	}

	if tokens[0].Kind != token.IDENT || tokens[0].Literal != "hello" {
		t.Errorf("first token mismatch: %v", tokens[0])
	}

	if tokens[1].Kind != token.IDENT || tokens[1].Literal != "world" {
		t.Errorf("second token mismatch: %v", tokens[1])
	}

	if tokens[2].Kind != token.EOF {
		t.Errorf("last token should be EOF: %v", tokens[2])
	}
}

func TestLexer_Keywords(t *testing.T) {
	src := "let const var func struct public private if else for return try catch throws extend"
	l := New(src)
	tokens := l.Tokens()

	expected := []token.TokenKind{
		token.LET, token.CONST, token.VAR, token.FUNC, token.STRUCT,
		token.PUBLIC, token.PRIVATE, token.IF, token.ELSE, token.FOR,
		token.RETURN, token.TRY, token.CATCH, token.THROWS, token.EXTEND,
	}

	for i, exp := range expected {
		if tokens[i].Kind != exp {
			t.Errorf("token %d: expected %v, got %v", i, exp, tokens[i].Kind)
		}
	}
}

func TestLexer_Numbers(t *testing.T) {
	l := New("42 3.14 100")
	tokens := l.Tokens()

	if tokens[0].Kind != token.INT || tokens[0].Literal != "42" {
		t.Errorf("int token mismatch: %v", tokens[0])
	}

	if tokens[1].Kind != token.FLOAT || tokens[1].Literal != "3.14" {
		t.Errorf("float token mismatch: %v", tokens[1])
	}

	if tokens[2].Kind != token.INT || tokens[2].Literal != "100" {
		t.Errorf("int token mismatch: %v", tokens[2])
	}
}

func TestLexer_String(t *testing.T) {
	l := New(`"hello world"`)
	tokens := l.Tokens()

	if tokens[0].Kind != token.STRING || tokens[0].Literal != `"hello world"` {
		t.Errorf("string token mismatch: %v", tokens[0])
	}
}

func TestLexer_Operators(t *testing.T) {
	l := New("== != <= >= && || ?? ?.")
	tokens := l.Tokens()

	if tokens[0].Kind != token.EQUAL {
		t.Errorf("expected EQUAL, got %v", tokens[0].Kind)
	}
	if tokens[1].Kind != token.NOT_EQUAL {
		t.Errorf("expected NOT_EQUAL, got %v", tokens[1].Kind)
	}
	if tokens[2].Kind != token.LESS_EQUAL {
		t.Errorf("expected LESS_EQUAL, got %v", tokens[2].Kind)
	}
	if tokens[3].Kind != token.GREATER_EQUAL {
		t.Errorf("expected GREATER_EQUAL, got %v", tokens[3].Kind)
	}
	if tokens[4].Kind != token.LOGICAL_AND {
		t.Errorf("expected LOGICAL_AND, got %v", tokens[4].Kind)
	}
	if tokens[5].Kind != token.LOGICAL_OR {
		t.Errorf("expected LOGICAL_OR, got %v", tokens[5].Kind)
	}
	if tokens[6].Kind != token.NULL_COALESCE {
		t.Errorf("expected NULL_COALESCE, got %v", tokens[6].Kind)
	}
	if tokens[7].Kind != token.SAFE_DOT {
		t.Errorf("expected SAFE_DOT, got %v", tokens[7].Kind)
	}
}

func TestLexer_Arrow(t *testing.T) {
	l := New("->")
	tokens := l.Tokens()

	if tokens[0].Kind != token.ARROW {
		t.Errorf("expected ARROW, got %v", tokens[0].Kind)
	}
}
