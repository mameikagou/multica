package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestCodexChatHandoffUsesTranscriptNotWorkingDirectory(t *testing.T) {
	for _, present := range []bool{true, false} {
		t.Run(map[bool]string{true: "history survives cwd change", false: "missing history is disclosed"}[present], func(t *testing.T) {
			home := t.TempDir()
			sessionID := "previous-thread"
			if present {
				dir := filepath.Join(home, "sessions")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "rollout-2026-09-09-"+sessionID+".jsonl"), []byte("persisted history"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			task := Task{ChatSessionID: "chat-a", PriorSessionID: sessionID, PriorWorkDir: filepath.Join(t.TempDir(), "retired-cwd")}
			taskCtx := execenv.TaskContextForEnv{PriorSessionResumed: true}
			if !gateResumeToReachableSession(&task, &taskCtx, "codex", t.TempDir(), true, false, discardLogger()) {
				t.Fatal("cwd change discarded Codex resume before checking its transcript")
			}
			gateCodexResumeToRolloutPresence(&task, &taskCtx, "codex", home, discardLogger())
			if present && (task.PriorSessionID != sessionID || !taskCtx.PriorSessionResumed || task.PriorSessionResumeUnavailable) {
				t.Fatalf("existing conversation lost: %+v", task)
			}
			if !present && (task.PriorSessionID != "" || taskCtx.PriorSessionResumed || !task.PriorSessionResumeUnavailable) {
				t.Fatalf("missing history claimed resumable: %+v", task)
			}
		})
	}
}
