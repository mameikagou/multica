package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAntigravityToolsWithSlowConsumer(t *testing.T) {
	for _, cancelWhileFull := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelWhileFull), func(t *testing.T) {
			t.Parallel()
			const calls = 400 // More lifecycle events than the message buffer holds.
			var script strings.Builder
			script.WriteString("#!/bin/sh\n")
			for i := 0; i < calls; i++ {
				fmt.Fprintf(&script, "printf '%%s\\n' '{\"event\":\"step_update\",\"step_update\":{\"step_index\":%d,\"state\":\"DONE\",\"step_type\":\"tool\",\"tool_name\":\"read_file\",\"tool_info\":{\"output\":\"ok\"}}}'\n", i)
			}
			script.WriteString("printf '%s\\n' '{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"response\":\"Finished\"}}'\n")
			fakePath := filepath.Join(t.TempDir(), "agy")
			writeTestExecutable(t, fakePath, []byte(script.String()))
			backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "ignored", ExecOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for len(session.Messages) < cap(session.Messages) {
				select {
				case <-ctx.Done():
					t.Fatal("message buffer never filled")
				case <-time.After(time.Millisecond):
				}
			}
			if cancelWhileFull {
				cancel()
				select {
				case result := <-session.Result:
					if result.Status != "aborted" {
						t.Fatalf("result = %+v, want cancellation", result)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("cancellation blocked behind the full message buffer")
				}
				return
			}
			// Without backpressure the process completes here, having discarded
			// most tool events. A live consumer may temporarily pause just like this.
			select {
			case <-session.Result:
				t.Fatal("execution completed while its tool events could not be delivered")
			case <-time.After(100 * time.Millisecond):
			}
			uses, results := map[string]bool{}, map[string]bool{}
			for message := range session.Messages {
				var seen map[string]bool
				switch message.Type {
				case MessageToolUse:
					seen = uses
				case MessageToolResult:
					seen = results
				default:
					continue
				}
				if message.CallID == "" || seen[message.CallID] {
					t.Fatalf("missing or duplicate call identity: %+v", message)
				}
				seen[message.CallID] = true
			}
			if len(uses) != calls || !reflect.DeepEqual(uses, results) {
				t.Fatalf("tool events lost: uses=%d results=%d, want %d matched pairs", len(uses), len(results), calls)
			}
			if result := <-session.Result; result.Status != "completed" {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

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
