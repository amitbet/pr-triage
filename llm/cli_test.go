package llm

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestStrictSchemaMakesOptionalNullable(t *testing.T) {
	in := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"bucket": map[string]any{"type": "string", "enum": []string{"a", "b"}},
			"reason": map[string]any{"type": "string"},
			"issues": map[string]any{"type": "array", "items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{"type": "string"},
					"line":  map[string]any{"type": "integer"},
				},
				"required": []string{"title"},
			}},
		},
		"required": []string{"bucket", "issues"},
	}
	out := strictSchema(in).(map[string]any)
	req := out["required"].([]string)
	sort.Strings(req)
	if !reflect.DeepEqual(req, []string{"bucket", "issues", "reason"}) || out["additionalProperties"] != false {
		t.Fatalf("top level: %v", out)
	}
	props := out["properties"].(map[string]any)
	if got := props["reason"].(map[string]any)["type"]; !reflect.DeepEqual(got, []any{"string", "null"}) {
		t.Fatalf("reason type = %v", got)
	}
	if got := props["bucket"].(map[string]any)["type"]; got != "string" {
		t.Fatalf("required bucket changed: %v", got)
	}
	item := props["issues"].(map[string]any)["items"].(map[string]any)
	if got := item["properties"].(map[string]any)["line"].(map[string]any)["type"]; !reflect.DeepEqual(got, []any{"integer", "null"}) {
		t.Fatalf("line type = %v", got)
	}
	// The input is left alone.
	if _, ok := in["additionalProperties"]; ok {
		t.Fatal("input mutated")
	}
}

func TestStructuredResponseDropsNulls(t *testing.T) {
	r, err := structuredResponse(ToolDefinition{Name: "submit"}, "```json\n{\"a\":1,\"b\":null,\"c\":[{\"d\":null}]}\n```", nil, Usage{})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(r.ToolCalls[0].Arguments)
	if string(got) != `{"a":1,"c":[{}]}` || r.ToolCalls[0].Name != "submit" {
		t.Fatalf("got %s", got)
	}
}

// fakeCLI builds a local executable that records its args and working
// directory, then prints the configured response.
func fakeCLI(t *testing.T, out string) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	bin = filepath.Join(dir, "cli")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	src := filepath.Join(dir, "cli.go")
	const program = `package main
import ("encoding/json"; "fmt"; "io"; "os")
func main() {
	args, _ := json.Marshal(os.Args[1:])
	_ = os.WriteFile(os.Getenv("PR_MANAGER_FAKE_CLI_ARGS"), args, 0600)
	cwd, _ := os.Getwd()
	_ = os.WriteFile(os.Getenv("PR_MANAGER_FAKE_CLI_ARGS")+".cwd", []byte(cwd), 0600)
	_, _ = io.Copy(io.Discard, os.Stdin)
	fmt.Println(os.Getenv("PR_MANAGER_FAKE_CLI_OUT"))
}`
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
		t.Fatalf("build fake CLI: %v: %s", err, output)
	}
	t.Setenv("PR_MANAGER_FAKE_CLI_ARGS", argsFile)
	t.Setenv("PR_MANAGER_FAKE_CLI_OUT", out)
	return bin, argsFile
}

func TestClaudeCodeWorkspaceEnablesReadOnlyTools(t *testing.T) {
	tool := ToolDefinition{Name: "submit", Description: "Submit.", InputSchema: map[string]any{"type": "object"}}
	call := func(ws *Workspace) ([]string, string, string) {
		bin, argsFile := fakeCLI(t, `{"is_error":false,"result":"","structured_output":{"a":1}}`)
		c := &ClaudeCodeCLI{Binary: bin}
		var prompt string
		if _, err := c.Call(context.Background(), LLMRequest{
			Messages: []ChatMessage{{Role: "user", Content: "hi"}}, Tools: []ToolDefinition{tool},
			ToolChoice: ToolChoiceRequired, Workspace: ws,
		}); err != nil {
			t.Fatal(err)
		}
		_, prompt = cliPrompt([]ChatMessage{{Role: "user", Content: "hi"}}, tool, ws != nil)
		b, _ := os.ReadFile(argsFile)
		cwd, _ := os.ReadFile(argsFile + ".cwd")
		var args []string
		if err := json.Unmarshal(b, &args); err != nil {
			t.Fatal(err)
		}
		return args, strings.TrimSpace(string(cwd)), prompt
	}
	flag := func(args []string, name string) []string {
		var vals []string
		for i, a := range args {
			if a == name && i+1 < len(args) {
				vals = append(vals, args[i+1])
			}
		}
		return vals
	}

	args, _, prompt := call(nil)
	if got := flag(args, "--tools"); len(got) != 1 || got[0] != "" || len(flag(args, "--add-dir")) != 0 || !strings.Contains(prompt, "do not run commands or read files") {
		t.Errorf("no workspace: args %q", args)
	}
	repo, lib := t.TempDir(), t.TempDir()
	args, cwd, prompt := call(&Workspace{Dir: repo, ReadDirs: []string{lib}})
	if got := flag(args, "--tools"); len(got) != 1 || got[0] != readOnlyTools {
		t.Errorf("--tools = %q", got)
	}
	if got := flag(args, "--allowedTools"); len(got) != 1 || got[0] != readOnlyTools {
		t.Errorf("--allowedTools = %q", got)
	}
	if got := flag(args, "--add-dir"); len(got) != 1 || got[0] != lib {
		t.Errorf("--add-dir = %q", got)
	}
	gotDir, gotErr := os.Stat(cwd)
	wantDir, wantErr := os.Stat(repo)
	if gotErr != nil || wantErr != nil || !os.SameFile(gotDir, wantDir) {
		t.Errorf("cwd = %q, want %q", cwd, repo)
	}
	if strings.Contains(prompt, "do not run commands") {
		t.Errorf("prompt still forbids reading: %q", prompt)
	}
}
