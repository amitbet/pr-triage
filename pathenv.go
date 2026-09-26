package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// inheritShellPath widens PATH for a GUI launch. Apps started from Finder or
// the Dock get launchd's minimal PATH (/usr/bin:/bin:/usr/sbin:/sbin), so gh,
// git, claude and codex installed via Homebrew or npm are not found. Ask the
// user's login shell for its PATH and fall back to the usual install dirs.
func inheritShellPath() {
	if runtime.GOOS == "windows" {
		return
	}
	home, _ := os.UserHomeDir()
	fallback := []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin"}
	if home != "" {
		fallback = append(fallback, filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"))
	}
	os.Setenv("PATH", mergePath(os.Getenv("PATH"), loginShellPath(), strings.Join(fallback, string(os.PathListSeparator))))
}

const pathMarker = "__PR_MANAGER_PATH__"

// loginShellPath returns the PATH an interactive login shell sets up, or ""
// if the shell fails or takes too long.
func loginShellPath() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Interactive so .zshrc/.bashrc run too; the markers skip anything the rc
	// files print.
	out, err := exec.CommandContext(ctx, shell, "-ilc", `printf '%s%s%s' "`+pathMarker+`" "$PATH" "`+pathMarker+`"`).Output()
	if err != nil {
		return ""
	}
	parts := strings.Split(string(out), pathMarker)
	if len(parts) < 3 {
		return ""
	}
	return parts[len(parts)-2]
}

// mergePath joins PATH lists in order, dropping empty and repeated entries.
func mergePath(lists ...string) string {
	seen := map[string]bool{}
	var dirs []string
	for _, l := range lists {
		for _, d := range filepath.SplitList(l) {
			if d != "" && !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}
