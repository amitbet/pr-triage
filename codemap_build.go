package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/amitbet/pr-triage/codemap"
	"github.com/amitbet/pr-triage/triage"
)

// codeMapBuildMu serializes code-map builds: a build re-ranks the whole
// workspace and writes the map, so two at once would clobber it.
var codeMapBuildMu sync.Mutex

// The indexer reads repos from a workspace, <ws>/code/<repo>: PR_TRIAGE_WORKSPACE
// if set, else <cache>/workspace. Repos get there three ways:
//   - a local code directory (-code-root, PR_TRIAGE_CODE_ROOT): its git
//     checkouts are symlinked in, so the map is built from the code on disk;
//   - an org (-org, PR_TRIAGE_ORG): every non-archived, non-fork repo of a
//     GitHub or GitHub Enterprise org or user is cloned (blobless);
//   - on demand: a PR's repo that isn't in the map yet is linked from the
//     code directory, or cloned.
//
// Cross-repo impact (which repos depend on the changed code) only sees the
// repos in the workspace, so indexing the whole org matters.

func workspaceDir(o options) (string, error) {
	ws := os.Getenv("PR_TRIAGE_WORKSPACE")
	if ws == "" {
		ws = filepath.Join(o.cache, "workspace")
	}
	ws, err := filepath.Abs(ws)
	if err != nil {
		return "", err
	}
	return ws, os.MkdirAll(filepath.Join(ws, "code"), 0o755)
}

// ensureCodeMap makes sure the PR's repo is in the code map before triage
// runs, so its impact scores, history and treemap exist.
func ensureCodeMap(ctx context.Context, o options, ref triage.PRRef, progress func(stage string, done, total int)) error {
	if o.codemapDir == "" || o.codemapDir == "off" || codeMapHas(loadCodeMap(o.codemapDir), ref.Repo) {
		return nil
	}
	codeMapBuildMu.Lock()
	defer codeMapBuildMu.Unlock()
	if codeMapHas(loadCodeMap(o.codemapDir), ref.Repo) {
		return nil // another job built it while this one waited
	}
	ws, err := workspaceDir(o)
	if err != nil {
		return err
	}
	var local map[string]string
	if o.codeRoot != "" {
		if local, err = scanCheckouts(o.codeRoot); err != nil {
			return err
		}
	}
	progress("clone", 0, 0)
	if err := addRepo(ctx, ws, ref.Host, ref.Owner, ref.Repo, local); err != nil {
		return err
	}
	progress("codemap", 0, 0)
	if err := buildCodeMap(ctx, o, ws, ref.Repo); err != nil {
		return err
	}
	if !codeMapHas(reloadCodeMap(o.codemapDir), ref.Repo) {
		return fmt.Errorf("code map: built the map but %s is still not in %s", ref.Repo, o.codemapDir)
	}
	return nil
}

// indexResult is what an explicit index run added.
type indexResult struct {
	Linked  []string `json:"linked"`  // from the local code directory
	Cloned  []string `json:"cloned"`  // cloned now
	Updated []string `json:"updated"` // clones that were already there, pulled
	Failed  []string `json:"failed,omitempty"`
	Repos   int      `json:"repos"` // repos in the map afterwards
}

// indexSources puts every repo of the code directory and the org into the
// workspace and rebuilds the map. Only repos that changed are re-extracted.
func indexSources(ctx context.Context, o options, progress func(stage string, done, total int)) (*indexResult, error) {
	if o.codemapDir == "" || o.codemapDir == "off" {
		return nil, errors.New("the code map is off (-codemap off)")
	}
	if o.codeRoot == "" && o.org == "" {
		return nil, errors.New("nothing to index: set a code directory (-code-root, PR_TRIAGE_CODE_ROOT) or an org (-org, PR_TRIAGE_ORG)")
	}
	codeMapBuildMu.Lock()
	defer codeMapBuildMu.Unlock()
	ws, err := workspaceDir(o)
	if err != nil {
		return nil, err
	}
	res := &indexResult{}
	var local map[string]string
	if o.codeRoot != "" {
		if local, err = scanCheckouts(o.codeRoot); err != nil {
			return nil, err
		}
		if len(local) == 0 && o.org == "" {
			return nil, fmt.Errorf("no git checkouts in %s or its subdirectories", o.codeRoot)
		}
	}
	// With an org, the code directory only supplies its repos; without
	// one, every checkout in it is indexed.
	var names []string
	var host, owner string
	if o.org != "" {
		if host, owner, err = parseOrg(o.org); err != nil {
			return nil, err
		}
		progress("list", 0, 0)
		if names, err = orgRepos(ctx, host, owner); err != nil {
			return nil, err
		}
	} else {
		for name := range local {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	for i, name := range names {
		progress("clone", i, len(names))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		dir := filepath.Join(ws, "code", name)
		_, isLocal := local[name]
		had := fileExists(filepath.Join(dir, ".git"))
		if err := addRepo(ctx, ws, host, owner, name, local); err != nil {
			log.Printf("index: %s: %v", name, err)
			res.Failed = append(res.Failed, name+": "+firstLine(err.Error()))
			continue
		}
		switch {
		case isLocal:
			res.Linked = append(res.Linked, name)
		case had:
			if _, err := triage.Git(dir, "pull", "--ff-only", "--quiet"); err != nil {
				log.Printf("index: pulling %s: %v", name, err) // index what is there
			}
			res.Updated = append(res.Updated, name)
		default:
			res.Cloned = append(res.Cloned, name)
		}
	}
	progress("codemap", 0, 0)
	if err := buildCodeMap(ctx, o, ws, ""); err != nil {
		return nil, err
	}
	if m := reloadCodeMap(o.codemapDir); m != nil {
		res.Repos = len(m.Meta.Repos)
	}
	return res, nil
}

// addRepo puts one repo in the workspace: a link to its checkout in the
// code directory, else a blobless clone. A repo already there is kept.
func addRepo(ctx context.Context, ws, host, owner, repo string, local map[string]string) error {
	dir := filepath.Join(ws, "code", repo)
	if src, ok := local[repo]; ok {
		if cur, err := os.Readlink(dir); err == nil && cur == src {
			return nil
		}
		if fileExists(filepath.Join(dir, ".git")) {
			if _, err := os.Readlink(dir); err != nil {
				return nil // a clone from before the code directory was set
			}
		}
		_ = os.Remove(dir) // a stale link
		return os.Symlink(src, dir)
	}
	if fileExists(filepath.Join(dir, ".git")) {
		return nil
	}
	if owner == "" {
		return fmt.Errorf("%s is not in the code directory and there is no org to clone it from", repo)
	}
	ref := triage.PRRef{Host: host, Owner: owner, Repo: repo}
	clone := exec.CommandContext(ctx, "gh", "repo", "clone", ref.RepoArg(), dir, "--", "--filter=blob:none", "--quiet")
	if out, err := clone.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		if gerr := triage.GHError(err, out); gerr != nil {
			return fmt.Errorf("code map: cloning %s: %w", ref.RepoArg(), gerr)
		}
		return fmt.Errorf("code map: cloning %s into %s: %v\n%s", ref.RepoArg(), dir, err, tail(out))
	}
	return nil
}

// buildCodeMap runs the indexer bundled in this executable over the
// workspace. repo forces that repo to be re-extracted; the rest reuse
// their cached graphs unless they changed.
func buildCodeMap(ctx context.Context, o options, ws, repo string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"codemap", "build", "-workspace", ws, "-output", o.codemapDir, "-cache", filepath.Join(o.cache, "graphs")}
	if repo != "" {
		args = append(args, "-repo", repo)
	}
	if o.codemapConfig != "" {
		args = append(args, "-config", o.codemapConfig)
	}
	out, err := exec.CommandContext(ctx, exe, args...).CombinedOutput()
	if err != nil {
		what := "the workspace"
		if repo != "" {
			what = repo
		}
		return fmt.Errorf("code map: building %s: %v\n%s", what, err, tail(out))
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "warn:") {
			log.Print(line)
		}
	}
	return nil
}

// scanCheckouts finds the git checkouts in root and one level below it
// (~/code/<repo>, ~/code/<org>/<repo>, a workspace's code/),
// keyed by repo name: the last part of the origin remote, so a checkout in
// a renamed directory still matches the PR's repo, else the directory name.
func scanCheckouts(root string) (map[string]string, error) {
	root, err := filepath.Abs(expandHome(root))
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("code directory %s: not a directory", root)
	}
	out := map[string]string{}
	add := func(dir string) {
		name := filepath.Base(dir)
		if url, err := triage.Git(dir, "remote", "get-url", "origin"); err == nil {
			if n := repoFromRemote(url); n != "" {
				name = n
			}
		}
		if _, dup := out[name]; !dup {
			out[name] = dir
		}
	}
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				continue
			}
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err != nil || !st.IsDir() {
				continue
			}
			if fileExists(filepath.Join(p, ".git")) {
				if real, err := filepath.EvalSymlinks(p); err == nil {
					add(real)
				}
			} else if depth < 2 {
				walk(p, depth+1)
			}
		}
	}
	if fileExists(filepath.Join(root, ".git")) {
		add(root) // the directory is itself one checkout
	} else {
		walk(root, 1)
	}
	return out, nil
}

// repoFromRemote: git@host:org/name.git, https://host/org/name -> name.
func repoFromRemote(url string) string {
	url = strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(url), "/"), ".git")
	if i := strings.LastIndexAny(url, "/:"); i >= 0 {
		url = url[i+1:]
	}
	return url
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// parseOrg reads an org or user: name, host/name, or its URL
// (https://github.com/acme, https://ghe.corp.com/platform).
func parseOrg(s string) (host, owner string, err error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		return triage.DefaultHost(), parts[0], nil
	case len(parts) == 2 && strings.Contains(parts[0], "."):
		return triage.NormalizeHost(parts[0]), parts[1], nil
	}
	return "", "", fmt.Errorf("org %q: want name, host/name or https://host/name", s)
}

// orgRepos lists an org's (or user's) repos with gh, leaving out archived
// repos and forks.
func orgRepos(ctx context.Context, host, owner string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "gh", "repo", "list", owner, "--no-archived", "--source", "--limit", "2000", "--json", "name")
	cmd.Env = append(os.Environ(), "GH_HOST="+orDefault(host, "github.com")) // gh repo list has no --hostname
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = ee.Stderr
		}
		if gerr := triage.GHError(err, stderr); gerr != nil {
			return nil, gerr
		}
		return nil, fmt.Errorf("listing repos of %s: %v %s", path.Join(orDefault(host, "github.com"), owner), err, strings.TrimSpace(string(stderr)))
	}
	var repos []struct{ Name string }
	if err := json.Unmarshal(out, &repos); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(repos))
	for _, r := range repos {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("%s has no repos you can see (archived repos and forks are left out)", owner)
	}
	return names, nil
}

func codeMapHas(m *codemap.Map, repo string) bool {
	if m == nil {
		return false
	}
	_, ok := m.Meta.Repos[repo]
	return ok
}

// tail keeps the last lines of a command's output for an error message.
func tail(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 15 {
		lines = lines[len(lines)-15:]
	}
	return strings.Join(lines, "\n")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
