package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/amitbet/pr-manager/codemap"
)

// treeNode is one area in the code-map treemap: a repo, directory or file,
// with its impact and likelihood.
// Short JSON keys keep the whole-workspace tree small.
type treeNode struct {
	Name   string `json:"n"`
	Path   string `json:"p"`           // repo-relative ("" for a repo)
	Kind   string `json:"k"`           // repo | dir | file
	Repo   string `json:"r,omitempty"` // set on repos (and on everything in "all" trees)
	Impact int    `json:"im"`
	Level  string `json:"l"` // impact level
	// Likelihood and the history and complexity behind it.
	Likelihood int         `json:"lk"`
	LkLevel    string      `json:"ll"`
	Commits    int         `json:"cm,omitempty"`
	Fixes      int         `json:"fx,omitempty"`
	Reverts    int         `json:"rv,omitempty"`
	Authors    int         `json:"au,omitempty"`
	AgeDays    int         `json:"ag,omitempty"`
	Cyclo      int         `json:"cy,omitempty"`
	Nest       int         `json:"ne,omitempty"`
	Rank       float64     `json:"rk"`
	Rollback   int         `json:"rb"`
	Tag        string      `json:"t,omitempty"` // strongest rollback tag
	Lines      int         `json:"s,omitempty"` // files: line count, the area
	DepRepos   int         `json:"dr,omitempty"`
	Children   []*treeNode `json:"c,omitempty"`

	byName map[string]*treeNode
}

func (n *treeNode) child(name, path, kind string) *treeNode {
	if n.byName == nil {
		n.byName = map[string]*treeNode{}
	}
	c, ok := n.byName[name]
	if !ok {
		c = &treeNode{Name: name, Path: path, Kind: kind, Level: "low", LkLevel: "low"}
		n.byName[name] = c
		n.Children = append(n.Children, c)
	}
	return c
}

func (n *treeNode) fill(r *codemap.Record) {
	n.Impact, n.Level, n.Rank, n.Rollback = r.Impact, r.ImpactLevel, r.Rank, r.Rollback
	n.Likelihood, n.LkLevel = r.Likelihood, r.LikelihoodLevel
	if n.LkLevel == "" {
		n.LkLevel = "low"
	}
	if h := r.Hist; h != nil {
		n.Commits, n.Fixes, n.Reverts, n.Authors, n.AgeDays = h.Commits, h.Fixes, h.Reverts, h.Authors, h.AgeDays
	}
	if c := r.Cx; c != nil {
		n.Cyclo, n.Nest = c.Cyclo, c.Nest
	}
	n.DepRepos = len(r.DepRepos)
	if len(r.RollbackTags) > 0 {
		n.Tag = r.RollbackTags[0].ID
	}
	if r.Level == "file" && len(r.Lines) == 2 {
		n.Lines = max(1, r.Lines[1])
	}
}

// buildRepoTree nests a repo's dir and file records by path. Symbol records
// are left out: the PR's changes are drawn on top as dots instead.
func buildRepoTree(m *codemap.Map, repo string) (*treeNode, error) {
	recs, err := codemap.ReadShard(m.Dir, repo)
	if err != nil {
		return nil, err
	}
	root := &treeNode{Name: repo, Kind: "repo", Repo: repo, Level: "low", LkLevel: "low"}
	if r, ok := m.Repos[repo]; ok {
		root.fill(&r)
	}
	for _, r := range recs {
		if r.Level != "dir" && r.Level != "file" {
			continue
		}
		parts := strings.Split(strings.Trim(r.Path, "/"), "/")
		n := root
		for i, part := range parts {
			kind := "dir"
			if i == len(parts)-1 && r.Level == "file" {
				kind = "file"
			}
			n = n.child(part, strings.Join(parts[:i+1], "/"), kind)
		}
		n.fill(r)
	}
	var sortRec func(*treeNode)
	sortRec = func(n *treeNode) {
		sort.Slice(n.Children, func(i, j int) bool { return n.Children[i].Name < n.Children[j].Name })
		for _, c := range n.Children {
			sortRec(c)
		}
	}
	sortRec(root)
	return root, nil
}

type treeCache struct {
	mu    sync.Mutex
	trees map[string]*treeNode // key: map version + repo
}

func (tc *treeCache) get(m *codemap.Map, repo string) (*treeNode, error) {
	key := codeMapVersion(m) + "|" + repo
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if t, ok := tc.trees[key]; ok {
		return t, nil
	}
	var t *treeNode
	if repo == "all" {
		t = &treeNode{Name: "workspace", Kind: "repo", Level: "low", LkLevel: "low"}
		names := make([]string, 0, len(m.Meta.Repos))
		for name := range m.Meta.Repos {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			rt, err := buildRepoTree(m, name)
			if err != nil {
				return nil, err
			}
			t.Children = append(t.Children, rt)
		}
	} else {
		if _, ok := m.Meta.Repos[repo]; !ok {
			return nil, fmt.Errorf("repo %q is not in the code map", repo)
		}
		var err error
		if t, err = buildRepoTree(m, repo); err != nil {
			return nil, err
		}
	}
	if tc.trees == nil {
		tc.trees = map[string]*treeNode{}
	}
	tc.trees[key] = t
	return t, nil
}

// treemapRoute serves GET /api/codemap/tree?repo=<name|all>.
func treemapRoute(mux *http.ServeMux, o options) {
	tc := &treeCache{}
	mux.HandleFunc("GET /api/codemap/tree", func(w http.ResponseWriter, r *http.Request) {
		m := loadCodeMap(o.codemapDir)
		if m == nil {
			writeErr(w, 404, fmt.Errorf("no code map (build it with make codemap)"))
			return
		}
		t, err := tc.get(m, r.URL.Query().Get("repo"))
		if err != nil {
			writeErr(w, 404, err)
			return
		}
		writeJSON(w, 200, map[string]any{"tree": t, "version": codeMapVersion(m), "levels": m.Meta.Levels})
	})
}
