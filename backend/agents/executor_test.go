package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStubExecutor_Execute(t *testing.T) {
	dir := t.TempDir()
	e := NewStubExecutor(dir)

	req := ExecutionRequest{
		ContractID:       "wish-abc123",
		VisiblePixelHash: "abc123",
		TaskID:           "task-1",
		Title:            "Test task",
		Description:      "Do the work described.",
		Workdir:          dir,
	}

	result, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("StubExecutor.Execute failed: %v", err)
	}
	if result.Notes == "" {
		t.Error("expected non-empty Notes")
	}
	if result.ResultFile == "" {
		t.Error("expected non-empty ResultFile")
	}
	if !strings.Contains(result.Notes, "Test task") {
		t.Error("expected Notes to contain task title")
	}
	if !strings.Contains(result.Notes, "placeholder") {
		t.Error("expected Notes to mention stub/placeholder")
	}

	reportPath := filepath.Join(dir, "task-1.md")
	if _, err := os.Stat(reportPath); os.IsNotExist(err) {
		t.Errorf("report file not created at %s", reportPath)
	}

	indexPath := filepath.Join(dir, "index.html")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Errorf("index.html not created at %s", indexPath)
	}
}

// TestAutoDetectExecutor_CommandConstruction validates that we build the correct
// arguments for each supported tool. This does not require the binaries to be present.
func TestAutoDetectExecutor_CommandConstruction(t *testing.T) {
	tests := []struct {
		name           string
		detectedName   string
		req            ExecutionRequest
		wantContains   []string // substrings that must appear in the arg list
		wantNotContain []string
	}{
		{
			name:         "opencode",
			detectedName: "opencode",
			req:          ExecutionRequest{Workdir: "/tmp/ws", Description: "build a todo app"},
			wantContains: []string{"run", "--dir", "/tmp/ws", "build a todo app"},
		},
		{
			name:         "claude",
			detectedName: "claude",
			req:          ExecutionRequest{Description: "fix the bug"},
			wantContains: []string{"-p", "fix the bug", "--output-format", "text"},
		},
		{
			name:         "grok with prompt file",
			detectedName: "grok",
			req:          ExecutionRequest{Workdir: "/tmp/grokws", Description: "implement feature"},
			wantContains: []string{"-p", "--prompt-file", "--cwd", "/tmp/grokws", "--no-wait-for-background"},
		},
		{
			name:         "agy",
			detectedName: "agy",
			req:          ExecutionRequest{Description: "do the thing"},
			wantContains: []string{"--print", "do the thing"},
		},
		{
			name:         "codex",
			detectedName: "codex",
			req:          ExecutionRequest{Workdir: "/tmp/codex", Description: "write tests"},
			wantContains: []string{"exec", "-C", "/tmp/codex", "write tests"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// We test the spec directly to avoid needing real detection
			spec, ok := toolSpecs[tc.detectedName]
			if !ok {
				t.Fatalf("no spec for %s", tc.detectedName)
			}

			// Simulate prompt file decision
			var promptFile string
			if spec.preferPromptFile {
				promptFile = "/tmp/.prompt.txt"
			}

			args := spec.buildArgs(tc.req.Description, tc.req.Workdir, promptFile)

			got := strings.Join(args, " ")
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("args %q missing %q", got, want)
				}
			}
			for _, notWant := range tc.wantNotContain {
				if strings.Contains(got, notWant) {
					t.Errorf("args %q should not contain %q", got, notWant)
				}
			}
		})
	}
}

func TestStubExecutor_ExecuteWithRework(t *testing.T) {
	dir := t.TempDir()
	e := NewStubExecutor(dir)

	req := ExecutionRequest{
		ContractID:        "wish-abc123",
		VisiblePixelHash:  "abc123",
		TaskID:            "task-rework-1",
		Title:             "Fix bug",
		Description:       "Fix the critical bug",
		RejectionFeedback: "Missing error handling",
		PreviousWork:      "Previous attempt without error handling",
		Workdir:           dir,
	}

	result, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("StubExecutor.Execute with rework failed: %v", err)
	}
	if !strings.Contains(result.Notes, "Rework Applied") {
		t.Error("expected rework status in notes, got:", result.Notes)
	}
	if !strings.Contains(result.Notes, "Missing error handling") {
		t.Error("expected rejection feedback in notes")
	}
}

func TestStubExecutor_ExecuteNoHash(t *testing.T) {
	dir := t.TempDir()
	e := NewStubExecutor(dir)

	req := ExecutionRequest{
		TaskID:   "task-no-hash",
		Title:    "No hash task",
		Workdir:  dir,
	}

	result, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute with no hash failed: %v", err)
	}
	if result.Notes == "" {
		t.Error("expected non-empty Notes even without hash")
	}
}

func TestStubExecutor_ExecuteEmptyUploadsDir(t *testing.T) {
	e := NewStubExecutor("")
	if e.uploadsDir == "" {
		t.Log("StubExecutor with empty dir defaults to empty (no crash)")
	}

	result, err := e.Execute(context.Background(), ExecutionRequest{
		TaskID:  "task-empty-uploads",
		Title:   "Empty uploads test",
	})
	if err != nil {
		t.Fatalf("Execute with empty dir failed: %v", err)
	}
	if result.Notes == "" {
		t.Error("expected non-empty Notes even with empty uploads dir")
	}
}

func TestStubExecutor_EnsureIndexHTML(t *testing.T) {
	dir := t.TempDir()
	ensureIndexHTML(dir, "test-hash-123")

	indexPath := filepath.Join(dir, "index.html")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Errorf("ensureIndexHTML did not create index.html")
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "test-hash-123") {
		t.Error("index.html should contain the visible hash")
	}
}

func TestStubExecutor_EnsureIndexHTMLExisting(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "index.html")
	_ = os.WriteFile(existing, []byte("existing content"), 0644)

	ensureIndexHTML(dir, "new-hash")

	data, _ := os.ReadFile(existing)
	if string(data) != "existing content" {
		t.Error("ensureIndexHTML should not overwrite existing index.html")
	}
}

func TestAutoDetectExecutor_IsAvailable(t *testing.T) {
	e := NewAutoDetectExecutor("")
	available := e.IsAvailable()

	if !available {
		t.Log("AutoDetectExecutor: no tool detected in PATH (expected in CI)")
	} else {
		t.Logf("AutoDetectExecutor detected: %s at %s", e.detectedName, e.detectedBinary)
	}
}

func TestAutoDetectExecutor_ExecuteFallsBackToStub(t *testing.T) {
	e := &AutoDetectExecutor{uploadsDir: t.TempDir()}
	if e.IsAvailable() {
		t.Skip("skipping fallback test: executor is available")
	}

	req := ExecutionRequest{
		ContractID:       "wish-stub-fallback",
		VisiblePixelHash: "stub-fallback",
		TaskID:           "task-fallback",
		Title:            "Fallback test",
		Description:      "Verify fallback to stub works",
	}

	result, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("AutoDetectExecutor fallback Execute failed: %v", err)
	}
	if result.Notes == "" {
		t.Error("expected non-empty notes from fallback")
	}
	if !strings.Contains(result.Notes, "placeholder") {
		t.Log("fallback executor produced notes (stub expected):", result.Notes)
	}
}

func TestBuildPrompt_NewWork(t *testing.T) {
	e := &AutoDetectExecutor{}
	req := ExecutionRequest{
		Description: "Build a todo list app",
		Title:       "Todo App",
	}
	prompt := e.buildPrompt(req)
	if !strings.Contains(prompt, "NEW WORK") {
		t.Error("expected NEW WORK header in prompt")
	}
	if !strings.Contains(prompt, "Build a todo list app") {
		t.Error("expected description in prompt")
	}
	if !strings.Contains(prompt, "index.html") {
		t.Error("expected index.html requirement in prompt")
	}
}

func TestBuildPrompt_Rework(t *testing.T) {
	e := &AutoDetectExecutor{}
	req := ExecutionRequest{
		Description:       "Fix the bug",
		Title:             "Bug fix",
		RejectionFeedback: "Code doesn't compile",
	}
	prompt := e.buildPrompt(req)
	if !strings.Contains(prompt, "REWORK") {
		t.Error("expected REWORK header")
	}
	if !strings.Contains(prompt, "Code doesn't compile") {
		t.Error("expected rejection feedback in prompt")
	}
}

func TestBuildPrompt_ReworkWithPreviousWork(t *testing.T) {
	e := &AutoDetectExecutor{}
	req := ExecutionRequest{
		Description:       "Implement feature",
		Title:             "Feature X",
		RejectionFeedback: "Missing tests",
		PreviousWork:      "Previous implementation without tests",
	}
	prompt := e.buildPrompt(req)
	if !strings.Contains(prompt, "REWORK") {
		t.Error("expected REWORK header")
	}
	if !strings.Contains(prompt, "Previous implementation") {
		t.Error("expected previous work in prompt")
	}
}

func TestBuildPrompt_WithProposalContext(t *testing.T) {
	e := &AutoDetectExecutor{}
	req := ExecutionRequest{
		Description:     "Build feature",
		Title:           "Feature Y",
		ProposalContext: "Proposal says to build Y with tests",
	}
	prompt := e.buildPrompt(req)
	if !strings.Contains(prompt, "Proposal context") && !strings.Contains(prompt, "PROPOSAL CONTEXT") {
		t.Error("expected proposal context in prompt")
	}
}

func TestBuildPrompt_ContinuationWork(t *testing.T) {
	e := &AutoDetectExecutor{}
	req := ExecutionRequest{
		Description:   "Continue work",
		Title:         "Continue Z",
		PreviousWork:  "Done some work already",
	}
	prompt := e.buildPrompt(req)
	if !strings.Contains(prompt, "CONTINUATION") {
		t.Error("expected CONTINUATION header")
	}
}

func TestStubExecutor_DefaultUploadsDir(t *testing.T) {
	prev := os.Getenv("UPLOADS_DIR")
	defer os.Setenv("UPLOADS_DIR", prev)

	os.Setenv("UPLOADS_DIR", "/tmp/stub-test-uploads")
	e := NewStubExecutor("")
	if e.uploadsDir != "/tmp/stub-test-uploads" {
		t.Errorf("expected /tmp/stub-test-uploads, got %s", e.uploadsDir)
	}
}

func TestAutoDetectExecutor_ForcedExecutor(t *testing.T) {
	prev := os.Getenv("STARGATE_AGENT_EXECUTOR")
	defer os.Setenv("STARGATE_AGENT_EXECUTOR", prev)

	os.Setenv("STARGATE_AGENT_EXECUTOR", "nonexistent-tool-xyz")
	e := NewAutoDetectExecutor("")
	if e.detectedBinary != "" {
		t.Logf("forced executor not found, fell back to auto-detect: %s", e.detectedName)
	}
}

func TestStubExecutor_ResultFile(t *testing.T) {
	dir := t.TempDir()
	e := NewStubExecutor(dir)

	req := ExecutionRequest{
		VisiblePixelHash: "abc123",
		TaskID:           "my-task",
		Workdir:          dir,
	}

	result, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	expected := "/uploads/results/abc123/my-task.md"
	if result.ResultFile != expected {
		t.Errorf("ResultFile: got %q, want %q", result.ResultFile, expected)
	}
	if result.CompletionProof == "" {
		t.Error("expected non-empty CompletionProof")
	}
	if !strings.HasPrefix(result.CompletionProof, "stub-") {
		t.Errorf("expected CompletionProof to start with 'stub-', got %s", result.CompletionProof)
	}
}
