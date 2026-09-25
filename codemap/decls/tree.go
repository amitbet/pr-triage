package decls

import (
	"os"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
	"unsafe"

	ts "github.com/odvcencio/gotreesitter"
)

// The Java, C#, Python and Rust parsers run a tree-sitter grammar
// (gotreesitter, pure Go) and read declarations off the syntax tree. They
// also flatten the tree's leaves into the same Tok stream the old lexers
// produced, so the indexer's name resolution keeps working on tokens:
// identifiers and keywords are 'i', every punctuation character is its own
// 'p' token (&& is two), a string literal is one 's' token followed by the
// tokens of any code in its interpolation holes, and comments are dropped.
// Complexity is measured on the tree while it is still there.

func init() {
	// gotreesitter's process-heap ceiling calls runtime.ReadMemStats, which
	// stops the world, every few hundred parser steps on files over 64 KiB.
	// With a parser per core those stops serialize the indexer (3-5x slower).
	// Each parse is still bounded by its own arena and scratch budgets. An
	// explicit setting wins; the library reads it once, on first use.
	if _, ok := os.LookupEnv("GOT_PARSE_MEMORY_HARD_CEILING_MB"); !ok {
		os.Setenv("GOT_PARSE_MEMORY_HARD_CEILING_MB", "0")
	}
}

// grammar is a lazily loaded language with a pool of parsers, safe for
// concurrent use.
type grammar struct {
	load func() *ts.Language
	once sync.Once
	lang *ts.Language
	pool *ts.ParserPool
}

func (g *grammar) init() {
	g.once.Do(func() {
		g.lang = g.load()
		g.pool = ts.NewParserPool(g.lang, ts.WithParserPoolTimeoutMicros(30_000_000))
	})
}

// lexRules says how a grammar's leaves become tokens.
type lexRules struct {
	comments map[string]bool // comment node types
	strings  map[string]bool // node types lexed as one string token
	holes    map[string]bool // node types inside a string whose code is lexed
	numbers  bool            // keep numbers as 'n' tokens (dropped otherwise)
	atIdent  bool            // @name is the identifier name (C# verbatim identifiers)
	// node, when set, may handle a node itself (skip it, or lex only some
	// children) and report whether it did.
	node func(t *tree, n *ts.Node, typ string) bool
}

// tree is a parsed file and its token stream.
type tree struct {
	lang   *ts.Language
	src    []byte
	root   *ts.Node
	toks   []Tok
	starts []uint32    // byte offset of each token
	doc    map[int]int // token index -> first line of the comment block right above it
	rules  *lexRules
	names  []string // node type by symbol, filled as seen (Node.Type builds it each call)

	cStart, cEnd, lastLine int
}

// parse runs g over src and lexes the tree. release must be called when the
// caller is done with the nodes; the tokens stay valid.
func parse(g *grammar, src string, rules *lexRules) (t *tree, release func()) {
	g.init()
	// The parser only reads the source, so it can share the string's bytes.
	b := unsafe.Slice(unsafe.StringData(src), len(src))
	t = &tree{lang: g.lang, src: b, doc: map[int]int{}, rules: rules, names: make([]string, len(g.lang.SymbolNames))}
	tr, err := g.pool.Parse(b)
	if err != nil || tr == nil || tr.RootNode() == nil {
		if tr != nil {
			tr.Release()
		}
		return t, func() {}
	}
	t.root = tr.RootNode()
	t.lex(t.root)
	return t, tr.Release
}

func (t *tree) typ(n *ts.Node) string {
	sym := int(n.Symbol())
	if sym >= len(t.names) || n.IsError() {
		return n.Type(t.lang)
	}
	if t.names[sym] == "" {
		t.names[sym] = n.Type(t.lang)
	}
	return t.names[sym]
}

func (t *tree) text(n *ts.Node) string {
	if n == nil {
		return ""
	}
	return n.Text(t.src)
}

func (t *tree) field(n *ts.Node, name string) *ts.Node {
	if n == nil {
		return nil
	}
	return n.ChildByFieldName(name, t.lang)
}

// children lists n's children with their field names.
func (t *tree) children(n *ts.Node) ([]*ts.Node, []string) {
	c := n.ChildCount()
	nodes, fields := make([]*ts.Node, c), make([]string, c)
	for i := range c {
		nodes[i], fields[i] = n.Child(i), n.FieldNameForChild(i, t.lang)
	}
	return nodes, fields
}

// named returns n's named children.
func (t *tree) named(n *ts.Node) []*ts.Node {
	var out []*ts.Node
	for i := range n.ChildCount() {
		if c := n.Child(i); c.IsNamed() && !c.IsExtra() {
			out = append(out, c)
		}
	}
	return out
}

// has reports whether n has an anonymous child (a keyword) spelled kw, or
// a child of type typ whose text is kw (modifiers).
func (t *tree) hasWord(n *ts.Node, typ, kw string) bool {
	if n == nil {
		return false
	}
	for i := range n.ChildCount() {
		c := n.Child(i)
		switch {
		case !c.IsNamed() && t.typ(c) == kw:
			return true
		case typ != "" && t.typ(c) == typ && strings.TrimSpace(t.text(c)) == kw:
			return true
		}
	}
	return false
}

func line(n *ts.Node) int { return int(n.StartPoint().Row) + 1 }

// endLine is the last line n covers; a node that ends at the start of a
// line (a // comment with its newline) ends on the line before.
func endLine(n *ts.Node) int {
	p := n.EndPoint()
	if p.Column == 0 && p.Row > n.StartPoint().Row {
		return int(p.Row)
	}
	return int(p.Row) + 1
}

// tokAt returns the index of the first token at or after byte b.
func (t *tree) tokAt(b uint32) int {
	return sort.Search(len(t.starts), func(i int) bool { return t.starts[i] >= b })
}

// span returns the token range [start, end) of the nodes from..to.
func (t *tree) span(from, to *ts.Node) (int, int) {
	s, e := t.tokAt(from.StartByte()), t.tokAt(to.EndByte())
	return s, max(e, s)
}

// declLine is the first line of the declaration starting at token start,
// including the comment block right above it.
func (t *tree) declLine(start int) int {
	if l, ok := t.doc[start]; ok {
		return l
	}
	if start < len(t.toks) {
		return t.toks[start].Line
	}
	return 1
}

// lastLine is the line of the last token before end.
func (t *tree) lastLineOf(start, end int) int {
	if end > start && end <= len(t.toks) {
		return t.toks[end-1].Line
	}
	return t.declLine(start)
}

func (t *tree) emit(k byte, text string, ln int, at uint32) {
	if t.cStart > 0 && t.cEnd >= ln-1 {
		t.doc[len(t.toks)] = t.cStart
	}
	t.cStart, t.lastLine = 0, ln
	t.toks = append(t.toks, Tok{Kind: k, Text: text, Line: ln})
	t.starts = append(t.starts, at)
}

// comment records a comment for the doc-line rule: a block of comments on
// consecutive lines that ends on the line above a token (or on its line)
// belongs to it; a comment that follows code on its line does not.
func (t *tree) comment(start, end int) {
	if start == t.lastLine {
		return
	}
	if t.cStart == 0 || start > t.cEnd+1 {
		t.cStart = start
	}
	t.cEnd = end
}

func (t *tree) lex(n *ts.Node) {
	typ := t.typ(n)
	switch {
	case n.IsMissing():
		return
	case t.rules.comments[typ]:
		t.comment(line(n), endLine(n))
		return
	case t.rules.node != nil && t.rules.node(t, n, typ):
		return
	case t.rules.strings[typ]:
		t.lexString(n)
		return
	case n.ChildCount() == 0:
		t.leaf(n)
		return
	}
	for i := range n.ChildCount() {
		t.lex(n.Child(i))
	}
}

// lexString emits a string literal as one token (its content, with {} for
// each interpolation hole), then the tokens of the code in the holes.
func (t *tree) lexString(n *ts.Node) {
	var b strings.Builder
	var holes []*ts.Node
	// The content is the named leaves (fragments, escapes); quotes and
	// prefixes are anonymous or *_start/*_end.
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		typ := t.typ(n)
		switch {
		case t.rules.holes[typ]:
			holes = append(holes, n)
			b.WriteString("{}")
		case n.ChildCount() == 0:
			if n.IsNamed() && !strings.HasSuffix(typ, "_start") && !strings.HasSuffix(typ, "_end") {
				b.WriteString(t.text(n))
			}
		default:
			for i := range n.ChildCount() {
				walk(n.Child(i))
			}
		}
	}
	if n.ChildCount() == 0 { // a leaf literal: strip its quotes
		s := t.text(n)
		if len(s) >= 2 {
			s = s[1 : len(s)-1]
		}
		b.WriteString(s)
	} else {
		walk(n)
	}
	t.emit('s', b.String(), line(n), n.StartByte())
	for _, h := range holes {
		for _, c := range t.named(h) {
			switch t.typ(c) {
			case "interpolation_brace", "format_specifier", "type_conversion", "interpolation_format_clause", "interpolation_alignment_clause":
				continue
			}
			t.lex(c)
		}
	}
}

// leaf splits a leaf into identifier and punctuation tokens.
func (t *tree) leaf(n *ts.Node) {
	s := t.text(n)
	if s == "" {
		return
	}
	ln, at := line(n), n.StartByte()
	if c := s[0]; c >= '0' && c <= '9' || c == '.' && len(s) > 1 && s[1] >= '0' && s[1] <= '9' {
		if t.rules.numbers {
			t.emit('n', s, ln, at)
		}
		return
	}
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		switch {
		case isIdentRune(r):
			j := i
			for j < len(s) {
				r, w := utf8.DecodeRuneInString(s[j:])
				if !isIdentRune(r) && !unicode.IsDigit(r) {
					break
				}
				j += w
			}
			t.emit('i', s[i:j], ln, at+uint32(i))
			i = j
		case r == '@' && t.rules.atIdent && i+1 < len(s) && isIdentStart(s[i+1]):
			i++
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			i += w
		default:
			t.emit('p', s[i:i+w], ln, at+uint32(i))
			i += w
		}
	}
}

func isIdentRune(r rune) bool { return r == '_' || unicode.IsLetter(r) }

// stripSpace drops whitespace from a name as written (a . b -> a.b).
func stripSpace(s string) string {
	if !strings.ContainsAny(s, " \t\r\n") {
		return s
	}
	return strings.Join(strings.Fields(s), "")
}

// typeName is a type reference as written without type arguments or
// arguments: Base<T> -> Base, a.b.C -> a.b.C.
func typeName(s string) string {
	s = stripSpace(s)
	if i := strings.IndexAny(s, "<(["); i >= 0 {
		s = s[:i]
	}
	return s
}

func generatedHead(src string) bool {
	head := src
	if len(head) > 600 {
		head = head[:600]
	}
	return strings.Contains(head, "DO NOT EDIT") || strings.Contains(head, "Do not edit") ||
		strings.Contains(head, "auto-generated") || strings.Contains(head, "Generated by") ||
		strings.Contains(head, "@generated")
}

func lastSeg(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// cxRules says what counts for complexity in a grammar.
type cxRules struct {
	// decide lists what adds a path: keywords and operators (anonymous
	// leaves, e.g. "if", "&&") and node types (e.g. "ternary_expression").
	decide map[string]bool
	// arm, when set, reports whether a node adds a path by itself (a match
	// arm that is not the catch-all).
	arm func(t *tree, n *ts.Node, typ string) bool
	// nest lists the node types that open a nesting level.
	nest map[string]bool
	// ifs lists the if node types: an if that is the else branch of another
	// (directly, or through an else_clause) continues the chain at the same
	// depth.
	ifs map[string]bool
}

// measure returns the cyclomatic complexity (1 + decision points) and the
// deepest control-flow nesting below n. n itself does not nest.
func (t *tree) measure(n *ts.Node, r *cxRules) (cyclo, nest int) {
	cyclo = 1
	var walk func(n *ts.Node, typ string, depth int)
	walk = func(n *ts.Node, typ string, depth int) {
		for i := range n.ChildCount() {
			c := n.Child(i)
			if c.IsExtra() {
				continue
			}
			ct := t.typ(c)
			if r.decide[ct] || r.arm != nil && r.arm(t, c, ct) {
				cyclo++
			}
			d := depth
			elseIf := r.ifs[ct] && (typ == "else_clause" || r.ifs[typ] && n.FieldNameForChild(i, t.lang) == "alternative")
			if r.nest[ct] && !elseIf {
				d++
				nest = max(nest, d)
			}
			walk(c, ct, d)
		}
	}
	walk(n, t.typ(n), 0)
	return cyclo, nest
}

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

func isP(t Tok, s string) bool  { return t.Kind == 'p' && t.Text == s }
func isKw(t Tok, s string) bool { return t.Kind == 'i' && t.Text == s }
