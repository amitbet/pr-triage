package indexer

import (
	"os"
	"path"
	"path/filepath"

	"github.com/amitbet/pr-manager/codemap/decls"
)

type Spec = decls.Spec
type SpecOp = decls.SpecOp

func isSpecPath(p string) bool { return decls.IsSpecPath(p) }

func parseSpec(repoRoot, p string) (*Spec, error) {
	b, err := os.ReadFile(filepath.Join(repoRoot, p))
	if err != nil {
		return nil, err
	}
	return decls.ParseSpec(b, p)
}

func extractSpecs(repo, repoRoot string, tracked []string, g *Graph) {
	for _, p := range tracked {
		if !isSpecPath(p) {
			continue
		}
		sp, err := parseSpec(repoRoot, p)
		if err != nil {
			g.Warnings = append(g.Warnings, "spec "+p+": "+err.Error())
			continue
		}
		if sp == nil {
			continue
		}
		g.Specs = append(g.Specs, *sp)
		dir := path.Dir(p)
		for _, op := range sp.Ops {
			g.Nodes = append(g.Nodes, &Node{
				Key: "api:" + repo + "/" + p + ":" + op.ID, Kind: "api-op", Repo: repo, File: p, Dir: dir,
				Sym: op.ID, Start: op.Start, End: op.End, Exported: true,
				Tags: []string{"http:" + op.Method + " " + op.Path},
			})
		}
	}
}
