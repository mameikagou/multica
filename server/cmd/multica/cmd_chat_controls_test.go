package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

const chatControlHeadID = "018f3caa-0000-7000-8000-000000000002"
const chatControlQueuedID = "018f3caa-0000-7000-8000-000000000003"

func chatControlPendingJSON(headID string) string {
	if headID == "" {
		return `{"supports_queue":true}`
	}
	return fmt.Sprintf(`{"task_id":%q,"status":"running","supports_queue":true,"queued_tasks":[{"task_id":%q,"status":"queued","created_at":"2026-10-06T01:00:00Z","content":"Next reply"}]}`, headID, chatControlQueuedID)
}

func chatControlTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mat_chat_test" || r.Header.Get("X-Workspace-ID") != "ws-1" || r.Header.Get("X-Agent-ID") != "agent-chat-test" || r.Header.Get("X-Task-ID") != "task-chat-test" {
			t.Error("missing task-scoped authorization or attribution")
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	chatSessionTestEnv(t, srv.URL)
	return srv
}

func TestChatControlsQueue(t *testing.T) {
	for _, output := range []string{"json", "table"} {
		t.Run(output, func(t *testing.T) {
			chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != chatSessionPath(chatSessionTestID)+"/pending-task" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				fmt.Fprint(w, chatControlPendingJSON(chatControlHeadID))
			})
			out, err := executeChatSessionTest(t, newChatQueueCmd(), chatSessionTestID, "--output", output)
			if err != nil || !strings.Contains(out, chatControlHeadID) || !strings.Contains(out, chatControlQueuedID) {
				t.Fatalf("out=%s err=%v", out, err)
			}
		})
	}
}

func TestChatControlsDryRunDoesNotMutate(t *testing.T) {
	for _, args := range [][]string{
		{"clear", chatSessionTestID}, {"now", chatSessionTestID, chatControlQueuedID},
		{"cancel", chatSessionTestID, chatControlQueuedID}, {"cancel", chatSessionTestID, chatControlQueuedID, "--edit"},
		{"stop", chatSessionTestID}, {"stop", chatSessionTestID, "--clear-queue"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Errorf("dry run mutated state: %s %s", r.Method, r.URL)
				}
				fmt.Fprint(w, chatControlPendingJSON(chatControlHeadID))
			})
			cmd := newChatQueueCmd()
			if args[0] == "stop" {
				cmd = newChatStopCmd()
				args = args[1:]
			}
			out, err := executeChatSessionTest(t, cmd, append(args, "--dry-run")...)
			if err != nil || !strings.Contains(out, `"dry_run": true`) || !strings.Contains(out, `"requests"`) {
				t.Fatalf("out=%s err=%v", out, err)
			}
		})
	}
}

func TestChatControlsInvalidInputDoesNotRequest(t *testing.T) {
	for _, args := range [][]string{
		{"clear", "../other"}, {"now", chatSessionTestID, "short-id"},
		{"cancel", "bad", chatControlQueuedID}, {chatSessionTestID, "--output", "bad"},
		{"clear"}, {"now", chatSessionTestID}, {"cancel", chatSessionTestID, chatControlQueuedID, "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("invalid input made request: %s", r.URL) })
			_, err := executeChatSessionTest(t, newChatQueueCmd(), args...)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestChatControlsClearPreservesHead(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == "GET" {
			fmt.Fprint(w, chatControlPendingJSON(chatControlHeadID))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	out, err := executeChatSessionTest(t, newChatQueueCmd(), "clear", chatSessionTestID)
	mu.Lock()
	defer mu.Unlock()
	if err != nil || !strings.Contains(out, `"head_preserved": true`) {
		t.Fatalf("out=%s err=%v", out, err)
	}
	want := "GET " + chatSessionPath(chatSessionTestID) + "/pending-task|DELETE " + chatSessionPath(chatSessionTestID) + "/queued-tasks"
	if strings.Join(requests, "|") != want {
		t.Fatalf("requests=%v", requests)
	}
}

func TestChatControlsNowOrderAndFailures(t *testing.T) {
	for _, test := range []struct {
		name, response            string
		firstStatus, cancelStatus int
		wantCalls                 int
		wantError                 string
	}{
		{"success", fmt.Sprintf(`{"task_id":%q,"active_task_id":%q}`, chatControlQueuedID, chatControlHeadID), 200, 200, 2, ""},
		{"priority-rejected", `{"error":"already started"}`, 409, 200, 1, "no cancel request was sent"},
		{"priority-server-error", `{}`, 500, 200, 1, "did not retry"},
		{"cancel-server-error", fmt.Sprintf(`{"task_id":%q,"active_task_id":%q}`, chatControlQueuedID, chatControlHeadID), 200, 500, 2, "follow-up was prioritized"},
		{"missing-active", fmt.Sprintf(`{"task_id":%q}`, chatControlQueuedID), 200, 200, 1, "no run was targeted"},
		{"bad-active", fmt.Sprintf(`{"task_id":%q,"active_task_id":"../x"}`, chatControlQueuedID), 200, 200, 1, "no run was targeted"},
		{"same-active", fmt.Sprintf(`{"task_id":%q,"active_task_id":%q}`, chatControlQueuedID, chatControlQueuedID), 200, 200, 1, "no run was targeted"},
		{"wrong-task", fmt.Sprintf(`{"task_id":%q,"active_task_id":%q}`, chatSessionTestID, chatControlHeadID), 200, 200, 1, "no run was targeted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			calls := 0
			chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				calls++
				if r.Method != "POST" {
					t.Errorf("unexpected method %s", r.Method)
				}
				if calls == 1 {
					if r.URL.Path != chatSessionPath(chatSessionTestID)+"/queued-tasks/"+chatControlQueuedID+"/prioritize" {
						t.Errorf("wrong prioritize path %s", r.URL)
					}
					w.WriteHeader(test.firstStatus)
					fmt.Fprint(w, test.response)
					return
				}
				if r.URL.Path != "/api/tasks/"+chatControlHeadID+"/cancel" {
					t.Errorf("wrong interrupt target %s", r.URL)
				}
				w.WriteHeader(test.cancelStatus)
				fmt.Fprintf(w, `{"id":%q,"status":"cancelled"}`, chatControlHeadID)
			})
			out, err := executeChatSessionTest(t, newChatQueueCmd(), "now", chatSessionTestID, chatControlQueuedID)
			mu.Lock()
			defer mu.Unlock()
			if calls != test.wantCalls {
				t.Fatalf("calls=%d want=%d", calls, test.wantCalls)
			}
			if test.wantError == "" {
				if err != nil || !strings.Contains(out, `"prioritized": true`) {
					t.Fatalf("out=%s err=%v", out, err)
				}
			} else if err == nil || out != "" || !strings.Contains(cli.FormatError(err, false), test.wantError) {
				t.Fatalf("out=%s err=%v formatted=%s", out, err, cli.FormatError(err, false))
			}
		})
	}
}

func TestChatControlsNowDroppedResponseDoesNotRetry(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err := executeChatSessionTest(t, newChatQueueCmd(), "now", chatSessionTestID, chatControlQueuedID)
	mu.Lock()
	defer mu.Unlock()
	if err == nil || calls != 1 || !strings.Contains(cli.FormatError(err, false), "did not retry") {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestChatControlsCancelGuardsAndDraft(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(fmt.Sprint(edit), func(t *testing.T) {
			chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				action := "remove"
				if edit {
					action = "edit"
				}
				if r.Method != "POST" || r.URL.Path != "/api/tasks/"+chatControlQueuedID+"/cancel" || q.Get("expected_status") != "queued" || q.Get("chat_session_id") != chatSessionTestID || q.Get("queue_action") != action {
					t.Errorf("unguarded cancel: %s %s", r.Method, r.URL)
				}
				fmt.Fprintf(w, `{"id":%q,"status":"cancelled","cancelled_chat_message":{"content":"original draft","attachments":[{"id":"attachment-1"}],"restore_to_input":true}}`, chatControlQueuedID)
			})
			args := []string{"cancel", chatSessionTestID, chatControlQueuedID}
			if edit {
				args = append(args, "--edit")
			}
			out, err := executeChatSessionTest(t, newChatQueueCmd(), args...)
			if err != nil || !strings.Contains(out, "original draft") || !strings.Contains(out, "attachment-1") {
				t.Fatalf("out=%s err=%v", out, err)
			}
		})
	}
}

func TestChatControlsStopAndHeadRace(t *testing.T) {
	for _, test := range []struct {
		name           string
		clear          bool
		initial, fresh string
		deleteStatus   int
		wantCancel     bool
		wantError      string
	}{
		{"stop-head-only", false, chatControlHeadID, chatControlHeadID, 204, true, ""},
		{"clear-then-stop", true, chatControlHeadID, chatControlHeadID, 204, true, ""},
		{"head-changed", true, chatControlHeadID, chatSessionTestID, 204, false, "head run changed"},
		{"head-finished", true, chatControlHeadID, "", 204, false, ""},
		{"idle", false, "", "", 204, false, ""},
		{"clear-failed", true, chatControlHeadID, chatControlHeadID, 500, false, "no cancel request was sent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []string
			gets, cancels := 0, 0
			chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch r.Method {
				case "GET":
					gets++
					head := test.initial
					if gets > 1 {
						head = test.fresh
					}
					fmt.Fprint(w, chatControlPendingJSON(head))
				case "DELETE":
					w.WriteHeader(test.deleteStatus)
				case "POST":
					cancels++
					if r.URL.Path != "/api/tasks/"+test.initial+"/cancel" {
						t.Errorf("wrong stop target %s", r.URL)
					}
					fmt.Fprintf(w, `{"id":%q,"status":"cancelled"}`, test.initial)
				}
			})
			args := []string{chatSessionTestID}
			if test.clear {
				args = append(args, "--clear-queue")
			}
			out, err := executeChatSessionTest(t, newChatStopCmd(), args...)
			mu.Lock()
			defer mu.Unlock()
			if (cancels == 1) != test.wantCancel || cancels > 1 {
				t.Fatalf("requests=%v", requests)
			}
			if test.clear && test.wantCancel && strings.Join(requests, "|") != "GET "+chatSessionPath(chatSessionTestID)+"/pending-task|DELETE "+chatSessionPath(chatSessionTestID)+"/queued-tasks|GET "+chatSessionPath(chatSessionTestID)+"/pending-task|POST /api/tasks/"+chatControlHeadID+"/cancel" {
				t.Fatalf("wrong stop order %v", requests)
			}
			if test.wantError == "" {
				if err != nil || !strings.Contains(out, `"cancel_requested"`) {
					t.Fatalf("out=%s err=%v", out, err)
				}
			} else if err == nil || !strings.Contains(cli.FormatError(err, false), test.wantError) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestChatControlsMalformedPendingDoesNotMutate(t *testing.T) {
	for _, response := range []string{`null`, `not-json`, `{"task_id":"../other","status":"running","supports_queue":true}`, `{"task_id":"` + chatControlHeadID + `","status":"finished","supports_queue":true}`, `{"supports_queue":true,"queued_tasks":[{"task_id":"` + chatControlQueuedID + `","status":"queued"}]}`} {
		t.Run(response, func(t *testing.T) {
			chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Errorf("malformed snapshot led to mutation: %s", r.Method)
				}
				fmt.Fprint(w, response)
			})
			_, err := executeChatSessionTest(t, newChatStopCmd(), chatSessionTestID, "--clear-queue")
			if err == nil {
				t.Fatal("expected malformed snapshot error")
			}
		})
	}
}

func TestChatControlsCommandRegistration(t *testing.T) {
	for _, path := range [][]string{{"queue"}, {"queue", "clear"}, {"queue", "now"}, {"queue", "cancel"}, {"stop"}} {
		cmd, _, err := chatCmd.Find(path)
		if err != nil || cmd.Name() != path[len(path)-1] {
			t.Fatalf("missing command %v: %v", path, err)
		}
	}
}

func TestChatControlsDeferredHead(t *testing.T) {
	chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("unexpected mutation: %s", r.Method)
		}
		fmt.Fprint(w, strings.Replace(chatControlPendingJSON(chatControlHeadID), `"status":"running"`, `"status":"deferred"`, 1))
	})
	out, err := executeChatSessionTest(t, newChatStopCmd(), chatSessionTestID, "--dry-run")
	if err != nil || !strings.Contains(out, "/api/tasks/"+chatControlHeadID+"/cancel") {
		t.Fatalf("out=%s err=%v", out, err)
	}
	_, err = executeChatSessionTest(t, newChatQueueCmd(), "now", chatSessionTestID, chatControlQueuedID, "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "no active run eligible") {
		t.Fatalf("err=%v", err)
	}
}

func TestChatControlsMalformedCancelAcknowledgement(t *testing.T) {
	for _, response := range []string{`null`, `{}`, `{"id":"` + chatControlHeadID + `","status":"cancelled"}`, `{"id":"` + chatControlQueuedID + `"}`, `not-json`} {
		t.Run(response, func(t *testing.T) {
			var mu sync.Mutex
			calls := 0
			chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls++
				mu.Unlock()
				fmt.Fprint(w, response)
			})
			out, err := executeChatSessionTest(t, newChatQueueCmd(), "cancel", chatSessionTestID, chatControlQueuedID)
			mu.Lock()
			defer mu.Unlock()
			if err == nil || out != "" || calls != 1 || !strings.Contains(cli.FormatError(err, false), "Cancellation may or may not have applied") {
				t.Fatalf("out=%s err=%v calls=%d", out, err, calls)
			}
		})
	}
}

func TestChatControlsPermissionFailureDoesNotFallback(t *testing.T) {
	for _, status := range []int{401, 403, 409} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var mu sync.Mutex
			calls := 0
			chatControlTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls++
				mu.Unlock()
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":"not allowed"}`)
			})
			out, err := executeChatSessionTest(t, newChatQueueCmd(), "cancel", chatSessionTestID, chatControlQueuedID)
			mu.Lock()
			defer mu.Unlock()
			if err == nil || out != "" || calls != 1 {
				t.Fatalf("out=%s err=%v calls=%d", out, err, calls)
			}
		})
	}
}
