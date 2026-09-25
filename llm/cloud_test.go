package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestVertexRequest(t *testing.T) {
	t.Setenv("ANTHROPIC_VERTEX_PROJECT_ID", "proj")
	for region, want := range map[string]string{
		"":         "https://aiplatform.googleapis.com/v1/projects/proj/locations/global/publishers/anthropic/models/claude-sonnet-5:rawPredict",
		"eu":       "https://aiplatform.eu.rep.googleapis.com/v1/projects/proj/locations/eu/publishers/anthropic/models/claude-sonnet-5:rawPredict",
		"us-east5": "https://us-east5-aiplatform.googleapis.com/v1/projects/proj/locations/us-east5/publishers/anthropic/models/claude-sonnet-5:rawPredict",
	} {
		t.Setenv("CLOUD_ML_REGION", region)
		if got, err := (vertex{}).URL("claude-sonnet-5"); err != nil || got != want {
			t.Errorf("region %q: %s %v, want %s", region, got, err, want)
		}
	}
	a := &AnthropicLLM{Model: VertexSonnet5, Platform: vertex{}}
	p := a.buildPayload(LLMRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	a.Platform.Body(p)
	if _, ok := p["model"]; ok || p["anthropic_version"] != anthropicVertex {
		t.Errorf("vertex body: %v", p)
	}
	t.Setenv("ANTHROPIC_VERTEX_PROJECT_ID", "")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GCLOUD_PROJECT", "")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "")
	if _, err := (vertex{}).URL("m"); err == nil {
		t.Error("vertex URL without a project")
	}
}

func TestFoundryAndAzureURLs(t *testing.T) {
	t.Setenv("ANTHROPIC_FOUNDRY_BASE_URL", "")
	t.Setenv("ANTHROPIC_FOUNDRY_RESOURCE", "acme-ai")
	if got, _ := (foundry{}).URL(""); got != "https://acme-ai.services.ai.azure.com/anthropic/v1/messages" {
		t.Errorf("foundry resource URL %s", got)
	}
	t.Setenv("ANTHROPIC_FOUNDRY_BASE_URL", "https://gw.example/anthropic/v1/")
	if got, _ := (foundry{}).URL(""); got != "https://gw.example/anthropic/v1/messages" {
		t.Errorf("foundry base URL %s", got)
	}
	for in, want := range map[string]string{
		"https://acme.openai.azure.com":            "https://acme.openai.azure.com/openai",
		"https://acme.openai.azure.com/":           "https://acme.openai.azure.com/openai",
		"https://acme.openai.azure.com/openai/v1/": "https://acme.openai.azure.com/openai",
	} {
		t.Setenv("AZURE_OPENAI_ENDPOINT", in)
		if got := azureOpenAIBase(); got != want {
			t.Errorf("azure base %q = %s, want %s", in, got, want)
		}
	}
}

func TestBedrockRouting(t *testing.T) {
	for id, mantle := range map[string]bool{
		"anthropic.claude-sonnet-5":                                   true,
		"anthropic.claude-haiku-4-5":                                  true,
		"global.anthropic.claude-haiku-4-5-20251001-v1:0":             false,
		"arn:aws:bedrock:us-east-1:1:application-inference-profile/x": false,
		"amazon.nova-pro-v1:0":                                        false,
	} {
		if got := mantleModel.MatchString(id); got != mantle {
			t.Errorf("%s: Messages API = %v, want %v", id, got, mantle)
		}
	}
}

func TestMantleSigV4(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_REGION", "eu-west-1")
	awsOnce = sync.Once{} // load the config with the keys above
	m := &mantle{}
	url, _ := m.URL("")
	if url != "https://bedrock-mantle.eu-west-1.api.aws/anthropic/v1/messages" {
		t.Errorf("mantle URL %s", url)
	}
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	if err := m.Auth(context.Background(), req, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	auth := req.Header.Get("Authorization")
	if !strings.Contains(auth, "AWS4-HMAC-SHA256") || !strings.Contains(auth, "/eu-west-1/bedrock-mantle/aws4_request") {
		t.Errorf("not signed for bedrock-mantle: %s", auth)
	}
	if req.Header.Get("anthropic-version") == "" {
		t.Error("no anthropic-version header")
	}

	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "tok")
	req, _ = http.NewRequest(http.MethodPost, url, nil)
	_ = m.Auth(context.Background(), req, nil)
	if req.Header.Get("x-api-key") != "tok" || req.Header.Get("Authorization") != "" {
		t.Errorf("bearer token: headers %v", req.Header)
	}
}

// A model that rejects a forced tool call gets tool_choice auto, either up
// front (known models) or after the 400 names tool_choice.
func TestForcedToolFallback(t *testing.T) {
	var choices []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var p struct {
			ToolChoice struct{ Type string } `json:"tool_choice"`
		}
		_ = json.Unmarshal(b, &p)
		choices = append(choices, p.ToolChoice.Type)
		if p.ToolChoice.Type == "tool" && strings.Contains(r.Header.Get("x-api-key"), "strict") {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"tool_choice: type \"tool\" and \"any\" are not supported for this model."}}`)
			return
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"tool_use","id":"t1","name":"submit","input":{"ok":true}}],"stop_reason":"tool_use"}`)
	}))
	defer srv.Close()
	tool := ToolDefinition{Name: "submit"}
	msgs := []ChatMessage{{Role: "user", Content: "go"}}

	// A deployment name that hides the model: learned from the 400.
	a := &AnthropicLLM{APIKey: "strict", BaseURL: srv.URL, Model: "my-deployment"}
	if args, _, err := CallTool(context.Background(), a, msgs, tool, 100); err != nil || args["ok"] != true {
		t.Fatalf("fallback call: %v %v", args, err)
	}
	if strings.Join(choices, ",") != "tool,auto" {
		t.Errorf("tool choices %v, want tool then auto", choices)
	}
	choices = nil
	if _, _, err := CallTool(context.Background(), a, msgs, tool, 100); err != nil || strings.Join(choices, ",") != "auto" {
		t.Errorf("second call: %v, choices %v (the model should be remembered)", err, choices)
	}

	// Opus 5.5 is known: no failed request first.
	choices = nil
	b := &AnthropicLLM{APIKey: "k", BaseURL: srv.URL, Model: "claude-opus-5-5"}
	if _, _, err := CallTool(context.Background(), b, msgs, tool, 100); err != nil || strings.Join(choices, ",") != "auto" {
		t.Errorf("opus 5.5: %v, choices %v", err, choices)
	}
	// Other models keep the forced call.
	choices = nil
	c := &AnthropicLLM{APIKey: "k", BaseURL: srv.URL, Model: "claude-sonnet-5"}
	if _, _, err := CallTool(context.Background(), c, msgs, tool, 100); err != nil || strings.Join(choices, ",") != "tool" {
		t.Errorf("sonnet 5: %v, choices %v", err, choices)
	}
}

func TestConfiguredCloud(t *testing.T) {
	for _, k := range []string{"CLAUDE_CODE_USE_BEDROCK", "AWS_BEARER_TOKEN_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "ANTHROPIC_VERTEX_PROJECT_ID",
		"CLAUDE_CODE_USE_FOUNDRY", "ANTHROPIC_FOUNDRY_RESOURCE", "ANTHROPIC_FOUNDRY_BASE_URL", "AZURE_OPENAI_ENDPOINT"} {
		t.Setenv(k, "")
	}
	if got := ConfiguredCloud(); got != "" {
		t.Errorf("nothing set: %q", got)
	}
	t.Setenv("CLAUDE_CODE_USE_VERTEX", "1")
	if got := ConfiguredCloud(); got != Vertex {
		t.Errorf("CLAUDE_CODE_USE_VERTEX: %q", got)
	}
	for alias, want := range map[string]string{"aws": Bedrock, "gcp": Vertex, "azure": AzureOpenAI, "microsoft-foundry": Foundry} {
		if got := ProviderID(alias); got != want {
			t.Errorf("ProviderID(%s) = %s", alias, got)
		}
	}
}
