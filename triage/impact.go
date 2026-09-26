package triage

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/amitbet/pr-manager/codemap"
	"github.com/amitbet/pr-manager/codemap/decls"
)

// Impact is the code-map assessment of the code a unit touches: how much a
// bad change there can break, from how central it is (CodeRank) and how hard
// it is to roll back.
type Impact struct {
	Score int    `json:"score"` // 0-100
	Level string `json:"level"` // low | medium | high | critical | unknown
	// Matched is the map level that answered: symbol | file | dir | repo | none.
	Matched  string        `json:"matched"`
	Basis    string        `json:"basis,omitempty"` // map record ID
	Also     []string      `json:"also,omitempty"`  // other symbols the hunks overlap
	Rank     float64       `json:"rank"`            // CodeRank percentile
	Rollback int           `json:"rollback"`
	Tags     []codemap.Tag `json:"tags,omitempty"`
	Callers  int           `json:"callers"`
	DepFiles int           `json:"dep_files"`
	// DepRepos are the other repos that can reach this code.
	DepRepos   []string `json:"dep_repos,omitempty"`
	TopCallers []string `json:"top_callers,omitempty"`
	Notes      []string `json:"notes,omitempty"`
	// Commit is the repo commit the map was built from.
	Commit string `json:"commit,omitempty"`
}

func (d *Impact) Known() bool { return d != nil && d.Level != "unknown" }

// CodeMap looks units up in a code map for one repo.
type CodeMap struct {
	Map  *codemap.Map
	Repo string // repo name as the map knows it, e.g. settings
}

// declSym matches the declaration names BuildUnits produces from go/parser.
// Git's hunk context ("func (s *S) Foo(ctx") is used when parsing fails and
// never matches a map symbol.
var declSym = regexp.MustCompile(`^(\(\*?\w+\)\.\w+|\w+|(type|var|const) \w+)$`)

// Assess returns the map's view of the unit, or an "unknown" impact when
// the repo is not in the map.
//
// Lookups go by name, never by the line numbers stored in the map, which
// drift as soon as the map and the PR's base differ. Hunks are resolved to
// declaration names on the PR's own base revision (base, may be nil):
// Go declarations, TS top-level declarations, Java types and methods,
// Python functions, classes and methods, C# types and members, Rust items,
// OpenAPI operationIds.
func (c *CodeMap) Assess(u *Unit, f FileDiff, base ContentFunc) *Impact {
	meta, ok := c.Map.Meta.Repos[c.Repo]
	if !ok {
		return &Impact{Level: "unknown", Matched: "none", Notes: []string{fmt.Sprintf("repo %q is not in the code map", c.Repo)}}
	}
	path := u.File
	if f.OldPath != "" && f.Status == StatusRenamed {
		path = f.OldPath // the map indexed the base side
	}
	q := codemap.Query{Repo: c.Repo, Path: path, NewFile: f.Status == StatusAdded}
	var results []codemap.Result
	var notes []string
	if f.Status != StatusAdded { // new files: only the directory and path rules apply
		names, useLines := resolveNames(u, path, base)
		var missing []string
		for _, n := range names {
			q.Sym = n
			if r := c.Map.Lookup(q); r.Matched == "symbol" {
				results = append(results, r)
			} else {
				missing = append(missing, n)
			}
		}
		q.Sym = ""
		if len(missing) > 0 && len(results) == 0 {
			notes = append(notes, "not in the map (new or below the emit threshold): "+strings.Join(missing, ", "))
		}
		if useLines && len(results) == 0 {
			// No base revision to parse: fall back to the map's own line
			// ranges, which are only right while the map matches the base.
			q.StartLine, q.EndLine = oldRange(u.Hunks)
			notes = append(notes, "placed by the map's line numbers (no base revision available)")
		}
	}
	if len(results) == 0 {
		results = append(results, c.Map.Lookup(q))
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Impact > results[j].Impact })
	res := results[0]
	d := &Impact{Score: res.Impact, Level: res.ImpactLevel, Matched: res.Matched, Rollback: res.Rollback,
		Notes: append(notes, res.Notes...), Commit: meta.Commit}
	if len(res.Basis) > 0 {
		b := res.Basis[0]
		d.Basis, d.Rank, d.Tags = b.ID, b.Rank, b.RollbackTags
		d.Callers, d.DepFiles, d.DepRepos, d.TopCallers = b.Callers, b.DepFiles, b.DepRepos, b.TopCallers
		for _, o := range res.Basis[1:] {
			d.Also = append(d.Also, o.ID)
		}
	}
	for _, r := range results[1:] {
		d.Also = append(d.Also, r.Basis[0].ID)
	}
	if len(d.Tags) == 0 {
		d.Tags = res.PathRules
	}
	return d
}

// resolveNames returns the map symbols a unit touches. useLines is set when
// the file kind has names but the base revision is not available.
func resolveNames(u *Unit, path string, base ContentFunc) (names []string, useLines bool) {
	isGo := strings.HasSuffix(path, ".go")
	hasNames := isGo || decls.HasDecls(path) || decls.IsSpecPath(path)
	if !hasNames {
		return nil, false // SQL, YAML, Dockerfile...: the file record is the answer
	}
	add := func(n string) {
		for _, x := range names {
			if x == n {
				return
			}
		}
		names = append(names, n)
	}
	if u.Symbol != "" && u.Symbol != "imports" && (!isGo || declSym.MatchString(u.Symbol)) {
		// The head-side name from BuildUnits. If the map knows it, that is
		// the answer; base-side names below cover renamed declarations.
		add(u.Symbol)
	}
	if base == nil {
		return names, len(names) == 0
	}
	src, err := base(path)
	if err != nil {
		return names, false
	}
	var ds []decls.Decl
	switch {
	case isGo:
		for _, d := range goDecls(src) {
			if d.name != "imports" && declSym.MatchString(d.name) {
				ds = append(ds, decls.Decl{Name: d.name, Start: d.start, End: d.end})
			}
		}
	case decls.HasDecls(path):
		ds = decls.Names(path, src)
	default:
		ds = decls.SpecDecls(src)
	}
	// Each changed line counts for the innermost declaration holding it.
	deleted, inserts := changedOld(u.Hunks)
	hit := map[string]bool{}
	for _, n := range deleted {
		if d := decls.Innermost(ds, n, n); d != nil {
			hit[d.Name] = true
		}
	}
	// A pure insertion belongs to a declaration only when it lands
	// strictly inside it; otherwise it is a new declaration.
	for _, n := range inserts {
		if d := decls.Innermost(ds, n-1, n); d != nil {
			hit[d.Name] = true
		}
	}
	for _, d := range ds {
		if hit[d.Name] {
			add(d.Name)
			hit[d.Name] = false
		}
	}
	return names, false
}

// changedOld returns the base-side lines the hunks delete or modify, and
// the base lines before which they insert new ones.
func changedOld(hunks []Hunk) (deleted, inserts []int) {
	for _, h := range hunks {
		o := h.OldStart
		prevDel := false
		for _, l := range h.Lines {
			switch {
			case strings.HasPrefix(l, "-"):
				deleted = append(deleted, o)
				o++
				prevDel = true
			case strings.HasPrefix(l, "+"):
				if !prevDel {
					inserts = append(inserts, o)
				}
			case strings.HasPrefix(l, "\\"):
			default:
				o++
				prevDel = false
			}
		}
	}
	return deleted, inserts
}

// oldRange is the base-side line span of the hunks, used only when there is
// no base revision to resolve names from.
func oldRange(hunks []Hunk) (start, end int) {
	for _, h := range hunks {
		s, e := h.OldStart, h.OldStart+h.OldLines-1
		if h.OldLines == 0 {
			e = h.OldStart + 1
		}
		if start == 0 || s < start {
			start = s
		}
		if e > end {
			end = e
		}
	}
	return max(start, 1), max(end, 1)
}

// Place returns the map's file and directory records for a path (either
// may be nil): the history and co-change partners behind likelihood.
func (c *CodeMap) Place(path string) (file, dir *codemap.Record) {
	if _, ok := c.Map.Meta.Repos[c.Repo]; !ok {
		return nil, nil
	}
	res := c.Map.Lookup(codemap.Query{Repo: c.Repo, Path: path})
	for _, r := range append(append([]*codemap.Record(nil), res.Basis...), res.Chain...) {
		switch {
		case r.Level == "file" && file == nil && res.Matched == "file":
			file = r
		case r.Level == "dir" && dir == nil:
			dir = r
		}
	}
	return file, dir
}

// reviewContext is the code-map text added to review prompts so the
// reviewer knows who depends on the code, what is hard to undo and why a
// change here is likely to go wrong.
func reviewContext(u *Unit) string {
	return mapContext(u.Impact) + likelihoodContext(u.Likelihood)
}

func mapContext(d *Impact) string {
	if !d.Known() {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Code map: impact %d/100 (%s), CodeRank percentile %.0f", d.Score, d.Level, d.Rank)
	if d.Basis != "" {
		fmt.Fprintf(&sb, ", matched %s %s", d.Matched, d.Basis)
	}
	sb.WriteString(".\n")
	if len(d.DepRepos) > 0 {
		fmt.Fprintf(&sb, "Other repos that depend on this code: %s.\n", strings.Join(d.DepRepos, ", "))
	}
	if d.Callers > 0 {
		fmt.Fprintf(&sb, "Direct callers: %d", d.Callers)
		if len(d.TopCallers) > 0 {
			fmt.Fprintf(&sb, " (e.g. %s)", strings.Join(d.TopCallers[:min(3, len(d.TopCallers))], ", "))
		}
		sb.WriteString(".\n")
	}
	var tags []string
	for _, t := range d.Tags {
		if t.Score >= 40 {
			tags = append(tags, t.ID)
		}
	}
	if len(tags) > 0 {
		fmt.Fprintf(&sb, "Hard to roll back because: %s.\n", strings.Join(tags, ", "))
	}
	return sb.String()
}
