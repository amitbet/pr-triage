package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/amitbet/pr-manager/llm"
)

// UnitText is the text of a unit the reader sees: what Translate
// translates. Code, evidence and severities are not part of it.
type UnitText struct {
	Headline string      `json:"headline,omitempty"`
	Summary  string      `json:"summary,omitempty"`
	Focus    []string    `json:"focus,omitempty"`
	Issues   []IssueText `json:"issues,omitempty"`
}

type IssueText struct {
	Title    string `json:"title,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Scenario string `json:"failure_scenario,omitempty"`
}

// TextOf is u's reader-facing text. The headline falls back to the
// classifier's, as the UI does.
func TextOf(u *Unit) UnitText {
	t := UnitText{Headline: u.Headline, Summary: u.Summary, Focus: append([]string(nil), u.Focus...)}
	if t.Headline == "" {
		t.Headline = u.Decision.Headline
	}
	for _, is := range u.Issues {
		t.Issues = append(t.Issues, IssueText{Title: is.Title, Detail: is.Detail, Scenario: is.Scenario})
	}
	return t
}

// ApplyText puts translated text on u. Empty fields and focus or issue
// lists of another length keep what u has.
func ApplyText(u *Unit, t UnitText) {
	if t.Headline != "" {
		u.Headline = t.Headline
	}
	if t.Summary != "" {
		u.Summary = t.Summary
	}
	if len(t.Focus) == len(u.Focus) {
		for i, f := range t.Focus {
			if f != "" {
				u.Focus[i] = f
			}
		}
	}
	if len(t.Issues) == len(u.Issues) {
		for i, is := range t.Issues {
			set(&u.Issues[i].Title, is.Title)
			set(&u.Issues[i].Detail, is.Detail)
			set(&u.Issues[i].Scenario, is.Scenario)
		}
	}
}

func set(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

// IsEnglish reports whether lang needs no translation.
func IsEnglish(lang string) bool {
	lang = strings.TrimSpace(lang)
	return lang == "" || strings.EqualFold(lang, "english")
}

const translateSystem = `You translate the notes of a code review into %s.

The user message is a JSON array of {"id", "text"} items. Translate each text and return every item with the same id. Keep code identifiers, file paths, flags, error strings, anything in backticks or quotes from the code, and severity words (low, medium, high, critical) as they are. Keep a leading "unverified:" or "summarizer:" label as it is. Translate the meaning, in the plain technical register a reviewer would write; don't add or drop anything.`

var translateTool = llm.ToolDefinition{
	Name:        "submit_translation",
	Description: "Submit the translated items.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":   map[string]any{"type": "integer"},
						"text": map[string]any{"type": "string"},
					},
					"required": []string{"id", "text"},
				},
			},
		},
		"required": []string{"items"},
	},
}

const (
	translateBatchChars  = 6000
	translateConcurrency = 4
	translateMaxTokens   = 16384
)

type translateItem struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

// Translate translates texts (keyed by unit ID) into lang, in batches.
// English returns texts as they are, without a call. Any failed batch
// fails the whole translation, so a half-translated PR is never cached.
func Translate(ctx context.Context, l llm.LLMTool, lang string, texts map[string]UnitText) (map[string]UnitText, error) {
	if IsEnglish(lang) {
		return texts, nil
	}
	// Copies of the texts, and pointers to each string to translate.
	copies := make(map[string]*UnitText, len(texts))
	var slots []*string
	for id, t := range texts {
		t.Focus = append([]string(nil), t.Focus...)
		t.Issues = append([]IssueText(nil), t.Issues...)
		copies[id] = &t
		for _, s := range textSlots(&t) {
			if strings.TrimSpace(*s) != "" {
				slots = append(slots, s)
			}
		}
	}
	if len(slots) == 0 {
		return texts, nil
	}

	var batches [][]translateItem
	var cur []translateItem
	size := 0
	for i, s := range slots {
		if size > 0 && size+len(*s) > translateBatchChars {
			batches = append(batches, cur)
			cur, size = nil, 0
		}
		cur = append(cur, translateItem{ID: i, Text: *s})
		size += len(*s)
	}
	batches = append(batches, cur)

	results := make([]map[int]string, len(batches))
	errs := make([]error, len(batches))
	sem := make(chan struct{}, translateConcurrency)
	var wg sync.WaitGroup
	for b, items := range batches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[b], errs[b] = translateBatch(ctx, l, lang, items)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	for _, r := range results {
		for i, text := range r {
			*slots[i] = text
		}
	}
	out := make(map[string]UnitText, len(copies))
	for id, t := range copies {
		out[id] = *t
	}
	return out, nil
}

// textSlots points at every string of t.
func textSlots(t *UnitText) []*string {
	s := []*string{&t.Headline, &t.Summary}
	for i := range t.Focus {
		s = append(s, &t.Focus[i])
	}
	for i := range t.Issues {
		s = append(s, &t.Issues[i].Title, &t.Issues[i].Detail, &t.Issues[i].Scenario)
	}
	return s
}

func translateBatch(ctx context.Context, l llm.LLMTool, lang string, items []translateItem) (map[int]string, error) {
	in, _ := json.Marshal(items)
	args, _, err := llm.CallTool(ctx, l, []llm.ChatMessage{
		{Role: "system", Content: fmt.Sprintf(translateSystem, lang)},
		{Role: "user", Content: string(in)},
	}, translateTool, translateMaxTokens)
	if err != nil {
		return nil, fmt.Errorf("translate: %w", err)
	}
	want := make(map[int]bool, len(items))
	for _, it := range items {
		want[it.ID] = true
	}
	got := map[int]string{}
	list, _ := args["items"].([]any)
	for _, v := range list {
		m, _ := v.(map[string]any)
		id, ok := m["id"].(float64)
		text, _ := m["text"].(string)
		if ok && want[int(id)] && strings.TrimSpace(text) != "" {
			got[int(id)] = text
		}
	}
	if len(got) < len(items) {
		return nil, fmt.Errorf("translate: %d of %d texts came back", len(got), len(items))
	}
	return got, nil
}
