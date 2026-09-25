package decls

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars/java"
)

// The Java parser reads types (nested ones too), methods, constructors and
// annotation elements, the package and imports off the tree-sitter tree.
// Anonymous and local classes stay part of the method that holds them, and
// fields, initializer blocks and enum constants part of their type.

type JavaDecl struct {
	Name     string   // Outer, Outer.Inner, Outer.method
	Kind     string   // class interface enum record annotation method ctor
	Owner    string   // enclosing type ("" for top-level types)
	Exported bool     // public, or a member of an interface
	Supers   []string // extends/implements as written (types only)
	Start    int      // token index
	End      int      // token index (exclusive)
	Line     int      // first line, including the doc comment
	EndLine  int
	Cyclo    int // methods and constructors: cyclomatic complexity
	Nest     int // and deepest control-flow nesting
}

type JavaImport struct {
	Path     string // a.b.C; a.b for a.b.*; a.b.C.m for static imports
	Static   bool
	Wildcard bool
}

type JavaFile struct {
	Path      string
	Package   string
	Toks      []Tok
	Imports   []JavaImport
	Decls     []*JavaDecl // a type comes before its members
	Lines     int
	Generated bool
}

// IsType reports whether d declares a type rather than a method.
func (d *JavaDecl) IsType() bool { return d.Kind != "method" && d.Kind != "ctor" }

var javaGrammar = &grammar{load: java.Language}

var javaLex = &lexRules{
	comments: set("line_comment", "block_comment"),
	strings:  set("string_literal", "character_literal"),
}

var javaCx = &cxRules{
	decide: set("if", "for", "while", "case", "catch", "&&", "||", "ternary_expression"),
	nest: set("if_statement", "for_statement", "enhanced_for_statement", "while_statement", "do_statement",
		"switch_expression", "switch_statement", "try_statement", "try_with_resources_statement",
		"synchronized_statement", "lambda_expression", "class_body"),
	ifs: set("if_statement"),
}

var javaTypes = map[string]string{
	"class_declaration": "class", "interface_declaration": "interface", "enum_declaration": "enum",
	"record_declaration": "record", "annotation_type_declaration": "annotation",
}

func ParseJava(p, src string) *JavaFile {
	f := &JavaFile{Path: p, Lines: strings.Count(src, "\n") + 1, Generated: generatedHead(src)}
	t, release := parse(javaGrammar, src, javaLex)
	defer release()
	f.Toks = t.toks
	if t.root == nil {
		return f
	}
	for _, c := range t.named(t.root) {
		switch t.typ(c) {
		case "package_declaration":
			for _, n := range t.named(c) {
				if k := t.typ(n); k == "scoped_identifier" || k == "identifier" {
					f.Package = stripSpace(t.text(n))
				}
			}
		case "import_declaration":
			im := JavaImport{Static: t.hasWord(c, "", "static")}
			for _, n := range t.named(c) {
				switch t.typ(n) {
				case "scoped_identifier", "identifier":
					im.Path = stripSpace(t.text(n))
				case "asterisk":
					im.Wildcard = true
				}
			}
			if im.Path != "" {
				f.Imports = append(f.Imports, im)
			}
		default:
			f.member(t, c, nil)
		}
	}
	return f
}

// member records the declaration n in a type body (or at the top level
// when owner is nil).
func (f *JavaFile) member(t *tree, n *ts.Node, owner *JavaDecl) {
	typ := t.typ(n)
	if kind, ok := javaTypes[typ]; ok {
		f.typeDecl(t, n, kind, owner)
		return
	}
	switch {
	case typ == "ERROR":
		for _, c := range t.named(n) {
			f.member(t, c, owner)
		}
	case owner == nil:
	case typ == "method_declaration", typ == "annotation_type_element_declaration", typ == "constructor_declaration":
		name := t.text(t.field(n, "name"))
		if name == "" {
			return
		}
		kind := "method"
		if typ == "constructor_declaration" {
			kind = "ctor"
		}
		d := f.add(t, n, owner.Name+"."+name, kind, owner)
		d.Exported = javaPublic(t, n) || owner.Kind == "interface" || owner.Kind == "annotation"
		d.Cyclo, d.Nest = t.measure(n, javaCx)
	}
}

func (f *JavaFile) add(t *tree, n *ts.Node, name, kind string, owner *JavaDecl) *JavaDecl {
	d := &JavaDecl{Name: name, Kind: kind}
	if owner != nil {
		d.Owner = owner.Name
	}
	d.Start, d.End = t.span(n, n)
	d.Line, d.EndLine = t.declLine(d.Start), t.lastLineOf(d.Start, d.End)
	f.Decls = append(f.Decls, d)
	return d
}

func javaPublic(t *tree, n *ts.Node) bool {
	for _, c := range t.named(n) {
		if t.typ(c) == "modifiers" {
			return t.hasWord(c, "", "public")
		}
	}
	return false
}

func (f *JavaFile) typeDecl(t *tree, n *ts.Node, kind string, owner *JavaDecl) {
	name := t.text(t.field(n, "name"))
	if name == "" {
		return
	}
	if owner != nil {
		name = owner.Name + "." + name
	}
	d := f.add(t, n, name, kind, owner)
	d.Exported = javaPublic(t, n) || owner != nil && owner.Kind == "interface"
	// extends/implements: superclass, super_interfaces and extends_interfaces
	// hold the types, directly or in a type_list.
	var supers func(n *ts.Node)
	supers = func(n *ts.Node) {
		for _, c := range t.named(n) {
			switch t.typ(c) {
			case "type_list":
				supers(c)
			case "type_identifier", "scoped_type_identifier", "generic_type":
				d.Supers = append(d.Supers, typeName(t.text(c)))
			}
		}
	}
	for _, c := range t.named(n) {
		switch t.typ(c) {
		case "superclass", "super_interfaces", "extends_interfaces":
			supers(c)
		}
	}
	body := t.field(n, "body")
	if body == nil {
		return
	}
	for _, c := range t.named(body) {
		if t.typ(c) == "enum_body_declarations" {
			for _, m := range t.named(c) {
				f.member(t, m, d)
			}
			continue
		}
		f.member(t, c, d)
	}
}

// JavaDecls lists every type and method with its line range. Types enclose
// their members; use Innermost to place a line.
func JavaDecls(src string) []Decl {
	f := ParseJava("", src)
	out := make([]Decl, 0, len(f.Decls))
	for _, d := range f.Decls {
		out = append(out, Decl{Name: d.Name, Start: d.Line, End: max(d.EndLine, d.Line)})
	}
	return out
}
