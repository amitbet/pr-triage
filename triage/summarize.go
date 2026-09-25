package triage

import (
	"context"
	"fmt"
	"strings"

	"github.com/amitbet/pr-triage/llm"
)

const summarizeSystem = `You write short review summaries for pull-request changes that a triage step judged low-risk.

For the change unit, write:
- headline: one line, at most 12 words, saying what changed (e.g. "Retry helper now caps attempts at 5"). No "This change...".
- summary: 1-3 sentences on what changed and why it is safe to skip a line-by-line review.
You are also a second opinion. If while reading you find anything that can alter runtime behavior in a way a reviewer must judge (logic, error handling, concurrency, API, security, data), set safe=false and say what.
` + issuesInstructions

var summaryTool = llm.ToolDefinition{
	Name:        "submit_summary",
	Description: "Submit the summary for this change unit.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"headline":        map[string]any{"type": "string", "description": "At most 12 words."},
			"summary":         map[string]any{"type": "string"},
			"safe":            map[string]any{"type": "boolean"},
			"escalate_reason": map[string]any{"type": "string", "description": "Required when safe=false."},
			"issues":          issuesSchema,
		},
		"required": []string{"headline", "summary", "safe", "issues"},
	},
}

const reviewNotesSystem = `You write review notes for pull-request changes that need a human reviewer.

For the change unit, write:
- headline: one line, at most 12 words, saying what changed. No "This change...".
- summary: 1-3 sentences on what changed and why it matters at runtime.
- focus: 1-4 short, specific things the reviewer should verify (e.g. "callers that relied on Framework being set before merge"). No generic advice like "check tests".
` + issuesInstructions

// issuesInstructions makes both prompts a code review, not just a summary.
// Every issue must carry its proof; decodeIssues enforces the caps in Go.
const issuesInstructions = `
Then review the unit for defects. Report a defect only when you can show it goes wrong:
- evidence: quote the line(s) that cause it, from the diff or from code you read.
- failure_scenario: the concrete input or state and the wrong result (crash, wrong value, leak, lost data). "May", "could" or "if X returns Y" when you have not seen that X returns Y is not a scenario.
- introduced_by_pr: false when the behavior already existed before this PR: code moved from removed lines in another unit, or logic the base already had. Such problems are capped at low.
- depends_on_unseen_code: true when the defect exists only if code you have not seen (a library, a caller, a file not shown) behaves a certain way. These become "check" items for the reviewer, not issues.
Look for:
- bugs, wrong conditions, off-by-one, nil/empty handling, swallowed or changed errors
- concurrency, resource leaks, retries/timeouts
- security problems
- breaking a contract other code relies on: API/wire/JSON shape, DB schema or queries, persisted formats, CRDs. If a code map line lists dependent repos, check the change is compatible with them.
Judge impact with the other changes of the PR shown after the diff: a fallback, caller or test elsewhere may limit the problem, or show that it is intended.
Severity: critical = outage, data loss or security hole; high = wrong behavior in production that you can show happening; medium = real risk worth a reviewer's time; low = minor problem with a concrete consequence.
Give the new-file line when there is one. No style, naming or "add tests" remarks. Most correct changes have no defects: an empty list is the right answer then, so don't look for something to report in every hunk.`

// toolsInstructions is added when the reviewer can read the repository.
const toolsInstructions = `
You can read the repository at the PR head: your working directory is its root, and you may read files and search the code. %s
Before reporting a defect that depends on code outside the diff (a library's types or errors, a caller, a config), read that code. Set depends_on_unseen_code only when you could not find it. Keep reading to what the review needs.`

var issuesSchema = map[string]any{
	"type":        "array",
	"description": "Defects found in the unit. Empty when the change looks correct.",
	"items": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"severity":               map[string]any{"type": "string", "enum": []string{"low", "medium", "high", "critical"}},
			"line":                   map[string]any{"type": "integer", "description": "New-file line number, if the issue is on one."},
			"title":                  map[string]any{"type": "string", "description": "At most 12 words."},
			"detail":                 map[string]any{"type": "string", "description": "1-2 sentences: what goes wrong and when."},
			"evidence":               map[string]any{"type": "string", "description": "The code line(s) that cause it, quoted."},
			"failure_scenario":       map[string]any{"type": "string", "description": "Concrete input or state -> wrong result."},
			"introduced_by_pr":       map[string]any{"type": "boolean", "description": "False if the behavior existed before this PR (moved or unchanged logic)."},
			"depends_on_unseen_code": map[string]any{"type": "boolean", "description": "True if it is only a defect when code you have not seen behaves a certain way."},
		},
		"required": []string{"severity", "title", "evidence", "failure_scenario", "introduced_by_pr", "depends_on_unseen_code"},
	},
}

var reviewNotesTool = llm.ToolDefinition{
	Name:        "submit_review_notes",
	Description: "Submit review notes for this change unit.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"headline": map[string]any{"type": "string", "description": "At most 12 words."},
			"summary":  map[string]any{"type": "string"},
			"focus":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "1-4 specific things to verify."},
			"issues":   issuesSchema,
		},
		"required": []string{"headline", "summary", "focus", "issues"},
	},
}

type Summarizer struct {
	LLM    llm.LLMTool
	Policy Policy
	// Tools lets providers that support it (the CLIs) read the repository at
	// the PR head while reviewing. Pipeline.Run sets up the workspace.
	Tools bool
	// Language, if set (e.g. "Hebrew"), is the language of the text the
	// reader sees: headline, summary, focus and issue text. Empty is English.
	Language string
	// workspace is what the reviewer may read; nil without Tools.
	workspace *llm.Workspace
}

// prompt is the review prompt: the unit's diff, the rest of the PR, the
// code-map context, and what triage decided.
func (s *Summarizer) prompt(u *Unit, triage string) string {
	p, _ := unitDiff(u, s.Policy.MaxUnitChars)
	return p + u.ReviewContext + reviewContext(u) + "\n" + triage
}

// languageInstructions is added when the reader wants another language.
const languageInstructions = `
Write headline, summary, focus, escalate_reason and each issue's title, detail and failure_scenario in %s. Keep code identifiers, file paths, flags and error strings as they are in the code, and quote evidence verbatim. Enum values (severity) stay in English.`

// system adds the language and tools instructions.
func (s *Summarizer) system(base string) string {
	if s.Language != "" && !strings.EqualFold(s.Language, "english") {
		base += fmt.Sprintf(languageInstructions, s.Language)
	}
	ws := s.workspace
	if ws == nil {
		return base
	}
	extra := ""
	if len(ws.ReadDirs) > 0 {
		extra = "Library sources (Go module cache) are under " + strings.Join(ws.ReadDirs, ", ") + "; check go.mod for the versions."
	}
	return base + fmt.Sprintf(toolsInstructions, extra)
}

// Summarize fills u.Summary. For summary-bucket units it is also a second
// opinion: it escalates to human when the model disagrees with the triage
// or the call fails. Human units get review notes instead.
func (s *Summarizer) Summarize(ctx context.Context, u *Unit) {
	if u.Decision.Bucket == BucketHuman {
		s.reviewNotes(ctx, u)
		return
	}
	prompt := s.prompt(u, "Triage said: "+string(u.Decision.Bucket)+" ("+u.Decision.Reason+")")
	args, _, err := llm.CallToolIn(ctx, s.LLM, s.workspace, []llm.ChatMessage{
		{Role: "system", Content: s.system(summarizeSystem)},
		{Role: "user", Content: prompt},
	}, summaryTool, reviewMaxTokens)
	if err != nil {
		u.Decision.escalate(BucketHuman, "summary failed: "+err.Error())
		return
	}
	u.Summary, _ = args["summary"].(string)
	u.Headline, _ = args["headline"].(string)
	s.setIssues(u, args)
	safe, ok := args["safe"].(bool)
	if !ok || !safe {
		why, _ := args["escalate_reason"].(string)
		u.Decision.escalate(BucketHuman, "summarizer: "+strings.TrimSpace(why))
	}
}

// reviewNotes writes a summary and a "what to check" list for a human
// unit. Failures leave the unit without notes; it is already human.
func (s *Summarizer) reviewNotes(ctx context.Context, u *Unit) {
	prompt := s.prompt(u, "Triage: human review ("+u.Decision.Reason+")")
	args, _, err := llm.CallToolIn(ctx, s.LLM, s.workspace, []llm.ChatMessage{
		{Role: "system", Content: s.system(reviewNotesSystem)},
		{Role: "user", Content: prompt},
	}, reviewNotesTool, reviewMaxTokens)
	if err != nil {
		return
	}
	u.Summary, _ = args["summary"].(string)
	u.Headline, _ = args["headline"].(string)
	if fs, ok := args["focus"].([]any); ok {
		for _, f := range fs {
			if str, ok := f.(string); ok && strings.TrimSpace(str) != "" {
				u.Focus = append(u.Focus, strings.TrimSpace(str))
			}
		}
	}
	s.setIssues(u, args)
}

// setIssues decodes issues. Claims that rest on code the reviewer did not
// see become things to check. A reply without an issues field was not a
// review, so it cannot count as "nothing found".
func (s *Summarizer) setIssues(u *Unit, args map[string]any) {
	v, ok := args["issues"]
	var checks []string
	u.Issues, checks = decodeIssues(v)
	u.Reviewed = ok
	for _, c := range checks {
		u.Focus = append(u.Focus, "unverified: "+c)
	}
}
