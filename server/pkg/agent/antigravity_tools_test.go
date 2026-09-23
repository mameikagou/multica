package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAntigravityToolMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		steps []string
		want  []Message
	}{
		{
			name: "repeated and interleaved snapshots",
			steps: []string{
				`{"step_index":0,"state":"ACTIVE","step_type":"tool","tool_name":"read_file","tool_info":{"parameters":{"path":"a.go"}}}`,
				`{"step_index":0,"state":"ACTIVE","step_type":"tool","tool_name":"read_file"}`,
				`{"step_index":1,"state":"ACTIVE","step_type":"tool","tool_info":{"name":"run_command"}}`,
				`{"step_index":1,"state":"DONE","step_type":"tool","tool_info":{"output":"ok"}}`,
				`{"step_index":0,"state":"DONE","step_type":"tool","tool_info":{"output":{"text":"source"}}}`,
				`{"step_index":0,"state":"DONE","step_type":"tool","tool_name":"read_file"}`,
				`{"step_index":1,"state":"ACTIVE","step_type":"tool","tool_name":"run_command"}`,
			},
			want: []Message{
				{Type: MessageToolUse, CallID: "agy-step-0", Tool: "read_file", Input: map[string]any{"path": "a.go"}},
				{Type: MessageToolUse, CallID: "agy-step-1", Tool: "run_command"},
				{Type: MessageToolResult, CallID: "agy-step-1", Tool: "run_command", Output: "ok"},
				{Type: MessageToolResult, CallID: "agy-step-0", Tool: "read_file", Output: `{"text":"source"}`},
			},
		},
		{
			name:  "done only tool failure remains a tool result",
			steps: []string{`{"step_index":2,"state":"DONE","step_type":"tool","tool_info":{"name":"run_command","output":"partial","error":{"type":"exit","message":"exit 1"}}}`},
			want: []Message{
				{Type: MessageToolUse, CallID: "agy-step-2", Tool: "run_command"},
				{Type: MessageToolResult, CallID: "agy-step-2", Tool: "run_command", Output: "partial\nTool error: exit: exit 1"},
			},
		},
		{
			name: "missing metadata can arrive later",
			steps: []string{
				`{"step_index":3,"state":"ACTIVE","step_type":"tool"}`,
				`{"step_index":3,"state":"DONE","step_type":"tool","tool_name":"list_dir","tool_info":{"output":null}}`,
			},
			want: []Message{
				{Type: MessageToolUse, CallID: "agy-step-3", Tool: "list_dir"},
				{Type: MessageToolResult, CallID: "agy-step-3", Tool: "list_dir"},
			},
		},
		{
			name: "ignore missing indices unknown states and non tools",
			steps: []string{
				`{"state":"DONE","step_type":"tool","tool_name":"read_file"}`,
				`{"step_index":-1,"state":"DONE","step_type":"tool","tool_name":"read_file"}`,
				`{"step_index":0,"state":"QUEUED","step_type":"tool","tool_name":"read_file"}`,
				`{"step_index":0,"state":"DONE","step_type":"agent_response","text_delta":"hello"}`,
			},
		},
		{
			name:  "unfinished tool is not fabricated as complete",
			steps: []string{`{"step_index":4,"state":"ACTIVE","step_type":"tool","tool_name":"read_file"}`},
			want:  []Message{{Type: MessageToolUse, CallID: "agy-step-4", Tool: "read_file"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			states := make(map[int]antigravityToolState)
			var got []Message
			for _, raw := range tt.steps {
				var step antigravityStreamStepUpdate
				if err := json.Unmarshal([]byte(raw), &step); err != nil {
					t.Fatal(err)
				}
				got = append(got, antigravityToolMessages(&step, states)...)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("messages = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// The fake stays alive until the test receives its tool event. This proves
// delivery is live, rather than reconstructed after process completion.
func TestAntigravityToolsStreamBeforeProcessExit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	fakePath := filepath.Join(dir, "agy")
	writeTestExecutable(t, fakePath, []byte(fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"event":"step_update","step_update":{"step_index":4,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"echo hello"}}}}'
while [ ! -f %q ]; do sleep 0.01; done
printf '%%s\n' '{"event":"step_update","step_update":{"step_index":4,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"output":"hello\n"}}}'
printf '%%s\n' '{"event":"result","result":{"status":"SUCCESS","response":"Finished"}}'
`, release)))
	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "ignored", ExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var use, result *Message
	for msg := range session.Messages {
		switch msg.Type {
		case MessageToolUse:
			if use != nil {
				t.Fatal("duplicate tool use")
			}
			copy := msg
			use = &copy
			if err := os.WriteFile(release, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		case MessageToolResult:
			copy := msg
			result = &copy
		}
	}
	if use == nil || result == nil {
		t.Fatalf("missing live tool lifecycle: use=%+v result=%+v", use, result)
	}
	if use.Tool != "run_command" || use.Input["CommandLine"] != "echo hello" || use.CallID == "" {
		t.Fatalf("unexpected tool use: %+v", use)
	}
	if result.CallID != use.CallID || result.Tool != use.Tool || result.Output != "hello\n" {
		t.Fatalf("unexpected tool result: %+v", result)
	}
	if got := <-session.Result; got.Status != "completed" || got.Output != "Finished" {
		t.Fatalf("unexpected execution result: %+v", got)
	}
}
