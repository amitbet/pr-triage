package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir, remote string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
		if remote == "" && args[0] == "remote" {
			continue
		}
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func TestScanCheckouts(t *testing.T) {
	root := t.TempDir()
	gitInit(t, filepath.Join(root, "api"), "git@github.com:acme/api.git")
	gitInit(t, filepath.Join(root, "acme", "billing-checkout"), "https://ghe.corp.example/acme/billing")
	gitInit(t, filepath.Join(root, "scratch"), "")
	gitInit(t, filepath.Join(root, "a", "b", "too-deep"), "")
	gitInit(t, filepath.Join(root, "node_modules", "dep"), "")

	got, err := scanCheckouts(root)
	if err != nil {
		t.Fatal(err)
	}
	real := func(p string) string { r, _ := filepath.EvalSymlinks(p); return r }
	want := map[string]string{
		"api":     real(filepath.Join(root, "api")),
		"billing": real(filepath.Join(root, "acme", "billing-checkout")), // named by its remote
		"scratch": real(filepath.Join(root, "scratch")),
	}
	if len(got) != len(want) {
		t.Errorf("found %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s, want %s", k, got[k], v)
		}
	}
	if _, err := scanCheckouts(filepath.Join(root, "missing")); err == nil {
		t.Error("a missing directory scanned without an error")
	}
}

func TestAddRepoLinksLocalCheckout(t *testing.T) {
	root, ws := t.TempDir(), t.TempDir()
	gitInit(t, filepath.Join(root, "api"), "git@github.com:acme/api.git")
	if err := os.MkdirAll(filepath.Join(ws, "code"), 0o755); err != nil {
		t.Fatal(err)
	}
	local, _ := scanCheckouts(root)
	if err := addRepo(context.Background(), ws, "", "", "api", local); err != nil {
		t.Fatal(err)
	}
	if dst, err := os.Readlink(filepath.Join(ws, "code", "api")); err != nil || dst != local["api"] {
		t.Errorf("link %s %v, want %s", dst, err, local["api"])
	}
	if err := addRepo(context.Background(), ws, "", "", "api", local); err != nil {
		t.Errorf("linking again: %v", err)
	}
	if err := addRepo(context.Background(), ws, "", "", "other", local); err == nil {
		t.Error("a repo neither local nor in an org was added")
	}
}

func TestParseOrg(t *testing.T) {
	t.Setenv("GH_HOST", "")
	for in, want := range map[string][2]string{
		"acme":                              {"", "acme"},
		"https://github.com/acme/":          {"", "acme"},
		"ghe.corp.example/platform":         {"ghe.corp.example", "platform"},
		"https://ghe.corp.example/platform": {"ghe.corp.example", "platform"},
	} {
		h, o, err := parseOrg(in)
		if err != nil || [2]string{h, o} != want {
			t.Errorf("parseOrg(%q) = %q %q %v, want %v", in, h, o, err, want)
		}
	}
	for _, bad := range []string{"", "acme/api", "https://github.com/acme/api"} {
		if _, _, err := parseOrg(bad); err == nil {
			t.Errorf("parseOrg(%q) accepted", bad)
		}
	}
}
