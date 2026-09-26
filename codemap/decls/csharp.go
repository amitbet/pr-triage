package decls

import (
	"strings"

	ts "github.com/amitbet/pr-triage/internal/sitter"
)

// The C# parser reads namespaces (block and file-scoped), usings, types
// (nested ones too; each part of a partial type separately), and methods,
// constructors, destructors, properties, indexers, events with accessors
// and operators off the tree-sitter tree. Fields and events without
// accessors stay part of their type; local functions, lambdas and
// anonymous types part of the member that holds them. Both sides of an #if
// are parsed; preprocessor lines produce no tokens.

type CSDecl struct {
	Name      string // Outer, Outer.Inner, Outer.Method
	Kind      string // class interface struct enum record delegate method ctor property
	Namespace string
	Owner     string   // enclosing type ("" for top-level types)
	Exported  bool     // public, or a member of an interface
	Partial   bool     // partial type
	Ext       bool     // extension method (first parameter is this)
	Supers    []string // base class and interfaces as written (types only)
	Start     int      // token index
	End       int      // token index (exclusive)
	Line      int      // first line, including the doc comment and attributes
	EndLine   int
	Cyclo     int // members: cyclomatic complexity
	Nest      int // and deepest control-flow nesting
}

type CSUsing struct {
	Path   string // A.B for using A.B; A.B.C for using static and aliases
	Alias  string // X in using X = A.B.C
	Static bool
	Global bool
}

type CSFile struct {
	Path       string
	Namespaces []string // every namespace declared, file-scoped included
	Toks       []Tok
	Usings     []CSUsing
	Decls      []*CSDecl // a type comes before its members
	Lines      int
	Generated  bool
}

// IsType reports whether d declares a type rather than a member.
func (d *CSDecl) IsType() bool {
	return d.Kind != "method" && d.Kind != "ctor" && d.Kind != "property"
}

var csGrammar = &grammar{load: loadCSharp}

var csLex = &lexRules{
	comments: set("comment"),
	strings: set("string_literal", "verbatim_string_literal", "raw_string_literal", "character_literal",
		"interpolated_string_expression", "utf8_string_literal"),
	holes:   set("interpolation"),
	atIdent: true,
	node: func(t *tree, n *ts.Node, typ string) bool {
		if !strings.HasPrefix(typ, "preproc_") && typ != "shebang_directive" {
			return false
		}
		switch typ {
		case "preproc_if", "preproc_elif", "preproc_else", "preproc_region":
			// Lex the code between the directives.
			kids, fields := t.children(n)
			for i, c := range kids {
				if c.IsNamed() && fields[i] != "condition" && !strings.HasPrefix(t.typ(c), "preproc_") ||
					csPreprocBlock[t.typ(c)] {
					t.lex(c)
				}
			}
		}
		return true
	},
}

var csPreprocBlock = set("preproc_if", "preproc_elif", "preproc_else", "preproc_region")

var csCx = &cxRules{
	decide: set("if", "for", "foreach", "while", "case", "catch", "&&", "||", "??", "??=", "conditional_expression"),
	arm: func(t *tree, n *ts.Node, typ string) bool {
		if typ != "switch_expression_arm" {
			return false
		}
		p := t.named(n)
		return len(p) == 0 || t.typ(p[0]) != "discard"
	},
	nest: set("if_statement", "for_statement", "foreach_statement", "while_statement", "do_statement",
		"switch_statement", "switch_expression", "try_statement", "lambda_expression", "anonymous_method_expression",
		"local_function_statement", "using_statement", "lock_statement", "fixed_statement", "checked_statement",
		"unsafe_statement"),
	ifs: set("if_statement"),
}

var csTypes = map[string]string{
	"class_declaration": "class", "struct_declaration": "struct", "interface_declaration": "interface",
	"enum_declaration": "enum", "record_declaration": "record", "record_struct_declaration": "record",
	"delegate_declaration": "delegate",
}

func ParseCS(p, src string) *CSFile {
	src = strings.TrimPrefix(src, "\ufeff") // Visual Studio saves a BOM
	f := &CSFile{Path: p, Lines: strings.Count(src, "\n") + 1,
		Generated: generatedHead(src) || strings.HasSuffix(p, ".g.cs") || strings.HasSuffix(p, ".g.i.cs") || strings.HasSuffix(p, ".Designer.cs")}
	t, release := parse(csGrammar, src, csLex)
	defer release()
	f.Toks = t.toks
	if t.root != nil {
		f.nsBody(t, t.root, "")
	}
	return f
}

// nsBody reads the top level or a namespace body. Top-level statements are
// skipped.
func (f *CSFile) nsBody(t *tree, n *ts.Node, ns string) {
	for _, c := range t.named(n) {
		switch typ := t.typ(c); typ {
		case "using_directive":
			f.using(t, c)
		case "namespace_declaration", "file_scoped_namespace_declaration":
			name := stripSpace(t.text(t.field(c, "name")))
			if ns != "" {
				name = ns + "." + name
			}
			f.Namespaces = append(f.Namespaces, name)
			if body := t.field(c, "body"); body != nil {
				f.nsBody(t, body, name)
			} else if typ == "file_scoped_namespace_declaration" {
				ns = name // the rest of the file
				f.nsBody(t, c, name)
			}
		case "declaration_list", "ERROR", "preproc_if", "preproc_elif", "preproc_else", "preproc_region":
			f.nsBody(t, c, ns)
		default:
			if kind, ok := csTypes[typ]; ok {
				f.typeDecl(t, c, kind, ns, nil)
			}
		}
	}
}

func (f *CSFile) using(t *tree, n *ts.Node) {
	u := CSUsing{Global: t.hasWord(n, "", "global"), Static: t.hasWord(n, "", "static")}
	alias := t.field(n, "name")
	if alias != nil {
		u.Alias = t.text(alias)
	}
	for _, c := range t.named(n) {
		if alias != nil && c.StartByte() == alias.StartByte() {
			continue
		}
		switch t.typ(c) {
		case "identifier", "qualified_name", "generic_name", "alias_qualified_name":
			u.Path = typeName(t.text(c))
		}
	}
	if u.Path != "" {
		f.Usings = append(f.Usings, u)
	}
}

func csMods(t *tree, n *ts.Node) (public, partial bool) {
	for _, c := range t.named(n) {
		if t.typ(c) == "modifier" {
			switch strings.TrimSpace(t.text(c)) {
			case "public":
				public = true
			case "partial":
				partial = true
			}
		}
	}
	return public, partial
}

func (f *CSFile) add(t *tree, n *ts.Node, name, kind, ns string, owner *CSDecl) *CSDecl {
	d := &CSDecl{Name: name, Kind: kind, Namespace: ns}
	if owner != nil {
		d.Name, d.Owner, d.Namespace = owner.Name+"."+name, owner.Name, owner.Namespace
	}
	d.Start, d.End = t.span(n, n)
	d.Line, d.EndLine = t.declLine(d.Start), t.lastLineOf(d.Start, d.End)
	f.Decls = append(f.Decls, d)
	return d
}

func (f *CSFile) typeDecl(t *tree, n *ts.Node, kind, ns string, owner *CSDecl) {
	name := t.text(t.field(n, "name"))
	if name == "" {
		return
	}
	public, partial := csMods(t, n)
	d := f.add(t, n, name, kind, ns, owner)
	d.Exported, d.Partial = public || owner != nil && owner.Kind == "interface", partial
	for _, c := range t.named(n) {
		if t.typ(c) == "base_list" {
			for _, b := range t.named(c) {
				if s := typeName(t.text(b)); s != "" {
					d.Supers = append(d.Supers, s)
				}
			}
		}
	}
	if body := t.field(n, "body"); body != nil && kind != "enum" {
		f.members(t, body, d)
	}
}

func (f *CSFile) members(t *tree, body *ts.Node, owner *CSDecl) {
	for _, c := range t.named(body) {
		typ := t.typ(c)
		if kind, ok := csTypes[typ]; ok {
			f.typeDecl(t, c, kind, owner.Namespace, owner)
			continue
		}
		name, kind := t.text(t.field(c, "name")), "method"
		switch typ {
		case "ERROR", "preproc_if", "preproc_elif", "preproc_else", "preproc_region", "declaration_list":
			f.members(t, c, owner)
			continue
		case "method_declaration":
		case "constructor_declaration":
			kind = "ctor"
		case "destructor_declaration":
			name = "~" + name
		case "property_declaration", "event_declaration":
			kind = "property"
		case "indexer_declaration":
			name, kind = "this[]", "property"
		case "operator_declaration":
			op := stripSpace(t.text(t.field(c, "operator")))
			if op != "" && isIdentStart(op[0]) {
				op = " " + op // operator true
			}
			name = "operator" + op
		case "conversion_operator_declaration":
			name = "operator " + stripSpace(t.text(t.field(c, "type")))
		default:
			continue // fields, event fields
		}
		if name == "" {
			continue
		}
		public, _ := csMods(t, c)
		d := f.add(t, c, name, kind, owner.Namespace, owner)
		d.Exported = public || owner.Kind == "interface"
		d.Ext = typ == "method_declaration" && csExtension(t, c)
		d.Cyclo, d.Nest = t.measure(c, csCx)
	}
}

// csExtension reports whether a method's first parameter is this T.
func csExtension(t *tree, n *ts.Node) bool {
	ps := t.field(n, "parameters")
	if ps == nil {
		return false
	}
	for _, p := range t.named(ps) {
		if t.typ(p) != "parameter" {
			continue
		}
		return t.hasWord(p, "modifier", "this")
	}
	return false
}

// CSDecls lists every type and member with its line range. Types enclose
// their members; use Innermost to place a line.
func CSDecls(src string) []Decl {
	f := ParseCS("", src)
	out := make([]Decl, 0, len(f.Decls))
	for _, d := range f.Decls {
		out = append(out, Decl{Name: d.Name, Start: d.Line, End: max(d.EndLine, d.Line)})
	}
	return out
}
