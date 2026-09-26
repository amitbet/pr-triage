package indexer

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amitbet/pr-manager/codemap/decls"
)

// The Rust extractor works from the tree-sitter parse in codemap/decls,
// without cargo or rustc. Every Cargo package is a crate named after its
// [lib] or [package] name. Its module tree follows the files under src/
// (a/b.rs and a/b/mod.rs are crate::a::b; lib.rs, main.rs and src/bin/*
// share the root) plus inline mod blocks. Names resolve through the
// module's own items and submodules, its use declarations (lists, aliases,
// globs, pub use re-exports) and the workspace's crate names; paths step
// through modules, enums and the functions and consts in a type's impl
// blocks. Method calls and fields resolve when the receiver's type is
// known: self, or a parameter, let binding or struct field declared with a
// workspace type (Box, Arc, Option, Result and references seen through,
// generic parameters by their bound), or a binding from a call that
// returns one. A trait's methods get "impl" edges to their implementations.
// #[path] on a mod declaration is followed. tests/, benches/, *_tests.rs,
// #[test] functions, #[cfg(test)] modules (inline or in their own file) and
// target/ are left out.

func isRsVendored(p string) bool {
	return strings.HasPrefix(p, "target/") || strings.Contains(p, "/target/") ||
		strings.HasPrefix(p, "vendor/") || strings.Contains(p, "/vendor/")
}

func isRsTestLike(p string) bool {
	return strings.HasPrefix(p, "tests/") || strings.Contains(p, "/tests/") ||
		strings.HasPrefix(p, "benches/") || strings.Contains(p, "/benches/") ||
		strings.HasSuffix(p, "_tests.rs") || strings.HasSuffix(p, "_test.rs")
}

type rsCrate struct {
	name   string // as written in paths (acme_store)
	dir    string // package directory ("" = repo root)
	root   *rsMod
	mods   map[string]*rsMod
	macros map[string]*rsItem // macro_rules, by name
}

func (c *rsCrate) mod(p string) *rsMod {
	if m := c.mods[p]; m != nil {
		return m
	}
	m := &rsMod{crate: c, path: p, subs: map[string]*rsMod{}, defs: map[string]*rsItem{}}
	c.mods[p] = m
	if p != "" {
		parent, name := "", p
		if i := strings.LastIndex(p, "::"); i >= 0 {
			parent, name = p[:i], p[i+2:]
		}
		m.parent = c.mod(parent)
		m.parent.subs[name] = m
	}
	return m
}

type rsMod struct {
	crate  *rsCrate
	path   string // "" for the crate root
	parent *rsMod
	subs   map[string]*rsMod
	defs   map[string]*rsItem
	uses   []decls.RsUse
	key    string // an inline module's node
	test   bool   // #[cfg(test)] mod x;
}

func (m *rsMod) isTest() bool {
	for ; m != nil; m = m.parent {
		if m.test {
			return true
		}
	}
	return false
}

type rsItem struct {
	key     string
	d       *decls.RsDecl
	mod     *rsMod
	members map[string]*rsItem // types and traits: the functions and consts of their impls and bodies
	traits  []*rsItem          // types: implemented traits; traits: supertraits
	self    *rsItem            // functions in an impl or trait: that type or trait
	ret     *rsItem            // functions: the workspace type returned, once resolved
	retDone bool
}

func (it *rsItem) member(name string, depth int) *rsItem {
	if m := it.members[name]; m != nil {
		return m
	}
	if depth > 6 {
		return nil
	}
	for _, t := range it.traits {
		if m := t.member(name, depth+1); m != nil {
			return m
		}
	}
	return nil
}

func (it *rsItem) isType() bool {
	switch it.d.Kind {
	case "struct", "enum", "union", "trait", "type":
		return true
	}
	return false
}

func (it *rsItem) variant(name string) bool {
	for _, v := range it.d.Variants {
		if v == name {
			return true
		}
	}
	return false
}

// rsRef is what a path resolves to: a module or an item.
type rsRef struct {
	mod  *rsMod
	item *rsItem
}

func (r rsRef) ok() bool { return r.mod != nil || r.item != nil }

type rsIndex struct {
	crates map[string]*rsCrate // by name
	memo   map[rsMemo]rsRef
}

type rsMemo struct {
	m    *rsMod
	name string
}

// lookup resolves a name in module m: its items and submodules, then its
// use bindings (the last one wins), glob imports, and crate names.
func (x *rsIndex) lookup(m *rsMod, name string, depth int) rsRef {
	if m == nil || depth > 10 {
		return rsRef{}
	}
	if it := m.defs[name]; it != nil {
		return rsRef{item: it}
	}
	if s := m.subs[name]; s != nil {
		return rsRef{mod: s}
	}
	if r, ok := x.memo[rsMemo{m, name}]; ok {
		return r
	}
	var r rsRef
	for i := len(m.uses) - 1; i >= 0 && !r.ok(); i-- {
		if u := m.uses[i]; !u.Wildcard && u.Local == name {
			r = x.path(m, u.Path, depth+1)
		}
	}
	for _, u := range m.uses {
		if r.ok() {
			break
		}
		if !u.Wildcard {
			continue
		}
		switch base := x.path(m, u.Path, depth+1); {
		case base.mod != nil && base.mod != m:
			r = x.lookup(base.mod, name, depth+1)
		case base.item != nil && base.item.variant(name):
			r = base
		}
	}
	if !r.ok() {
		if c := x.crates[name]; c != nil {
			r = rsRef{mod: c.root}
		}
	}
	if depth == 0 {
		x.memo[rsMemo{m, name}] = r
	}
	return r
}

// path resolves a::b::c written in module m. A head that m does not know is
// tried at the crate root (2015-edition paths).
func (x *rsIndex) path(m *rsMod, p string, depth int) rsRef {
	segs := strings.Split(p, "::")
	var r rsRef
	switch segs[0] {
	case "crate":
		r = rsRef{mod: m.crate.root}
	case "self":
		r = rsRef{mod: m}
	case "super":
		r = rsRef{mod: m.parent}
		if m.parent == nil {
			r = rsRef{mod: m}
		}
	default:
		if r = x.lookup(m, segs[0], depth); !r.ok() && m != m.crate.root {
			r = x.lookup(m.crate.root, segs[0], depth)
		}
	}
	for _, s := range segs[1:] {
		if !r.ok() {
			return rsRef{}
		}
		r = x.step(r, s, depth)
	}
	return r
}

// step resolves ::name on r.
func (x *rsIndex) step(r rsRef, name string, depth int) rsRef {
	switch {
	case r.mod != nil && name == "super":
		if r.mod.parent != nil {
			return rsRef{mod: r.mod.parent}
		}
		return r
	case r.mod != nil && name == "self":
		return r
	case r.mod != nil:
		return x.lookup(r.mod, name, depth+1)
	case r.item != nil:
		if it := r.item.member(name, 0); it != nil {
			return rsRef{item: it}
		}
		if r.item.variant(name) {
			return r // an enum variant stands for its enum
		}
	}
	return rsRef{}
}

// typeOf resolves a type path as written in d (which scopes generic
// parameters and Self) to a workspace type.
func (x *rsIndex) typeOf(m *rsMod, d *decls.RsDecl, self *rsItem, ty string) *rsItem {
	for k := 0; k < 3 && ty != ""; k++ {
		switch {
		case ty == "Self":
			return self
		case strings.HasPrefix(ty, "Self::"):
			if self == nil {
				return nil
			}
			r := rsRef{item: self}
			for _, s := range strings.Split(ty, "::")[1:] {
				if r = x.step(r, s, 0); !r.ok() {
					return nil
				}
			}
			if r.item != nil && r.item.isType() {
				return r.item
			}
			return nil
		}
		if b := rsBound(d, ty); b != "" {
			ty = b
			continue
		}
		if r := x.path(m, ty, 0); r.item != nil && r.item.isType() {
			return r.item
		}
		return nil
	}
	return nil
}

// rsBound finds a generic parameter's bound on d or the items around it.
func rsBound(d *decls.RsDecl, name string) string {
	for ; d != nil; d = d.In() {
		if b := d.Bounds[name]; b != "" {
			return b
		}
	}
	return ""
}

// returns resolves the workspace type a function returns.
func (x *rsIndex) returns(fn *rsItem) *rsItem {
	if !fn.retDone {
		fn.retDone = true
		fn.ret = x.typeOf(fn.mod, fn.d, fn.self, fn.d.Returns)
	}
	return fn.ret
}

// valueOf resolves the type of a parameter or let binding in fn: its
// declared type, or what the call initializing it returns.
func (x *rsIndex) valueOf(fn *rsItem, ty string) *rsItem {
	call, ok := strings.CutPrefix(ty, "=")
	if !ok {
		return x.typeOf(fn.mod, fn.d, fn.self, ty)
	}
	var r rsRef
	if rest, ok := strings.CutPrefix(call, "Self::"); ok && fn.self != nil {
		r = x.step(rsRef{item: fn.self}, rest, 0)
	} else {
		r = x.path(fn.mod, call, 0)
	}
	switch {
	case r.item == nil:
		return nil
	case r.item.isType(): // a tuple struct or an enum variant
		return r.item
	case r.item.d.IsFunc():
		return x.returns(r.item)
	}
	return nil
}

// field resolves the type of field name of struct t.
func (x *rsIndex) field(t *rsItem, name string) *rsItem {
	if t.d.Fields == nil {
		return nil
	}
	return x.typeOf(t.mod, t.d, t, t.d.Fields[name])
}

// rsModPath maps a file of the crate in dir to its module path.
func rsModPath(dir, p string) (string, bool) {
	rel := p
	if dir != "" {
		rel = strings.TrimPrefix(p, dir+"/")
	}
	rest, ok := strings.CutPrefix(rel, "src/")
	if !ok {
		return "", false
	}
	rest = strings.TrimSuffix(rest, ".rs")
	if b, ok := strings.CutPrefix(rest, "bin/"); ok { // bin/x.rs and bin/x/main.rs are roots
		_, sub, nested := strings.Cut(b, "/")
		if !nested || sub == "main" {
			return "", true
		}
		rest = sub
	}
	if rest == "lib" || rest == "main" {
		return "", true
	}
	return strings.ReplaceAll(strings.TrimSuffix(rest, "/mod"), "/", "::"), true
}

// cargoName reads the crate name from a Cargo.toml: [lib] name, else
// [package] name, with - as _.
func cargoName(b []byte) string {
	var section, pkg, lib string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "[") {
			section = strings.Trim(l, "[] ")
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok || strings.TrimSpace(k) != "name" {
			continue
		}
		v = strings.TrimSpace(v)
		if q := v[:min(1, len(v))]; q == `"` || q == "'" {
			v, _, _ = strings.Cut(v[1:], q)
		}
		switch section {
		case "package":
			pkg = v
		case "lib":
			lib = v
		}
	}
	if lib != "" {
		pkg = lib
	}
	return strings.ReplaceAll(pkg, "-", "_")
}

var rsKeywords = map[string]bool{
	"as": true, "async": true, "await": true, "break": true, "const": true, "continue": true, "dyn": true,
	"else": true, "enum": true, "extern": true, "false": true, "fn": true, "for": true, "if": true, "impl": true,
	"in": true, "let": true, "loop": true, "match": true, "mod": true, "move": true, "mut": true, "pub": true,
	"ref": true, "return": true, "static": true, "struct": true, "trait": true, "true": true, "type": true,
	"unsafe": true, "use": true, "where": true, "while": true,
}

// rsSameType are methods whose result is used as the receiver's type.
var rsSameType = map[string]bool{
	"clone": true, "as_ref": true, "as_mut": true, "unwrap": true, "expect": true, "borrow": true,
	"borrow_mut": true, "lock": true, "read": true, "write": true, "deref": true, "to_owned": true,
}

type rsFile struct {
	f     *decls.RsFile
	crate *rsCrate
	mod   *rsMod
}

func extractRust(repo, repoRoot string, tracked []string, g *Graph) {
	var paths, manifests []string
	for _, p := range tracked {
		switch {
		case isRsVendored(p):
		case path.Base(p) == "Cargo.toml":
			manifests = append(manifests, p)
		case decls.IsRustPath(p) && !isRsTestLike(p):
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return
	}
	x := &rsIndex{crates: map[string]*rsCrate{}, memo: map[rsMemo]rsRef{}}
	var crates []*rsCrate
	newCrate := func(name, dir string) *rsCrate {
		c := &rsCrate{name: name, dir: dir, mods: map[string]*rsMod{}, macros: map[string]*rsItem{}}
		c.root = c.mod("")
		crates = append(crates, c)
		if name != "" && x.crates[name] == nil {
			x.crates[name] = c
		}
		return c
	}
	for _, p := range manifests {
		b, err := os.ReadFile(filepath.Join(repoRoot, p))
		if err != nil {
			continue
		}
		newCrate(cargoName(b), pyDir(p))
	}
	loose := len(crates) == 0 // no Cargo.toml: module paths from the tree
	if loose {
		newCrate("", "")
	}
	sort.Slice(crates, func(i, j int) bool { return len(crates[i].dir) > len(crates[j].dir) })
	crateOf := func(p string) *rsCrate {
		for _, c := range crates {
			if c.dir == "" || strings.HasPrefix(p, c.dir+"/") {
				return c
			}
		}
		return nil
	}

	files := map[string]*rsFile{}
	var order []string
	for i, f := range parseFiles(repoRoot, "rs", paths, decls.ParseRust) {
		p := paths[i]
		c := crateOf(p)
		if f == nil || c == nil {
			continue
		}
		mp, ok := rsModPath(c.dir, p)
		if !ok {
			if !loose {
				continue // build.rs, examples/
			}
			mp = strings.ReplaceAll(strings.TrimSuffix(strings.TrimSuffix(p, ".rs"), "/mod"), "/", "::")
		}
		files[p] = &rsFile{f: f, crate: c, mod: c.mod(mp)}
		order = append(order, p)
	}
	// #[path = "f.rs"] mod x; puts f.rs at x, and #[cfg(test)] mod x; makes
	// x a test.
	for _, p := range order {
		fi := files[p]
		for _, m := range fi.f.Mods {
			if m.Path == "" {
				continue
			}
			if target := files[path.Join(path.Dir(p), m.Path)]; target != nil && target.crate == fi.crate {
				target.mod = fi.crate.mod(rsJoin(fi.mod.path, m.Mod))
			}
		}
	}
	for _, p := range order {
		fi := files[p]
		for _, m := range fi.f.Mods {
			if m.Test {
				fi.crate.mod(rsJoin(fi.mod.path, m.Mod)).test = true
			}
		}
	}
	kept := order[:0]
	for _, p := range order {
		if files[p].mod.isTest() {
			delete(files, p)
			continue
		}
		kept = append(kept, p)
	}
	order = kept
	if len(order) == 0 {
		return
	}
	sort.Strings(order)
	key := func(file, sym string) string { return "rs:" + repo + "/" + file + ":" + sym }
	modKey := func(file string) string { return key(file, "<module>") }

	itemOf := map[*decls.RsDecl]*rsItem{}
	declMod := map[*decls.RsDecl]*rsMod{}
	type rsImpl struct {
		d       *decls.RsDecl
		mod     *rsMod
		members []*rsItem
		owner   *rsItem
		trait   *rsItem
	}
	var impls []*rsImpl
	implOf := map[*decls.RsDecl]*rsImpl{}
	for _, p := range order {
		fi := files[p]
		f := fi.f
		dir := pyDir(p)
		g.Files = append(g.Files, FileInfo{Path: p, Lang: "rs", Lines: f.Lines, Generated: f.Generated})
		g.Nodes = append(g.Nodes, &Node{Key: modKey(p), Kind: "module", Repo: repo, File: p, Dir: dir, Sym: "<module>", Start: 1, End: f.Lines, Internal: true})
		for _, u := range f.Uses {
			m := fi.crate.mod(rsJoin(fi.mod.path, u.Mod))
			m.uses = append(m.uses, u)
		}
		first := map[string]*rsItem{}
		for _, d := range f.Decls {
			if d.Test {
				continue
			}
			m := fi.crate.mod(rsJoin(fi.mod.path, d.Mod))
			declMod[d] = m
			if d.Kind == "impl" {
				im := &rsImpl{d: d, mod: m}
				impls = append(impls, im)
				implOf[d] = im
				continue
			}
			if it := first[d.Name]; it != nil {
				itemOf[d] = it // the same name twice (impls of two traits): one node
				continue
			}
			n := &Node{Key: key(p, d.Name), Kind: "rs-" + d.Kind, Repo: repo, File: p, Dir: dir, Sym: d.Name,
				Start: d.Line, End: max(d.EndLine, d.Line), Exported: d.Exported}
			if d.IsFunc() {
				n.Cyclo, n.Nest = d.Cyclo, d.Nest
			}
			if f.Generated {
				n.Tags = append(n.Tags, "generated")
			}
			g.Nodes = append(g.Nodes, n)
			it := &rsItem{key: n.Key, d: d, mod: m, members: map[string]*rsItem{}}
			itemOf[d], first[d.Name] = it, it
			name := rsLast(d.Name)
			switch in := d.In(); {
			case d.Kind == "mod":
				fi.crate.mod(rsJoin(m.path, name)).key = n.Key
			case in != nil && in.Kind == "trait":
				if t := itemOf[in]; t != nil {
					it.self = t
					if t.members[name] == nil {
						t.members[name] = it
					}
				}
			case in != nil && in.Kind == "impl":
				if im := implOf[in]; im != nil {
					im.members = append(im.members, it)
				}
			default:
				if m.defs[name] == nil {
					m.defs[name] = it
				}
				if d.Kind == "macro" && fi.crate.macros[name] == nil {
					fi.crate.macros[name] = it
				}
			}
		}
	}
	// impl blocks attach their functions to the type, and the trait to the
	// type's traits.
	for _, im := range impls {
		im.owner = x.typeOf(im.mod, im.d, nil, im.d.Owner)
		im.trait = x.typeOf(im.mod, im.d, nil, im.d.Trait)
		for _, it := range im.members {
			it.self = im.owner
			if im.owner != nil && im.owner.members[rsLast(it.d.Name)] == nil {
				im.owner.members[rsLast(it.d.Name)] = it
			}
		}
		if im.owner != nil && im.trait != nil && im.trait != im.owner {
			im.owner.traits = append(im.owner.traits, im.trait)
		}
	}
	for d, it := range itemOf {
		if d.Kind == "trait" && it.d == d {
			for _, s := range d.Supers {
				if st := x.typeOf(it.mod, d, it, s); st != nil && st != it {
					it.traits = append(it.traits, st)
				}
			}
		}
	}

	edges := edgeAcc{}
	for _, p := range order {
		fi := files[p]
		t := fi.f.Toks
		owner := make([]*decls.RsDecl, len(t))
		test := make([]bool, len(t))
		for _, d := range fi.f.Decls {
			for i := d.Start; i < d.End && i < len(t); i++ {
				owner[i], test[i] = d, d.Test
			}
		}
		isP := func(i int, s string) bool { return i >= 0 && i < len(t) && t[i].Kind == 'p' && t[i].Text == s }
		isPath := func(i int) bool { return isP(i, ":") && isP(i+1, ":") }
		ownerKey := func(i int) string {
			d := owner[i]
			switch {
			case d == nil:
				return modKey(p)
			case d.Kind == "impl":
				if im := implOf[d]; im != nil && im.owner != nil {
					return im.owner.key
				}
				if m := declMod[d]; m != nil && m.key != "" {
					return m.key
				}
				return modKey(p)
			}
			return itemOf[d].key
		}
		modOf := func(i int) *rsMod {
			d := owner[i]
			switch {
			case d == nil:
				return fi.mod
			case d.Kind == "mod":
				return fi.crate.mod(rsJoin(declMod[d].path, rsLast(d.Name)))
			}
			return declMod[d]
		}
		// The type self is, and the function a token is in.
		selfOf := func(i int) *rsItem {
			for d := owner[i]; d != nil; d = d.In() {
				switch d.Kind {
				case "impl":
					if im := implOf[d]; im != nil {
						return im.owner
					}
					return nil
				case "trait":
					return itemOf[d]
				}
			}
			return nil
		}
		fnOf := func(i int) *rsItem {
			for d := owner[i]; d != nil; d = d.In() {
				if d.IsFunc() {
					return itemOf[d]
				}
			}
			return nil
		}
		for i := 0; i < len(t); i++ {
			tk := t[i]
			if tk.Kind != 'i' || test[i] || owner[i] != nil && itemOf[owner[i]] == nil && owner[i].Kind != "impl" {
				continue
			}
			if tk.Text == "use" { // imports are not uses
				for i < len(t) && !isP(i, ";") {
					i++
				}
				continue
			}
			if rsKeywords[tk.Text] || isP(i-1, ".") || isP(i-1, ":") && isP(i-2, ":") {
				continue
			}
			from, m := ownerKey(i), modOf(i)
			if isP(i+1, "!") && !isP(i+2, "=") { // a macro call
				r := x.lookup(m, tk.Text, 0)
				if r.item == nil || r.item.d.Kind != "macro" {
					r = rsRef{item: m.crate.macros[tk.Text]}
				}
				if r.item != nil {
					edges.add(from, r.item.key, "")
				}
				continue
			}
			var r rsRef
			var val *rsItem // the type of the value so far
			fn := fnOf(i)
			switch {
			case tk.Text == "self" && !isPath(i+1):
				val = selfOf(i)
			case tk.Text == "self":
				r = rsRef{mod: m}
			case tk.Text == "Self":
				if st := selfOf(i); st != nil {
					r = rsRef{item: st}
				}
			case tk.Text == "crate":
				r = rsRef{mod: m.crate.root}
			case tk.Text == "super":
				r = rsRef{mod: m.parent}
			case fn != nil && fn.d.Locals[tk.Text]:
				if ty := fn.d.Vars[tk.Text]; ty != "" {
					val = x.valueOf(fn, ty)
				}
			default:
				r = x.lookup(m, tk.Text, 0)
			}
			if r.mod == nil && r.item == nil && tk.Text == "super" {
				continue
			}
			// a::b::c: an edge to every item on the way.
			j := i
			if r.item != nil {
				edges.add(from, r.item.key, "")
			}
			for r.ok() && isPath(j+1) && j+3 < len(t) && t[j+3].Kind == 'i' {
				n := x.step(r, t[j+3].Text, 0)
				if !n.ok() {
					break
				}
				r, j = n, j+3
				if r.item != nil {
					edges.add(from, r.item.key, "")
				}
			}
			if r.item != nil && isP(j+1, "(") {
				switch {
				case r.item.d.IsFunc():
					val = x.returns(r.item)
				case r.item.isType():
					val = r.item // Tuple(..), Enum::Variant(..)
				}
				j = skipParens(t, j+1) - 1
			}
			// .method() and .field, while the type is known.
			for val != nil {
				if isP(j+1, "?") {
					j++
				}
				if !isP(j+1, ".") || j+2 >= len(t) || t[j+2].Kind != 'i' && t[j+2].Kind != 'n' {
					break
				}
				k := j + 2
				name := t[k].Text
				switch {
				case name == "await":
					j = k
				case isP(k+1, "("):
					if mt := val.member(name, 0); mt != nil {
						edges.add(from, mt.key, "")
						if val = x.returns(mt); val == nil && mt.d.Returns == "Self" {
							val = mt.self
						}
					} else if !rsSameType[name] {
						val = nil
					}
					j = skipParens(t, k+1) - 1
				default:
					val, j = x.field(val, name), k
				}
			}
		}
	}
	// A trait's methods reach their implementations.
	for _, im := range impls {
		if im.trait == nil {
			continue
		}
		for _, it := range im.members {
			if tm := im.trait.member(rsLast(it.d.Name), 0); tm != nil && tm != it {
				edges.add(tm.key, it.key, "impl")
			}
		}
	}
	g.Edges = append(g.Edges, edges.list()...)
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

func rsLast(p string) string {
	if i := strings.LastIndex(p, "::"); i >= 0 {
		return p[i+2:]
	}
	return p
}
