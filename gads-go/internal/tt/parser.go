package tt

import (
	"fmt"
	"strconv"
	"strings"
)

// parser state, as Template::Parser
type parser struct {
	inBlock   []string
	defBlocks []string
	defblock  map[string]*tmplBody
	line      string
	text      string
	hasText   bool
	metadata  [][2]string
}

// ParseError is a template parse error ("line N: message").
type ParseError struct{ Msg string }

func (e *ParseError) Error() string { return e.Msg }

// document is a compiled template
type document struct {
	name     string
	body     *tmplBody
	blocks   map[string]*tmplBody
	metadata map[string]string
}

func parseTemplate(text string, anycase bool) (*document, error) {
	p := &parser{defblock: map[string]*tmplBody{}}
	items := splitText(text, anycase)
	res, err := p.parse(items)
	if err != nil {
		return nil, err
	}
	body, _ := res.(*tmplBody)
	if body == nil {
		body = &tmplBody{}
	}
	doc := &document{body: body, blocks: p.defblock, metadata: map[string]string{}}
	for _, m := range p.metadata {
		doc.metadata[m[0]] = m[1]
	}
	return doc, nil
}

type stackEntry struct {
	state int
	val   any
}

func (p *parser) parse(tokens []lexItem) (any, error) {
	stack := []stackEntry{{0, nil}}
	var token string
	var value any
	haveToken := false
	valueDefined := false
	for {
		stateno := stack[len(stack)-1].state
		state := &grammarStates[stateno]
		var action int
		hasAction := false
		if state.actions != nil {
			for !haveToken && len(tokens) > 0 {
				t := tokens[0]
				tokens = tokens[1:]
				if t.dir {
					p.text, p.line, p.hasText = t.dirText, t.line, true
					exp := make([]lexItem, 0, len(t.toks)+2+len(tokens))
					exp = append(exp, t.toks...)
					exp = append(exp, tok(";", ";"), tok(";", ";"))
					exp = append(exp, tokens...)
					tokens = exp
					continue
				}
				token, value, haveToken, valueDefined = t.typ, t.val, true, true
			}
			if !haveToken {
				token = ""
			}
			if a, ok := state.actions[token]; ok {
				action, hasAction = a, true
			} else if state.hasDef {
				action, hasAction = state.def, true
			}
		} else if state.hasDef {
			action, hasAction = state.def, true
		}
		if !hasAction {
			break
		}
		if action > 0 {
			stack = append(stack, stackEntry{action, value})
			haveToken, valueDefined, value = false, false, nil
			continue
		}
		ruleNo := -action
		rule := grammarRules[ruleNo]
		vals := make([]any, rule.len)
		for i := 0; i < rule.len; i++ {
			vals[i] = stack[len(stack)-rule.len+i].val
		}
		var ret any
		if action == 0 {
			// accept
			if rule.len > 0 {
				ret = vals[0]
			}
			return ret, nil
		}
		r, err := p.reduce(ruleNo, vals)
		if err != nil {
			return nil, p.parseError(err.Error(), false)
		}
		ret = r
		stack = stack[:len(stack)-rule.len]
		top := grammarStates[stack[len(stack)-1].state]
		stack = append(stack, stackEntry{top.gotos[rule.lhs], ret})
	}
	if !valueDefined {
		return nil, p.parseError("unexpected end of input", false)
	}
	if s, ok := value.(string); ok && s == ";" {
		return nil, p.parseError("unexpected end of directive", true)
	}
	return nil, p.parseError(fmt.Sprintf("unexpected token (%v)", value), true)
}

func (p *parser) parseError(msg string, withText bool) error {
	line := p.line
	if l, ok := firstLine(line); !ok || l == 0 {
		if line == "" || line == "0" {
			line = "unknown"
		}
	}
	m := "line " + line + ": " + msg
	if withText && p.hasText {
		m += "\n  [% " + p.text + " %]"
	}
	return &ParseError{Msg: m}
}

func firstLine(s string) (int, bool) {
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func (p *parser) enterBlock(name string) { p.inBlock = append(p.inBlock, name) }

func (p *parser) leaveBlock() string {
	l := p.blockLabel()
	p.inBlock = p.inBlock[:len(p.inBlock)-1]
	return l
}

func (p *parser) inBlockNamed(name string) bool {
	return len(p.inBlock) > 0 && p.inBlock[len(p.inBlock)-1] == name
}

func (p *parser) blockLabel() string {
	if len(p.inBlock) == 0 {
		return ""
	}
	return p.inBlock[len(p.inBlock)-1] + strconv.Itoa(len(p.inBlock))
}

// ----------------------------------------------------------------------
// semantic value helpers
// ----------------------------------------------------------------------

func asBlock(v any) block {
	if b, ok := v.(block); ok {
		return b
	}
	return nil
}

func asFlat(v any) flatExpr {
	if f, ok := v.(flatExpr); ok {
		return f
	}
	return nil
}

func asArgs(v any) *argList {
	if a, ok := v.(*argList); ok {
		return a
	}
	return &argList{}
}

func asIdent(v any) []identPart {
	if a, ok := v.([]identPart); ok {
		return a
	}
	return nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func stmtBlock(s any) block {
	if s == nil {
		return block{}
	}
	return block{s}
}

func cmpOp(op string) string {
	switch op {
	case "==":
		return "eq"
	case "!=":
		return "ne"
	}
	return op
}

// perlLiteral decodes a LITERAL token value (Perl single quoted string)
func perlLiteral(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		s = s[1 : len(s)-1]
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '\\' || s[i+1] == '\'') {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// perlNumberLiteral converts a NUMBER token as Perl compiles the numeric
// literal (a leading 0 means octal).
func perlNumberLiteral(s string) any {
	neg := strings.HasPrefix(s, "-")
	t := strings.TrimPrefix(s, "-")
	if !strings.Contains(t, ".") {
		base := 10
		if len(t) > 1 && t[0] == '0' {
			base = 8
		}
		if n, err := strconv.ParseInt(t, base, 64); err == nil {
			if neg {
				n = -n
			}
			return n
		}
		f, _ := strconv.ParseFloat(t, 64)
		if neg {
			f = -f
		}
		return f
	}
	f, _ := strconv.ParseFloat(t, 64)
	if neg {
		f = -f
	}
	return f
}

func identExpr(parts []identPart) Expr { return &exprIdent{parts: parts} }

func literalString(s string) Expr { return &exprLiteral{v: s} }

func toTarget(v any) assignTarget {
	switch t := v.(type) {
	case []identPart:
		return assignTarget{ident: &exprIdent{parts: t}}
	case string: // LITERAL token
		return assignTarget{literal: literalString(perlLiteral(t))}
	}
	return assignTarget{}
}

// reduce performs the semantic action of a grammar rule (see
// Template::Grammar / parser/Parser.yp for the Perl originals).
func (p *parser) reduce(rule int, v []any) (any, error) {
	switch rule {
	case 1: // template: block
		return &tmplBody{body: asBlock(v[0])}, nil
	case 2:
		return asBlock(v[0]), nil
	case 3:
		return block{}, nil
	case 4: // chunks chunk
		b := asBlock(v[0])
		if v[1] != nil {
			b = append(b, v[1])
		}
		return b, nil
	case 5:
		if v[0] != nil {
			return block{v[0]}, nil
		}
		return block{}, nil
	case 6: // chunk: TEXT
		return &stmtText{text: asString(v[0])}, nil
	case 7: // chunk: statement ';'
		return v[0], nil
	case 8, 9, 10, 11, 12, 13, 14, 15:
		return v[0], nil
	case 16: // statement: expr
		return &stmtGet{e: buildExpr(asFlat(v[0]))}, nil
	case 17: // META metadata
		if m, ok := v[1].([][2]string); ok {
			p.metadata = append(p.metadata, m...)
		}
		return nil, nil
	case 18:
		return nil, nil
	case 19: // directive: setlist
		return setStmt(v[0], false), nil
	case 20, 21, 22, 23, 24, 25:
		return v[0], nil
	case 26: // atomexpr: expr
		return &stmtGet{e: buildExpr(asFlat(v[0]))}, nil
	case 27:
		return v[0], nil
	case 28: // GET expr
		return &stmtGet{e: buildExpr(asFlat(v[1]))}, nil
	case 29: // CALL expr
		return &stmtCall{e: buildExpr(asFlat(v[1]))}, nil
	case 30:
		return setStmt(v[1], false), nil
	case 31:
		return setStmt(v[1], true), nil
	case 32: // INSERT nameargs
		na := v[1].(*nameArgs)
		return &stmtInsert{names: na.names}, nil
	case 33, 34: // INCLUDE/PROCESS nameargs
		na := v[1].(*nameArgs)
		return &stmtInclude{names: na.names, named: na.args.named, process: rule == 34}, nil
	case 35: // THROW nameargs
		na := v[1].(*nameArgs)
		t := &stmtThrow{typ: na.names[0], named: na.args.named}
		if len(na.args.pos) > 0 {
			t.info = na.args.pos[0]
			t.pos = na.args.pos[1:]
		}
		return t, nil
	case 36:
		return &stmtReturn{}, nil
	case 37:
		return &stmtStop{}, nil
	case 38:
		return &stmtClear{}, nil
	case 39: // LAST
		return &stmtLast{label: p.blockLabel()}, nil
	case 40: // NEXT
		if p.inBlockNamed("FOR") {
			return &stmtNext{label: p.blockLabel(), advance: true}, nil
		}
		return &stmtNext{label: p.blockLabel()}, nil
	case 41: // DEBUG nameargs
		return nil, nil
	case 42, 43:
		return v[0], nil
	case 44: // IF expr ';' block else END
		return ifStmt(buildExpr(asFlat(v[1])), asBlock(v[3]), v[4]), nil
	case 45: // atomexpr IF expr
		return ifStmt(buildExpr(asFlat(v[2])), stmtBlock(v[0]), nil), nil
	case 46: // UNLESS
		return ifStmt(&exprNot{e: &exprParen{e: buildExpr(asFlat(v[1]))}}, asBlock(v[3]), v[4]), nil
	case 47:
		return ifStmt(&exprNot{e: &exprParen{e: buildExpr(asFlat(v[2]))}}, stmtBlock(v[0]), nil), nil
	case 48: // else: ELSIF expr ';' block else
		rest := v[4].([]any)
		return append([]any{ifBranch{cond: buildExpr(asFlat(v[1])), body: asBlock(v[3])}}, rest...), nil
	case 49: // ELSE ';' block
		return []any{asBlock(v[2])}, nil
	case 50:
		return []any{nil}, nil
	case 51: // SWITCH expr ';' block case END
		cases := v[4].([]any)
		s := &stmtSwitch{e: buildExpr(asFlat(v[1]))}
		last := cases[len(cases)-1]
		for _, c := range cases[:len(cases)-1] {
			s.cases = append(s.cases, c.(switchCase))
		}
		if last != nil {
			s.def, s.hasDef = last.(block), true
		}
		return s, nil
	case 52: // CASE term ';' block case
		rest := v[4].([]any)
		return append([]any{switchCase{match: v[1].(Expr), body: asBlock(v[3])}}, rest...), nil
	case 53: // CASE DEFAULT ';' block
		return []any{asBlock(v[3])}, nil
	case 54: // CASE ';' block
		return []any{asBlock(v[2])}, nil
	case 55:
		return []any{nil}, nil
	case 56:
		p.enterBlock("FOR")
		return nil, nil
	case 57: // FOR loopvar ';' @1-3 block END
		lv := v[1].(*loopVar)
		return &stmtForeach{target: lv.target, list: lv.list, body: asBlock(v[4]), label: p.leaveBlock()}, nil
	case 58: // atomexpr FOR loopvar
		lv := v[2].(*loopVar)
		return &stmtForeach{target: lv.target, list: lv.list, body: stmtBlock(v[0]), label: "LOOP"}, nil
	case 59:
		p.enterBlock("WHILE")
		return nil, nil
	case 60: // WHILE expr ';' @2-3 block END
		return &stmtWhile{cond: buildExpr(asFlat(v[1])), body: asBlock(v[4]), label: p.leaveBlock()}, nil
	case 61:
		return &stmtWhile{cond: buildExpr(asFlat(v[2])), body: stmtBlock(v[0]), label: "LOOP"}, nil
	case 62, 63: // IDENT ASSIGN|IN term args
		return &loopVar{target: asString(v[0]), list: v[2].(Expr), args: asArgs(v[3])}, nil
	case 64: // term args
		return &loopVar{list: v[0].(Expr), args: asArgs(v[1])}, nil
	case 65: // WRAPPER nameargs ';' block END
		na := v[1].(*nameArgs)
		return &stmtWrapper{names: na.names, named: na.args.named, body: asBlock(v[3])}, nil
	case 66:
		na := v[2].(*nameArgs)
		return &stmtWrapper{names: na.names, named: na.args.named, body: stmtBlock(v[0])}, nil
	case 67: // TRY ';' block final END
		t := &stmtTry{body: asBlock(v[2])}
		fin := v[3].([]any)
		last := fin[len(fin)-1]
		for _, c := range fin[:len(fin)-1] {
			t.catches = append(t.catches, c.(catchBlock))
		}
		if b, ok := last.(block); ok {
			t.final, t.hasFinal = b, true
		}
		return t, nil
	case 68: // CATCH filename ';' block final
		rest := v[4].([]any)
		return append([]any{catchBlock{typ: asString(v[1]), body: asBlock(v[3])}}, rest...), nil
	case 69: // CATCH DEFAULT ';' block final
		rest := v[4].([]any)
		return append([]any{catchBlock{body: asBlock(v[3])}}, rest...), nil
	case 70: // CATCH ';' block final
		rest := v[3].([]any)
		return append([]any{catchBlock{body: asBlock(v[2])}}, rest...), nil
	case 71: // FINAL ';' block
		return []any{asBlock(v[2])}, nil
	case 72:
		return []any{0}, nil
	case 73: // USE lnameargs
		na := v[1].(*nameArgs)
		return &stmtUse{name: na.names[0]}, nil
	case 74:
		return nil, nil
	case 75: // VIEW
		return &stmtThrowFixed{typ: "view", info: "VIEW is not supported"}, nil
	case 76, 78:
		return nil, nil
	case 77, 79: // PERL / RAWPERL (EVAL_PERL is not set)
		return &stmtThrowFixed{typ: "perl", info: "EVAL_PERL not set"}, nil
	case 80: // FILTER lnameargs ';' block END
		return filterStmt(v[1].(*nameArgs), asBlock(v[3])), nil
	case 81: // atomexpr FILTER lnameargs
		return filterStmt(v[2].(*nameArgs), stmtBlock(v[0])), nil
	case 82: // defblockname blockargs ';' template END
		name := strings.Join(p.defBlocks, "/")
		p.defBlocks = p.defBlocks[:len(p.defBlocks)-1]
		body := v[3].(*tmplBody)
		body.name = name
		p.defblock[name] = body
		return nil, nil
	case 83: // BLOCK blockname
		p.defBlocks = append(p.defBlocks, asString(v[1]))
		return v[1], nil
	case 84:
		return v[0], nil
	case 85: // blockname: LITERAL
		s := asString(v[0])
		if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
			s = s[1 : len(s)-1]
		}
		return s, nil
	case 86:
		return v[0], nil
	case 87:
		return nil, nil
	case 88: // BLOCK blockargs ';' block END
		return &stmtAnon{body: &tmplBody{body: asBlock(v[3])}}, nil
	case 89: // ident ASSIGN mdir
		return &stmtCapture{target: toTarget(v[0]), body: mdirBlock(v[2])}, nil
	case 90: // MACRO IDENT '(' margs ')' mdir
		return &stmtMacro{name: asString(v[1]), args: v[3].([]string), hasArgs: true, body: mdirBlock(v[5])}, nil
	case 91:
		return &stmtMacro{name: asString(v[1]), body: mdirBlock(v[2])}, nil
	case 92:
		return v[0], nil
	case 93: // BLOCK ';' block END
		return asBlock(v[2]), nil
	case 94:
		return append(v[0].([]string), asString(v[1])), nil
	case 95:
		return v[0], nil
	case 96:
		return []string{asString(v[0])}, nil
	case 97: // metadata meta
		m, _ := v[0].([][2]string)
		return append(m, v[1].([][2]string)...), nil
	case 98:
		return v[0], nil
	case 99:
		return v[0], nil
	case 100: // IDENT ASSIGN LITERAL
		s := asString(v[2])
		s = strings.TrimPrefix(s, "'")
		s = strings.TrimSuffix(s, "'")
		s = strings.ReplaceAll(s, `\'`, "'")
		return [][2]string{{asString(v[0]), s}}, nil
	case 101: // IDENT ASSIGN '"' TEXT '"'
		return [][2]string{{asString(v[0]), asString(v[3])}}, nil
	case 102:
		return [][2]string{{asString(v[0]), asString(v[2])}}, nil
	case 103, 104:
		return v[0], nil
	case 105: // '[' list ']'
		return &exprList{items: v[1].([]Expr)}, nil
	case 106: // '[' range ']'
		return &exprList{items: []Expr{v[1].(Expr)}}, nil
	case 107:
		return &exprList{}, nil
	case 108: // '{' hash '}'
		pairs, _ := v[1].([]hashPair)
		return &exprHash{pairs: pairs}, nil
	case 109: // sterm: ident
		return identExpr(asIdent(v[0])), nil
	case 110: // REF ident
		return &exprIdent{parts: asIdent(v[1]), ref: true}, nil
	case 111: // '"' quoted '"'
		return &exprQuoted{parts: v[1].([]Expr)}, nil
	case 112: // LITERAL
		return literalString(perlLiteral(asString(v[0]))), nil
	case 113: // NUMBER
		return &exprLiteral{v: perlNumberLiteral(asString(v[0]))}, nil
	case 114: // list term
		return append(v[0].([]Expr), v[1].(Expr)), nil
	case 115:
		return v[0], nil
	case 116:
		return []Expr{v[0].(Expr)}, nil
	case 117: // sterm TO sterm
		return &exprRange{from: v[0].(Expr), to: v[2].(Expr)}, nil
	case 118:
		return v[0], nil
	case 119:
		return []hashPair(nil), nil
	case 120:
		return append(v[0].([]hashPair), v[1].(hashPair)), nil
	case 121:
		return v[0], nil
	case 122:
		return []hashPair{v[0].(hashPair)}, nil
	case 123: // LITERAL ASSIGN expr
		return hashPair{key: literalString(perlLiteral(asString(v[0]))), val: buildExpr(asFlat(v[2]))}, nil
	case 124: // item ASSIGN expr
		return hashPair{key: v[0].(Expr), val: buildExpr(asFlat(v[2]))}, nil
	case 125: // ident DOT node
		return append(asIdent(v[0]), v[2].(identPart)), nil
	case 126: // ident DOT NUMBER
		parts := asIdent(v[0])
		for _, n := range strings.Split(asString(v[2]), ".") {
			parts = append(parts, identPart{name: &exprLiteral{v: perlNumberLiteral(n)}})
		}
		return parts, nil
	case 127:
		return []identPart{v[0].(identPart)}, nil
	case 128: // node: item
		return identPart{name: v[0].(Expr)}, nil
	case 129: // item '(' args ')'
		a := asArgs(v[2])
		if a.empty() {
			a = nil
		}
		return identPart{name: v[0].(Expr), args: a}, nil
	case 130: // item: IDENT
		return literalString(asString(v[0])), nil
	case 131: // '${' sterm '}'
		return v[1], nil
	case 132: // '$' IDENT
		return identExpr([]identPart{{name: literalString(asString(v[1]))}}), nil
	case 133, 134, 135: // expr BINOP|/|+ expr
		return joinFlat(asFlat(v[0]), asString(v[1]), asFlat(v[2])), nil
	case 136: // expr DIV expr
		inner := joinFlat(asFlat(v[0]), "/", asFlat(v[2]))
		return flatExpr{{atom: &exprInt{e: buildExpr(inner)}}}, nil
	case 137: // expr MOD expr
		return joinFlat(asFlat(v[0]), "%", asFlat(v[2])), nil
	case 138: // CMPOP
		return joinFlat(asFlat(v[0]), cmpOp(asString(v[1])), asFlat(v[2])), nil
	case 139: // CAT
		return joinFlat(asFlat(v[0]), ".", asFlat(v[2])), nil
	case 140:
		return joinFlat(asFlat(v[0]), "&&", asFlat(v[2])), nil
	case 141:
		return joinFlat(asFlat(v[0]), "||", asFlat(v[2])), nil
	case 142: // NOT expr
		return append(flatExpr{{op: "!"}}, asFlat(v[1])...), nil
	case 143: // expr ? expr : expr
		f := append(flatExpr{}, asFlat(v[0])...)
		f = append(f, piece{op: "?"})
		f = append(f, asFlat(v[2])...)
		f = append(f, piece{op: ":"})
		f = append(f, asFlat(v[4])...)
		return f, nil
	case 144: // '(' assign ')'
		a := v[1].(assignPair)
		return flatExpr{{atom: &exprAssign{target: a.target, val: a.val}}}, nil
	case 145: // '(' expr ')'
		return flatExpr{{atom: &exprParen{e: buildExpr(asFlat(v[1]))}}}, nil
	case 146: // expr: term
		return flatExpr{{atom: v[0].(Expr)}}, nil
	case 147: // setlist assign
		return append(v[0].([]assignPair), v[1].(assignPair)), nil
	case 148:
		return v[0], nil
	case 149:
		return []assignPair{v[0].(assignPair)}, nil
	case 150: // ident ASSIGN expr
		return assignPair{target: toTarget(v[0]), val: buildExpr(asFlat(v[2]))}, nil
	case 151: // LITERAL ASSIGN expr
		return assignPair{target: toTarget(v[0]), val: buildExpr(asFlat(v[2]))}, nil
	case 152: // args expr
		a := asArgs(v[0])
		a.pos = append(a.pos, buildExpr(asFlat(v[1])))
		return a, nil
	case 153: // args param
		a := asArgs(v[0])
		hp := v[1].(hashPair)
		a.named = append(a.named, namedArg{key: hp.key, val: hp.val})
		return a, nil
	case 154: // args ident ASSIGN expr
		a := asArgs(v[0])
		a.named = append(a.named, namedArg{set: &exprAssign{target: toTarget(v[1]), val: buildExpr(asFlat(v[3]))}})
		return a, nil
	case 155:
		return v[0], nil
	case 156:
		return &argList{}, nil
	case 157: // lvalue ASSIGN nameargs
		na := v[2].(*nameArgs)
		na.alias = v[0].(Expr)
		return na, nil
	case 158:
		return v[0], nil
	case 159: // lvalue: item
		return v[0], nil
	case 160:
		return &exprQuoted{parts: v[1].([]Expr)}, nil
	case 161:
		return literalString(perlLiteral(asString(v[0]))), nil
	case 162: // '$' ident args
		return &nameArgs{names: []Expr{identExpr(asIdent(v[1]))}, args: asArgs(v[2])}, nil
	case 163: // names args
		return &nameArgs{names: v[0].([]Expr), args: asArgs(v[1])}, nil
	case 164: // names '(' args ')'
		return &nameArgs{names: v[0].([]Expr), args: asArgs(v[2])}, nil
	case 165:
		return append(v[0].([]Expr), v[2].(Expr)), nil
	case 166:
		return []Expr{v[0].(Expr)}, nil
	case 167:
		return &exprQuoted{parts: v[1].([]Expr)}, nil
	case 168: // name: filename
		return literalString(asString(v[0])), nil
	case 169:
		return literalString(perlLiteral(asString(v[0]))), nil
	case 170:
		return asString(v[0]) + "." + asString(v[2]), nil
	case 171, 172, 173, 174:
		return v[0], nil
	case 175: // quoted quotable
		q := v[0].([]Expr)
		if v[1] != nil {
			q = append(q, v[1].(Expr))
		}
		return q, nil
	case 176:
		return []Expr{}, nil
	case 177: // quotable: ident
		return identExpr(asIdent(v[0])), nil
	case 178: // TEXT
		return literalString(asString(v[0])), nil
	case 179:
		return nil, nil
	}
	return nil, fmt.Errorf("unknown grammar rule %d", rule)
}

func mdirBlock(v any) block {
	if b, ok := v.(block); ok {
		return b
	}
	return stmtBlock(v)
}

func setStmt(v any, def bool) *stmtSet {
	s := &stmtSet{def: def}
	s.assigns = append(s.assigns, v.([]assignPair)...)
	return s
}

func ifStmt(cond Expr, body block, els any) *stmtIf {
	s := &stmtIf{branches: []ifBranch{{cond: cond, body: body}}}
	if els != nil {
		list := els.([]any)
		for _, e := range list[:len(list)-1] {
			s.branches = append(s.branches, e.(ifBranch))
		}
		if b := list[len(list)-1]; b != nil {
			s.els, s.hasElse = b.(block), true
		}
	}
	return s
}

func filterStmt(na *nameArgs, body block) *stmtFilter {
	f := &stmtFilter{name: na.names[0], alias: na.alias, body: body}
	if !na.args.empty() {
		f.args = na.args
	}
	return f
}

func joinFlat(a flatExpr, op string, b flatExpr) flatExpr {
	f := make(flatExpr, 0, len(a)+len(b)+1)
	f = append(f, a...)
	f = append(f, piece{op: op})
	return append(f, b...)
}

// ----------------------------------------------------------------------
// Perl precedence expression builder
// ----------------------------------------------------------------------

type exprBuilder struct {
	f   flatExpr
	pos int
}

func buildExpr(f flatExpr) Expr {
	if len(f) == 1 && f[0].op == "" {
		return f[0].atom
	}
	b := &exprBuilder{f: f}
	return b.ternary()
}

func (b *exprBuilder) peekOp() string {
	if b.pos < len(b.f) && b.f[b.pos].op != "" {
		return b.f[b.pos].op
	}
	return ""
}

func (b *exprBuilder) ternary() Expr {
	c := b.oror()
	if b.peekOp() == "?" {
		b.pos++
		t := b.ternary()
		if b.peekOp() == ":" {
			b.pos++
		}
		f := b.ternary()
		return &exprTernary{c: c, t: t, f: f}
	}
	return c
}

func (b *exprBuilder) oror() Expr {
	l := b.andand()
	for b.peekOp() == "||" {
		b.pos++
		r := b.andand()
		l = &exprBinary{op: "||", l: l, r: r}
	}
	return l
}

func (b *exprBuilder) andand() Expr {
	l := b.equality()
	for b.peekOp() == "&&" {
		b.pos++
		r := b.equality()
		l = &exprBinary{op: "&&", l: l, r: r}
	}
	return l
}

func (b *exprBuilder) equality() Expr {
	l := b.relational()
	var ch *exprChain
	for {
		op := b.peekOp()
		if op != "eq" && op != "ne" {
			break
		}
		b.pos++
		r := b.relational()
		if ch == nil {
			ch = &exprChain{terms: []Expr{l}}
		}
		ch.ops = append(ch.ops, op)
		ch.terms = append(ch.terms, r)
	}
	if ch != nil {
		return ch
	}
	return l
}

func (b *exprBuilder) relational() Expr {
	l := b.additive()
	var ch *exprChain
	for {
		op := b.peekOp()
		if op != "<" && op != ">" && op != "<=" && op != ">=" {
			break
		}
		b.pos++
		r := b.additive()
		if ch == nil {
			ch = &exprChain{terms: []Expr{l}}
		}
		ch.ops = append(ch.ops, op)
		ch.terms = append(ch.terms, r)
	}
	if ch != nil {
		return ch
	}
	return l
}

func (b *exprBuilder) additive() Expr {
	l := b.multiplicative()
	for {
		op := b.peekOp()
		if op != "+" && op != "-" && op != "." {
			break
		}
		b.pos++
		r := b.multiplicative()
		l = &exprBinary{op: op, l: l, r: r}
	}
	return l
}

func (b *exprBuilder) multiplicative() Expr {
	l := b.unary()
	for {
		op := b.peekOp()
		if op != "*" && op != "/" && op != "%" {
			break
		}
		b.pos++
		r := b.unary()
		l = &exprBinary{op: op, l: l, r: r}
	}
	return l
}

func (b *exprBuilder) unary() Expr {
	if b.peekOp() == "!" {
		b.pos++
		return &exprNot{e: b.unary()}
	}
	if b.pos < len(b.f) {
		a := b.f[b.pos].atom
		b.pos++
		return a
	}
	return &exprLiteral{v: nil}
}
