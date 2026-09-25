package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedConfig(t *testing.T) {
	cfg, err := loadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output != "codemap" || len(cfg.rules) == 0 || len(cfg.sinks) == 0 {
		t.Fatal("embedded rules or output missing")
	}
	if len(cfg.Contracts.GeneratedClients) > 0 || len(cfg.Rank.VirtualConsumers) > 0 {
		t.Fatal("default config includes workspace-specific bindings")
	}
}

func TestGoIndexWithoutToolchain(t *testing.T) {
	root := t.TempDir()
	src := "package sample\ntype Thing struct{}\nfunc (t *Thing) Run(x bool) { if x { println(x) } }\n"
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	var g Graph
	mods := []Module{{Path: "example.com/sample", Dir: "."}}
	extractGo("sample", root, mods, mods, &g)
	found := false
	for _, n := range g.Nodes {
		if n.Key == "go:example.com/sample:(*Thing).Run" {
			found = true
			if n.Cyclo < 2 {
				t.Fatal("lost complexity")
			}
		}
	}
	if !found || len(g.Files) != 1 {
		t.Fatalf("missing fallback declarations: %+v", g)
	}
	if len(g.Edges) != 0 {
		t.Fatal("fallback invented typed edges")
	}
	if len(g.Warnings) == 0 || !strings.Contains(strings.Join(g.Warnings, " "), "declarations only") {
		t.Fatal("missing fallback warning")
	}
}
