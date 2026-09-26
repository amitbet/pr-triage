package triage

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGHError(t *testing.T) {
	exit := errors.New("exit status 1")
	cases := []struct {
		name   string
		err    error
		output string
		want   string
	}{
		{"missing", &exec.Error{Name: "gh", Err: exec.ErrNotFound}, "", "not installed"},
		{"old json field", exit, "Unknown JSON field: \"baseRefOid\"\nAvailable fields:\n  additions", `too old`},
		{"old flag", exit, "unknown flag: --input\n\nUsage:  gh api", "unknown flag: --input"},
		{"logged out", exit, "To get started with GitHub CLI, please run:  gh auth login", "not logged in"},
		{"other", exit, "GraphQL: Could not resolve to a PullRequest", ""},
	}
	for _, c := range cases {
		got := GHError(c.err, []byte(c.output))
		if c.want == "" {
			if got != nil {
				t.Errorf("%s: got %v, want nil", c.name, got)
			}
			continue
		}
		if got == nil || !strings.Contains(got.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, got, c.want)
		}
		if got != nil && strings.Contains(got.Error(), "Available fields") {
			t.Errorf("%s: field list leaked into %v", c.name, got)
		}
	}
}

func TestParsePRRefHosts(t *testing.T) {
	t.Setenv("GH_HOST", "")
	cases := []struct {
		in   string
		want PRRef
		url  string
	}{
		{"https://github.com/acme/api/pull/12", PRRef{Owner: "acme", Repo: "api", Number: 12}, "https://github.com/acme/api/pull/12"},
		{"https://github.com/acme/api/pull/12/files", PRRef{Owner: "acme", Repo: "api", Number: 12}, ""},
		{"github.com/acme/api/pull/12", PRRef{Owner: "acme", Repo: "api", Number: 12}, ""},
		{"https://ghe.corp.example/platform/billing/pull/7", PRRef{Host: "ghe.corp.example", Owner: "platform", Repo: "billing", Number: 7}, "https://ghe.corp.example/platform/billing/pull/7"},
		{"https://GHE.Corp.Example:8443/platform/billing/pull/7", PRRef{Host: "ghe.corp.example:8443", Owner: "platform", Repo: "billing", Number: 7}, ""},
		{"https://octo.ghe.com/team/svc/pull/3", PRRef{Host: "octo.ghe.com", Owner: "team", Repo: "svc", Number: 3}, ""},
		{"acme/api#12", PRRef{Owner: "acme", Repo: "api", Number: 12}, ""},
		{"ghe.corp.example/platform/billing#7", PRRef{Host: "ghe.corp.example", Owner: "platform", Repo: "billing", Number: 7}, ""},
	}
	for _, c := range cases {
		got, err := ParsePRRef(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParsePRRef(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
		if c.url != "" && got.URL() != c.url {
			t.Errorf("%q: URL() = %s, want %s", c.in, got.URL(), c.url)
		}
	}
	if _, err := ParsePRRef("https://github.com/acme/api/issues/12"); err == nil {
		t.Error("an issue link parsed as a PR")
	}

	t.Setenv("GH_HOST", "ghe.corp.example")
	if got, _ := ParsePRRef("platform/billing#7"); got.Host != "ghe.corp.example" {
		t.Errorf("short ref with GH_HOST: host %q", got.Host)
	}
	if got, _ := ParsePRRef("https://github.com/acme/api/pull/1"); got.Host != "" || got.RepoArg() != "github.com/acme/api" {
		t.Errorf("github.com URL with GH_HOST set: %+v (%s)", got, got.RepoArg())
	}
}

func TestPRRefKeys(t *testing.T) {
	gh := PRRef{Owner: "acme", Repo: "api", Number: 5}
	ent := PRRef{Host: "ghe.corp.example", Owner: "acme", Repo: "api", Number: 5}
	if gh.FileKey() != "acme__api__5" {
		t.Errorf("github.com key %s: must stay what cached results use", gh.FileKey())
	}
	if ent.FileKey() != "ghe.corp.example__acme__api__5" || ent.RepoArg() != "ghe.corp.example/acme/api" {
		t.Errorf("enterprise ref: key %s, repo %s", ent.FileKey(), ent.RepoArg())
	}
	f := &PRFetcher{Dir: t.TempDir()}
	if f.RepoDir(gh) != filepath.Join(f.Dir, "acme", "api") || f.RepoDir(ent) != filepath.Join(f.Dir, "ghe.corp.example", "acme", "api") {
		t.Errorf("clone dirs %s, %s", f.RepoDir(gh), f.RepoDir(ent))
	}
}

func TestParseRepo(t *testing.T) {
	t.Setenv("GH_HOST", "")
	for in, want := range map[string][3]string{
		"acme/api":                         {"", "acme", "api"},
		"github.com/acme/api":              {"", "acme", "api"},
		"https://ghe.corp.example/p/b.git": {"ghe.corp.example", "p", "b"},
	} {
		h, o, r, err := ParseRepo(in)
		if err != nil || [3]string{h, o, r} != want {
			t.Errorf("ParseRepo(%q) = %s %s %s %v, want %v", in, h, o, r, err, want)
		}
	}
	if _, _, _, err := ParseRepo("api"); err == nil {
		t.Error("ParseRepo accepted a bare name")
	}
}
