package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func findCommentOutcome(t *testing.T, outcomes []CommentTriggerOutcome, targetID string) CommentTriggerOutcome {
	t.Helper()
	for _, o := range outcomes {
		if o.TargetID == targetID {
			return o
		}
	}
	t.Fatalf("no trigger outcome for target %s in %+v", targetID, outcomes)
	return CommentTriggerOutcome{}
}

func TestCommentTriggerOutcomesSkipHandledReceipts(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		statuses   []DispatchStatus
		wantStored bool
	}{
		{name: "no mentions"},
		{name: "queued", statuses: []DispatchStatus{DispatchQueued}},
		{name: "coalesced", statuses: []DispatchStatus{DispatchCoalesced}},
		{name: "deferred", statuses: []DispatchStatus{DispatchDeferred}},
		{name: "steered", statuses: []DispatchStatus{DispatchSteered}},
		{name: "all handled", statuses: []DispatchStatus{DispatchQueued, DispatchCoalesced, DispatchDeferred, DispatchSteered}},
		{name: "partially blocked", statuses: []DispatchStatus{DispatchQueued, DispatchBlocked}, wantStored: true},
		{name: "unknown status", statuses: []DispatchStatus{"future"}, wantStored: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issueID := dbfx.Issue(t, "mention receipt "+tc.name)
			commentID := dbfx.Comment(t, issueID, "handoff")
			initial, err := testHandler.Queries.GetComment(ctx, parseUUID(commentID))
			if err != nil {
				t.Fatal(err)
			}
			outcomes := make([]CommentTriggerOutcome, 0, len(tc.statuses))
			for i, status := range tc.statuses {
				outcomes = append(outcomes, CommentTriggerOutcome{TargetType: "agent", TargetID: fmt.Sprintf("target-%d", i), Status: status})
			}
			h := *testHandler
			h.Bus = events.New()
			broadcasts := 0
			h.Bus.Subscribe(protocol.EventCommentUpdated, func(events.Event) { broadcasts++ })
			saved := h.saveCommentTriggerOutcomes(ctx, initial, outcomes)
			response := h.commentTriggerResponse(newRequest(http.MethodPost, "/", nil), commentToResponse(initial, nil, nil), saved, outcomes, "member", testUserID)
			if !reflect.DeepEqual(response.TriggerOutcomes, outcomes) {
				t.Errorf("submission outcomes = %+v, want %+v", response.TriggerOutcomes, outcomes)
			}
			current, err := h.Queries.GetComment(ctx, initial.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantStored {
				if !reflect.DeepEqual(storedCommentTriggerOutcomes(current), outcomes) || current.Revision != initial.Revision+1 || broadcasts != 1 {
					t.Errorf("warning receipt not saved and broadcast: outcomes=%+v revision=%d broadcasts=%d", storedCommentTriggerOutcomes(current), current.Revision, broadcasts)
				}
			} else if len(storedCommentTriggerOutcomes(current)) != 0 || current.Revision != initial.Revision || broadcasts != 0 {
				t.Errorf("handled mentions caused a receipt write or broadcast: outcomes=%+v revision=%d broadcasts=%d", storedCommentTriggerOutcomes(current), current.Revision, broadcasts)
			}
		})
	}
}

func TestCommentTriggerOutcomesPersistAcrossReadsAndEdits(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	privateAgentID, _, readerID := privateAgentTestFixture(t)
	allowedAgentID := createHandlerTestAgent(t, "Receipt allowed agent", nil)
	issueID := createCommentTriggerPreviewIssue(t, "persisted mention receipts", "", "")
	content := fmt.Sprintf("[@Private](mention://agent/%s) [@Allowed](mention://agent/%s)", privateAgentID, allowedAgentID)
	h := *testHandler
	h.Bus = events.New()
	var broadcast CommentResponse
	for _, eventType := range []string{protocol.EventCommentCreated, protocol.EventCommentUpdated} {
		h.Bus.Subscribe(eventType, func(event events.Event) {
			if payload, ok := event.Payload.(map[string]any); ok {
				if comment, ok := payload["comment"].(CommentResponse); ok && comment.IssueID == issueID {
					broadcast = comment
				}
			}
		})
	}
	var created CommentResponse
	testutil.Call(t, h.CreateComment, withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": content,
	}), "id", issueID)).Want(http.StatusCreated).JSON(&created)
	if blocked := findCommentOutcome(t, created.TriggerOutcomes, privateAgentID); blocked.Status != DispatchBlocked {
		t.Fatalf("private mention = %+v", blocked)
	}
	if allowed := findCommentOutcome(t, created.TriggerOutcomes, allowedAgentID); allowed.Status != DispatchQueued {
		t.Fatalf("allowed mention = %+v", allowed)
	}
	if !reflect.DeepEqual(broadcast.TriggerOutcomes, created.TriggerOutcomes) {
		t.Errorf("broadcast outcomes = %+v, want %+v", broadcast.TriggerOutcomes, created.TriggerOutcomes)
	}
	for _, query := range []string{"", "?roots_only=true", "?summary=true", "?thread=" + created.ID} {
		var comments []CommentResponse
		testutil.Call(t, h.ListComments, withURLParam(newRequestAs(readerID, http.MethodGet, "/api/issues/"+issueID+"/comments"+query, nil), "id", issueID)).Want(http.StatusOK).JSON(&comments)
		if len(comments) != 1 || !reflect.DeepEqual(comments[0].TriggerOutcomes, created.TriggerOutcomes) {
			t.Errorf("read %q outcomes = %+v, want persisted outcomes", query, comments)
		}
	}
	var timeline []TimelineEntry
	testutil.Call(t, h.ListTimeline, withURLParam(newRequestAs(readerID, http.MethodGet, "/api/issues/"+issueID+"/timeline", nil), "id", issueID)).Want(http.StatusOK).JSON(&timeline)
	found := false
	for _, entry := range timeline {
		if entry.ID == created.ID {
			found = reflect.DeepEqual(entry.TriggerOutcomes, created.TriggerOutcomes)
		}
	}
	if !found {
		t.Error("timeline did not retain mention outcomes")
	}
	var unchanged CommentResponse
	testutil.Call(t, h.UpdateComment, withURLParam(newRequest(http.MethodPut, "/api/comments/"+created.ID, map[string]any{
		"content": content,
	}), "commentId", created.ID)).Want(http.StatusOK).JSON(&unchanged)
	if !reflect.DeepEqual(unchanged.TriggerOutcomes, created.TriggerOutcomes) {
		t.Errorf("unchanged edit lost outcomes: %+v", unchanged.TriggerOutcomes)
	}
	var updated CommentResponse
	testutil.Call(t, h.UpdateComment, withURLParam(newRequest(http.MethodPut, "/api/comments/"+created.ID, map[string]any{
		"content": "No handoff needed",
	}), "commentId", created.ID)).Want(http.StatusOK).JSON(&updated)
	if len(updated.TriggerOutcomes) != 0 || len(broadcast.TriggerOutcomes) != 0 {
		t.Errorf("edited comment retained old outcomes: response=%+v broadcast=%+v", updated.TriggerOutcomes, broadcast.TriggerOutcomes)
	}
	_, comments := listComments(t, issueID, "")
	if len(comments) != 1 || len(comments[0].TriggerOutcomes) != 0 {
		t.Errorf("edited read retained old outcomes: %+v", comments)
	}
	testutil.Call(t, h.UpdateComment, withURLParam(newRequest(http.MethodPut, "/api/comments/"+created.ID, map[string]any{
		"content": fmt.Sprintf("[@Allowed](mention://agent/%s) please handle", allowedAgentID),
	}), "commentId", created.ID)).Want(http.StatusOK).JSON(&updated)
	if len(updated.TriggerOutcomes) != 1 || updated.TriggerOutcomes[0].Status == DispatchBlocked {
		t.Errorf("successful edited handoff = %+v", updated.TriggerOutcomes)
	}
	_, comments = listComments(t, issueID, "")
	if len(comments) != 1 || len(comments[0].TriggerOutcomes) != 0 || len(broadcast.TriggerOutcomes) != 0 {
		t.Errorf("successful handoff stored or broadcast a warning receipt: comments=%+v broadcast=%+v", comments, broadcast.TriggerOutcomes)
	}
}

func TestCommentTriggerOutcomesFollowContentAttempt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	for _, change := range []string{"metadata", "unchanged", "edited", "edited back", "deleted"} {
		t.Run(change, func(t *testing.T) {
			issueID := createCommentTriggerPreviewIssue(t, "receipt race "+change, "", "")
			commentID := dbfx.Comment(t, issueID, "original content")
			initial, err := testHandler.Queries.GetComment(ctx, parseUUID(commentID))
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "metadata":
				_, err = testHandler.Queries.BumpCommentRevision(ctx, db.BumpCommentRevisionParams{ID: initial.ID, WorkspaceID: initial.WorkspaceID})
			case "unchanged":
				_, err = testHandler.Queries.UpdateComment(ctx, db.UpdateCommentParams{ID: initial.ID, Content: initial.Content})
			case "edited", "edited back":
				_, err = testHandler.Queries.UpdateComment(ctx, db.UpdateCommentParams{ID: initial.ID, Content: "new content"})
				if err == nil && change == "edited back" {
					_, err = testHandler.Queries.UpdateComment(ctx, db.UpdateCommentParams{ID: initial.ID, Content: initial.Content})
				}
			case "deleted":
				_, err = testHandler.Queries.TombstoneComment(ctx, db.TombstoneCommentParams{ID: initial.ID, WorkspaceID: initial.WorkspaceID})
			}
			if err != nil {
				t.Fatal(err)
			}
			h := *testHandler
			h.Bus = events.New()
			broadcasts := 0
			h.Bus.Subscribe(protocol.EventCommentUpdated, func(events.Event) { broadcasts++ })
			outcomes := []CommentTriggerOutcome{{TargetType: "agent", TargetID: "00000000-0000-0000-0000-0000000000ff", Status: DispatchBlocked, ReasonCode: ReasonInvocationNotAllowed}}
			saved := h.saveCommentTriggerOutcomes(ctx, initial, outcomes)
			h.commentTriggerResponse(newRequest(http.MethodPost, "/", nil), commentToResponse(initial, nil, nil), saved, outcomes, "member", testUserID)
			current, err := h.Queries.GetComment(ctx, initial.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantSaved := change == "metadata" || change == "unchanged"
			if wantSaved {
				if !reflect.DeepEqual(storedCommentTriggerOutcomes(current), outcomes) || broadcasts != 1 {
					t.Errorf("receipt was lost: outcomes=%+v broadcasts=%d", storedCommentTriggerOutcomes(current), broadcasts)
				}
			} else if len(storedCommentTriggerOutcomes(current)) != 0 || broadcasts != 0 {
				t.Errorf("stale receipt was saved or broadcast: outcomes=%+v broadcasts=%d", storedCommentTriggerOutcomes(current), broadcasts)
			}
		})
	}
}

// TestCreateComment_MixedMentionSurfacesPartialTriggerOutcomes is the MUL-4525 §2
// acceptance test for Bohan's exact scenario: a comment @mentions an agent the
// author can invoke AND a squad whose private leader they cannot. The comment is
// still saved (one blocked mention must not reject it), and the response carries
// per-target outcomes — queued for the allowed agent, blocked +
// invocation_not_allowed for the squad — so the client can show partial success
// instead of a silent no-op. The preview surfaces the same split before sending.
func TestCreateComment_MixedMentionSurfacesPartialTriggerOutcomes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	allowedAgentID := createHandlerTestAgent(t, "Outcome Allowed Agent", nil)
	// A private leader owned by someone other than testUserID: the workspace
	// owner can VIEW it but cannot INVOKE it (no admin bypass).
	privateLeaderID, _, _ := privateAgentTestFixture(t)
	squadID := createCommentTriggerPreviewSquad(t, "Outcome Private Squad", privateLeaderID)
	issueID := createCommentTriggerPreviewIssue(t, "mixed mention partial outcomes", "", "")

	content := fmt.Sprintf(
		"[@Allowed](mention://agent/%s) [@Squad](mention://squad/%s) please take a look",
		allowedAgentID, squadID,
	)

	// Preview surfaces both the allowed agent and the blocked squad.
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	requirePreviewAgents(t, preview, allowedAgentID)
	if len(preview.Blocked) != 1 {
		t.Fatalf("preview blocked = %+v, want 1 entry", preview.Blocked)
	}
	if b := preview.Blocked[0]; b.TargetType != "squad" || b.TargetID != squadID ||
		b.Status != DispatchBlocked || b.ReasonCode != ReasonInvocationNotAllowed {
		t.Fatalf("preview blocked[0] = %+v, want squad %s blocked/invocation_not_allowed", b, squadID)
	}

	// Create the comment: it must save (201) and report partial outcomes.
	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201 (comment must save despite blocked mention), got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if resp.ID == "" {
		t.Fatal("comment was not saved")
	}
	if len(resp.TriggerOutcomes) != 2 {
		t.Fatalf("trigger_outcomes = %+v, want 2 (one queued, one blocked)", resp.TriggerOutcomes)
	}

	allowed := findCommentOutcome(t, resp.TriggerOutcomes, allowedAgentID)
	if allowed.TargetType != "agent" || allowed.Status != DispatchQueued {
		t.Errorf("allowed outcome = %+v, want agent/queued", allowed)
	}
	blocked := findCommentOutcome(t, resp.TriggerOutcomes, squadID)
	if blocked.TargetType != "squad" || blocked.Status != DispatchBlocked || blocked.ReasonCode != ReasonInvocationNotAllowed {
		t.Errorf("blocked outcome = %+v, want squad/blocked/invocation_not_allowed", blocked)
	}

	// The allowed agent ran; the private leader was never enqueued.
	if got := countQueuedCommentTriggerTasks(t, issueID, allowedAgentID); got != 1 {
		t.Errorf("allowed agent queued tasks = %d, want 1", got)
	}
	var leaderTasks int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, privateLeaderID).Scan(&leaderTasks); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderTasks != 0 {
		t.Errorf("blocked private leader tasks = %d, want 0", leaderTasks)
	}
}

// TestCreateComment_BlockedMentionReasonDoesNotEnumeratePrivateAgent pins the
// enumeration-safety rule (MUL-4525 §2): a mention the author cannot invoke and a
// mention of a truly nonexistent agent both return the same generic
// invocation_not_allowed, so a blocked reason can never confirm a private
// agent's existence.
func TestCreateComment_BlockedMentionReasonDoesNotEnumeratePrivateAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	privateAgentID, _, _ := privateAgentTestFixture(t)
	issueID := createCommentTriggerPreviewIssue(t, "blocked mention enumeration safety", "", "")
	nonexistentID := "00000000-0000-0000-0000-0000000000ff"

	content := fmt.Sprintf(
		"[@Private](mention://agent/%s) [@Ghost](mention://agent/%s) ping",
		privateAgentID, nonexistentID,
	)
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	if len(preview.Agents) != 0 {
		t.Fatalf("preview agents = %+v, want none", preview.Agents)
	}
	if len(preview.Blocked) != 2 {
		t.Fatalf("preview blocked = %+v, want 2", preview.Blocked)
	}
	for _, b := range preview.Blocked {
		if b.ReasonCode != ReasonInvocationNotAllowed {
			t.Errorf("blocked %s reason = %q, want invocation_not_allowed (must not distinguish private-exists from not-found)", b.TargetID, b.ReasonCode)
		}
	}
	var created CommentResponse
	testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": content,
	}), "id", issueID)).Want(http.StatusCreated).JSON(&created)
	_, comments := listComments(t, issueID, "")
	if len(comments) != 1 || !reflect.DeepEqual(comments[0].TriggerOutcomes, preview.Blocked) {
		t.Errorf("saved outcomes disclose different results: %+v", comments)
	}
}

// TestCreateComment_AgentAndSameLeaderSquad is Elon's round-3 must-fix 1
// acceptance test: when a comment names BOTH @Agent A and @Squad S whose leader
// is A, the run coalesces to ONE task that carries the LEADER role
// (is_leader_task + squad_id=S, so the daemon injects S's briefing) regardless
// of mention order, and each explicitly-named target still gets its own outcome
// (MUL-4525). The old first-mention-wins dedup could drop the leader role when
// @Agent A came first — this asserts the role independent of order.
func TestCreateComment_AgentAndSameLeaderSquad(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	cases := []struct {
		name    string
		content func(agentID, squadID string) string
	}{
		{"agent mention first", func(a, s string) string {
			return fmt.Sprintf("[@A](mention://agent/%s) [@S](mention://squad/%s) please", a, s)
		}},
		{"squad mention first", func(a, s string) string {
			return fmt.Sprintf("[@S](mention://squad/%s) [@A](mention://agent/%s) please", s, a)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentID := createHandlerTestAgent(t, "Shared Leader Agent "+tc.name, nil)
			squadID := createCommentTriggerPreviewSquad(t, "Shared Leader Squad "+tc.name, agentID)
			issueID := createCommentTriggerPreviewIssue(t, "same-leader "+tc.name, "", "")

			w := httptest.NewRecorder()
			r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": tc.content(agentID, squadID)}), "id", issueID)
			testHandler.CreateComment(w, r)
			if w.Code != http.StatusCreated {
				t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
			}
			var resp CommentResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode comment: %v", err)
			}

			// One coalesced execution carrying the leader role for squad S.
			var taskCount int
			var isLeader bool
			var taskSquadID string
			if err := testPool.QueryRow(ctx, `
				SELECT count(*), COALESCE(bool_or(is_leader_task), false), COALESCE(max(squad_id::text), '')
				FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
			`, issueID, agentID).Scan(&taskCount, &isLeader, &taskSquadID); err != nil {
				t.Fatalf("read task role: %v", err)
			}
			if taskCount != 1 {
				t.Fatalf("queued tasks = %d, want 1 (coalesced execution)", taskCount)
			}
			if !isLeader {
				t.Errorf("execution is_leader_task = false, want true (leader role must win regardless of order)")
			}
			if taskSquadID != squadID {
				t.Errorf("execution squad_id = %q, want %q", taskSquadID, squadID)
			}

			// Two outcomes — one per explicitly-named target — both queued.
			if len(resp.TriggerOutcomes) != 2 {
				t.Fatalf("trigger_outcomes = %+v, want 2 (agent + squad)", resp.TriggerOutcomes)
			}
			if o := findCommentOutcome(t, resp.TriggerOutcomes, agentID); o.TargetType != "agent" || o.Status != DispatchQueued {
				t.Errorf("agent outcome = %+v, want agent/queued", o)
			}
			if o := findCommentOutcome(t, resp.TriggerOutcomes, squadID); o.TargetType != "squad" || o.Status != DispatchQueued {
				t.Errorf("squad outcome = %+v, want squad/queued", o)
			}
		})
	}
}

// TestCreateComment_TwoSquadsSharingLeaderCoalescesNonWinner is Elon's round-3
// must-fix 1 (multi-squad case): two DIFFERENT squads share the same leader and
// both are @mentioned. The single leader agent runs ONCE carrying one squad's
// context; the other squad's mention folds into that run and is reported
// coalesced — never a second task, and never both reported queued (MUL-4525).
func TestCreateComment_TwoSquadsSharingLeaderCoalescesNonWinner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Two-Squad Shared Leader", nil)
	squad1 := createCommentTriggerPreviewSquad(t, "Two-Squad S1", leaderID)
	squad2 := createCommentTriggerPreviewSquad(t, "Two-Squad S2", leaderID)
	issueID := createCommentTriggerPreviewIssue(t, "two squads sharing leader", "", "")
	content := fmt.Sprintf("[@S1](mention://squad/%s) [@S2](mention://squad/%s) please", squad1, squad2)

	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}

	// Exactly one queued task (a single leader agent can only run once).
	if got := countQueuedCommentTriggerTasks(t, issueID, leaderID); got != 1 {
		t.Fatalf("queued tasks = %d, want 1", got)
	}
	// Two squad outcomes; exactly one queued (the executed squad) and one
	// coalesced (the folded squad) — never both queued.
	if len(resp.TriggerOutcomes) != 2 {
		t.Fatalf("trigger_outcomes = %+v, want 2", resp.TriggerOutcomes)
	}
	statuses := map[DispatchStatus]int{}
	for _, o := range resp.TriggerOutcomes {
		if o.TargetType != "squad" {
			t.Errorf("outcome %+v: want squad target", o)
		}
		statuses[o.Status]++
	}
	if statuses[DispatchQueued] != 1 || statuses[DispatchCoalesced] != 1 {
		t.Errorf("outcome statuses = %v, want exactly 1 queued + 1 coalesced (not both queued)", statuses)
	}
}

// TestCreateComment_SquadLeaderSelfMentionCompletedTaskDoesNotFakeSuccess is
// Elon's round-3 must-fix 2: a squad leader's own @mention of its squad is
// suppressed by the self-trigger guard, but when its latest task is already
// TERMINAL (no active run), the outcome must NOT be a success-shaped `deferred`
// — it is a non-success `blocked/self_trigger_suppressed`, and no new task runs.
func TestCreateComment_SquadLeaderSelfMentionCompletedTaskDoesNotFakeSuccess(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderID := createHandlerTestAgent(t, "Self-Mention Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Self-Mention Squad", leaderID)
	issueID := createCommentTriggerPreviewIssue(t, "self-mention completed task", "", "")

	// The leader's latest task on this issue is a COMPLETED leader task: the
	// self-trigger guard suppresses (latest role is leader) AND there is no
	// active run to cover the comment.
	var completedTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, is_leader_task, squad_id, started_at, completed_at)
		VALUES ($1, $2, 'completed', 0, $3, true, $4, now(), now())
		RETURNING id
	`, leaderID, handlerTestRuntimeID(t), issueID, squadID).Scan(&completedTaskID); err != nil {
		t.Fatalf("seed completed leader task: %v", err)
	}

	content := fmt.Sprintf("[@S](mention://squad/%s) revisit please", squadID)
	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	// Author the comment AS the leader agent (A2A self-mention).
	r.Header.Set("X-Agent-ID", leaderID)
	r.Header.Set("X-Task-ID", completedTaskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}

	// The self-mention neither re-fired the leader nor is covered by an active
	// run: the outcome is non-success, never a fake `deferred`.
	if len(resp.TriggerOutcomes) != 1 {
		t.Fatalf("trigger_outcomes = %+v, want 1", resp.TriggerOutcomes)
	}
	o := resp.TriggerOutcomes[0]
	if o.TargetType != "squad" || o.TargetID != squadID {
		t.Fatalf("outcome target = %+v, want squad %s", o, squadID)
	}
	if o.Status != DispatchBlocked || o.ReasonCode != ReasonSelfTriggerSuppressed {
		t.Errorf("outcome = %+v, want blocked/self_trigger_suppressed (must not fake success)", o)
	}
	// No new task was enqueued (only the pre-seeded completed one exists).
	var total int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, leaderID).Scan(&total); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if total != 1 {
		t.Errorf("leader tasks = %d, want 1 (self-mention suppressed, no new task)", total)
	}
}
