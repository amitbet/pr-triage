// Package cx measures code complexity: cyclomatic complexity and the
// deepest control-flow nesting, per function. The code-map indexer and
// pr-triage share it, so a unit's "before" and "after" use the same rules as
// the map.
//
// Go is measured on the go/ast syntax tree. Java, C#, Python, Rust and the
// generic-parser languages (shell, PowerShell, C, C++, PHP, Scala, Kotlin,
// Ruby, Swift, Dart) are measured on their tree-sitter trees by
// codemap/decls while it parses them (the rules are next to each parser),
// so a declaration carries its own Cyclo and Nest. TypeScript is measured on the tokens from codemap/decls,
// which is an approximation: it counts decision keywords and operators, and
// nesting by the braces that open blocks.
package cx

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/amitbet/pr-triage/codemap/decls"
)

// Stat is the complexity of one declaration.
type Stat struct {
	Cyclo int // 1 + decision points
	Nest  int // deepest nesting of if/for/switch/select/func literals
}

func (s *Stat) deeper(d int) { s.Nest = max(s.Nest, d) }

// Go measures a declaration (or any node).
func Go(n ast.Node) Stat {
	s := Stat{Cyclo: 1}
	ast.Walk(goVisitor{s: &s}, n)
	return s
}

type goVisitor struct {
	s     *Stat
	depth int
}

func (v goVisitor) Visit(n ast.Node) ast.Visitor {
	in := goVisitor{v.s, v.depth + 1}
	switch n := n.(type) {
	case *ast.IfStmt:
		v.s.Cyclo++
		v.s.deeper(in.depth)
		if n.Init != nil {
			ast.Walk(in, n.Init)
		}
		ast.Walk(in, n.Cond)
		ast.Walk(in, n.Body)
		// "else if" continues the chain at the same depth; a plain else
		// block is as deep as the if body.
		if e, ok := n.Else.(*ast.IfStmt); ok {
			ast.Walk(v, e)
		} else if n.Else != nil {
			ast.Walk(in, n.Else)
		}
		return nil
	case *ast.ForStmt, *ast.RangeStmt:
		v.s.Cyclo++
		v.s.deeper(in.depth)
		return in
	case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.FuncLit:
		v.s.deeper(in.depth)
		return in
	case *ast.CaseClause:
		if n.List != nil {
			v.s.Cyclo++
		}
	case *ast.CommClause:
		if n.Comm != nil {
			v.s.Cyclo++
		}
	case *ast.BinaryExpr:
		if n.Op == token.LAND || n.Op == token.LOR {
			v.s.Cyclo++
		}
	}
	return v
}

// Decl is a named declaration with its line range and complexity. Names
// use the pr-triage unit format: Func, (*T).M, type T, var X, const X.
type Decl struct {
	Name       string
	Start, End int
	Stat
}

// GoDecls measures every top-level function and method in a Go file, and
// var declarations that hold function literals. nil if the file does not
// parse.
func GoDecls(src []byte) []Decl {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		return nil
	}
	var out []Decl
	for _, d := range f.Decls {
		start := d.Pos()
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = fmt.Sprintf("(%s).%s", RecvName(d.Recv.List[0].Type), name)
			}
			out = append(out, Decl{Name: name, Start: fset.Position(start).Line, End: fset.Position(d.End()).Line, Stat: Go(d)})
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, sp := range d.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok || len(vs.Values) == 0 || len(vs.Names) == 0 {
					continue
				}
				if !hasFuncLit(vs) {
					continue
				}
				out = append(out, Decl{Name: "var " + vs.Names[0].Name, Start: fset.Position(vs.Pos()).Line, End: fset.Position(vs.End()).Line, Stat: Go(vs)})
			}
		}
	}
	return out
}

func hasFuncLit(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			found = true
		}
		return !found
	})
	return found
}

// RecvName spells a receiver type the way unit and map symbols do: *T or T.
func RecvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return "*" + RecvName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return RecvName(t.X)
	case *ast.IndexListExpr:
		return RecvName(t.X)
	}
	return "?"
}

// TS measures a token range (a TypeScript declaration).
func TS(toks []decls.Tok) Stat {
	s := Stat{Cyclo: 1}
	depth := 0
	var blocks []bool // per open brace: does it open a block (vs an object/JSX)?
	for i, t := range toks {
		prev := func(k int) decls.Tok {
			if i-k >= 0 {
				return toks[i-k]
			}
			return decls.Tok{}
		}
		next := decls.Tok{}
		if i+1 < len(toks) {
			next = toks[i+1]
		}
		switch t.Kind {
		case 'i':
			switch t.Text {
			case "if", "for", "while", "case", "catch":
				s.Cyclo++
			}
		case 'p':
			switch t.Text {
			case "&", "|":
				// && and || arrive as two tokens; count the pair once.
				if next.Kind == 'p' && next.Text == t.Text && !(prev(1).Kind == 'p' && prev(1).Text == t.Text) {
					s.Cyclo++
				}
			case "?":
				p := prev(1)
				switch {
				case next.Kind == 'p' && next.Text == "?":
					s.Cyclo++ // ??
				case p.Kind == 'p' && p.Text == "?":
				case next.Kind == 'p' && (next.Text == "." || next.Text == ":" || next.Text == ")" || next.Text == "," || next.Text == "="):
					// optional chaining / optional property or parameter
				default:
					s.Cyclo++ // ternary
				}
			case "{":
				p := prev(1)
				block := (p.Kind == 'p' && p.Text == ")") ||
					(p.Kind == 'p' && p.Text == ">" && prev(2).Kind == 'p' && prev(2).Text == "=") ||
					(p.Kind == 'i' && (p.Text == "else" || p.Text == "try" || p.Text == "finally" || p.Text == "do"))
				blocks = append(blocks, block)
				if block {
					depth++
					s.deeper(depth - 1) // the declaration's own body is not nesting
				}
			case "}":
				if n := len(blocks); n > 0 {
					if blocks[n-1] {
						depth--
					}
					blocks = blocks[:n-1]
				}
			}
		}
	}
	return s
}

// TSDecls measures the named top-level declarations and class methods of a
// TS/JS file (a class itself is not measured, like a Java type).
func TSDecls(p, src string) []Decl {
	f := decls.ParseTS(p, src)
	seen := map[string]bool{}
	var out []Decl
	for _, d := range f.Decls {
		if d.Name == "<export>" || d.Name == "<destructure>" || d.Kind == "class" || seen[d.Name] {
			continue
		}
		seen[d.Name] = true
		end := min(d.End, len(f.Toks))
		start := min(max(0, d.Start), end)
		out = append(out, Decl{Name: d.Name, Start: d.Line, End: max(d.EndLine, d.Line), Stat: TS(f.Toks[start:end])})
	}
	return out
}

// JavaDecls measures the methods and constructors of a Java file.
func JavaDecls(src string) []Decl {
	f := decls.ParseJava("", src)
	var out []Decl
	for _, d := range f.Decls {
		if !d.IsType() {
			out = append(out, Decl{Name: d.Name, Start: d.Line, End: max(d.EndLine, d.Line), Stat: Stat{d.Cyclo, d.Nest}})
		}
	}
	return out
}

// PyDecls measures the functions and methods of a Python file.
func PyDecls(src string) []Decl {
	f := decls.ParsePy("", src)
	var out []Decl
	for _, d := range f.Decls {
		if d.Kind == "function" || d.Kind == "method" {
			out = append(out, Decl{Name: d.Name, Start: d.Line, End: max(d.EndLine, d.Line), Stat: Stat{d.Cyclo, d.Nest}})
		}
	}
	return out
}

// CSDecls measures the methods, constructors and properties of a C# file.
func CSDecls(src string) []Decl {
	f := decls.ParseCS("", src)
	var out []Decl
	for _, d := range f.Decls {
		if !d.IsType() {
			out = append(out, Decl{Name: d.Name, Start: d.Line, End: max(d.EndLine, d.Line), Stat: Stat{d.Cyclo, d.Nest}})
		}
	}
	return out
}

// RustDecls measures the functions and methods of a Rust file.
func RustDecls(src string) []Decl {
	f := decls.ParseRust("", src)
	var out []Decl
	for _, d := range f.Decls {
		if d.IsFunc() {
			out = append(out, Decl{Name: d.Name, Start: d.Line, End: max(d.EndLine, d.Line), Stat: Stat{d.Cyclo, d.Nest}})
		}
	}
	return out
}

// GenDecls measures the functions and methods of a generic-parser file.
func GenDecls(path, src string) []Decl {
	f := decls.ParseGen(path, src)
	if f == nil {
		return nil
	}
	var out []Decl
	for _, d := range f.Decls {
		if !d.IsType() {
			out = append(out, Decl{Name: d.Name, Start: d.Line, End: max(d.EndLine, d.Line), Stat: Stat{d.Cyclo, d.Nest}})
		}
	}
	return out
}

// FileDecls measures a Go, TS/JS, Java, Python, C#, Rust or generic-parser
// file by its path; nil for anything else.
func FileDecls(path string, src []byte) []Decl {
	switch {
	case len(path) > 3 && path[len(path)-3:] == ".go":
		return GoDecls(src)
	case decls.IsTSPath(path):
		return TSDecls(path, string(src))
	case decls.IsJavaPath(path):
		return JavaDecls(string(src))
	case decls.IsPyPath(path):
		return PyDecls(string(src))
	case decls.IsCSPath(path):
		return CSDecls(string(src))
	case decls.IsRustPath(path):
		return RustDecls(string(src))
	case decls.LangFor(path) != nil:
		return GenDecls(path, string(src))
	}
	return nil
}
