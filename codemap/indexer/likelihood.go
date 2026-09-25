package indexer

import (
	"math"
	"strings"

	"github.com/amitbet/pr-triage/codemap"
	"github.com/amitbet/pr-triage/codemap/githist"
)

// repoHistory is one repo's git history summarized for the map.
type repoHistory struct {
	st        *githist.Stats
	cfg       *Config
	generated map[string]bool
}

func newRepoHistory(g *Graph, cfg *Config) *repoHistory {
	h := &repoHistory{cfg: cfg, generated: map[string]bool{}}
	if g == nil || g.HeadTime.IsZero() {
		return h
	}
	for _, f := range g.Files {
		if f.Generated {
			h.generated[f.Path] = true
		}
	}
	h.st = githist.Summarize(g.History, g.HeadTime, cfg.hist)
	return h
}

// followsSource: generated code and mocks change because their source did,
// so they are never a "usual partner" a PR forgot.
func (h *repoHistory) followsSource(p string) bool {
	return h.generated[p] || strings.Contains(p, "/mock/") || strings.Contains(p, "/mocks/") ||
		strings.HasPrefix(p, "mock/") || strings.HasPrefix(p, "mocks/") || strings.Contains(p, ".gen.") || strings.HasSuffix(p, "_gen.go")
}

func (h *repoHistory) stats(p string) *githist.FileStats {
	if h.st == nil {
		return nil
	}
	return h.st.Files[p]
}

// file is a file's history record: nil when the repo has no history, zero
// counts when the file did not change inside the window.
func (h *repoHistory) file(p string) *codemap.History {
	if h.st == nil {
		return nil
	}
	fs := h.st.Files[p]
	if fs == nil {
		return &codemap.History{AgeDays: -1}
	}
	return &codemap.History{
		Commits: fs.Commits, Fixes: fs.Fixes, Reverts: fs.Reverts, Authors: len(fs.Authors),
		Recent: round2(fs.Recent), RecentFixes: round2(fs.RecentFixes), AgeDays: h.st.AgeDays(fs),
	}
}

func (h *repoHistory) partners(p string) []codemap.Partner {
	if h.st == nil {
		return nil
	}
	c := h.cfg.History.CoChange
	var out []codemap.Partner
	for _, x := range h.st.Partners(p, c.MinCommits, c.MinConf, c.Max+5) {
		if !h.followsSource(x.Path) && len(out) < c.Max {
			out = append(out, codemap.Partner{Path: x.Path, N: x.N, Conf: x.Conf})
		}
	}
	return out
}

// histAgg sums file histories for a directory or repo.
type histAgg struct {
	h       codemap.History
	authors map[string]bool
	has     bool
}

func (a *histAgg) addFile(h *codemap.History, fs *githist.FileStats) {
	if h == nil {
		return
	}
	if !a.has {
		a.has, a.h.AgeDays, a.authors = true, -1, map[string]bool{}
	}
	a.h.Commits += h.Commits
	a.h.Fixes += h.Fixes
	a.h.Reverts += h.Reverts
	a.h.Recent += h.Recent
	a.h.RecentFixes += h.RecentFixes
	if h.AgeDays >= 0 && (a.h.AgeDays < 0 || h.AgeDays < a.h.AgeDays) {
		a.h.AgeDays = h.AgeDays
	}
	if fs != nil {
		for e := range fs.Authors {
			a.authors[e] = true
		}
	}
}

func (a *histAgg) merge(o *histAgg) {
	if !o.has {
		return
	}
	if !a.has {
		a.has, a.h.AgeDays, a.authors = true, -1, map[string]bool{}
	}
	a.h.Commits += o.h.Commits
	a.h.Fixes += o.h.Fixes
	a.h.Reverts += o.h.Reverts
	a.h.Recent += o.h.Recent
	a.h.RecentFixes += o.h.RecentFixes
	if o.h.AgeDays >= 0 && (a.h.AgeDays < 0 || o.h.AgeDays < a.h.AgeDays) {
		a.h.AgeDays = o.h.AgeDays
	}
	for e := range o.authors {
		a.authors[e] = true
	}
}

func (a *histAgg) record() *codemap.History {
	if !a.has {
		return nil
	}
	h := a.h
	h.Authors = len(a.authors)
	h.Recent, h.RecentFixes = round2(h.Recent), round2(h.RecentFixes)
	return &h
}

// cxAgg folds function complexity into a file/dir summary: the worst
// function, total and count.
func cxAdd(c *codemap.Complexity, cyclo, nest, total, funcs int) {
	c.Cyclo = max(c.Cyclo, cyclo)
	c.Nest = max(c.Nest, nest)
	c.Total += total
	c.Funcs += funcs
}

func cxPtr(c codemap.Complexity) *codemap.Complexity {
	if c.Funcs == 0 && c.Cyclo == 0 {
		return nil
	}
	return &c
}

func (e *emitter) likelihood(h *codemap.History, c *codemap.Complexity) (int, string) {
	v, _ := e.cfg.Likelihood.PlaceLikelihood(h, c, e.cfg.History.Days)
	return v, scoreLevel(e.cfg, v)
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
