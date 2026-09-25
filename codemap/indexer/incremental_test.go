package indexer

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// copyTree copies testdata/dir to a temp dir and lists its files.
func copyTree(t *testing.T, dir string) (string, []string) {
	t.Helper()
	src, _ := filepath.Abs(filepath.Join("testdata", dir))
	dst := t.TempDir()
	var files []string
	filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		b, _ := os.ReadFile(p)
		os.MkdirAll(filepath.Dir(filepath.Join(dst, rel)), 0o755)
		os.WriteFile(filepath.Join(dst, rel), b, 0o644)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return dst, files
}

// canon renders a graph's nodes, edges and files in a fixed order.
func canon(g *Graph) string {
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].Key < g.Nodes[j].Key })
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From != g.Edges[j].From {
			return g.Edges[i].From < g.Edges[j].From
		}
		return g.Edges[i].To < g.Edges[j].To
	})
	sort.Slice(g.Files, func(i, j int) bool { return g.Files[i].Path < g.Files[j].Path })
	b, _ := json.Marshal([]any{g.Nodes, g.Edges, g.Files, g.Warnings})
	return string(b)
}

func extractWith(root string, files []string, pc *parseCache, extract func(repo, root string, tracked []string, g *Graph)) string {
	if pc != nil {
		parseCaches.Store(root, pc)
		defer parseCaches.Delete(root)
	}
	g := &Graph{}
	extract("ws", root, files, g)
	return canon(g)
}

// Extraction with a parse cache, cold and after some files changed, builds
// the same graph as a full extraction.
func TestIncrementalParseMatchesFull(t *testing.T) {
	for _, c := range []struct {
		dir, comment string
		extract      func(repo, root string, tracked []string, g *Graph)
		edit         string // file that gets a new function
		fn           string
	}{
		{"java", "//", extractJava, "src/main/java/com/acme/orders/Util.java", ""},
		{"py", "#", extractPy, "src/shop/util.py", "\n\ndef shout(s):\n    return clean(s).upper()\n"},
		{"cs", "//", extractCS, "src/Acme.Orders/Util.cs", ""},
		{"rs", "//", extractRust, "orders/src/util.rs", "\npub fn shout(s: &str) -> String {\n    clean(s).to_uppercase()\n}\n"},
	} {
		t.Run(c.dir, func(t *testing.T) {
			root, files := copyTree(t, c.dir)
			full := extractWith(root, files, nil, c.extract)
			pc := &parseCache{dir: t.TempDir()}
			if got := extractWith(root, files, pc, c.extract); got != full {
				t.Fatalf("cold cache graph differs from a full extraction")
			}
			if pc.reused.Load() != 0 || pc.seen.Load() == 0 {
				t.Fatalf("cold run reused %d of %d", pc.reused.Load(), pc.seen.Load())
			}
			// Change two files: one comment-only, one with new code.
			changed := 0
			for _, f := range files {
				if !strings.HasSuffix(f, "."+c.dir) && !(c.dir == "rs" && strings.HasSuffix(f, ".rs")) || strings.Contains(f, "test") {
					continue
				}
				p := filepath.Join(root, f)
				b, _ := os.ReadFile(p)
				switch {
				case f == c.edit && c.fn != "":
					b = append(b, c.fn...)
				case changed == 0:
					b = append(b, "\n"+c.comment+" touched\n"...)
				default:
					continue
				}
				os.WriteFile(p, b, 0o644)
				changed++
			}
			full = extractWith(root, files, nil, c.extract)
			pc2 := &parseCache{dir: pc.dir}
			if got := extractWith(root, files, pc2, c.extract); got != full {
				t.Fatalf("incremental graph differs from a full extraction")
			}
			if seen, reused := pc2.seen.Load(), pc2.reused.Load(); reused != seen-int64(changed) {
				t.Fatalf("reused %d of %d with %d changed", reused, seen, changed)
			}
		})
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@x", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@x")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func commit(t *testing.T, dir, file, msg string, at time.Time) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, file), []byte(msg), 0o644)
	git(t, dir, "add", file)
	d := at.Format(time.RFC3339)
	cmd := exec.Command("git", "-C", dir, "commit", "-qm", msg)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@x", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@x",
		"GIT_AUTHOR_DATE="+d, "GIT_COMMITTER_DATE="+d)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v %s", err, out)
	}
}

// Reading only the commits since the last extraction gives the same history
// as reading it all, and a rewritten history is read in full.
func TestIncrementalHistory(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	base := time.Now().AddDate(0, 0, -historyDays-10) // the first commit falls out of the window
	commit(t, dir, "a.go", "one", base)
	commit(t, dir, "b.go", "fix: two", base.AddDate(0, 0, 20))
	commit(t, dir, "a.go", "three", base.AddDate(0, 0, 400))
	prev := &Graph{}
	loadHistory(dir, prev, nil)
	head, _ := gitOut(dir, "rev-parse", "HEAD")
	prev.Commit = strings.TrimSpace(head)

	commit(t, dir, "c.go", "four", base.AddDate(0, 0, 745))
	commit(t, dir, "a.go", "five", base.AddDate(0, 0, 750))
	inc, full := &Graph{}, &Graph{}
	loadHistory(dir, inc, prev)
	loadHistory(dir, full, nil)
	if len(full.History) != 4 || !reflect.DeepEqual(inc.History, full.History) || !inc.HeadTime.Equal(full.HeadTime) {
		t.Fatalf("incremental %d commits, full %d:\n%+v\n%+v", len(inc.History), len(full.History), inc.History, full.History)
	}

	// Rewrite: prev's HEAD is no longer an ancestor.
	git(t, dir, "reset", "-q", "--hard", "HEAD~3")
	commit(t, dir, "d.go", "six", base.AddDate(0, 0, 760))
	inc, full = &Graph{}, &Graph{}
	loadHistory(dir, inc, prev)
	loadHistory(dir, full, nil)
	if !reflect.DeepEqual(inc.History, full.History) {
		t.Fatalf("after a reset: incremental %+v, full %+v", inc.History, full.History)
	}
}
