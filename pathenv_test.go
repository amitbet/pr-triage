package main

import (
	"runtime"
	"testing"
)

func TestMergePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses : separated lists")
	}
	got := mergePath("/usr/bin:/bin", "/opt/homebrew/bin::/usr/bin", "", "/usr/local/bin:/bin")
	want := "/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin"
	if got != want {
		t.Fatalf("mergePath = %q, want %q", got, want)
	}
}
