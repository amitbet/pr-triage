package decls

import (
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Spec is a parsed OpenAPI document. Each operation spans from its method
// key to the line before the next operation.
type Spec struct {
	File    string   `json:"file"`
	Servers []string `json:"servers"`
	Ops     []SpecOp `json:"ops"`
}

type SpecOp struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
}

var httpMethods = map[string]bool{"get": true, "put": true, "post": true, "delete": true, "patch": true, "head": true, "options": true}

// IsSpecPath limits spec discovery to the service API definitions; vendored
// third-party SDK specs (e.g. commons/sdk) are indexed as plain files.
func IsSpecPath(p string) bool {
	b := path.Base(p)
	if b != "api.yaml" && b != "openapi.yaml" && b != "api.yml" && b != "openapi.yml" && b != "swagger.yaml" {
		return false
	}
	return strings.HasPrefix(p, "api/") || strings.Contains(p, "/api/")
}

func ParseSpec(b []byte, p string) (*Spec, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	sp := &Spec{File: p}
	get := func(m *yaml.Node, k string) *yaml.Node {
		if m == nil || m.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == k {
				return m.Content[i+1]
			}
		}
		return nil
	}
	if get(root, "openapi") == nil && get(root, "swagger") == nil {
		return nil, nil
	}
	if srv := get(root, "servers"); srv != nil && srv.Kind == yaml.SequenceNode {
		for _, s := range srv.Content {
			if u := get(s, "url"); u != nil {
				sp.Servers = append(sp.Servers, u.Value)
			}
		}
	}
	if bp := get(root, "basePath"); bp != nil {
		sp.Servers = append(sp.Servers, bp.Value)
	}
	paths := get(root, "paths")
	if paths == nil || paths.Kind != yaml.MappingNode {
		return sp, nil
	}
	lines := strings.Count(string(b), "\n") + 1
	for i := 0; i+1 < len(paths.Content); i += 2 {
		pth := paths.Content[i].Value
		item := paths.Content[i+1]
		if item.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(item.Content); j += 2 {
			m := strings.ToLower(item.Content[j].Value)
			if !httpMethods[m] {
				continue
			}
			op := item.Content[j+1]
			id := ""
			if o := get(op, "operationId"); o != nil {
				id = o.Value
			}
			if id == "" {
				id = strings.ToUpper(m) + " " + pth
			}
			sp.Ops = append(sp.Ops, SpecOp{ID: id, Method: strings.ToUpper(m), Path: pth, Start: item.Content[j].Line})
		}
	}
	sort.Slice(sp.Ops, func(i, j int) bool { return sp.Ops[i].Start < sp.Ops[j].Start })
	// An operation spans until the next one starts; the last one runs until
	// the components section (or EOF).
	end := lines
	if c := get(root, "components"); c != nil {
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == "components" && root.Content[i].Line > 0 {
				if len(sp.Ops) > 0 && root.Content[i].Line > sp.Ops[len(sp.Ops)-1].Start {
					end = root.Content[i].Line - 1
				}
			}
		}
	}
	for i := range sp.Ops {
		if i+1 < len(sp.Ops) {
			sp.Ops[i].End = sp.Ops[i+1].Start - 1
		} else {
			sp.Ops[i].End = end
		}
	}
	return sp, nil
}
