package decls

import (
	"strconv"
	"strings"
	"unicode"

	ts "github.com/amitbet/pr-triage/internal/sitter"
)

// The Rust parser reads items off the tree-sitter tree: functions, structs,
// enums, unions, traits, type aliases, consts, statics, macro_rules, inline
// modules and impl blocks, and the functions and consts inside impls and
// traits. It also records what name resolution needs and tokens cannot
// give: use trees, struct field types, the declared type of parameters and
// let bindings, function return types and generic bounds. Nested functions
// and closures stay part of the function that holds them.

type RsDecl struct {
	Name string // f, Type, Type::method, Trait::method, inner::f
	// Kind: fn struct enum union trait type const static macro mod impl, or
	// method for functions in an impl or trait.
	Kind     string
	Mod      string // inline module path inside the file ("a::b"; "" at the top)
	Parent   int    // index in Decls of the mod, impl or trait holding the item, or -1
	Owner    string // impl blocks and their items: the type as written, without type arguments
	Trait    string // impl blocks and their items: the implemented trait as written ("" for inherent impls)
	Exported bool   // pub (not pub(crate)); items of a pub trait or of a trait impl
	Test     bool   // #[test] or #[cfg(test)], or inside such an item
	Supers   []string
	// Returns is a function's return type as a path (Self, Order,
	// crate::a::Order); Box, Arc, Option, Result and references are seen
	// through.
	Returns string
	// Vars maps parameters and let bindings with a known type to it; a
	// binding initialized by a call (let x = T::new()) maps to "=" and the
	// called path.
	Vars   map[string]string
	Locals map[string]bool   // every name a function binds (parameters, let, closures, for, match)
	Fields map[string]string // struct fields (0, 1, ... for tuple structs) -> type path
	Bounds map[string]string // generic parameter -> its first trait bound
	// Variants lists an enum's variants.
	Variants []string
	Start    int // token index (the first attribute)
	End      int // token index (exclusive)
	Line     int // first line, including doc comments and attributes
	EndLine  int
	Cyclo    int // functions: cyclomatic complexity
	Nest     int // and deepest control-flow nesting

	in    *RsDecl // Decls[Parent]
	index int     // d's own index in Decls
}

// In returns the mod, impl or trait holding d, or nil.
func (d *RsDecl) In() *RsDecl { return d.in }

// RsUse is one name bound by a use declaration. use a::{b, c as d, e::*}
// is three.
type RsUse struct {
	Path     string // crate::a::B; for globs the module or enum
	Local    string // the name bound ("" for globs)
	Wildcard bool
	Mod      string // inline module holding the use
	Pub      bool   // pub use: a re-export
}

// RsModFile is a mod x; declaration, whose code is in another file.
type RsModFile struct {
	Mod  string // the declared module as an inline-module path (a::x)
	Path string // #[path = "..."], relative to the declaring file's directory
	Test bool   // #[cfg(test)]
}

type RsFile struct {
	Path  string
	Toks  []Tok
	Uses  []RsUse
	Decls []*RsDecl // containers come before their items
	Mods  []RsModFile
	Lines int
	// Generated is set for @generated / DO NOT EDIT files.
	Generated bool
}

// Relink restores In after f was decoded (only Parent is serialized).
func (f *RsFile) Relink() {
	for _, d := range f.Decls {
		d.in = nil
		if d.Parent >= 0 && d.Parent < len(f.Decls) {
			d.in = f.Decls[d.Parent]
		}
	}
}

// IsFunc reports whether d is a function or method.
func (d *RsDecl) IsFunc() bool { return d.Kind == "fn" || d.Kind == "method" }

var rsGrammar = &grammar{load: loadRust}

var rsLex = &lexRules{
	comments: set("line_comment", "block_comment"),
	strings:  set("string_literal", "raw_string_literal", "char_literal"),
	numbers:  true, // tuple fields: self.0
}

var rsCx = &cxRules{
	decide: set("if", "while", "for", "&&", "||"),
	arm: func(t *tree, n *ts.Node, typ string) bool {
		if typ != "match_arm" {
			return false
		}
		p := t.field(n, "pattern")
		return p == nil || strings.TrimSpace(t.text(p)) != "_"
	},
	nest: set("if_expression", "while_expression", "for_expression", "loop_expression", "match_expression",
		"closure_expression"),
	ifs: set("if_expression"),
}

var rsItems = map[string]string{
	"function_item": "fn", "function_signature_item": "fn", "struct_item": "struct", "enum_item": "enum",
	"union_item": "union", "trait_item": "trait", "type_item": "type", "const_item": "const",
	"static_item": "static", "macro_definition": "macro", "mod_item": "mod", "impl_item": "impl",
}

// rsWrappers are seen through when a type names what a value is: a
// Box<dyn Repo> field is used as a Repo.
var rsWrappers = set("Box", "Arc", "Rc", "RefCell", "Cell", "Mutex", "RwLock", "Option", "Result", "Cow", "Pin",
	"Weak", "MutexGuard", "Ref", "RefMut")

type rsParser struct {
	f *RsFile
	t *tree
}

func ParseRust(p, src string) *RsFile {
	f := &RsFile{Path: p, Lines: strings.Count(src, "\n") + 1, Generated: generatedHead(src)}
	t, release := parse(rsGrammar, src, rsLex)
	defer release()
	f.Toks = t.toks
	if t.root == nil {
		return f
	}
	x := &rsParser{f: f, t: t}
	x.items(t.root, "", nil, false)
	x.uses(t.root, "")
	return f
}

// items reads the items of a file, inline module, impl or trait body.
func (x *rsParser) items(n *ts.Node, mod string, in *RsDecl, test bool) {
	t := x.t
	var attrs []*ts.Node // attribute_items right above the next item
	for _, c := range t.named(n) {
		typ := t.typ(c)
		if typ == "attribute_item" {
			attrs = append(attrs, c)
			continue
		}
		pending := attrs
		attrs = nil
		if typ == "ERROR" || typ == "declaration_list" {
			x.items(c, mod, in, test)
			continue
		}
		kind, ok := rsItems[typ]
		if !ok {
			continue
		}
		itemTest := test || rsTestAttr(t, pending)
		if kind == "mod" && t.field(c, "body") == nil { // mod x; is a file of its own
			x.f.Mods = append(x.f.Mods, RsModFile{Mod: rsJoin(mod, t.text(t.field(c, "name"))),
				Path: rsAttrValue(t, pending, "path"), Test: itemTest})
			continue
		}
		x.item(c, typ, kind, mod, in, itemTest, pending)
	}
}

func (x *rsParser) item(n *ts.Node, typ, kind, mod string, in *RsDecl, test bool, attrs []*ts.Node) {
	t := x.t
	d := &RsDecl{Kind: kind, Mod: mod, Parent: -1, in: in, Test: test}
	if in != nil {
		d.Parent = in.index
	}
	name := t.text(t.field(n, "name"))
	container := in != nil && (in.Kind == "impl" || in.Kind == "trait")
	switch {
	case kind == "impl":
		d.Owner = rsPath(t, t.field(n, "type"))
		d.Trait = rsPath(t, t.field(n, "trait"))
		name = rsLast(d.Owner)
		d.Bounds = x.bounds(n, nil)
	case container:
		owner := in.Owner
		if in.Kind == "trait" {
			owner = rsLast(in.Name)
		}
		d.Owner, d.Trait = owner, in.Trait
		if kind == "fn" {
			d.Kind = "method"
		}
		name = rsLast(owner) + "::" + name
	}
	if name == "" || name == "::" {
		return
	}
	d.Name = rsJoin(mod, name) // names carry the inline module path
	first := n
	if len(attrs) > 0 {
		first = attrs[0]
	}
	d.Start, d.End = t.span(first, n)
	d.Line, d.EndLine = t.declLine(d.Start), t.lastLineOf(d.Start, d.End)
	d.Exported = rsPub(t, n) || container && (in.Kind == "trait" && in.Exported || in.Trait != "")
	if kind == "macro" {
		d.Exported = rsHasAttr(t, attrs, "macro_export")
	}
	d.index = len(x.f.Decls)
	x.f.Decls = append(x.f.Decls, d)

	switch kind {
	case "fn":
		d.Returns = rsPath(t, t.field(n, "return_type"))
		d.Bounds = x.bounds(n, nil)
		d.Vars, d.Locals = map[string]string{}, map[string]bool{}
		if ps := t.field(n, "parameters"); ps != nil {
			for _, p := range t.named(ps) {
				if t.typ(p) != "parameter" {
					continue
				}
				pat := t.field(p, "pattern")
				x.binds(pat, d.Locals)
				if pat != nil && t.typ(pat) == "identifier" {
					if ty := rsPath(t, t.field(p, "type")); ty != "" {
						d.Vars[t.text(pat)] = ty
					}
				}
			}
		}
		if body := t.field(n, "body"); body != nil {
			x.locals(body, d)
			d.Cyclo, d.Nest = t.measure(body, rsCx)
		}
	case "struct", "union":
		d.Bounds = x.bounds(n, nil)
		d.Fields = map[string]string{}
		if body := t.field(n, "body"); body != nil {
			i := 0
			for _, fd := range t.named(body) {
				switch t.typ(fd) {
				case "field_declaration":
					d.Fields[t.text(t.field(fd, "name"))] = rsPath(t, t.field(fd, "type"))
				default: // ordered_field_declaration_list: types, possibly with visibility
					if fd.IsNamed() && t.typ(fd) != "visibility_modifier" && t.typ(fd) != "attribute_item" {
						d.Fields[strconv.Itoa(i)] = rsPath(t, fd)
						i++
					}
				}
			}
		}
	case "enum":
		if body := t.field(n, "body"); body != nil {
			for _, v := range t.named(body) {
				if t.typ(v) == "enum_variant" {
					d.Variants = append(d.Variants, t.text(t.field(v, "name")))
				}
			}
		}
	case "trait":
		d.Bounds = x.bounds(n, nil)
		if b := t.field(n, "bounds"); b != nil {
			for _, s := range t.named(b) {
				if p := rsPath(t, s); p != "" {
					d.Supers = append(d.Supers, p)
				}
			}
		}
		if body := t.field(n, "body"); body != nil {
			x.items(body, mod, d, test)
		}
	case "impl":
		if body := t.field(n, "body"); body != nil {
			x.items(body, mod, d, test)
		}
	case "mod":
		if body := t.field(n, "body"); body != nil {
			x.items(body, rsJoin(mod, t.text(t.field(n, "name"))), d, test)
		}
	}
}

// bounds reads generic parameters and where clauses: T: Trait.
func (x *rsParser) bounds(n *ts.Node, into map[string]string) map[string]string {
	t := x.t
	if into == nil {
		into = map[string]string{}
	}
	first := func(b *ts.Node) string {
		if b == nil {
			return ""
		}
		for _, c := range t.named(b) {
			if p := rsPath(t, c); p != "" {
				return p
			}
		}
		return ""
	}
	if tp := t.field(n, "type_parameters"); tp != nil {
		for _, p := range t.named(tp) {
			switch t.typ(p) {
			case "type_parameter", "constrained_type_parameter":
				name := t.field(p, "name")
				if name == nil {
					name = t.field(p, "left")
				}
				if b := first(t.field(p, "bounds")); name != nil && b != "" {
					into[t.text(name)] = b
				}
			}
		}
	}
	for _, c := range t.named(n) {
		if t.typ(c) != "where_clause" {
			continue
		}
		for _, w := range t.named(c) {
			if l := t.field(w, "left"); l != nil {
				if b := first(t.field(w, "bounds")); b != "" {
					into[stripSpace(t.text(l))] = b
				}
			}
		}
	}
	return into
}

// locals records what a function body binds, and the type of let bindings
// whose type is written or follows from the initializer.
func (x *rsParser) locals(n *ts.Node, d *RsDecl) {
	t := x.t
	for _, c := range t.named(n) {
		switch t.typ(c) {
		case "let_declaration":
			pat := t.field(c, "pattern")
			x.binds(pat, d.Locals)
			if pat != nil && t.typ(pat) == "mut_pattern" {
				if ps := t.named(pat); len(ps) > 0 {
					pat = ps[0]
				}
			}
			if pat != nil && t.typ(pat) == "identifier" {
				if ty := rsPath(t, t.field(c, "type")); ty != "" {
					d.Vars[t.text(pat)] = ty
				} else if v := rsInit(t, t.field(c, "value")); v != "" {
					d.Vars[t.text(pat)] = v
				}
			}
		case "closure_parameters":
			for _, p := range t.named(c) {
				x.binds(p, d.Locals)
			}
		case "for_expression":
			x.binds(t.field(c, "pattern"), d.Locals)
		case "let_condition":
			x.binds(t.field(c, "pattern"), d.Locals)
		case "match_pattern":
			x.binds(c, d.Locals)
		case "function_item", "impl_item", "struct_item", "enum_item", "trait_item":
			continue // nested items bind their own names
		}
		x.locals(c, d)
	}
}

// binds adds the identifiers a pattern binds.
func (x *rsParser) binds(p *ts.Node, into map[string]bool) {
	if p == nil {
		return
	}
	t := x.t
	switch t.typ(p) {
	case "identifier", "shorthand_field_identifier":
		// An upper-case name in a pattern is a constant or unit variant.
		if s := t.text(p); s != "" && !unicode.IsUpper(rune(s[0])) {
			into[s] = true
		}
		return
	case "scoped_identifier", "field_identifier", "type_identifier":
		return // an enum variant or path in a pattern
	}
	kids, fields := t.children(p)
	for i, c := range kids {
		switch {
		case !c.IsNamed(), fields[i] == "condition", fields[i] == "value":
		case t.typ(p) == "tuple_struct_pattern" && fields[i] == "type":
		case t.typ(p) == "struct_pattern" && fields[i] == "type":
		case t.typ(p) == "parameter" && fields[i] == "type":
		case t.typ(p) == "field_pattern" && fields[i] == "name" && len(kids) > 1:
		default:
			x.binds(c, into)
		}
	}
}

// rsInit names what a let initializer is: the called path for T::new(..)
// or f(..) ("=" prefix), the struct for T { .. }. ?, .await and & are seen
// through.
func rsInit(t *tree, v *ts.Node) string {
	for v != nil {
		switch t.typ(v) {
		case "try_expression", "reference_expression", "parenthesized_expression":
			ns := t.named(v)
			if len(ns) == 0 {
				return ""
			}
			v = ns[0]
		case "await_expression":
			ns := t.named(v)
			if len(ns) == 0 {
				return ""
			}
			v = ns[0]
		case "call_expression":
			fn := t.field(v, "function")
			if fn == nil {
				return ""
			}
			switch t.typ(fn) {
			case "identifier", "scoped_identifier":
				return "=" + stripSpace(t.text(fn))
			case "generic_function":
				return "=" + stripSpace(t.text(t.field(fn, "function")))
			}
			return ""
		case "struct_expression":
			return typeName(stripSpace(t.text(t.field(v, "name"))))
		default:
			return ""
		}
	}
	return ""
}

// rsPath names the type a type expression is used as: references, dyn,
// impl and wrapper types are seen through, type arguments dropped.
func rsPath(t *tree, n *ts.Node) string {
	for n != nil {
		switch t.typ(n) {
		case "reference_type", "pointer_type":
			n = t.field(n, "type")
		case "dynamic_type", "abstract_type":
			tr := t.field(n, "trait")
			if tr == nil {
				ns := t.named(n)
				if len(ns) == 0 {
					return ""
				}
				tr = ns[0]
			}
			n = tr
		case "trait_bounds":
			ns := t.named(n)
			if len(ns) == 0 {
				return ""
			}
			n = ns[0]
		case "generic_type":
			base := stripSpace(t.text(t.field(n, "type")))
			if args := t.field(n, "type_arguments"); args != nil && rsWrappers[rsLast(base)] {
				if as := t.named(args); len(as) > 0 {
					n = as[0]
					continue
				}
			}
			return base
		case "type_identifier", "scoped_type_identifier", "identifier", "scoped_identifier":
			return stripSpace(t.text(n))
		default:
			return ""
		}
	}
	return ""
}

func rsPub(t *tree, n *ts.Node) bool {
	for _, c := range t.named(n) {
		if t.typ(c) == "visibility_modifier" {
			return stripSpace(t.text(c)) == "pub"
		}
	}
	return false
}

// rsAttrs lists the attributes as written, without spaces: derive(Debug),
// path="x.rs".
func rsAttrs(t *tree, attrs []*ts.Node) []string {
	var out []string
	for _, a := range attrs {
		for _, c := range t.named(a) {
			out = append(out, stripSpace(t.text(c)))
		}
	}
	return out
}

func rsHasAttr(t *tree, attrs []*ts.Node, name string) bool {
	for _, s := range rsAttrs(t, attrs) {
		if s == name || strings.HasPrefix(s, name+"(") {
			return true
		}
	}
	return false
}

// rsAttrValue returns the string of #[name = "value"].
func rsAttrValue(t *tree, attrs []*ts.Node, name string) string {
	for _, s := range rsAttrs(t, attrs) {
		if v, ok := strings.CutPrefix(s, name+"="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// rsTestAttr reports #[test], #[tokio::test(...)] and the like, and
// #[cfg(test)] or #[cfg(all(test, ...))] (not #[cfg(not(test))]).
func rsTestAttr(t *tree, attrs []*ts.Node) bool {
	for _, s := range rsAttrs(t, attrs) {
		name, args, _ := strings.Cut(s, "(")
		switch {
		case name == "test" || strings.HasSuffix(name, "::test"):
			return true
		case name == "cfg" && !strings.Contains(args, "not(") && strings.Contains(","+strings.NewReplacer("(", ",", ")", ",").Replace(args)+",", ",test,"):
			return true
		}
	}
	return false
}

// uses reads every use declaration, at any depth, with the inline module
// holding it.
func (x *rsParser) uses(n *ts.Node, mod string) {
	t := x.t
	for _, c := range t.named(n) {
		switch t.typ(c) {
		case "use_declaration":
			pub := false
			for _, v := range t.named(c) {
				if t.typ(v) == "visibility_modifier" {
					pub = stripSpace(t.text(v)) == "pub"
				}
			}
			x.useTree(t.field(c, "argument"), "", mod, pub)
		case "mod_item":
			if body := t.field(c, "body"); body != nil {
				x.uses(body, rsJoin(mod, t.text(t.field(c, "name"))))
			}
		default:
			x.uses(c, mod)
		}
	}
}

func (x *rsParser) useTree(n *ts.Node, prefix, mod string, pub bool) {
	if n == nil {
		return
	}
	t := x.t
	add := func(u RsUse) {
		u.Mod, u.Pub = mod, pub
		x.f.Uses = append(x.f.Uses, u)
	}
	switch t.typ(n) {
	case "scoped_use_list":
		p := prefix
		if pn := t.field(n, "path"); pn != nil {
			p = rsJoin(prefix, stripSpace(t.text(pn)))
		}
		x.useTree(t.field(n, "list"), p, mod, pub)
	case "use_list":
		for _, c := range t.named(n) {
			x.useTree(c, prefix, mod, pub)
		}
	case "use_as_clause":
		p := rsJoin(prefix, stripSpace(t.text(t.field(n, "path"))))
		if alias := t.text(t.field(n, "alias")); alias != "_" {
			add(RsUse{Path: p, Local: alias})
		}
	case "use_wildcard":
		p := prefix
		for _, c := range t.named(n) {
			p = rsJoin(prefix, stripSpace(t.text(c)))
		}
		add(RsUse{Path: p, Wildcard: true})
	default: // identifier, scoped_identifier, self, crate, super
		p := rsJoin(prefix, stripSpace(t.text(n)))
		local := rsLast(p)
		if local == "self" { // use a::{self}
			p = strings.TrimSuffix(strings.TrimSuffix(p, "self"), "::")
			local = rsLast(p)
		}
		if p != "" {
			add(RsUse{Path: p, Local: local})
		}
	}
}

func rsJoin(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "::" + b
}

// rsLast is the last segment of a path.
func rsLast(p string) string {
	if i := strings.LastIndex(p, "::"); i >= 0 {
		return p[i+2:]
	}
	return p
}

// RustDecls lists every item with its line range. Modules, impls and traits
// enclose their items; use Innermost to place a line.
func RustDecls(src string) []Decl {
	f := ParseRust("", src)
	out := make([]Decl, 0, len(f.Decls))
	for _, d := range f.Decls {
		out = append(out, Decl{Name: d.Name, Start: d.Line, End: max(d.EndLine, d.Line)})
	}
	return out
}
