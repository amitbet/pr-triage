package triage

import (
	"bytes"
	"regexp"
	"strings"
)

// Presorter applies deterministic rules. Units it decides never reach an LLM.
type Presorter struct {
	Policy Policy
	// GitattributesGenerated are linguist-generated patterns.
	GitattributesGenerated []string
}

// Presort sets Decision on units a rule can decide and returns the rest.
func (p *Presorter) Presort(units []*Unit, src *Source) (rest []*Unit) {
	binary := map[string]bool{}
	for _, f := range src.Files {
		binary[f.Path] = f.Binary
	}
	for _, u := range units {
		if d, ok := p.rule(u, binary[u.File], src); ok {
			d.Source = "rule"
			u.Decision = d
			continue
		}
		rest = append(rest, u)
	}
	return rest
}

func (p *Presorter) rule(u *Unit, binary bool, src *Source) (Decision, bool) {
	// Forced-human wins over everything, including "generated".
	if pat, ok := MatchAny(p.Policy.ForceHuman, u.File); ok {
		return Decision{Bucket: BucketHuman, ChangeKind: "config", Reason: "path matches force_human policy " + pat}, true
	}
	if binary {
		return Decision{Bucket: BucketHuman, ChangeKind: "binary", Reason: "binary file"}, true
	}
	if pat, ok := MatchAny(p.Policy.Generated, u.File); ok {
		return Decision{Bucket: BucketNone, ChangeKind: "generated", Reason: "generated file (" + pat + ")"}, true
	}
	if pat, ok := MatchAny(p.GitattributesGenerated, u.File); ok {
		return Decision{Bucket: BucketNone, ChangeKind: "generated", Reason: "linguist-generated in .gitattributes (" + pat + ")"}, true
	}
	if u.Status != StatusDeleted && src.Content != nil && hasGeneratedHeader(src.Content, u.File) {
		return Decision{Bucket: BucketNone, ChangeKind: "generated", Reason: "file has a Code generated ... DO NOT EDIT header"}, true
	}
	if u.Status == StatusRenamed && len(u.Hunks) == 0 {
		return Decision{Bucket: BucketNone, ChangeKind: "rename", Reason: "pure rename, content identical"}, true
	}
	if src.RealChanges != nil && len(u.Hunks) > 0 && !src.RealChanges[u.File] {
		return Decision{Bucket: BucketNone, ChangeKind: "format", Reason: "whitespace/blank-line only (empty under git diff -w)"}, true
	}
	if strings.HasSuffix(u.File, ".go") {
		if d, ok := goBoilerplate(u); ok {
			return d, true
		}
	}
	if isDocs(u.File) {
		return Decision{Bucket: BucketSummary, ChangeKind: "docs", Reason: "documentation file", Confidence: 1}, true
	}
	return Decision{}, false
}

func hasGeneratedHeader(content ContentFunc, file string) bool {
	src, err := content(file)
	if err != nil {
		return false
	}
	head := src
	if len(head) > 2048 {
		head = head[:2048]
	}
	for _, line := range bytes.Split(head, []byte("\n")) {
		s := string(line)
		if strings.Contains(s, "Code generated") && strings.Contains(s, "DO NOT EDIT") {
			return true
		}
	}
	return false
}

func isDocs(file string) bool {
	lower := strings.ToLower(file)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".rst")
}

var (
	blankOrDotImport = regexp.MustCompile(`^[+-]\s*(import\s+)?[_.]\s+"`)
	importLine       = regexp.MustCompile(`^[+-]\s*(import\s*\(?|\)|(\w+\s+)?"[^"]*"|import\s+(\w+\s+)?"[^"]*")?\s*(//.*)?$`)
	packageLine      = regexp.MustCompile(`^[+-]\s*(package\s+\w+)?\s*(//.*)?$`)
)

// goBoilerplate decides units whose changed lines are only import specs or
// the package clause. Blank and dot imports run init code, so they go to
// the classifier.
func goBoilerplate(u *Unit) (Decision, bool) {
	var changed []string
	for _, h := range u.Hunks {
		for _, l := range h.Lines {
			if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
				changed = append(changed, l)
			}
		}
	}
	if len(changed) == 0 {
		return Decision{}, false
	}
	all := func(re *regexp.Regexp) bool {
		for _, l := range changed {
			if !re.MatchString(l) {
				return false
			}
		}
		return true
	}
	switch {
	case u.Symbol == "imports" && all(importLine):
		for _, l := range changed {
			if blankOrDotImport.MatchString(l) {
				return Decision{}, false
			}
		}
		return Decision{Bucket: BucketNone, ChangeKind: "format", Reason: "import list only (the compiler rejects unused imports; uses are in other units)"}, true
	case u.Symbol == "" && all(packageLine):
		return Decision{Bucket: BucketNone, ChangeKind: "format", Reason: "package clause / blank lines only"}, true
	}
	return Decision{}, false
}
