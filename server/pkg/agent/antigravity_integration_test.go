//go:build agentintegration

package agent

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestAntigravityRealStreamJSONUsage is opt-in because it consumes the signed-
// in Antigravity account's quota. It verifies the installed agy binary's wire
// contract instead of relying only on hand-written JSON fixtures.
func TestAntigravityRealStreamJSONUsage(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	execPath, err := exec.LookPath("agy")
	if err != nil {
		t.Skipf("agy is not installed: %v", err)
	}

	backend, err := New("antigravity", Config{
		ExecutablePath: execPath,
		Logger:         quietAntigravityLogger(),
	})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	session, err := backend.Execute(ctx, "Reply with exactly PONG and nothing else.", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "PONG") {
		t.Fatalf("output = %q, want PONG", result.Output)
	}
	if result.SessionID == "" {
		t.Fatal("expected stream-json conversation id")
	}
	var total int64
	for model, usage := range result.Usage {
		t.Logf("model=%q usage=%+v", model, usage)
		total += usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
	}
	if total <= 0 {
		t.Fatalf("expected non-zero provider usage, got %+v", result.Usage)
	}
}
