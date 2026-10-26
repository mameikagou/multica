package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestCreateAgent_CustomArgs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "custom-args-claude", testutil.Cols{"provider": "claude"})
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"permission mode", []string{"--permission-mode", "dontAsk", "--tools", "Bash"}, http.StatusBadRequest},
		{"inline permission mode", []string{"--permission-mode=dontAsk"}, http.StatusBadRequest},
		{"quoted permission mode", []string{"'--permission-mode'", "dontAsk"}, http.StatusBadRequest},
		{"allowed arguments", []string{"--tools", "Bash", "--allowedTools", "Bash(multica issue *)"}, http.StatusCreated},
		{"empty arguments", []string{}, http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "custom-args-create-" + tc.name
			dbfx.Cleanup(t, `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name)
			w := httptest.NewRecorder()
			testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", map[string]any{
				"name": name, "runtime_id": runtimeID, "custom_args": tc.args,
			}))
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if tc.want == http.StatusBadRequest {
				if !strings.Contains(w.Body.String(), "--permission-mode") {
					t.Fatalf("error must identify the rejected flag: %s", w.Body.String())
				}
				if count := dbfx.Count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name); count != 0 {
					t.Fatalf("rejected create persisted %d agents", count)
				}
			}
		})
	}
}

func TestUpdateAgent_CustomArgs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	claudeID := dbfx.Runtime(t, "custom-args-claude", testutil.Cols{"provider": "claude"})
	codexID := dbfx.Runtime(t, "custom-args-codex", testutil.Cols{"provider": "codex"})
	allowed := []string{"--verbose"}
	blocked := []string{"--permission-mode", "dontAsk"}
	for _, tc := range []struct {
		name       string
		runtimeID  string
		storedArgs []string
		body       map[string]any
		want       int
		wantArgs   []string
	}{
		{"reject update", claudeID, allowed, map[string]any{"custom_args": blocked}, http.StatusBadRequest, allowed},
		{"reject runtime switch", codexID, blocked, map[string]any{"runtime_id": claudeID}, http.StatusBadRequest, blocked},
		{"switch and clear", codexID, blocked, map[string]any{"runtime_id": claudeID, "custom_args": []string{}}, http.StatusOK, []string{}},
		{"repair existing args", claudeID, blocked, map[string]any{"custom_args": allowed}, http.StatusOK, allowed},
		{"rename existing agent", claudeID, blocked, map[string]any{"name": "custom-args-renamed"}, http.StatusOK, blocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored, err := json.Marshal(tc.storedArgs)
			if err != nil {
				t.Fatal(err)
			}
			id := dbfx.Agent(t, "custom-args-update-"+tc.name, tc.runtimeID, testutil.Cols{"custom_args": stored})
			w := httptest.NewRecorder()
			testHandler.UpdateAgent(w, withURLParam(newRequest(http.MethodPut, "/api/agents/"+id, tc.body), "id", id))
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			var runtimeID string
			var args []string
			dbfx.QueryRow(t, `SELECT runtime_id::text, custom_args FROM agent WHERE id = $1`, id).Scan(&runtimeID, &args)
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("stored args = %v, want %v", args, tc.wantArgs)
			}
			if tc.want == http.StatusBadRequest && runtimeID != tc.runtimeID {
				t.Fatalf("rejected update changed runtime to %s", runtimeID)
			}
			if tc.want == http.StatusOK && tc.body["runtime_id"] != nil && runtimeID != tc.body["runtime_id"] {
				t.Fatalf("runtime = %s, want %s", runtimeID, tc.body["runtime_id"])
			}
		})
	}
}
