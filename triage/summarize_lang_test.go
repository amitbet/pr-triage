package triage

import (
	"strings"
	"testing"
)

func TestSummarizerLanguage(t *testing.T) {
	for _, tc := range []struct {
		lang string
		want bool
	}{{"", false}, {"English", false}, {"Hebrew", true}} {
		got := (&Summarizer{Language: tc.lang}).system(summarizeSystem)
		if has := strings.Contains(got, "in "+tc.lang+"."); has != tc.want {
			t.Errorf("Language %q: language instruction present = %v, want %v", tc.lang, has, tc.want)
		}
	}
}
