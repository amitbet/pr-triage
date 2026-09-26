package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/amitbet/pr-manager/llm"
	"github.com/amitbet/pr-manager/triage"
)

type countingLLM struct{ calls int }

func (c *countingLLM) Call(_ context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
	c.calls++
	var in []struct {
		ID   int    `json:"id"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal([]byte(req.Messages[1].Content), &in)
	var out []any
	for _, it := range in {
		out = append(out, map[string]any{"id": float64(it.ID), "text": "he:" + it.Text})
	}
	return &llm.LLMResponse{ToolCalls: []llm.ToolCall{{Name: "submit_translation", Arguments: map[string]any{"items": out}}}}, nil
}
func (c *countingLLM) ModelID() string { return "fake" }
func (c *countingLLM) Name() string    { return "fake" }

func TestTranslationCache(t *testing.T) {
	fake := &countingLLM{}
	old := newTranslator
	newTranslator = func(options) (llm.LLMTool, error) { return fake, nil }
	defer func() { newTranslator = old }()

	tr, err := newTriager(options{cache: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	u := &triage.Unit{ID: "a.go", File: "a.go", Headline: "Adds retries", Summary: "Retries Fetch."}
	r := &PRResult{Key: "k1", Files: []resultFile{{Units: []resultUnit{{Unit: u}}}}}
	lang := func(s string) jobOptions { return jobOptions{SummaryLang: &s} }

	for _, l := range []string{"", "English", "english"} {
		got, err := tr.translation(context.Background(), r, lang(l))
		if err != nil || got.Lang != "" || len(got.Units) != 0 {
			t.Errorf("%q: %+v %v, want no translation", l, got, err)
		}
	}
	if fake.calls != 0 {
		t.Fatalf("English made %d calls", fake.calls)
	}

	for range 2 {
		got, err := tr.translation(context.Background(), r, lang("Hebrew"))
		if err != nil || got.Lang != "Hebrew" || got.Units["a.go"].Summary != "he:Retries Fetch." {
			t.Fatalf("Hebrew: %+v %v", got, err)
		}
	}
	if fake.calls != 1 {
		t.Errorf("calls = %d, want the second from the cache", fake.calls)
	}

	u.Summary = "Retries Fetch twice."
	if got, _ := tr.translation(context.Background(), r, lang("Hebrew")); got.Units["a.go"].Summary != "he:Retries Fetch twice." || fake.calls != 2 {
		t.Errorf("changed text should be translated again: %+v calls=%d", got, fake.calls)
	}

	r.SummaryLang = "Hebrew" // reviewed in Hebrew before translation
	if got, _ := tr.translation(context.Background(), r, lang("Hebrew")); got.Lang != "" || fake.calls != 2 {
		t.Errorf("a result already in the language is shown as it is: %+v", got)
	}
}
