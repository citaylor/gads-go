package tt

// ----------------------------------------------------------------------
// Expressions
// ----------------------------------------------------------------------

// Expr is an expression node. TT's grammar builds Perl source text from
// expressions and Perl then parses that text with its own operator
// precedence; the parser therefore collects binary operators into a flat
// piece list (flatExpr) and builds the tree with Perl's precedence rules
// once the whole expression is known (buildExpr).
type Expr interface{}

type exprLiteral struct{ v any }

type identPart struct {
	name Expr     // usually an exprLiteral with the item name
	args *argList // nil = no arguments
}

type exprIdent struct {
	parts []identPart
	ref   bool // \ident (getref)
}

type exprQuoted struct{ parts []Expr }

type exprList struct{ items []Expr }

type exprRange struct{ from, to Expr }

type hashPair struct{ key, val Expr }

type exprHash struct{ pairs []hashPair }

// lvalue of an assignment: either an ident or a literal name
type assignTarget struct {
	ident   *exprIdent
	literal Expr
}

type exprAssign struct {
	target assignTarget
	val    Expr
	def    bool
}

type exprBinary struct {
	op   string // + - * / % . && || eq ne < > <= >=
	l, r Expr
}

// exprChain is a chain of comparisons (Perl 5.32 chained comparisons)
type exprChain struct {
	ops   []string
	terms []Expr
}

type exprNot struct{ e Expr }

type exprTernary struct{ c, t, f Expr }

type exprInt struct{ e Expr }

type exprParen struct{ e Expr }

// args of a call: positional expressions and named parameters. Named
// parameters are passed as a hash after the positional ones.
type namedArg struct {
	key Expr
	val Expr
	// set: a "foo.bar = baz" argument (rule args ident ASSIGN expr): the
	// assignment is made when the arguments are evaluated and the result
	// stored under the key ''
	set *exprAssign
}

type argList struct {
	pos   []Expr
	named []namedArg
}

func (a *argList) empty() bool { return a == nil || (len(a.pos) == 0 && len(a.named) == 0) }

// piece of a flat expression: either an operator or an atom
type piece struct {
	op   string
	atom Expr
}

type flatExpr []piece

// ----------------------------------------------------------------------
// Statements
// ----------------------------------------------------------------------

type stmt interface{}

type block []stmt

type stmtText struct{ text string }

type stmtGet struct{ e Expr }

type stmtCall struct{ e Expr }

type assignPair struct {
	target assignTarget
	val    Expr
}

type stmtSet struct {
	assigns []assignPair
	def     bool
}

type stmtInclude struct {
	names   []Expr
	named   []namedArg
	process bool
}

type stmtInsert struct{ names []Expr }

type stmtThrow struct {
	typ   Expr
	info  Expr // nil = no info
	pos   []Expr
	named []namedArg
}

type stmtReturn struct{}
type stmtStop struct{}
type stmtClear struct{}

type stmtLast struct{ label string }

type stmtNext struct {
	label   string
	advance bool // NEXT in a FOREACH: fetch the next item first
}

type ifBranch struct {
	cond Expr
	body block
}

type stmtIf struct {
	branches []ifBranch
	els      block
	hasElse  bool
}

type switchCase struct {
	match Expr
	body  block
}

type stmtSwitch struct {
	e      Expr
	cases  []switchCase
	def    block
	hasDef bool
}

type stmtForeach struct {
	target string
	list   Expr
	body   block
	label  string
}

type stmtWhile struct {
	cond  Expr
	body  block
	label string
}

type stmtWrapper struct {
	names []Expr
	named []namedArg
	body  block
}

type stmtFilter struct {
	name  Expr
	args  *argList
	alias Expr
	body  block
}

type stmtAnon struct{ body *tmplBody }

type stmtCapture struct {
	target assignTarget
	body   block
}

type stmtMacro struct {
	name    string
	args    []string
	hasArgs bool
	body    block
}

type catchBlock struct {
	typ  string // "" = default
	body block
}

type stmtTry struct {
	body     block
	catches  []catchBlock
	final    block
	hasFinal bool
}

// stmtThrowFixed throws a fixed exception (unsupported features such as
// PERL blocks, which GADS does not enable)
type stmtThrowFixed struct{ typ, info string }

type stmtUse struct{ name Expr }

// tmplBody is a compiled template sub: the main template or a BLOCK. It
// catches RETURN.
type tmplBody struct {
	body block
	name string
}

type nameArgs struct {
	names []Expr
	args  *argList
	alias Expr
}

type loopVar struct {
	target string
	list   Expr
	args   *argList
}
