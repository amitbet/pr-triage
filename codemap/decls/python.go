package decls

import (
	"strings"

	ts "github.com/amitbet/pr-triage/internal/sitter"
)

// The Python parser reads module-level functions, classes, their methods
// (nested classes too), module-level assignments and every import off the
// tree-sitter tree. Definitions under a module-level or class-level if,
// try, with or loop count as if they were not nested; functions nested in
// functions stay part of the outer one.

// PyLine is the start of a statement.
type PyLine struct {
	Tok    int // index of the first token
	Indent int
	Line   int
}

type PyDecl struct {
	Name     string   // f, C, C.m, C.D.m, X
	Kind     string   // function class method var
	Class    string   // enclosing class for methods and nested classes
	Exported bool     // no leading underscore
	Bases    []string // class bases as written, dotted
	Start    int      // token index (the first decorator)
	End      int      // token index (exclusive)
	Line     int      // first line, including comments right above
	EndLine  int
	Cyclo    int // functions and methods: cyclomatic complexity
	Nest     int // and deepest control-flow nesting
}

type PyImport struct {
	Module string // dotted; leading dots for relative imports
	Name   string // from-imports: the imported name, or "*"; "" for import M
	Local  string // the name bound in this file
	As     bool   // import a.b as x binds x to a.b; plain import a.b binds a
}

type PyFile struct {
	Path      string
	Toks      []Tok
	LogLines  []PyLine  // every statement, in token order
	Decls     []*PyDecl // a class comes before its methods
	Imports   []PyImport
	Lines     int
	Generated bool
}

var pyGrammar = &grammar{load: loadPython}

var pyLex = &lexRules{
	comments: set("comment"),
	strings:  set("string"),
	holes:    set("interpolation"),
	numbers:  true, // a line may hold only a number
	node:     func(t *tree, n *ts.Node, typ string) bool { return typ == "line_continuation" },
}

var pyCx = &cxRules{
	decide: set("if", "elif", "for", "while", "except", "and", "or", "case"),
	nest: set("if_statement", "for_statement", "while_statement", "try_statement", "with_statement",
		"match_statement", "function_definition", "class_definition", "lambda"),
}

var pyKeywords = map[string]bool{
	"False": true, "None": true, "True": true, "and": true, "as": true, "assert": true, "async": true,
	"await": true, "break": true, "class": true, "continue": true, "def": true, "del": true, "elif": true,
	"else": true, "except": true, "finally": true, "for": true, "from": true, "global": true, "if": true,
	"import": true, "in": true, "is": true, "lambda": true, "nonlocal": true, "not": true, "or": true,
	"pass": true, "raise": true, "return": true, "try": true, "while": true, "with": true, "yield": true,
}

// IsPyKeyword reports whether s is a reserved word.
func IsPyKeyword(s string) bool { return pyKeywords[s] }

type pyParser struct {
	f       *PyFile
	t       *tree
	seenVar map[string]bool
}

func ParsePy(p, src string) *PyFile {
	f := &PyFile{Path: p, Lines: strings.Count(src, "\n") + 1, Generated: generatedHead(src) || strings.HasSuffix(p, "_pb2.py") || strings.HasSuffix(p, "_pb2_grpc.py")}
	t, release := parse(pyGrammar, src, pyLex)
	defer release()
	f.Toks = t.toks
	if t.root == nil {
		return f
	}
	x := &pyParser{f: f, t: t, seenVar: map[string]bool{}}
	x.scan(t.root)
	x.block(t.root, nil)
	return f
}

// scan records every statement start and every import, in source order.
func (x *pyParser) scan(n *ts.Node) {
	t := x.t
	for _, c := range t.named(n) {
		typ := t.typ(c)
		if k := t.typ(n); k == "module" || k == "block" {
			if s, _ := t.span(c, c); s < len(t.toks) {
				x.f.LogLines = append(x.f.LogLines, PyLine{Tok: s, Indent: int(c.StartPoint().Column), Line: t.toks[s].Line})
			}
		}
		switch typ {
		case "import_statement", "import_from_statement", "future_import_statement":
			x.imports(c, typ)
		case "string", "comment":
		default:
			x.scan(c)
		}
	}
}

func (x *pyParser) imports(n *ts.Node, typ string) {
	t := x.t
	kids, fields := t.children(n)
	if typ == "import_statement" {
		for i, c := range kids {
			if fields[i] != "name" {
				continue
			}
			switch t.typ(c) {
			case "dotted_name":
				mod := stripSpace(t.text(c))
				x.f.Imports = append(x.f.Imports, PyImport{Module: mod, Local: strings.SplitN(mod, ".", 2)[0]})
			case "aliased_import":
				mod := stripSpace(t.text(t.field(c, "name")))
				x.f.Imports = append(x.f.Imports, PyImport{Module: mod, Local: t.text(t.field(c, "alias")), As: true})
			}
		}
		return
	}
	mod := stripSpace(t.text(t.field(n, "module_name")))
	if typ == "future_import_statement" {
		mod = "__future__"
	}
	if mod == "" {
		return
	}
	for i, c := range kids {
		switch {
		case t.typ(c) == "wildcard_import":
			x.f.Imports = append(x.f.Imports, PyImport{Module: mod, Name: "*"})
		case fields[i] != "name":
		case t.typ(c) == "aliased_import":
			name := stripSpace(t.text(t.field(c, "name")))
			x.f.Imports = append(x.f.Imports, PyImport{Module: mod, Name: name, Local: t.text(t.field(c, "alias"))})
		default:
			name := stripSpace(t.text(c))
			x.f.Imports = append(x.f.Imports, PyImport{Module: mod, Name: name, Local: name})
		}
	}
}

// block reads the statements of the module (parent nil) or a class body.
func (x *pyParser) block(n *ts.Node, parent *PyDecl) {
	t := x.t
	for _, c := range t.named(n) {
		switch typ := t.typ(c); typ {
		case "decorated_definition":
			if def := t.field(c, "definition"); def != nil {
				x.def(c, def, parent)
			}
		case "function_definition", "class_definition":
			x.def(c, c, parent)
		case "expression_statement":
			x.block(c, parent)
		case "assignment":
			if parent == nil {
				x.assign(c)
			}
		case "if_statement", "elif_clause", "else_clause", "try_statement", "except_clause", "except_group_clause",
			"finally_clause", "with_statement", "for_statement", "while_statement", "block", "ERROR":
			x.block(c, parent) // definitions under a module- or class-level if/try/with/loop
		}
	}
}

// assign records a module-level NAME = ... or NAME: T = ..., the first time
// NAME is bound.
func (x *pyParser) assign(n *ts.Node) {
	t := x.t
	left := t.field(n, "left")
	if left == nil || t.typ(left) != "identifier" {
		return
	}
	name := t.text(left)
	if x.seenVar[name] {
		return
	}
	x.seenVar[name] = true
	d := &PyDecl{Name: name, Kind: "var", Exported: !strings.HasPrefix(name, "_")}
	d.Start, d.End = t.span(n, n)
	d.Line, d.EndLine = t.declLine(d.Start), t.lastLineOf(d.Start, d.End)
	x.f.Decls = append(x.f.Decls, d)
}

// def records a function or class; outer includes its decorators.
func (x *pyParser) def(outer, n *ts.Node, parent *PyDecl) {
	t := x.t
	name := t.text(t.field(n, "name"))
	if name == "" {
		return
	}
	d := &PyDecl{Name: name, Kind: "function", Exported: !strings.HasPrefix(name, "_")}
	class := t.typ(n) == "class_definition"
	if class {
		d.Kind = "class"
		if sc := t.field(n, "superclasses"); sc != nil {
			for _, b := range t.named(sc) {
				switch t.typ(b) {
				case "identifier", "attribute":
					d.Bases = append(d.Bases, stripSpace(t.text(b)))
				case "subscript": // Generic[T]
					if v := t.field(b, "value"); v != nil {
						d.Bases = append(d.Bases, stripSpace(t.text(v)))
					}
				}
			}
		}
	}
	if parent != nil {
		d.Name, d.Class = parent.Name+"."+name, parent.Name
		if !class {
			d.Kind = "method"
		}
	}
	d.Start, d.End = t.span(outer, outer)
	d.Line, d.EndLine = t.declLine(d.Start), t.lastLineOf(d.Start, d.End)
	x.f.Decls = append(x.f.Decls, d)
	if class {
		if body := t.field(n, "body"); body != nil {
			x.block(body, d)
		}
	} else {
		d.Cyclo, d.Nest = t.measure(n, pyCx)
	}
}

// PyDecls lists every function, class, method and module-level assignment
// with its line range. Classes enclose their methods; use Innermost to place
// a line.
func PyDecls(src string) []Decl {
	f := ParsePy("", src)
	out := make([]Decl, 0, len(f.Decls))
	for _, d := range f.Decls {
		out = append(out, Decl{Name: d.Name, Start: d.Line, End: max(d.EndLine, d.Line)})
	}
	return out
}
