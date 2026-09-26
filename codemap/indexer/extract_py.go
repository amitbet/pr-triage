package indexer

import (
	"path"
	"sort"
	"strings"

	"github.com/amitbet/pr-manager/codemap/decls"
)

// The Python extractor resolves imports to files the way the interpreter
// would with the repo's source roots on sys.path: the repo root, src/, the
// directory of each pyproject.toml/setup.py, and the parent of every
// top-level package. Names then resolve through import bindings, the
// module's own definitions and star imports, attribute chains through
// modules (pkg.mod.func) and classes (Cls.method). Calls on self/cls, and on
// variables assigned or annotated with a workspace class, resolve to the
// method, looking through base classes. Overriding methods get "impl" edges
// from the method they override.

func isPyTestLike(p string) bool {
	b := path.Base(p)
	return strings.HasPrefix(b, "test_") || strings.HasSuffix(b, "_test.py") || b == "conftest.py" ||
		strings.HasPrefix(p, "tests/") || strings.Contains(p, "/tests/") || strings.HasPrefix(p, "test/") || strings.Contains(p, "/test/")
}

func isPyVendored(p string) bool {
	return strings.Contains(p, "site-packages/") || strings.HasPrefix(p, "venv/") || strings.HasPrefix(p, ".venv/") ||
		strings.Contains(p, "/venv/") || strings.Contains(p, "/.venv/") || strings.Contains(p, "node_modules/")
}

func pyDir(p string) string {
	d := path.Dir(p)
	if d == "." {
		return ""
	}
	return d
}

func pyJoin(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "/" + b
}

// pyRef is what a name resolves to: a node, or a module (a path prefix
// without .py, which may be a package directory).
type pyRef struct {
	key string
	mod string
}

func (r pyRef) ok() bool { return r.key != "" || r.mod != "" }

type pyClass struct {
	file    string
	d       *decls.PyDecl
	methods map[string]string // name -> node key
	bases   []*pyClass
}

func (c *pyClass) method(name string, depth int) string {
	if k, ok := c.methods[name]; ok {
		return k
	}
	if depth > 6 {
		return ""
	}
	for _, b := range c.bases {
		if k := b.method(name, depth+1); k != "" {
			return k
		}
	}
	return ""
}

type pyIndex struct {
	repo    string
	files   map[string]*decls.PyFile
	all     map[string]bool // every tracked .py file
	dirs    map[string]bool // directories holding .py files, and their parents
	roots   []string        // longest first
	defs    map[string]map[string]string
	classes map[string]*pyClass // node key -> class
	memo    map[string]pyRef
}

func (x *pyIndex) key(file, sym string) string { return "py:" + x.repo + "/" + file + ":" + sym }

// modFile returns the file that implements module prefix m, or "".
func (x *pyIndex) modFile(m string) string {
	if m != "" && x.all[m+".py"] {
		return m + ".py"
	}
	if f := pyJoin(m, "__init__.py"); x.all[f] {
		return f
	}
	return ""
}

func (x *pyIndex) isMod(m string) bool { return x.modFile(m) != "" || (m != "" && x.dirs[m]) }

// absMod resolves an absolute dotted module name imported from file.
func (x *pyIndex) absMod(from, dotted string) string {
	rel := strings.ReplaceAll(dotted, ".", "/")
	var near, far []string
	for _, r := range x.roots {
		if r == "" || strings.HasPrefix(from, r+"/") {
			near = append(near, r)
		} else {
			far = append(far, r)
		}
	}
	for _, r := range append(near, far...) {
		if m := pyJoin(r, rel); x.isMod(m) {
			return m
		}
	}
	// A script's own directory is on sys.path when it is not a package.
	if d := pyDir(from); !x.all[pyJoin(d, "__init__.py")] {
		if m := pyJoin(d, rel); x.modFile(m) != "" {
			return m
		}
	}
	return ""
}

func (x *pyIndex) relMod(from, dotted string) string {
	n := len(dotted) - len(strings.TrimLeft(dotted, "."))
	base := pyDir(from)
	for k := 1; k < n; k++ {
		base = pyDir(base)
	}
	return pyJoin(base, strings.ReplaceAll(dotted[n:], ".", "/"))
}

func (x *pyIndex) module(from, dotted string) string {
	if strings.HasPrefix(dotted, ".") {
		return x.relMod(from, dotted)
	}
	return x.absMod(from, dotted)
}

// attr resolves name inside module m: a definition or import in its file,
// else a submodule.
func (x *pyIndex) attr(m, name string, depth int) pyRef {
	if f := x.modFile(m); f != "" {
		if r := x.name(f, name, depth+1); r.ok() {
			return r
		}
	}
	if sub := pyJoin(m, name); x.isMod(sub) {
		return pyRef{mod: sub}
	}
	return pyRef{}
}

// name resolves a module-level name in file.
func (x *pyIndex) name(file, name string, depth int) pyRef {
	if depth > 8 {
		return pyRef{}
	}
	mk := file + "\x00" + name
	if r, ok := x.memo[mk]; ok {
		return r
	}
	var r pyRef
	if k := x.defs[file][name]; k != "" {
		r = pyRef{key: k}
	} else if f := x.files[file]; f != nil {
		// The last binding wins, as at run time.
		for i := len(f.Imports) - 1; i >= 0 && !r.ok(); i-- {
			im := f.Imports[i]
			switch {
			case im.Name == "*":
			case im.Local != name:
			case im.Name == "":
				mod := im.Module
				if !im.As {
					mod = im.Local
				}
				if m := x.module(file, mod); m != "" {
					r = pyRef{mod: m}
				}
			default:
				if m := x.module(file, im.Module); m != "" {
					r = x.attr(m, im.Name, depth)
				}
			}
		}
		for _, im := range f.Imports {
			if r.ok() {
				break
			}
			if im.Name == "*" {
				if m := x.module(file, im.Module); m != "" {
					r = x.attr(m, name, depth)
				}
			}
		}
	}
	if depth == 0 {
		x.memo[mk] = r
	}
	return r
}

// dotted resolves a.b.c in file.
func (x *pyIndex) dotted(file, s string) pyRef {
	parts := strings.Split(s, ".")
	r := x.name(file, parts[0], 0)
	for _, p := range parts[1:] {
		if !r.ok() {
			break
		}
		r = x.step(r, p)
	}
	return r
}

// step resolves .name on r.
func (x *pyIndex) step(r pyRef, name string) pyRef {
	if r.mod != "" {
		return x.attr(r.mod, name, 0)
	}
	if c := x.classes[r.key]; c != nil {
		if k := c.method(name, 0); k != "" {
			return pyRef{key: k}
		}
		if k := x.defs[c.file][c.d.Name+"."+name]; k != "" {
			return pyRef{key: k} // nested class
		}
	}
	return pyRef{}
}

func pyRoots(all, dirs map[string]bool, tracked []string) []string {
	set := map[string]bool{"": true}
	for p := range all {
		if path.Base(p) == "__init__.py" {
			parent := pyDir(pyDir(p))
			if !all[pyJoin(parent, "__init__.py")] {
				set[parent] = true
			}
		}
	}
	for _, p := range tracked {
		switch path.Base(p) {
		case "pyproject.toml", "setup.py", "setup.cfg":
			d := pyDir(p)
			set[d] = true
			if dirs[pyJoin(d, "src")] {
				set[pyJoin(d, "src")] = true
			}
		}
	}
	if dirs["src"] {
		set["src"] = true
	}
	var out []string
	for r := range set {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

func extractPy(repo, repoRoot string, tracked []string, g *Graph) {
	x := &pyIndex{repo: repo, files: map[string]*decls.PyFile{}, all: map[string]bool{}, dirs: map[string]bool{},
		defs: map[string]map[string]string{}, classes: map[string]*pyClass{}, memo: map[string]pyRef{}}
	var paths, order []string
	for _, p := range tracked {
		if !decls.IsPyPath(p) || isPyVendored(p) {
			continue
		}
		x.all[p] = true
		for d := pyDir(p); d != ""; d = pyDir(d) {
			x.dirs[d] = true
		}
		if !isPyTestLike(p) {
			paths = append(paths, p)
		}
	}
	for i, f := range parseFiles(repoRoot, "py", paths, decls.ParsePy) {
		if f != nil {
			x.files[paths[i]] = f
			order = append(order, paths[i])
		}
	}
	if len(order) == 0 {
		return
	}
	sort.Strings(order)
	x.roots = pyRoots(x.all, x.dirs, tracked)
	modKey := func(file string) string { return x.key(file, "<module>") }

	for _, p := range order {
		f := x.files[p]
		dir := pyDir(p)
		g.Files = append(g.Files, FileInfo{Path: p, Lang: "py", Lines: f.Lines, Generated: f.Generated})
		g.Nodes = append(g.Nodes, &Node{Key: modKey(p), Kind: "module", Repo: repo, File: p, Dir: dir, Sym: "<module>", Start: 1, End: f.Lines, Internal: true})
		defs := map[string]string{}
		x.defs[p] = defs
		for _, d := range f.Decls {
			if defs[d.Name] != "" {
				continue // redefinition: the first one names the node
			}
			n := &Node{Key: x.key(p, d.Name), Kind: "py-" + d.Kind, Repo: repo, File: p, Dir: dir, Sym: d.Name,
				Start: d.Line, End: max(d.EndLine, d.Line), Exported: d.Exported && !strings.Contains(d.Name, "._")}
			if d.Kind == "function" || d.Kind == "method" {
				n.Cyclo, n.Nest = d.Cyclo, d.Nest
			}
			if f.Generated {
				n.Tags = append(n.Tags, "generated")
			}
			g.Nodes = append(g.Nodes, n)
			defs[d.Name] = n.Key
			switch d.Kind {
			case "class":
				x.classes[n.Key] = &pyClass{file: p, d: d, methods: map[string]string{}}
			case "method":
				if c := x.classes[x.key(p, d.Class)]; c != nil {
					c.methods[lastSeg(d.Name)] = n.Key
				}
			}
		}
	}
	for _, c := range x.classes {
		for _, b := range c.d.Bases {
			if r := x.dotted(c.file, b); r.key != "" {
				if bc := x.classes[r.key]; bc != nil && bc != c {
					c.bases = append(c.bases, bc)
				}
			}
		}
	}

	edges := edgeAcc{}
	for _, p := range order {
		f := x.files[p]
		t := f.Toks
		owner := make([]*decls.PyDecl, len(t))
		for _, d := range f.Decls {
			for i := d.Start; i < d.End && i < len(t); i++ {
				owner[i] = d
			}
		}
		ownerKey := func(i int) string {
			if owner[i] == nil {
				return modKey(p)
			}
			return x.key(p, owner[i].Name)
		}
		classOf := func(i int) *pyClass {
			d := owner[i]
			switch {
			case d == nil:
				return nil
			case d.Kind == "class":
				return x.classes[x.key(p, d.Name)]
			case d.Class != "":
				return x.classes[x.key(p, d.Class)]
			}
			return nil
		}
		scopeOf := func(i int) string {
			if owner[i] == nil {
				return ""
			}
			return owner[i].Name
		}
		isDot := func(i int) bool { return i >= 0 && i < len(t) && t[i].Kind == 'p' && t[i].Text == "." }
		// chain resolves the dotted expression starting at t[i]; it returns
		// the deepest ref and the index of its last name.
		var vars map[string]map[string]*pyClass
		attrs := map[*pyClass]map[string]*pyClass{} // self.x = Cls(...)
		chain := func(i int) (pyRef, int, bool) {
			tk := t[i]
			var r pyRef
			self := false
			switch {
			case tk.Text == "self" || tk.Text == "cls":
				c := classOf(i)
				if c == nil {
					return pyRef{}, i, false
				}
				r, self = pyRef{key: x.key(p, c.d.Name)}, true
				if i+2 < len(t) && isDot(i+1) && t[i+2].Kind == 'i' {
					if ac := attrs[c][t[i+2].Text]; ac != nil && c.method(t[i+2].Text, 0) == "" {
						r, i, self = pyRef{key: x.key(ac.file, ac.d.Name)}, i+2, false
					}
				}
			default:
				if c := vars[scopeOf(i)][tk.Text]; c != nil {
					r = pyRef{key: x.key(c.file, c.d.Name)}
				} else if c := vars[""][tk.Text]; c != nil && owner[i] != nil {
					r = pyRef{key: x.key(c.file, c.d.Name)}
				} else {
					r = x.name(p, tk.Text, 0)
				}
			}
			for r.ok() && i+2 < len(t) && isDot(i+1) && t[i+2].Kind == 'i' {
				n := x.step(r, t[i+2].Text)
				if !n.ok() {
					break
				}
				r, i, self = n, i+2, false
			}
			return r, i, self
		}
		// Variables assigned or annotated with a workspace class.
		vars = map[string]map[string]*pyClass{}
		classAt := func(i int) *pyClass {
			if i >= len(t) || t[i].Kind != 'i' {
				return nil
			}
			r, _, _ := chain(i)
			return x.classes[r.key]
		}
		for i, tk := range t {
			if tk.Kind != 'i' || isDot(i-1) || i+2 >= len(t) || t[i+1].Kind != 'p' {
				continue
			}
			var c *pyClass
			switch {
			case t[i+1].Text == "=" && t[i+2].Kind == 'i':
				if cc := classAt(i + 2); cc != nil {
					if _, j, _ := chain(i + 2); j+1 < len(t) && t[j+1].Kind == 'p' && t[j+1].Text == "(" {
						c = cc
					}
				}
			case t[i+1].Text == ":" && t[i+2].Kind == 'i':
				c = classAt(i + 2)
			}
			if c == nil {
				continue
			}
			sc := scopeOf(i)
			if vars[sc] == nil {
				vars[sc] = map[string]*pyClass{}
			}
			vars[sc][tk.Text] = c
		}
		for i := 0; i+4 < len(t); i++ {
			// self.x = Cls(...) / self.x: Cls
			if !(t[i].Kind == 'i' && t[i].Text == "self" && isDot(i+1) && t[i+2].Kind == 'i' && t[i+3].Kind == 'p') {
				continue
			}
			var c *pyClass
			switch t[i+3].Text {
			case "=":
				c = classAt(i + 4)
			case ":":
				c = classAt(i + 4)
			}
			if oc := classOf(i); c != nil && oc != nil {
				if attrs[oc] == nil {
					attrs[oc] = map[string]*pyClass{}
				}
				attrs[oc][t[i+2].Text] = c
			}
		}
		// Names that bind rather than use: def/class names, parameters and
		// keyword arguments. Parameters and names assigned in a function
		// are its locals and shadow module-level names.
		binding := make([]bool, len(t))
		locals := map[*decls.PyDecl]map[string]bool{}
		local := func(i int) {
			if d := owner[i]; d != nil && d.Kind != "class" && d.Kind != "var" {
				if locals[d] == nil {
					locals[d] = map[string]bool{}
				}
				locals[d][t[i].Text] = true
			}
		}
		for i := 1; i < len(t); i++ {
			if t[i].Kind != 'i' {
				continue
			}
			prev := t[i-1]
			if prev.Kind == 'i' && (prev.Text == "def" || prev.Text == "class") {
				binding[i] = true
				if prev.Text == "def" && i+1 < len(t) && t[i+1].Kind == 'p' && t[i+1].Text == "(" {
					end := skipParens(t, i+1)
					for k := i + 2; k < end; k++ {
						if t[k].Kind == 'i' && t[k-1].Kind == 'p' && strings.Contains("(,*", t[k-1].Text) {
							binding[k] = true
							local(k)
						}
					}
				}
				continue
			}
			if i+2 < len(t) && prev.Kind == 'p' && (prev.Text == "(" || prev.Text == ",") && t[i+1].Kind == 'p' && t[i+1].Text == "=" && !(t[i+2].Kind == 'p' && t[i+2].Text == "=") {
				binding[i] = true
			}
			if prev.Kind == 'i' && (prev.Text == "for" || prev.Text == "as") {
				local(i)
			}
		}
		for _, l := range f.LogLines {
			// x = ..., x: T = ...
			k := l.Tok
			if k+2 < len(t) && t[k].Kind == 'i' && t[k+1].Kind == 'p' && (t[k+1].Text == "=" || t[k+1].Text == ":") && !(t[k+2].Kind == 'p' && t[k+2].Text == "=") {
				local(k)
			}
		}
		for i := 0; i < len(t); i++ {
			tk := t[i]
			if tk.Kind != 'i' || isDot(i-1) || binding[i] || decls.IsPyKeyword(tk.Text) {
				continue
			}
			if d := owner[i]; d != nil && locals[d][tk.Text] && vars[d.Name][tk.Text] == nil && tk.Text != "self" && tk.Text != "cls" {
				continue
			}
			r, j, self := chain(i)
			if !r.ok() || self {
				continue
			}
			to := r.key
			if to == "" {
				if mf := x.modFile(r.mod); mf != "" && x.files[mf] != nil {
					to = modKey(mf)
				}
			}
			edges.add(ownerKey(i), to, "")
			i = j
		}
	}
	for _, c := range x.classes {
		for m, k := range c.methods {
			for _, b := range c.bases {
				if bk := b.method(m, 0); bk != "" && bk != k {
					edges.add(bk, k, "impl")
				}
			}
		}
	}
	g.Edges = append(g.Edges, edges.list()...)
}

// skipParens returns the index after the parenthesis that closes t[i].
func skipParens(t []decls.Tok, i int) int {
	d := 0
	for ; i < len(t); i++ {
		if t[i].Kind != 'p' {
			continue
		}
		switch t[i].Text {
		case "(", "[", "{":
			d++
		case ")", "]", "}":
			d--
			if d == 0 {
				return i + 1
			}
		}
	}
	return i
}
