//go:build agentintegration

package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestMagentRealGrokACP verifies company auth, model selection and persisted
// session loading through the real Grok backend, without a protocol shim.
func TestMagentRealGrokACP(t *testing.T) {
	requireRealAgentSmoke(t)
	path, err := exec.LookPath("magent")
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"kimi-k3", "deepseek-v4.1-flash"} {
		t.Run(model, func(t *testing.T) {
			backend, err := New("grok", Config{ExecutablePath: path, Logger: slog.Default()})
			if err != nil {
				t.Fatal(err)
			}
			cwd := t.TempDir()
			run := func(prompt, sessionID string) Result {
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				s, err := backend.Execute(ctx, prompt, ExecOptions{Cwd: cwd, Model: model, ResumeSessionID: sessionID, Timeout: 85 * time.Second})
				if err != nil {
					t.Fatal(err)
				}
				go func() {
					for range s.Messages {
					}
				}()
				select {
				case r := <-s.Result:
					if r.Status != "completed" {
						t.Fatalf("status=%s error=%s output=%s", r.Status, r.Error, r.Output)
					}
					return r
				case <-ctx.Done():
					t.Fatal(ctx.Err())
					return Result{}
				}
			}
			first := run("Remember this test word: ORBIT_7391. Do not use tools. Reply exactly READY.", "")
			if first.SessionID == "" || !strings.Contains(first.Output, "READY") {
				t.Fatalf("invalid first result: %+v", first)
			}
			second := run("What was the test word from my previous message? Reply only that word. Do not use tools.", first.SessionID)
			if second.SessionID != first.SessionID || !strings.Contains(second.Output, "ORBIT_7391") {
				t.Fatalf("resume failed: %+v", second)
			}
			t.Logf("model=%s auth=company.credential session=%s first=%q resumed=%q", model, first.SessionID, first.Output, second.Output)
		})
	}
}
