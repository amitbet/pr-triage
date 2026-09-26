package indexer

import (
	"path"
	"sort"
	"strings"

	"github.com/amitbet/pr-manager/codemap/decls"
)

// The extractor for the generic-parser languages (shell, PowerShell, C,
// C++, PHP, Scala, Kotlin, Ruby, Swift and Dart) works from codemap/decls'
// declarations and tokens, the same way for all of them, without a compiler
// or build files. A name resolves, in order, to a member of the enclosing
// type (where the language has an implicit this), a top-level declaration
// of the same file, what the file imports (#include, source, require, Dart
// imports, qualified and wildcard imports), what its package, namespace or
// Swift module declares, and finally to the one exported declaration of
// that name in the repo. Member access resolves when the receiver's type is
// known: this/self, a type name (static access), or a variable, field or
// parameter declared with a workspace type (x: T, T x, x = new T / T(...) /
// T.new); otherwise to the one member of that name in the repo, if there is
// only one. Overriding members get "impl" edges from the member they
// override. C and C++ resolve together, so a .c file reaches what its
// headers declare.

// genFamilies lists the extractor passes; each resolves on its own.
var genFamilies = []string{"sh", "ps1", "c", "php", "scala", "kt", "rb", "swift", "dart"}

func extractGenAll(repo, repoRoot string, tracked []string, g *Graph) {
	for _, fam := range genFamilies {
		extractGen(repo, repoRoot, tracked, g, fam)
	}
}

// isGenVendored reports dependency and build-output directories. Scripts
// keep build/ (it often holds the build scripts themselves).
func isGenVendored(p string, l *decls.Lang) bool {
	for _, s := range strings.Split(path.Dir(p), "/") {
		switch s {
		case "vendor", "third_party", "third-party", "node_modules", "bower_components":
			return true
		case "build", ".build", "Pods", "Carthage", ".dart_tool", "target", ".gradle", "DerivedData":
			if l == nil || (l.Family != "sh" && l.Family != "ps1") {
				return true
			}
		}
		if strings.HasPrefix(s, "cmake-build-") {
			return true
		}
	}
	return false
}

type genType struct {
	key, fqn string
	d        *decls.GenDecl // the first part (reopened classes, extensions and companions share members)
	outer    *genType
	members  map[string]string // simple name -> node key
	nested   map[string]*genType
	ctor     string
	supers   []*genType
	fields   map[string]*genType // fields and properties declared with a workspace type
	sups     []genSuper
}

type genSuper struct{ file, name string }

func (gt *genType) member(name string, depth int) string {
	if k, ok := gt.members[name]; ok {
		return k
	}
	if depth > 6 {
		return ""
	}
	for _, s := range gt.supers {
		if k := s.member(name, depth+1); k != "" {
			return k
		}
	}
	return ""
}

func (gt *genType) field(name string, depth int) *genType {
	if ft := gt.fields[name]; ft != nil {
		return ft
	}
	if depth > 6 {
		return nil
	}
	for _, s := range gt.supers {
		if ft := s.field(name, depth+1); ft != nil {
			return ft
		}
	}
	return nil
}

// genSimple is the last segment of a qualified name (A::B, a.b.C, A\B).
func genSimple(s string) string {
	s = typeName(s)
	for _, sep := range []string{"::", ".", `\`} {
		if i := strings.LastIndex(s, sep); i >= 0 {
			s = s[i+len(sep):]
		}
	}
	return s
}

// typeName drops type arguments and array brackets: Base<T> -> Base.
func typeName(s string) string {
	s = strings.Join(strings.Fields(s), "")
	if i := strings.IndexAny(s, "<(["); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, "?!*&")
}

func genFQN(ns, name, sep string) string {
	name = strings.ReplaceAll(name, sep, ".")
	if ns == "" {
		return name
	}
	return ns + "." + name
}

func extractGen(repo, repoRoot string, tracked []string, g *Graph, family string) {
	var paths []string
	for _, p := range tracked {
		if l := decls.LangFor(p); l != nil && l.Family == family && !l.IsTest(p) && !isGenVendored(p, l) {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return
	}
	files := map[string]*decls.GenFile{}
	var order []string
	for i, f := range parseFiles(repoRoot, "gen-"+family, paths, decls.ParseGen) {
		if f != nil {
			files[paths[i]] = f
			order = append(order, paths[i])
		}
	}
	sort.Strings(order)
	key := func(f *decls.GenFile, sym string) string { return f.Lang().ID + ":" + repo + "/" + f.Path + ":" + sym }

	byFQN := map[string]*genType{}
	var types []*genType // in file order
	bySimple := map[string][]*genType{}
	typeOf := map[string]*genType{}               // node key -> type
	fileTypes := map[string]map[string]*genType{} // file -> decl name -> type
	declType := map[*decls.GenDecl]*genType{}     // member -> its type
	top := map[string]map[string]string{}         // file -> simple name -> key
	pkg := map[string]map[string][]string{}       // namespace -> simple name -> keys
	mods := map[string]map[string][]string{}      // Swift module -> simple name -> keys
	global := map[string][]string{}               // simple name -> exported top-level keys
	memberIdx := map[string][]string{}            // member name -> keys
	baseIdx := map[string][]string{}              // file base name -> paths
	type pendingMember struct {
		f *decls.GenFile
		d *decls.GenDecl
		k string
	}
	var pending []pendingMember
	addIdx := func(m map[string]map[string][]string, scope, name, k string) {
		if m[scope] == nil {
			m[scope] = map[string][]string{}
		}
		m[scope][name] = append(m[scope][name], k)
	}
	for _, p := range order {
		f := files[p]
		l := f.Lang()
		dir := path.Dir(p)
		if dir == "." {
			dir = ""
		}
		baseIdx[path.Base(p)] = append(baseIdx[path.Base(p)], p)
		g.Files = append(g.Files, FileInfo{Path: p, Lang: l.ID, Lines: f.Lines, Generated: f.Generated})
		g.Nodes = append(g.Nodes, &Node{Key: key(f, "<module>"), Kind: "module", Repo: repo, File: p, Dir: dir, Sym: "<module>", Start: 1, End: f.Lines, Internal: true})
		ft := map[string]*genType{}
		fileTypes[p] = ft
		tp := map[string]string{}
		top[p] = tp
		mod := l.Module(p)
		seen := map[string]bool{}
		for _, d := range f.Decls {
			k := key(f, d.Name)
			if !seen[d.Name] {
				seen[d.Name] = true // overloads and reopened types share a node
				n := &Node{Key: k, Kind: l.ID + "-" + d.Kind, Repo: repo, File: p, Dir: dir, Sym: d.Name,
					Start: d.Line, End: max(d.EndLine, d.Line), Exported: d.Exported}
				if !d.IsType() {
					n.Cyclo, n.Nest = d.Cyclo, d.Nest
				}
				if f.Generated {
					n.Tags = append(n.Tags, "generated")
				}
				g.Nodes = append(g.Nodes, n)
			}
			if !d.IsType() && d.Owner != "" {
				pending = append(pending, pendingMember{f, d, k})
				continue
			}
			if d.IsType() {
				fqn := genFQN(d.Namespace, d.Name, l.TypeSep)
				gt := byFQN[fqn]
				if gt == nil {
					gt = &genType{key: k, fqn: fqn, d: d, members: map[string]string{}, nested: map[string]*genType{}, fields: map[string]*genType{}}
					byFQN[fqn] = gt
					types = append(types, gt)
					bySimple[d.Simple] = append(bySimple[d.Simple], gt)
				}
				typeOf[k] = gt
				for _, s := range d.Supers {
					gt.sups = append(gt.sups, genSuper{p, s})
				}
				ft[d.Name] = gt
				if d.Owner != "" {
					if o := ft[d.Owner]; o != nil && o != gt {
						gt.outer = o
						o.nested[d.Simple] = gt
					}
					if d.Exported && gt.key == k {
						global[d.Simple] = append(global[d.Simple], k)
					}
					continue
				}
			}
			if _, ok := tp[d.Simple]; !ok {
				tp[d.Simple] = k
			}
			addIdx(pkg, d.Namespace, d.Simple, k)
			addIdx(mods, mod, d.Simple, k)
			if d.Exported {
				global[d.Simple] = append(global[d.Simple], k)
			}
		}
	}
	// Members, once every type is known: out-of-line C++ definitions and
	// extensions in other files find their type by name.
	for _, pm := range pending {
		d, l := pm.d, pm.f.Lang()
		gt := fileTypes[pm.f.Path][d.Owner]
		if gt == nil {
			gt = byFQN[genFQN(d.Namespace, d.Owner, l.TypeSep)]
		}
		if gt == nil {
			gt = byFQN[genFQN("", d.Owner, l.TypeSep)]
		}
		if gt == nil {
			if c := bySimple[genSimple(d.Owner)]; len(c) == 1 {
				gt = c[0]
			}
		}
		if gt == nil {
			continue
		}
		declType[d] = gt
		if _, ok := gt.members[d.Simple]; !ok {
			gt.members[d.Simple] = pm.k
		}
		if d.Kind == "ctor" && gt.ctor == "" {
			gt.ctor = pm.k
		}
		if d.Kind != "ctor" {
			memberIdx[d.Simple] = appendUniq(memberIdx[d.Simple], pm.k)
		}
	}

	// What each file sees, besides its own top level: named imports, then
	// imported files, then its package or namespace, then its module.
	resolveFile := func(from, s string) string {
		if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
			if c := path.Clean(path.Join(path.Dir(from), s)); files[c] != nil {
				return c
			}
			for strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
				s = s[strings.IndexByte(s, '/')+1:]
			}
		}
		s = strings.TrimPrefix(s, "/")
		if files[s] != nil {
			return s
		}
		best := ""
		for _, c := range baseIdx[path.Base(s)] {
			if c == from || !strings.HasSuffix(c, "/"+s) {
				continue
			}
			if best == "" || commonDir(c, from) > commonDir(best, from) || commonDir(c, from) == commonDir(best, from) && len(c) < len(best) {
				best = c
			}
		}
		return best
	}
	scope := map[string]map[string]string{}
	aliasFile := map[string]map[string]string{}
	for _, p := range order {
		f := files[p]
		sc := map[string]string{}
		put := func(name, k string) {
			if _, ok := sc[name]; !ok && k != "" {
				sc[name] = k
			}
		}
		putUnique := func(m map[string][]string) {
			for name, ks := range m {
				if len(ks) == 1 {
					put(name, ks[0])
				}
			}
		}
		for _, imp := range f.Imports {
			if imp.File || imp.Module || imp.Wildcard {
				continue
			}
			ns, name := "", imp.Path
			if i := strings.LastIndexByte(imp.Path, '.'); i >= 0 {
				ns, name = imp.Path[:i], imp.Path[i+1:]
			}
			as := name
			if imp.Alias != "" {
				as = imp.Alias
			}
			if ks := pkg[ns][name]; len(ks) == 1 {
				put(as, ks[0])
			} else if gt := byFQN[imp.Path]; gt != nil {
				put(as, gt.key)
			}
		}
		for _, imp := range f.Imports {
			switch {
			case imp.File:
				tf := resolveFile(p, imp.Path)
				if tf == "" {
					continue
				}
				if imp.Alias != "" {
					if aliasFile[p] == nil {
						aliasFile[p] = map[string]string{}
					}
					aliasFile[p][imp.Alias] = tf
					continue
				}
				for name, k := range top[tf] {
					put(name, k)
				}
			case imp.Wildcard:
				putUnique(pkg[imp.Path])
			}
		}
		for _, ns := range f.Namespaces {
			putUnique(pkg[ns])
		}
		if f.Lang().Family == "swift" {
			putUnique(mods[f.Lang().Module(p)])
			for _, imp := range f.Imports {
				if imp.Module {
					putUnique(mods[imp.Path])
				}
			}
		}
		scope[p] = sc
	}
	lookup := func(p, name string) string {
		if k := top[p][name]; k != "" {
			return k
		}
		if k := scope[p][name]; k != "" {
			return k
		}
		if ks := global[name]; len(ks) == 1 {
			return ks[0]
		}
		return ""
	}
	resolveType := func(p, name string) *genType {
		if gt := typeOf[lookup(p, genSimple(name))]; gt != nil {
			return gt
		}
		if gt := byFQN[strings.NewReplacer("::", ".", `\`, ".").Replace(typeName(name))]; gt != nil {
			return gt
		}
		if c := bySimple[genSimple(name)]; len(c) == 1 {
			return c[0]
		}
		return nil
	}
	for _, gt := range types {
		for _, s := range gt.sups {
			if st := resolveType(s.file, s.name); st != nil && st != gt && !containsGen(gt.supers, st) {
				gt.supers = append(gt.supers, st)
			}
		}
	}

	edges := edgeAcc{}
	for _, p := range order {
		extractGenFile(files[p], genCtx{
			key: key, lookup: lookup, typeOf: typeOf, fileTypes: fileTypes[p], declType: declType,
			memberIdx: memberIdx, top: top, aliasFile: aliasFile[p], edges: edges,
		})
	}
	// Overrides: callers of the overridden member can reach the override.
	for _, gt := range types {
		for m, k := range gt.members {
			for _, s := range gt.supers {
				if sk := s.member(m, 0); sk != "" && sk != k {
					edges.add(sk, k, "impl")
				}
			}
		}
	}
	g.Edges = append(g.Edges, edges.list()...)
}

func commonDir(a, b string) int {
	n := 0
	as, bs := strings.Split(path.Dir(a), "/"), strings.Split(path.Dir(b), "/")
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return n
}

func containsGen(s []*genType, gt *genType) bool {
	for _, x := range s {
		if x == gt {
			return true
		}
	}
	return false
}

type genCtx struct {
	key       func(f *decls.GenFile, sym string) string
	lookup    func(p, name string) string
	typeOf    map[string]*genType
	fileTypes map[string]*genType
	declType  map[*decls.GenDecl]*genType
	memberIdx map[string][]string
	top       map[string]map[string]string
	aliasFile map[string]string
	edges     edgeAcc
}

var genLocalWords = map[string]bool{"var": true, "val": true, "let": true, "const": true, "final": true, "local": true,
	"auto": true, "my": true, "lateinit": true}

func extractGenFile(f *decls.GenFile, c genCtx) {
	t, l, p := f.Toks, f.Lang(), f.Path
	isP := func(i int, s string) bool { return i >= 0 && i < len(t) && t[i].Kind == 'p' && t[i].Text == s }
	isI := func(i int) bool { return i >= 0 && i < len(t) && t[i].Kind == 'i' }
	// The innermost declaration owns each token; types come before their
	// members, so later ranges overwrite.
	owner := make([]*decls.GenDecl, len(t))
	for _, d := range f.Decls {
		for i := d.Start; i < d.End && i < len(t); i++ {
			owner[i] = d
		}
	}
	ownerKey := func(i int) string {
		if owner[i] == nil {
			return c.key(f, "<module>")
		}
		return c.key(f, owner[i].Name)
	}
	enclosing := func(i int) *genType {
		d := owner[i]
		switch {
		case d == nil:
			return nil
		case d.IsType():
			return c.fileTypes[d.Name]
		}
		return c.declType[d]
	}
	// resolve looks a plain name up: types nested in the enclosing types
	// (lexical scope), then the file's scope.
	resolve := func(i int, name string) string {
		for et := enclosing(i); et != nil; et = et.outer {
			if nt := et.nested[name]; nt != nil {
				return nt.key
			}
		}
		return c.lookup(p, name)
	}
	typeAt := func(i int) *genType {
		if !isI(i) {
			return nil
		}
		return c.typeOf[resolve(i, t[i].Text)]
	}
	// qualType reads a type name that may be qualified (A::B, a.b.C) from
	// token i and returns it with the index of its last token.
	qualType := func(i int) (*genType, int) {
		gt := typeAt(i)
		if gt == nil {
			return nil, i
		}
		for {
			j := i + 2
			if isP(i+1, ":") && isP(i+2, ":") {
				j = i + 3
			} else if !isP(i+1, ".") {
				break
			}
			if !isI(j) || gt.nested[t[j].Text] == nil {
				break
			}
			gt, i = gt.nested[t[j].Text], j
		}
		return gt, i
	}
	// access returns the receiver token of a member name at i (a.b, a?.b,
	// a->b, A::b), or -1.
	access := func(i int) int {
		j := i - 1
		switch {
		case isP(j, "."):
			j--
			for isP(j, "?") || isP(j, "!") || isP(j, "&") || isP(j, ".") {
				j--
			}
		case isP(j, ">") && isP(j-1, "-"):
			j -= 2
			if isP(j, "?") {
				j--
			}
		case isP(j, ":") && isP(j-1, ":"):
			j -= 2
		default:
			return -1
		}
		return j
	}
	// ctorType is the type constructed at i: new T, T(...), T.new, T{},
	// [T]::new.
	ctorType := func(i int) *genType {
		if isI(i) && t[i].Text == "new" {
			i++
		}
		if isP(i, "[") && isP(i+2, "]") {
			if gt := typeAt(i + 1); gt != nil && isP(i+3, ":") && isP(i+4, ":") && isI(i+5) && t[i+5].Text == l.CtorAlias {
				return gt
			}
			return nil
		}
		isNew := i > 0 && isI(i-1) && t[i-1].Text == "new"
		gt, i := qualType(i)
		if gt == nil {
			return nil
		}
		if isP(i+1, "(") || isP(i+1, "{") || isP(i+1, ".") && isI(i+2) && t[i+2].Text == l.CtorAlias || isP(i+1, "<") || isNew {
			return gt
		}
		return nil
	}

	// Variables, parameters and fields declared with a workspace type, and
	// the local names that shadow functions.
	vars := map[*decls.GenDecl]map[string]*genType{}
	locals := map[*decls.GenDecl]map[string]bool{}
	declare := func(i int, name string, gt *genType) {
		d := owner[i]
		if d != nil && d.IsType() {
			if et := c.fileTypes[d.Name]; et != nil {
				et.fields[name] = gt
			}
			return
		}
		if vars[d] == nil {
			vars[d] = map[string]*genType{}
		}
		vars[d][name] = gt
		if d != nil && d.Kind == "ctor" {
			if et := c.declType[d]; et != nil && et.fields[name] == nil {
				et.fields[name] = gt // constructor parameters usually become fields
			}
		}
	}
	local := func(i int, name string) {
		d := owner[i]
		if locals[d] == nil {
			locals[d] = map[string]bool{}
		}
		locals[d][name] = true
	}
	varType := func(i int, name string) *genType {
		if vt := vars[owner[i]][name]; vt != nil {
			return vt
		}
		for et := enclosing(i); et != nil; et = et.outer {
			if ft := et.field(name, 0); ft != nil {
				return ft
			}
		}
		return vars[nil][name]
	}
	assign := func(i int) bool {
		return isP(i, "=") && !isP(i+1, "=") && !isP(i+1, ">") && !isP(i-1, "=") && !isP(i-1, "!")
	}
	for i, tk := range t {
		if tk.Kind != 'i' {
			continue
		}
		if a := access(i); a >= 0 {
			// this.x = <typed>, self.x = T(...), @x = T.new, $this->x = new T
			if isI(a) && (l.Self[t[a].Text]) && assign(i+1) {
				v := i + 2
				if isP(v, l.VarPrefix) && l.VarPrefix != "" {
					v++
				}
				gt := ctorType(v)
				if gt == nil && isI(v) && !isP(v+1, "(") && !isP(v+1, ".") {
					gt = varType(v, t[v].Text)
				}
				if et := enclosing(i); gt != nil && et != nil {
					et.fields[tk.Text] = gt
				}
			}
			if typeAt(i) == nil { // a qualified type (ns::T x) still declares
				continue
			}
		}
		if isP(i-1, "@") && assign(i+1) { // Ruby @x = T.new
			if gt := ctorType(i + 2); gt != nil {
				if et := enclosing(i); et != nil {
					et.fields[tk.Text] = gt
				}
			}
			continue
		}
		switch {
		case access(i) >= 0:
		case isP(i+1, ":") && !isP(i+2, ":") && !isP(i-1, ":") && isI(i+2) && typeAt(i) == nil: // x: T
			local(i, tk.Text)
			if gt := typeAt(i + 2); gt != nil {
				declare(i, tk.Text, gt)
			}
			continue
		case assign(i + 1): // x = new T / T(...) / T.new
			local(i, tk.Text)
			if gt := ctorType(i + 2); gt != nil {
				declare(i, tk.Text, gt)
			}
			continue
		case genLocalWords[tk.Text] && isI(i+1): // let x =, val x:, final T x;
			j := i + 1
			if isI(j + 1) {
				j++
			}
			if j+1 >= len(t) || t[j+1].Kind == 'p' && strings.Contains("=:;,", t[j+1].Text) || t[j+1].Text == "in" {
				local(j, t[j].Text)
			}
			continue
		}
		// T x, T* x, T<U> x, T? x, PgRepo $x, [T]$x
		gt := typeAt(i)
		if gt == nil {
			continue
		}
		j := i + 1
		if isP(j, "<") {
			for depth := 0; j < len(t); j++ {
				if isP(j, "<") {
					depth++
				} else if isP(j, ">") {
					if depth--; depth == 0 {
						j++
						break
					}
				} else if t[j].Kind == 'p' && !strings.Contains(",?.:[]*&", t[j].Text) {
					break
				}
			}
		}
		for isP(j, "*") || isP(j, "&") || isP(j, "?") || isP(j, "!") || isP(j, "]") || l.VarPrefix != "" && isP(j, l.VarPrefix) {
			j++
		}
		if isI(j) && (j+1 >= len(t) || t[j+1].Kind == 'p' && strings.Contains("=;,){[:", t[j+1].Text) || t[j+1].Text == "in") && typeAt(j) == nil {
			local(j, t[j].Text)
			declare(j, t[j].Text, gt)
		}
	}

	// recv is the type of the receiver expression ending at token j.
	var recv func(j, depth int) *genType
	recv = func(j, depth int) *genType {
		if j < 0 || depth > 8 {
			return nil
		}
		if t[j].Kind == 'p' {
			switch {
			case t[j].Text == ")": // T(...).m, new T(...).m
				d := 0
				for k := j; k >= 0; k-- {
					if isP(k, ")") {
						d++
					} else if isP(k, "(") {
						if d--; d == 0 {
							if access(k-1) < 0 {
								return ctorType(k - 1)
							}
							return nil
						}
					}
				}
			case t[j].Text == "]" && isP(j-2, "["): // [T]::m
				return typeAt(j - 1)
			}
			return nil
		}
		name := t[j].Text
		switch {
		case l.Self[name]:
			return enclosing(j)
		case isP(j-1, "@"):
			for et := enclosing(j); et != nil; et = et.outer {
				if ft := et.field(name, 0); ft != nil {
					return ft
				}
			}
			return nil
		}
		if a := access(j); a >= 0 {
			rt := recv(a, depth+1)
			if rt == nil {
				return nil
			}
			if nt := rt.nested[name]; nt != nil {
				return nt
			}
			return rt.field(name, 0)
		}
		if vt := varType(j, name); vt != nil {
			return vt
		}
		if l.VarPrefix != "" && isP(j-1, l.VarPrefix) {
			return nil
		}
		return typeAt(j)
	}

	for i, tk := range t {
		if tk.Kind != 'i' || l.Self[tk.Text] || l.Super[tk.Text] {
			continue
		}
		from := ownerKey(i)
		name := tk.Text
		if a := access(i); a >= 0 {
			if isI(a) && l.Super[t[a].Text] {
				if et := enclosing(i); et != nil {
					for _, s := range et.supers {
						if k := s.member(name, 0); k != "" {
							c.edges.add(from, k, "")
						}
					}
				}
				continue
			}
			if rt := recv(a, 0); rt != nil {
				if k := rt.member(name, 0); k != "" {
					c.edges.add(from, k, "")
				} else if nt := rt.nested[name]; nt != nil {
					c.edges.add(from, nt.key, "")
				}
				if name == l.CtorAlias && rt.ctor != "" {
					c.edges.add(from, rt.ctor, "")
				}
				continue
			}
			if gt := typeAt(i); gt != nil { // ns::T, pkg.T
				c.edges.add(from, gt.key, "")
				if gt.ctor != "" && ctorType(i) == gt {
					c.edges.add(from, gt.ctor, "")
				}
				continue
			}
			if isI(a) && c.aliasFile[t[a].Text] != "" {
				if k := c.top[c.aliasFile[t[a].Text]][name]; k != "" {
					c.edges.add(from, k, "")
				}
				continue
			}
			if ks := c.memberIdx[name]; len(ks) == 1 {
				c.edges.add(from, ks[0], "") // the only member of that name
			}
			continue
		}
		if l.VarPrefix != "" && isP(i-1, l.VarPrefix) || isP(i-1, "@") {
			continue // a variable
		}
		if locals[owner[i]][name] {
			continue
		}
		found := false
		// Implicit this; not for a qualifier (A::f) or a constructor (its
		// type name resolves below), nor at a C++ type's own level, where
		// names are member declarations.
		if l.ImplicitSelf && !(isP(i+1, ":") && isP(i+2, ":")) && !(l.OutOfLine && owner[i] != nil && owner[i].IsType()) {
			for et := enclosing(i); et != nil; et = et.outer {
				if k := et.member(name, 0); k != "" && k != et.ctor {
					c.edges.add(from, k, "")
					found = true
					break
				}
			}
		}
		if found {
			continue
		}
		k := resolve(i, name)
		if k == "" {
			continue
		}
		c.edges.add(from, k, "")
		if gt := c.typeOf[k]; gt != nil && gt.ctor != "" && ctorType(i) == gt && !isP(i+1, ".") && from != gt.key {
			c.edges.add(from, gt.ctor, "")
		}
	}
}
