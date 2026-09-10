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

func TestCodexChatHandoffRejectsUnusableHome(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	root := t.TempDir()
	workDir := filepath.Join(root, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, codexHomeDirName), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if env := Reuse(ReuseParams{WorkDir: workDir, Provider: "codex"}, testLogger()); env != nil {
		t.Fatalf("unusable home returned an environment that could launch with ambient CODEX_HOME: %q", env.CodexHome)
	}
}

func TestCodexChatHandoffDefersFailedExport(t *testing.T) {
	shared := t.TempDir()
	t.Setenv("CODEX_HOME", shared)
	root := t.TempDir()
	workDir := filepath.Join(root, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, codexHomeDirName)
	task := TaskContextForEnv{AgentID: "agent-a", ChatSessionID: "chat-a"}
	key := codexSessionStoreKey("test-profile", task)
	store := codexSessionStoreDir(shared, key)
	sessionID := "existing-thread"
	rollout := seedFakeRollout(t, filepath.Join(home, "sessions"), "2026", "09", "09", sessionID, 32)
	original, err := os.ReadFile(rollout)
	if err != nil {
		t.Fatal(err)
	}
	// A file blocks the destination deterministically, even under a privileged
	// test runner. The task-local transcript remains readable and authoritative.
	if err := os.MkdirAll(filepath.Dir(store), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := CodexHomeOptions{PersistentSessionStore: true, SessionStoreKey: key, ResumeSessionID: sessionID}
	if err := prepareCodexSessionsDir(home, shared, opts, testLogger()); err != nil {
		t.Errorf("optional export invalidated usable task-local history: %v", err)
	}
	params := ReuseParams{WorkDir: workDir, Provider: "codex", CodexVersion: "0.151.0", Profile: "test-profile", Task: task, ResumeSessionID: sessionID}
	reused := Reuse(params, testLogger())
	if reused == nil || reused.CodexHome != home || !CodexResumeRolloutPresent(reused.CodexHome, sessionID) {
		t.Fatal("failed export lost the reused home or its original rollout")
	}
	if info, err := os.Lstat(filepath.Join(home, "sessions")); err != nil || !info.IsDir() {
		t.Fatalf("failed export replaced authoritative sessions directory: %v", err)
	}
	if len(findCodexRollouts(store, sessionID)) != 0 {
		t.Fatal("blocked store unexpectedly contains exported history")
	}
	if err := os.Remove(store); err != nil {
		t.Fatal(err)
	}
	// A later reuse must retry persistence without replacing local history.
	reused = Reuse(params, testLogger())
	if reused == nil || reused.CodexHome != home || !CodexResumeRolloutPresent(home, sessionID) {
		t.Fatal("retry lost the original session")
	}
	if len(findCodexRollouts(store, sessionID)) != 1 {
		t.Fatal("export did not recover after the destination became available")
	}
	current, err := os.ReadFile(rollout)
	if err != nil || string(current) != string(original) {
		t.Fatalf("export changed original transcript: %v", err)
	}
	newHome := t.TempDir()
	if err := prepareCodexSessionsDir(newHome, shared, opts, testLogger()); err != nil {
		t.Fatal(err)
	}
	if !CodexResumeRolloutPresent(newHome, sessionID) {
		t.Fatal("recovered export cannot hand off to a new task home")
	}
}
