package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// OpenJev is a client for a local OpenJev server (github.com/lookski/openjev).
// OpenJev does not generate text: it runs one forward pass and reads the
// softmax over the answer tokens, so every answer comes with a real
// probability distribution instead of a self-reported confidence.
// That makes it fast and cheap for fixed-label decisions, and useless for
// anything that needs reasoning before the first answer token.
type OpenJev struct {
	BaseURL    string
	HTTPClient *http.Client
}

// JevQuestion is one question in a /v1/systemone request.
//   - Type "choice": Criteria is map[label]description
//   - Type "score":  Criteria is []string of ordered levels (2-10)
//   - Type "noul":   yes/no, Criteria unused
type JevQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

type JevAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         *int               `json:"score,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
}

type JevResponse struct {
	Answers map[string]JevAnswer `json:"answers"`
	Usage   struct {
		ForwardPasses int     `json:"forward_passes"`
		InputTokens   int     `json:"input_tokens"`
		LatencyMS     float64 `json:"latency_ms"`
	} `json:"usage"`
}

func (j *OpenJev) baseURL() string {
	if strings.TrimSpace(j.BaseURL) != "" {
		return strings.TrimRight(j.BaseURL, "/")
	}
	if env := strings.TrimSpace(os.Getenv("OPENJEV_BASE_URL")); env != "" {
		return strings.TrimRight(env, "/")
	}
	return "http://127.0.0.1:8771"
}

func (j *OpenJev) client() *http.Client {
	if j.HTTPClient != nil {
		return j.HTTPClient
	}
	return &http.Client{Timeout: 120 * time.Second}
}

// Decide asks every question against the same state in one request.
func (j *OpenJev) Decide(ctx context.Context, state string, questions map[string]JevQuestion) (*JevResponse, error) {
	b, err := json.Marshal(map[string]any{"state": state, "questions": questions})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.baseURL()+"/v1/systemone", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := j.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("openjev request failed (is the server running on %s?): %w", j.baseURL(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openjev error: status %d, body: %s", resp.StatusCode, string(body))
	}
	var out JevResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
