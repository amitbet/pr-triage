package llm

import "testing"

func TestOpenAIEndpointAndPayload(t *testing.T) {
	req := LLMRequest{
		Messages:   []ChatMessage{{Role: "system", Content: "s"}, {Role: "user", Content: "u"}},
		Tools:      []ToolDefinition{{Name: "submit", InputSchema: map[string]any{"type": "object"}}},
		ToolChoice: ToolChoiceRequired,
		MaxTokens:  100,
	}
	sol := &OpenAILLM{Model: OpenAIGPT56Sol, Effort: "medium"}
	if !sol.useResponses() {
		t.Fatal("effort on a reasoning model should use /v1/responses")
	}
	p := sol.buildResponsesPayload(req)
	if p["reasoning"].(map[string]any)["effort"] != "medium" || p["max_output_tokens"] != int32(100) ||
		p["tool_choice"].(map[string]any)["name"] != "submit" || p["tools"].([]map[string]any)[0]["name"] != "submit" {
		t.Errorf("responses payload = %+v", p)
	}
	none := &OpenAILLM{Model: OpenAIGPT56Sol, Effort: "none"}
	if none.useResponses() || none.buildPayload(req)["reasoning_effort"] != "none" {
		t.Error("effort none should stay on chat completions with reasoning_effort=none")
	}
	if (&OpenAILLM{Model: OpenAIGPT54Mini}).useResponses() {
		t.Error("no effort should keep chat completions")
	}
	legacy := (&OpenAILLM{Model: "gpt-4.1", Effort: "high"})
	if legacy.useResponses() || legacy.buildPayload(req)["temperature"] != 0.0 {
		t.Error("non-reasoning models ignore effort")
	}
}
