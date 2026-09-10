package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexChatHandoffPrepareAcrossTaskIDs(t *testing.T) {
	// Exercise actual environment preparation, not just its sessions helper.
	// Both homes are private fixtures; no installed CLI or account is accessed.
	t.Setenv("CODEX_HOME", t.TempDir())
	params := PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "ws-chat-handoff",
		TaskID: "11111111-1111-4111-8111-111111111111", Profile: "test-chat-handoff",
		Provider: "codex", CodexVersion: "0.151.0", AgentName: "Test Agent",
		Task: TaskContextForEnv{AgentID: "agent-a", ChatSessionID: "chat-a"},
	}
	first, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Cleanup(true)
	sessionID := "persisted-thread"
	seedFakeRollout(t, filepath.Join(first.CodexHome, "sessions"), "2026", "09", "09", sessionID, 32)
	first.ReleaseLock()
	params.TaskID = "22222222-2222-4222-8222-222222222222"
	params.CodexResumeSessionID = sessionID
	second, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Cleanup(true)
	if first.WorkDir == second.WorkDir {
		t.Fatal("fixture must prepare a different task directory")
	}
	if !CodexResumeRolloutPresent(second.CodexHome, sessionID) {
		t.Fatal("Prepare discarded the first turn's transcript on a new task ID")
	}
}

func TestCodexChatHandoffNativeStoreToManagedHome(t *testing.T) {
	t.Parallel()
	shared := t.TempDir()
	key := codexSessionStoreKey("test-profile", TaskContextForEnv{AgentID: "agent-a", ChatSessionID: "chat-a"})
	oldHome := filepath.Join(t.TempDir(), "old-native-home")
	if err := os.MkdirAll(oldHome, 0o755); err != nil {
		t.Fatal(err)
	}
	// The old fork used the persistent local-directory store for native cwd.
	if err := prepareCodexSessionsDir(oldHome, shared, CodexHomeOptions{IsLocalDirectory: true, SessionStoreKey: key}, testLogger()); err != nil {
		t.Fatal(err)
	}
	sessionID := "old-native-thread"
	oldRollout := seedFakeRollout(t, filepath.Join(oldHome, "sessions"), "2026", "09", "09", sessionID, 32)
	newHome := filepath.Join(t.TempDir(), "new-managed-home")
	if err := os.MkdirAll(newHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareCodexSessionsDir(newHome, shared, CodexHomeOptions{PersistentSessionStore: true, SessionStoreKey: key, ResumeSessionID: sessionID}, testLogger()); err != nil {
		t.Fatal(err)
	}
	if !CodexResumeRolloutPresent(newHome, sessionID) {
		t.Fatal("managed follow-up lost the existing native conversation")
	}
	oldInfo, err := os.Stat(oldRollout)
	if err != nil {
		t.Fatal(err)
	}
	newInfo, err := os.Stat(findCodexRollouts(filepath.Join(newHome, "sessions"), sessionID)[0])
	if err != nil || !os.SameFile(oldInfo, newInfo) {
		t.Fatalf("follow-up did not mount the original transcript: %v", err)
	}
	otherHome := filepath.Join(t.TempDir(), "other-chat-home")
	if err := os.MkdirAll(otherHome, 0o755); err != nil {
		t.Fatal(err)
	}
	otherKey := codexSessionStoreKey("test-profile", TaskContextForEnv{AgentID: "agent-a", ChatSessionID: "chat-b"})
	if err := prepareCodexSessionsDir(otherHome, shared, CodexHomeOptions{PersistentSessionStore: true, SessionStoreKey: otherKey}, testLogger()); err != nil {
		t.Fatal(err)
	}
	if CodexResumeRolloutPresent(otherHome, sessionID) {
		t.Fatal("another chat inherited the transcript")
	}
}

func TestCodexChatHandoffFromScopedLegacySource(t *testing.T) {
	t.Parallel()
	shared := t.TempDir()
	source := t.TempDir()
	sessionID := "requested-thread"
	oldRollout := seedFakeRollout(t, source, "2026", "09", "09", sessionID, 32)
	seedFakeRollout(t, source, "2026", "09", "09", "unrelated-thread", 32)
	home := filepath.Join(t.TempDir(), "new-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareCodexSessionsDir(home, shared, CodexHomeOptions{PersistentSessionStore: true, SessionStoreKey: "chat-a", ResumeSessionID: sessionID, ResumeSessionsSource: source}, testLogger()); err != nil {
		t.Fatal(err)
	}
	if !CodexResumeRolloutPresent(home, sessionID) {
		t.Fatal("scoped prior transcript was not migrated")
	}
	if CodexResumeRolloutPresent(home, "unrelated-thread") {
		t.Fatal("migration exposed unrelated history")
	}
	if _, err := os.Stat(oldRollout); err != nil {
		t.Fatalf("migration removed original transcript: %v", err)
	}
}

func TestCodexChatHandoffKeepsAuthoritativeLocalHistory(t *testing.T) {
	t.Parallel()
	shared := t.TempDir()
	home := t.TempDir()
	sessionID := "existing-thread"
	rollout := seedFakeRollout(t, filepath.Join(home, "sessions"), "2026", "09", "09", sessionID, 32)
	if err := prepareCodexSessionsDir(home, shared, CodexHomeOptions{PersistentSessionStore: true, SessionStoreKey: "chat-a", ResumeSessionID: sessionID}, testLogger()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rollout); err != nil {
		t.Fatalf("original history removed: %v", err)
	}
	info, err := os.Lstat(filepath.Join(home, "sessions"))
	if err != nil || !info.IsDir() {
		t.Fatalf("authoritative directory replaced: %v", err)
	}
	if len(findCodexRollouts(codexSessionStoreDir(shared, "chat-a"), sessionID)) != 1 {
		t.Fatal("legacy history was not persisted for a future handoff")
	}
}
