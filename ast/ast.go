package ast

import (
	"github.com/gox-lang/gox/token"
)

type Position = token.Position

type Node interface {
	node()
	Pos() token.Position
}

type Expr interface {
	Node
	expr()
}

type Stmt interface {
	Node
	stmt()
}

type Decl interface {
	Node
	decl()
}

type Program struct {
	Package *PackageClause
	Decls   []Decl
	Stmts   []Stmt  // Global statements
}

func (p *Program) node()   {}
func (p *Program) decl()  {}
func (p *Program) Pos() token.Position { return token.Position{} }

type PackageClause struct {
	Name string
	P    Position
}

func (p *PackageClause) node()  {}
func (p *PackageClause) decl() {}
func (p *PackageClause) Pos() token.Position { return p.P }

type ImportDecl struct {
	Path       string
	SourceType string // "go", "gox", or "" (default: "gox")
	P          Position
}

func (i *ImportDecl) node()  {}
func (i *ImportDecl) decl()  {}
func (i *ImportDecl) Pos() token.Position { return i.P }

type Visibility struct {
	Public  bool
	Private bool
}

type Field struct {
	Visibility Visibility
	Name       string
	Type       Expr
}

type StructDecl struct {
	Visibility Visibility
	Name       string
	TypeParams []*TypeParam
	Fields     []*Field
	Methods    []*FuncDecl
	Mixed      []*BaseType  // Embedded structs (mixed)
	P          Position
}

func (s *StructDecl) node()  {}
func (s *StructDecl) decl() {}
func (s *StructDecl) Pos() token.Position { return s.P }

type FuncParam struct {
	Name string
	Type Expr
}

type FuncDecl struct {
	Visibility  Visibility
	Name        string
	Receiver    *FuncParam  // Go receiver (for struct methods)
	TypeParams  []*TypeParam
	Params      []*FuncParam
	ReturnType  Expr
	Throws      bool
	IsFx        bool        // FX function (lit-html style)
	Body        *BlockStmt
	P           Position
}

func (f *FuncDecl) node()  {}
func (f *FuncDecl) decl() {}
func (f *FuncDecl) Pos() token.Position { return f.P }

type ArrowFunc struct {
	Params     []*FuncParam
	Body       Expr  // For expression body
	Block      *BlockStmt  // For block body
	ReturnType Expr
	P          Position
}

func (a *ArrowFunc) node() {}
func (a *ArrowFunc) expr() {}
func (a *ArrowFunc) Pos() token.Position { return a.P }

type TypeParam struct {
	Name       string
	Constraint Expr
	P          Position
}

func (t *TypeParam) node() {}
func (t *TypeParam) expr() {}
func (t *TypeParam) Pos() token.Position { return t.P }

type VarDecl struct {
	Visibility Visibility
	Name       string
	Type       Expr
	Value      Expr
	P          Position
}

func (v *VarDecl) node()  {}
func (v *VarDecl) decl() {}
func (v *VarDecl) stmt() {}
func (v *VarDecl) Pos() token.Position { return v.P }

type ConstDecl struct {
	Visibility Visibility
	Name       string
	Type       Expr
	Value      Expr
	P          Position
}

func (c *ConstDecl) node()  {}
func (c *ConstDecl) decl() {}
func (c *ConstDecl) stmt() {}
func (c *ConstDecl) Pos() token.Position { return c.P }

// SigDecl Signal 声明 (sig x = value)
type SigDecl struct {
	Visibility Visibility
	Name       string
	Value      Expr
	P          Position
}

func (s *SigDecl) node()  {}
func (s *SigDecl) decl() {}
func (s *SigDecl) stmt() {}
func (s *SigDecl) Pos() token.Position { return s.P }

type ExtendDecl struct {
	Type    Expr
	Methods []*FuncDecl
	P       Position
}

func (e *ExtendDecl) node()  {}
func (e *ExtendDecl) decl() {}
func (e *ExtendDecl) Pos() token.Position { return e.P }

type InterfaceDecl struct {
	Visibility Visibility
	Name       string
	Methods    []*FuncDecl  // Interface methods (no body)
	Mixed      []*BaseType  // Embedded interfaces (mixed)
	P          Position
}

func (i *InterfaceDecl) node()  {}
func (i *InterfaceDecl) decl() {}
func (i *InterfaceDecl) Pos() token.Position { return i.P }

type BlockStmt struct {
	List []Stmt
	P    Position
}

func (b *BlockStmt) node() {}
func (b *BlockStmt) stmt() {}
func (b *BlockStmt) Pos() token.Position { return b.P }

type ExprStmt struct {
	X Expr
}

func (e *ExprStmt) node() {}
func (e *ExprStmt) stmt() {}
func (e *ExprStmt) Pos() token.Position { return token.Position{} }

type ReturnStmt struct {
	Result Expr
	P      Position
}

func (r *ReturnStmt) node() {}
func (r *ReturnStmt) stmt() {}
func (r *ReturnStmt) Pos() token.Position { return r.P }

type IfStmt struct {
	Cond   Expr
	Body   *BlockStmt
	Else   Stmt
	P      Position
}

func (i *IfStmt) node() {}
func (i *IfStmt) stmt() {}
func (i *IfStmt) Pos() token.Position { return i.P }

type ForStmt struct {
	Cond  Expr
	Body  *BlockStmt
	P     Position
}

func (f *ForStmt) node() {}
func (f *ForStmt) stmt() {}
func (f *ForStmt) Pos() token.Position { return f.P }

type WhileStmt struct {
	Cond Expr
	Body *BlockStmt
	P    Position
}

func (w *WhileStmt) node() {}
func (w *WhileStmt) stmt() {}
func (w *WhileStmt) Pos() token.Position { return w.P }

type ForInStmt struct {
	Var   string
	Iter  Expr
	Body  *BlockStmt
	P     Position
}

func (f *ForInStmt) node() {}
func (f *ForInStmt) stmt() {}
func (f *ForInStmt) Pos() token.Position { return f.P }

type StructLit struct {
	Type   Expr
	Fields []*StructField
	P      Position
}

type StructField struct {
	Name  string
	Value Expr
	P     Position
}

func (s *StructLit) node() {}
func (s *StructLit) expr() {}
func (s *StructLit) Pos() token.Position { return s.P }

func (f *StructField) node() {}
func (f *StructField) Pos() token.Position { return f.P }

// TSX Nodes
type TSXElement struct {
	TagName     string
	Attributes  []*TSXAttr
	Children    []Expr
	SelfClosing bool
	P           Position
}

type TSXAttr struct {
	Name  string
	Value Expr  // Can be StringLit or {expression}
	P     Position
}

func (t *TSXElement) node() {}
func (t *TSXElement) expr() {}
func (t *TSXElement) Pos() token.Position { return t.P }

func (a *TSXAttr) node() {}
func (a *TSXAttr) Pos() token.Position { return a.P }

type BreakStmt struct {
	P Position
}

func (b *BreakStmt) node() {}
func (b *BreakStmt) stmt() {}
func (b *BreakStmt) Pos() token.Position { return b.P }

type ContinueStmt struct {
	P Position
}

func (c *ContinueStmt) node() {}
func (c *ContinueStmt) stmt() {}
func (c *ContinueStmt) Pos() token.Position { return c.P }

type SwitchStmt struct {
	Cond   Expr
	Cases  []*SwitchCase
	P      Position
}

type SwitchCase struct {
	Cond Expr
	Body *BlockStmt
	P    Position
}

func (s *SwitchStmt) node() {}
func (s *SwitchStmt) stmt() {}
func (s *SwitchStmt) Pos() token.Position { return s.P }

func (c *SwitchCase) node() {}
func (c *SwitchCase) stmt() {}
func (c *SwitchCase) Pos() token.Position { return c.P }

type WhenStmt struct {
	Cond   Expr
	Cases  []*WhenCase
	P      Position
}

type WhenCase struct {
	Cond Expr
	Body *BlockStmt
	P    Position
}

func (w *WhenStmt) node() {}
func (w *WhenStmt) stmt() {}
func (w *WhenStmt) Pos() token.Position { return w.P }

func (c *WhenCase) node() {}
func (c *WhenCase) stmt() {}
func (c *WhenCase) Pos() token.Position { return c.P }

type TryStmt struct {
	TryBlock   *BlockStmt
	CatchErr   string
	CatchBlock *BlockStmt
	P          Position
}

func (t *TryStmt) node() {}
func (t *TryStmt) stmt() {}
func (t *TryStmt) Pos() token.Position { return t.P }

type AssignStmt struct {
	LHS Expr
	RHS Expr
	P   Position
}

func (a *AssignStmt) node() {}
func (a *AssignStmt) stmt() {}
func (a *AssignStmt) Pos() token.Position { return a.P }

type IncDecStmt struct {
	X      Expr
	TokPos token.TokenKind
	P      Position
}

func (i *IncDecStmt) node() {}
func (i *IncDecStmt) stmt() {}
func (i *IncDecStmt) Pos() token.Position { return i.P }

type BaseType struct {
	Name string
}

func (b *BaseType) node() {}
func (b *BaseType) expr() {}
func (b *BaseType) Pos() token.Position { return token.Position{} }

type ArrayType struct {
	Element Expr
}

func (a *ArrayType) node() {}
func (a *ArrayType) expr() {}
func (a *ArrayType) Pos() token.Position { return token.Position{} }

type NullableType struct {
	Element Expr
}

func (n *NullableType) node() {}
func (n *NullableType) expr() {}
func (n *NullableType) Pos() token.Position { return token.Position{} }

type PointerType struct {
	Base Expr
}

func (p *PointerType) node() {}
func (p *PointerType) expr() {}
func (p *PointerType) Pos() token.Position { return token.Position{} }

type StructType struct {
	Fields []*Field
}

func (s *StructType) node() {}
func (s *StructType) expr() {}
func (s *StructType) Pos() token.Position { return token.Position{} }

type ArrayLit struct {
	Elements []Expr
	P        Position
}

func (a *ArrayLit) node() {}
func (a *ArrayLit) expr() {}
func (a *ArrayLit) Pos() token.Position { return a.P }

type FuncType struct {
	Params     []*FuncParam
	ReturnType Expr
	Throws     bool
}

func (f *FuncType) node() {}
func (f *FuncType) expr() {}
func (f *FuncType) Pos() token.Position { return token.Position{} }

type Ident struct {
	Name string
	Obj  interface{}
	P    Position
}

func (i *Ident) node() {}
func (i *Ident) expr() {}
func (i *Ident) Pos() token.Position { return i.P }

type SelectorExpr struct {
	X   Expr
	Sel *Ident
}

func (s *SelectorExpr) node() {}
func (s *SelectorExpr) expr() {}
func (s *SelectorExpr) Pos() token.Position { return token.Position{} }

type IndexExpr struct {
	X     Expr
	Index Expr
}

func (i *IndexExpr) node() {}
func (i *IndexExpr) expr() {}
func (i *IndexExpr) Pos() token.Position { return token.Position{} }

type CallExpr struct {
	Fun       Expr
	Args      []Expr
	HasThrows bool
}

func (c *CallExpr) node() {}
func (c *CallExpr) expr() {}
func (c *CallExpr) Pos() token.Position { return token.Position{} }

type FunctionLiteral struct {
	Params     []*FuncParam
	ReturnType Expr
	Body       *BlockStmt
	IsArrow    bool
	P          Position
}

func (f *FunctionLiteral) node() {}
func (f *FunctionLiteral) expr() {}
func (f *FunctionLiteral) Pos() token.Position { return f.P }

type TryExpr struct {
	X      Expr
	Throws bool
}

func (t *TryExpr) node() {}
func (t *TryExpr) expr() {}
func (t *TryExpr) Pos() token.Position { return token.Position{} }

type BinaryExpr struct {
	Op token.TokenKind
	X  Expr
	Y  Expr
}

func (b *BinaryExpr) node() {}
func (b *BinaryExpr) expr() {}
func (b *BinaryExpr) Pos() token.Position { return token.Position{} }

type UnaryExpr struct {
	Op   token.TokenKind
	X    Expr
	Post bool
}

func (u *UnaryExpr) node() {}
func (u *UnaryExpr) expr() {}
func (u *UnaryExpr) Pos() token.Position { return token.Position{} }

type MemberExpr struct {
	X       Expr
	Name    string
	HasSafe bool
}

func (m *MemberExpr) node() {}
func (m *MemberExpr) expr() {}
func (m *MemberExpr) Pos() token.Position { return token.Position{} }

type NilCoalesceExpr struct {
	X Expr
	Y Expr
}

func (n *NilCoalesceExpr) node() {}
func (n *NilCoalesceExpr) expr() {}
func (n *NilCoalesceExpr) Pos() token.Position { return token.Position{} }

type SliceExpr struct {
	X    Expr
	Low  Expr
	High Expr
}

func (s *SliceExpr) node() {}
func (s *SliceExpr) expr() {}
func (s *SliceExpr) Pos() token.Position { return token.Position{} }

type CompositeLit struct {
	Type Expr
	Elts []Expr
}

func (c *CompositeLit) node() {}
func (c *CompositeLit) expr() {}
func (c *CompositeLit) Pos() token.Position { return token.Position{} }

type ParenExpr struct {
	X Expr
}

func (p *ParenExpr) node() {}
func (p *ParenExpr) expr() {}
func (p *ParenExpr) Pos() token.Position { return token.Position{} }

type StringLit struct {
	Value string
	P     Position
}

func (s *StringLit) node() {}
func (s *StringLit) expr() {}
func (s *StringLit) Pos() token.Position { return s.P }

type TemplateString struct {
	Parts []string
	Exprs []Expr
	P     Position
}

func (t *TemplateString) node() {}
func (t *TemplateString) expr() {}
func (t *TemplateString) Pos() token.Position { return t.P }

type IntLit struct {
	Value int64
	P     Position
}

func (i *IntLit) node() {}
func (i *IntLit) expr() {}
func (i *IntLit) Pos() token.Position { return i.P }

type FloatLit struct {
	Value float64
	P     Position
}

func (f *FloatLit) node() {}
func (f *FloatLit) expr() {}
func (f *FloatLit) Pos() token.Position { return f.P }

type BoolLit struct {
	Value bool
	P     Position
}

func (b *BoolLit) node() {}
func (b *BoolLit) expr() {}
func (b *BoolLit) Pos() token.Position { return b.P }

type NilLit struct {
	P Position
}

func (n *NilLit) node() {}
func (n *NilLit) expr() {}
func (n *NilLit) Pos() token.Position { return n.P }
