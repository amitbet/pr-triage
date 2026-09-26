package decls

import (
	"path"
	"strings"

	ts "github.com/amitbet/pr-triage/internal/sitter"
)

// Shell, PowerShell, C, C++, PHP, Scala, Kotlin, Ruby, Swift and Dart share
// one parser driven by a small spec per language (gen_langs.go). It reads
// types (and the members in them), functions and methods off the
// tree-sitter tree, the file's imports and package, and lexes the leaves
// into the same Tok stream as the other parsers. Name resolution happens in
// the indexer on those tokens, the same way for every language here.

// Lang describes one language the generic parser handles.
type Lang struct {
	ID     string // node key prefix and file language: sh, ps1, c, cpp, php, scala, kt, rb, swift, dart
	Family string // languages resolved together (C and C++ share headers)
	Exts   []string
	// TypeSep joins a nested type to its outer type, MemberSep a member to
	// its type.
	TypeSep, MemberSep string
	Self, Super        map[string]bool // receiver words: this/self, super/parent
	// ImplicitSelf: an unqualified name can be a member of the enclosing
	// type (not in PHP, PowerShell or shell).
	ImplicitSelf bool
	// VarPrefix marks variables ($x in PHP and PowerShell).
	VarPrefix string
	// CtorAlias is the member name that constructs (Ruby's new).
	CtorAlias string
	// chunks: re-parse a file that fails as a whole one top-level
	// declaration at a time.
	chunks bool
	// OutOfLine: members are declared in the type and defined outside it
	// (C++): a top-level function named A::f is a member of A, and names at
	// the type's own level are declarations, not uses.
	OutOfLine bool

	g   *grammar
	lex *lexRules
	cx  *genCx
	// decl reports what n declares: kind and name, or "" for nothing.
	decl func(t *tree, n *ts.Node, typ string) (kind, name string)
	// body returns the node holding a type's members; nil means the
	// "body" field.
	body func(t *tree, n *ts.Node) *ts.Node
	// through lists node types searched for declarations at the same level
	// (wrappers, preprocessor branches, parse errors).
	through map[string]bool
	// namespace reports a namespace or package: its name and body, or a
	// nil body for one that covers the rest of the file.
	namespace func(t *tree, n *ts.Node, typ string) (name string, body *ts.Node, ok bool)
	supers    func(t *tree, n *ts.Node) []string
	private   func(t *tree, n *ts.Node, owner *GenDecl) bool
	// section reports an access section (C++ public:, Ruby private) that
	// applies to the members after it.
	section func(t *tree, n *ts.Node, typ string) (private, ok bool)
	// privateDefault reports the members of a type kind that are private
	// until a section says otherwise (C++ class).
	privateDefault map[string]bool
	// bodySibling lists node types that are the body of the declaration
	// right before them (Dart signatures).
	bodySibling map[string]bool
	ctorName    string // method name that is a constructor (__construct, initialize, init)
	importTypes map[string]bool
	imports     func(t *tree, n *ts.Node, typ string, f *GenFile)
	test        func(p, base string) bool
	generated   func(p string) bool
	module      func(p string) string
}

// GenDecl is a type or member of a file the generic parser read.
type GenDecl struct {
	Name      string // Type, Outer::Inner, Type.method, function
	Simple    string // the last segment of Name
	Kind      string // class struct interface enum module object trait protocol extension mixin union typedef | function method ctor property macro
	Owner     string // enclosing type's Name, or the qualifier of an out-of-line C++ definition; "" at top level
	Namespace string // package or namespace, dot-separated
	Exported  bool
	Supers    []string // base types, interfaces and mixins as written
	Start     int      // token index
	End       int      // token index (exclusive)
	Line      int      // first line, including the doc comment above
	EndLine   int
	Cyclo     int // members: cyclomatic complexity
	Nest      int // and deepest control-flow nesting
}

var genTypeKinds = set("class", "struct", "interface", "enum", "module", "object", "trait", "protocol",
	"extension", "mixin", "union", "typedef", "actor")

// IsType reports whether d declares a type rather than a function or member.
func (d *GenDecl) IsType() bool { return genTypeKinds[d.Kind] }

// GenImport is a file or name a file brings into scope.
type GenImport struct {
	Path     string // a file path as written (./x is relative to the file), or a dotted name
	Alias    string
	File     bool // source, #include, require, Dart import
	Wildcard bool // every name in package Path
	Module   bool // a Swift module
}

type GenFile struct {
	Path       string
	Namespaces []string // every package or namespace declared
	Toks       []Tok
	Decls      []*GenDecl // a type comes before its members
	Imports    []GenImport
	Lines      int
	Generated  bool
}

// Lang is the file's language. (Not a field: GenFile goes through gob in
// the indexer's parse cache.)
func (f *GenFile) Lang() *Lang { return LangFor(f.Path) }

// IsTest reports whether p is test code in l.
func (l *Lang) IsTest(p string) bool {
	for _, s := range strings.Split(path.Dir(p), "/") {
		switch s {
		case "test", "tests", "spec", "specs", "__tests__", "testing", "Tests", "androidTest", "integration_test":
			return true
		}
	}
	return l.test != nil && l.test(p, path.Base(p))
}

// Module is the module p belongs to ("" for the whole repo). Only Swift
// has them: the directory under Sources/.
func (l *Lang) Module(p string) string {
	if l.module == nil {
		return ""
	}
	return l.module(p)
}

var genByExt = map[string]*Lang{}

func init() {
	for _, l := range genLangs {
		for _, e := range l.Exts {
			genByExt[e] = l
		}
	}
}

// LangFor returns the generic-parser language of p, or nil.
func LangFor(p string) *Lang { return genByExt[strings.ToLower(path.Ext(p))] }

// Langs lists the languages of the generic parser.
func Langs() []*Lang { return genLangs }

// ParseGen parses a file of any generic-parser language; nil if p is none.
func ParseGen(p, src string) *GenFile {
	l := LangFor(p)
	if l == nil {
		return nil
	}
	return l.Parse(p, src)
}

func (l *Lang) Parse(p, src string) *GenFile {
	src = strings.TrimPrefix(src, "\ufeff")
	f := &GenFile{Path: p, Lines: strings.Count(src, "\n") + 1,
		Generated: generatedHead(src) || l.generated != nil && l.generated(p)}
	if l.Family == "c" {
		src = blankData(src)
	}
	t, release := parse(l.g, src, l.lex)
	if l.chunks && t.root != nil && errorShare(t, f.Lines) > 0.2 {
		release()
		ns := ""
		for _, c := range topChunks(src) {
			// Newlines in front keep the chunk's line numbers.
			ct, rel := parse(l.g, strings.Repeat("\n", c.line-1)+c.text, l.lex)
			ns = l.read(f, ct, ns)
			rel()
		}
		l.fileLocal(f)
		return f
	}
	defer release()
	l.read(f, t, "")
	l.fileLocal(f)
	return f
}

// fileLocal marks what only its own file can see: a macro defined in a C
// or C++ source file rather than a header.
func (l *Lang) fileLocal(f *GenFile) {
	if l.Family != "c" || strings.HasPrefix(path.Ext(f.Path), ".h") {
		return
	}
	for _, d := range f.Decls {
		if d.Kind == "macro" {
			d.Exported = false
		}
	}
}

// read adds a parsed tree's declarations, imports and tokens to f. ns is
// the package in effect at its start; read returns the one at its end.
func (l *Lang) read(f *GenFile, t *tree, ns string) string {
	x := &genParser{f: f, t: t, l: l, base: len(f.Toks)}
	f.Toks = append(f.Toks, t.toks...)
	if t.root == nil {
		return ns
	}
	ns = x.walk(t.root, nil, ns, false)
	if l.imports != nil {
		x.scanImports(t.root)
	}
	return ns
}

// errorShare is the share of the file's lines under top-level parse
// errors. gotreesitter sometimes gives up on a whole C++ file (macros it
// can't expand, ambiguous casts) that parses fine a declaration at a time.
func errorShare(t *tree, lines int) float64 {
	n := 0
	for i := range t.root.ChildCount() {
		if c := t.root.Child(i); c.IsError() {
			n += int(c.EndPoint().Row-c.StartPoint().Row) + 1
		}
	}
	return float64(n) / float64(max(lines, 1))
}

// blankData replaces the inside of large brace initializers that hold
// only numbers (font bitmaps, images, register tables) with spaces, keeping
// newlines. They declare nothing, and gotreesitter parses a long flat list
// superlinearly: a 40 KB font file hit the 30 s parse timeout.
func blankData(src string) string {
	var b []byte
	for i := 0; i < len(src); i++ {
		if src[i] != '{' {
			continue
		}
		j, depth, data := i+1, 1, true
		for ; j < len(src) && depth > 0 && data; j++ {
			switch c := src[j]; {
			case c == '{':
				depth++
			case c == '}':
				depth--
			case c == '/' && j+1 < len(src) && src[j+1] == '*': // a comment
				if k := strings.Index(src[j+2:], "*/"); k >= 0 {
					j += k + 3
				} else {
					data = false
				}
			case c == '/' && j+1 < len(src) && src[j+1] == '/', c == '#' && src[j-1] == '\n': // a comment, #if LV_COLOR_DEPTH
				if k := strings.IndexByte(src[j:], '\n'); k >= 0 {
					j += k - 1
				} else {
					data = false
				}
			case c == '.' && j+1 < len(src) && isIdentStart(src[j+1]): // .field = 1
				for j+1 < len(src) && (isIdentStart(src[j+1]) || src[j+1] >= '0' && src[j+1] <= '9') {
					j++
				}
			case c >= '0' && c <= '9', c == ',', c == ' ', c == '\t', c == '\n', c == '\r', c == 'x', c == 'X', c == '=',
				c >= 'a' && c <= 'f', c >= 'A' && c <= 'F', c == 'u', c == 'U', c == 'l', c == 'L', c == '.', c == '-', c == '+':
			default:
				data = false
			}
		}
		if !data || depth > 0 || j-i < 2048 {
			continue
		}
		if b == nil {
			b = []byte(src)
		}
		for k := i + 1; k < j-1; k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
		i = j - 1
	}
	if b == nil {
		return src
	}
	return string(b)
}

type chunk struct {
	line int // first line
	text string
}

// topChunks splits C-like source at top-level declarations: code starting
// in column 0 after a line that ends a statement or block. Comments and
// blank lines go with the code below them.
func topChunks(src string) []chunk {
	lines := strings.SplitAfter(src, "\n")
	var out []chunk
	start, lastCode := 0, -1
	ended := true // the last code line ended a statement
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		switch {
		case trim == "", strings.HasPrefix(trim, "//"), strings.HasPrefix(trim, "/*"), strings.HasPrefix(trim, "*"):
			continue
		}
		c := ln[0]
		if ended && c != ' ' && c != '\t' && c != '}' && c != ')' && c != '{' && lastCode >= 0 && i > start {
			out = append(out, chunk{start + 1, strings.Join(lines[start:lastCode+1], "")})
			start = lastCode + 1
		}
		lastCode = i
		ended = trim[0] == '#' || strings.HasSuffix(trim, ";") || strings.HasSuffix(trim, "}") || strings.HasSuffix(trim, "{") ||
			strings.HasSuffix(trim, "*/")
		if strings.HasSuffix(trim, "\\") {
			ended = false // a macro continues
		}
	}
	if start < len(lines) {
		out = append(out, chunk{start + 1, strings.Join(lines[start:], "")})
	}
	return out
}

type genParser struct {
	f    *GenFile
	t    *tree
	l    *Lang
	base int // index of the tree's first token in f.Toks
}

func (x *genParser) scanImports(n *ts.Node) {
	for i := range n.ChildCount() {
		c := n.Child(i)
		if !c.IsNamed() {
			continue
		}
		typ := x.t.typ(c)
		if x.l.importTypes[typ] {
			x.l.imports(x.t, c, typ, x.f)
		}
		if c.ChildCount() > 0 {
			x.scanImports(c)
		}
	}
}

// walk reads the declarations below n and returns the namespace in effect
// after them (a file-scoped package changes it).
func (x *genParser) walk(n *ts.Node, owner *GenDecl, ns string, private bool) string {
	t, l := x.t, x.l
	kids := t.named(n)
	for i, c := range kids {
		typ := t.typ(c)
		if l.section != nil {
			if p, ok := l.section(t, c, typ); ok {
				private = p
				continue
			}
		}
		if l.namespace != nil {
			if name, body, ok := l.namespace(t, c, typ); ok {
				full := name
				if ns != "" && name != "" {
					full = ns + "." + name
				} else if name == "" {
					full = ns
				}
				if name != "" {
					x.f.Namespaces = appendNew(x.f.Namespaces, full)
				}
				if body != nil {
					x.walk(body, owner, full, private || name == "") // an anonymous namespace is file-private
				} else {
					ns = full // the rest of the file
				}
				continue
			}
		}
		kind, name := l.decl(t, c, typ)
		if kind == "" || name == "" {
			if l.through[typ] {
				x.walk(c, owner, ns, private)
			}
			continue
		}
		end := c
		if l.bodySibling != nil && i+1 < len(kids) && l.bodySibling[t.typ(kids[i+1])] {
			end = kids[i+1]
		}
		d := x.add(c, end, kind, name, ns, owner)
		d.Exported = !private && (l.private == nil || !l.private(t, c, owner))
		if d.IsType() {
			if l.supers != nil {
				d.Supers = l.supers(t, c)
			}
			body := t.field(c, "body")
			if l.body != nil {
				body = l.body(t, c)
			}
			if body != nil {
				x.walk(body, d, ns, l.privateDefault[d.Kind])
			}
			continue
		}
		d.Cyclo, d.Nest = l.cx.measure(t, c)
		if end != c {
			cy, ne := l.cx.measure(t, end)
			d.Cyclo, d.Nest = d.Cyclo+cy-1, max(d.Nest, ne)
		}
	}
	return ns
}

func (x *genParser) add(from, to *ts.Node, kind, name, ns string, owner *GenDecl) *GenDecl {
	l := x.l
	name = stripSpace(name)
	isType := genTypeKinds[kind]
	d := &GenDecl{Name: name, Simple: name, Kind: kind, Namespace: ns}
	switch {
	case owner != nil:
		sep := l.MemberSep
		if isType {
			sep = l.TypeSep
		}
		d.Name, d.Owner = owner.Name+sep+name, owner.Name
		if kind == "function" {
			d.Kind = "method"
		}
		if !isType && (name == owner.Simple || l.ctorName != "" && name == l.ctorName) {
			d.Kind = "ctor"
		}
	case !isType && l.OutOfLine:
		if i := strings.LastIndex(name, l.MemberSep); i > 0 {
			d.Owner, d.Simple, d.Kind = name[:i], name[i+len(l.MemberSep):], "method"
			if d.Simple == lastSegSep(d.Owner, l.TypeSep) {
				d.Kind = "ctor"
			}
		}
	}
	if isType {
		d.Simple = lastSegSep(name, l.TypeSep)
	}
	d.Start, d.End = x.t.span(from, to)
	d.Line, d.EndLine = x.t.declLine(d.Start), x.t.lastLineOf(d.Start, d.End)
	d.Start, d.End = d.Start+x.base, d.End+x.base
	x.f.Decls = append(x.f.Decls, d)
	return d
}

func lastSegSep(s, sep string) string {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[i+len(sep):]
	}
	return s
}

func appendNew(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// genCx says what counts for complexity in a generic-parser language.
type genCx struct {
	// decide lists keywords and operators (anonymous leaves) and node
	// types that add a path.
	decide map[string]bool
	// leaves: decide matches anonymous nodes only (Ruby's if node is
	// named "if" too).
	leaves bool
	// arm, when set, reports whether a node adds a path by itself.
	arm func(t *tree, n *ts.Node, typ string) bool
	// nest lists the named node types that open a nesting level.
	nest map[string]bool
	// ifs lists the if node types: an if right after an else keyword (or
	// the only statement of the else branch) continues the chain at the
	// same depth.
	ifs map[string]bool
}

// measure returns the cyclomatic complexity (1 + decision points) and the
// deepest control-flow nesting below n. n itself does not nest.
func (r *genCx) measure(t *tree, n *ts.Node) (cyclo, nest int) {
	cyclo = 1
	var walk func(n *ts.Node, depth int, afterElse bool)
	walk = func(n *ts.Node, depth int, afterElse bool) {
		prevElse := false
		only := len(t.named(n)) == 1
		for i := range n.ChildCount() {
			c := n.Child(i)
			if c.IsExtra() {
				continue
			}
			ct := t.typ(c)
			named := c.IsNamed()
			if r.decide[ct] && (!r.leaves || !named) || r.arm != nil && r.arm(t, c, ct) {
				cyclo++
			}
			d := depth
			if named && r.nest[ct] && !(r.ifs[ct] && (prevElse || afterElse && only)) {
				d++
				nest = max(nest, d)
			}
			walk(c, d, prevElse)
			prevElse = ct == "else" // a keyword; Swift names it
		}
	}
	walk(n, 0, false)
	return cyclo, nest
}

// genLex builds lexing rules: whole lists leaf-ish node types that are one
// identifier token (a command name with dashes, a Ruby method name ending
// in ?), str node types that become one string token and are not resolved,
// skip node types that produce no tokens.
func genLex(r *lexRules, whole, str, skip []string) *lexRules {
	w, s, k := set(whole...), set(str...), set(skip...)
	r.node = func(t *tree, n *ts.Node, typ string) bool {
		switch {
		case k[typ]:
		case w[typ]:
			if txt := strings.TrimSpace(t.text(n)); txt != "" {
				t.emit('i', txt, line(n), n.StartByte())
			}
		case s[typ]:
			t.emit('s', t.text(n), line(n), n.StartByte())
		default:
			return false
		}
		return true
	}
	return r
}

// GenDecls lists every type and member with its line range. Types enclose
// their members; use Innermost to place a line.
func GenDecls(p, src string) []Decl {
	f := ParseGen(p, src)
	if f == nil {
		return nil
	}
	out := make([]Decl, 0, len(f.Decls))
	seen := map[string]bool{}
	for _, d := range f.Decls {
		if seen[d.Name] {
			continue // overloads, reopened types: the first range
		}
		seen[d.Name] = true
		out = append(out, Decl{Name: d.Name, Start: d.Line, End: max(d.EndLine, d.Line)})
	}
	return out
}

// Helpers the language specs share.

func fieldText(t *tree, n *ts.Node, field string) string { return t.text(t.field(n, field)) }

// firstOf returns n's first named child of one of the types.
func firstOf(t *tree, n *ts.Node, types ...string) *ts.Node {
	for _, c := range t.named(n) {
		ct := t.typ(c)
		for _, ty := range types {
			if ct == ty {
				return c
			}
		}
	}
	return nil
}

// allOf returns n's named children of the types.
func allOf(t *tree, n *ts.Node, types ...string) []*ts.Node {
	var out []*ts.Node
	for _, c := range t.named(n) {
		ct := t.typ(c)
		for _, ty := range types {
			if ct == ty {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// find returns the first node of one of the types at or below n, depth
// first, at most depth levels down.
func find(t *tree, n *ts.Node, depth int, types ...string) *ts.Node {
	if n == nil {
		return nil
	}
	for _, ty := range types {
		if t.typ(n) == ty {
			return n
		}
	}
	if depth == 0 {
		return nil
	}
	for _, c := range t.named(n) {
		if r := find(t, c, depth-1, types...); r != nil {
			return r
		}
	}
	return nil
}

// modText reports whether n, or a modifiers child of n, has a modifier
// child of type typ whose text is one of words.
func modText(t *tree, n *ts.Node, typ string, words ...string) bool {
	for _, c := range t.named(n) {
		ct := t.typ(c)
		if ct == "modifiers" {
			if modText(t, c, typ, words...) {
				return true
			}
			continue
		}
		if ct != typ {
			continue
		}
		s := strings.TrimSpace(t.text(c))
		for _, w := range words {
			if s == w || strings.HasPrefix(s, w+"[") {
				return true
			}
		}
	}
	return false
}

// unquote strips a literal's quotes.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'' || s[0] == '`') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// stripTemplates drops <...> from a C++ name: A<T>::f -> A::f.
func stripTemplates(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	var b strings.Builder
	d := 0
	for _, r := range s {
		switch {
		case r == '<':
			d++
		case r == '>' && d > 0:
			d--
		case d == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func hasSuffixAny(s string, sufs ...string) bool {
	for _, x := range sufs {
		if strings.HasSuffix(s, x) {
			return true
		}
	}
	return false
}
