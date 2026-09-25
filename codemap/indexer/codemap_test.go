package indexer

import (
	"path/filepath"
	"testing"
)

func TestExpandTemplateAndURLMatch(t *testing.T) {
	consts := map[string]string{"URL": "/settings/api/v1"}
	val := expandTemplate("\x00URL\x00/profiles/\x00id\x00", func(e string) (string, bool) {
		v, ok := consts[e]
		return v, ok
	}, nil, nil, nil, 0)
	if val != "/settings/api/v1/profiles/{}" {
		t.Fatalf("expand = %q", val)
	}
	m := svcURLRe.FindStringSubmatch(val)
	if m[1] != "settings" || m[2] != "/api/v1/profiles/{}" {
		t.Fatalf("svc match = %q", m)
	}
	for lit, want := range map[string]string{
		"{}/billing/api/v1/x":            "billing",
		"https://gw.example/orders/v1/x": "orders",
		"./local/path":                   "",
		"a/b/c":                          "",
	} {
		got := ""
		if m := svcURLRe.FindStringSubmatch(lit); m != nil && m[2] != "" {
			got = m[1]
		}
		if got != want {
			t.Errorf("service of %q = %q, want %q", lit, got, want)
		}
	}
	ref := splitSegs(m[2])
	byID := matchURL(ref, []string{"/api/v1"}, "/profiles/{id}")
	list := matchURL(ref, []string{"/api/v1"}, "/profiles")
	other := matchURL(ref, []string{"/api/v1"}, "/clusters/{id}")
	if byID <= 0 || list != 0 || other != 0 {
		t.Fatalf("scores byID=%d list=%d other=%d", byID, list, other)
	}
}

func TestGlob(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"**/migrations/**", "dao/postgresql/migrations/001.sql", true},
		{"**/migrations/**", "migrations/001.sql", true},
		{"**/migrations/**", "dao/migrationsx/001.sql", false},
		{"api/**/public/**", "api/v1/public/api.yaml", true},
		{"api/**/public/**", "api/public/api.yaml", true},
		{"**/*_test.go", "a/b_test.go", true},
		{"**/crds/**/*.{yaml,yml}", "helm/crds/a.yaml", true},
		{"**/crds/**/*.{yaml,yml}", "helm/crds/a.go", false},
		{"main.go", "cmd/main.go", false},
	}
	for _, c := range cases {
		if got := globRe(c.glob).MatchString(c.path); got != c.want {
			t.Errorf("%s ~ %s = %v", c.glob, c.path, got)
		}
	}
}

func TestPageRankFlowsToDependencies(t *testing.T) {
	// a -> c, b -> c, c -> d: d is most central, then c.
	out := [][]wedge{{{to: 2, w: 1}}, {{to: 2, w: 1}}, {{to: 3, w: 1}}, nil}
	real := []bool{true, true, true, true}
	pr := pagerank(4, out, teleport(4, real, nil), 0.85)
	if !(pr[3] > pr[2] && pr[2] > pr[0] && pr[0] == pr[1]) {
		t.Fatalf("pr = %v", pr)
	}
	pct := percentiles(pr, real)
	if pct[0] != 0 || pct[3] != 75 {
		t.Fatalf("pct = %v", pct)
	}
}

func TestExtractGo(t *testing.T) {
	root, _ := filepath.Abs("testdata/gomod")
	mods := []Module{{Path: "example.com/ws", Dir: ""}}
	g := &Graph{}
	extractGo("ws", root, mods, mods, g)
	nodes := map[string]*Node{}
	for _, n := range g.Nodes {
		nodes[n.Key] = n
	}
	for _, k := range []string{
		"go:example.com/ws/lib:type Store",
		"go:example.com/ws/lib:(Store).Save",
		"go:example.com/ws/lib:type Event",
		"go:example.com/ws/svc:(*pgStore).Save",
		"go:example.com/ws/svc:(*Service).Handle",
	} {
		if nodes[k] == nil {
			t.Errorf("missing node %s", k)
		}
	}
	if n := nodes["go:example.com/ws/lib:type Event"]; n != nil && !contains(n.Tags, "parquet-struct") {
		t.Errorf("Event tags = %v", n.Tags)
	}
	edges := map[string]string{}
	for _, e := range g.Edges {
		edges[e.From+" -> "+e.To] = e.Kind
	}
	for k, kind := range map[string]string{
		"go:example.com/ws/svc:(*Service).Handle -> go:example.com/ws/lib:(Store).Save": "",
		"go:example.com/ws/svc:(*Service).Handle -> go:example.com/ws/lib:Helper":       "",
		"go:example.com/ws/svc:(*Service).Handle -> go:example.com/ws/lib:type Event":   "",
		"go:example.com/ws/lib:(Store).Save -> go:example.com/ws/svc:(*pgStore).Save":   "impl",
	} {
		if got, ok := edges[k]; !ok || got != kind {
			t.Errorf("edge %s: present=%v kind=%q", k, ok, got)
		}
	}
}
