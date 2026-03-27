package token

type TokenKind int

const (
	EOF TokenKind = iota

	COMMENT

	IDENT
	INT
	FLOAT
	STRING
	TEMPLATE

	ASSIGN
	SEMICOLON
	COLON
	COMMA
	DOT

	LPAREN
	RPAREN
	LBRACE
	RBRACE
	LBRACK
	RBRACK

	PLUS
	MINUS
	STAR
	SLASH
	PERCENT

	AMP
	PIPE
	LOGICAL_AND
	LOGICAL_OR
	CARET
	TILDE
	BANG

	LESS
	GREATER
	EQUAL
	NOT_EQUAL
	LESS_EQUAL
	GREATER_EQUAL

	QUESTION
	NULL_COALESCE
	SAFE_DOT

	ARROW

	LET
	CONST
	VAR

	FUNC
	STRUCT
	MIXED
	INTERFACE
	PUBLIC
	PRIVATE
	GO
	GOX

	IF
	ELSE
	FOR
	WHILE
	SWITCH
	CASE
	WHEN
	BREAK
	CONTINUE
	RETURN
	TSX_OPEN
	TSX_CLOSE
	TSX_SLASH_OPEN
	TSX_SELF_CLOSE

	TRY
	CATCH
	THROWS

	EXTEND
	SELF

	TRUE
	FALSE
	NIL

	IMPORT
	PACKAGE

	NEWLINE
)

var keywords = map[string]TokenKind{
	"let":       LET,
	"const":     CONST,
	"var":       VAR,
	"func":      FUNC,
	"struct":    STRUCT,
	"mixed":     MIXED,
	"interface": INTERFACE,
	"public":    PUBLIC,
	"private":   PRIVATE,
	"go":        GO,
	"gox":       GOX,
	"if":        IF,
	"else":      ELSE,
	"for":       FOR,
	"while":     WHILE,
	"switch":    SWITCH,
	"case":      CASE,
	"when":      WHEN,
	"break":     BREAK,
	"continue":  CONTINUE,
	"return":    RETURN,
	"try":       TRY,
	"catch":     CATCH,
	"throws":    THROWS,
	"extend":    EXTEND,
	"self":      SELF,
	"true":      TRUE,
	"false":     FALSE,
	"nil":       NIL,
	"import":    IMPORT,
	"package":   PACKAGE,
}

func LookupKeyword(ident string) TokenKind {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}

type Position struct {
	Line int
	Col  int
}

func (p Position) IsValid() bool {
	return p.Line > 0
}

type Token struct {
	Kind    TokenKind
	Literal string
	Pos     int
	Line    int
	Col     int
}

func (t Token) String() string {
	switch t.Kind {
	case EOF:
		return "EOF"
	case IDENT:
		return "ident(" + t.Literal + ")"
	case INT:
		return "int(" + t.Literal + ")"
	case FLOAT:
		return "float(" + t.Literal + ")"
	case STRING:
		return "string(" + t.Literal + ")"
	default:
		return t.Literal
	}
}

type Visibility struct {
	Public  bool
	Private bool
}
