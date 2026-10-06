package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/util"
)

type chatQueuedRun struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	MessageID string `json:"message_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type chatPendingRuns struct {
	TaskID        string          `json:"task_id,omitempty"`
	Status        string          `json:"status,omitempty"`
	CreatedAt     string          `json:"created_at,omitempty"`
	WaitReason    string          `json:"wait_reason,omitempty"`
	SupportsQueue bool            `json:"supports_queue"`
	QueuedTasks   []chatQueuedRun `json:"queued_tasks,omitempty"`
}

func init() {
	chatCmd.AddCommand(newChatQueueCmd(), newChatStopCmd())
}

func newChatQueueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "queue <session-id>", Short: "Show the current run and queued follow-ups in a chat",
		Long: "Read a chat's pending runs using its full session UUID. The head run is separate from queued follow-ups. Server-side session ownership and task credential checks apply.",
		Args: exactArgs(1), RunE: runChatQueue,
	}
	cmd.Flags().String("output", "json", "Output format: json or table (full UUIDs)")
	cmd.AddCommand(newChatQueueClearCmd(), newChatQueueNowCmd(), newChatQueueCancelCmd())
	return cmd
}

func chatControlCommand(use, short, long string, argc int, run func(*cobra.Command, []string) error) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Long: long, Args: exactArgs(argc), RunE: run}
	cmd.Flags().Bool("dry-run", false, "Read state and print the planned requests without changing anything")
	return cmd
}

func newChatQueueClearCmd() *cobra.Command {
	return chatControlCommand("clear <session-id>", "Cancel queued follow-ups, preserving the head run",
		"Cancel the queued follow-ups present when the server processes the request. The head run is preserved, even if it has not started yet. This does not prevent new messages from being queued. Output is JSON. No automatic retry.", 1, runChatQueueClear)
}

func newChatQueueNowCmd() *cobra.Command {
	return chatControlCommand("now <session-id> <task-id>", "Prioritize a queued follow-up and interrupt the current run",
		"Use a queued task_id from chat queue. First promote that follow-up, then cancel the active run returned by the server, following the web Send now flow. These are separate requests; failure can leave only the priority change applied. This is not live steering or a guarantee of immediate dispatch. Output is JSON. No automatic retry.", 2, runChatQueueNow)
}

func newChatQueueCancelCmd() *cobra.Command {
	cmd := chatControlCommand("cancel <session-id> <task-id>", "Remove a queued follow-up or restore it as a draft",
		"Cancel only a still-queued run in the specified chat. Server-side expected_status and chat_session_id guards reject a run that has started or belongs elsewhere. --edit asks the server to restore its original message and attachments as a draft and returns the restore payload; it does not open an editor or resubmit. Output is JSON. No automatic retry.", 2, runChatQueueCancel)
	cmd.Flags().Bool("edit", false, "Restore the queued message as a draft instead of removing it")
	return cmd
}

func newChatStopCmd() *cobra.Command {
	cmd := chatControlCommand("stop <session-id>", "Cancel a chat's current head run",
		"Cancel the head run identified by a fresh queue read. By default follow-ups remain queued and may start next. --clear-queue clears follow-ups first, then rechecks the head identity before cancelling. These requests are not atomic and cannot prevent concurrent sends. This does not freeze a session, undo completed actions, or support resuming a paused process. Output is JSON. No automatic retry.", 1, runChatStop)
	cmd.Flags().Bool("clear-queue", false, "Clear queued follow-ups before cancelling the captured head run")
	return cmd
}

func chatControlID(raw, label string) (string, error) {
	if !uuidRegexp.MatchString(raw) {
		return "", fmt.Errorf("%s must be a full UUID", label)
	}
	id, err := util.ParseUUID(raw)
	if err != nil {
		return "", fmt.Errorf("invalid %s: %w", label, err)
	}
	return util.UUIDToString(id), nil
}

func chatControlIDs(args []string) (string, string, error) {
	sessionID, err := chatControlID(args[0], "session-id")
	if err != nil {
		return "", "", err
	}
	if len(args) == 1 {
		return sessionID, "", nil
	}
	taskID, err := chatControlID(args[1], "task-id")
	return sessionID, taskID, err
}

func chatSessionPath(sessionID string) string { return "/api/chat/sessions/" + sessionID }

func fetchChatPending(ctx context.Context, client *cli.APIClient, sessionID string) (*chatPendingRuns, error) {
	var pending *chatPendingRuns
	if err := client.GetJSON(ctx, chatSessionPath(sessionID)+"/pending-task", &pending); err != nil {
		return nil, err
	}
	if pending == nil {
		return nil, fmt.Errorf("invalid pending-task response: null")
	}
	seen := map[string]bool{}
	if pending.TaskID != "" {
		id, err := chatControlID(pending.TaskID, "head task_id")
		if err != nil {
			return nil, err
		}
		pending.TaskID = id
		switch pending.Status {
		case "queued", "dispatched", "running", "waiting_local_directory", "deferred":
		default:
			return nil, fmt.Errorf("invalid pending head status %q", pending.Status)
		}
		seen[id] = true
	} else if pending.Status != "" || len(pending.QueuedTasks) > 0 {
		return nil, fmt.Errorf("pending-task response is missing the head task_id")
	}
	for i := range pending.QueuedTasks {
		run := &pending.QueuedTasks[i]
		id, err := chatControlID(run.TaskID, "queued task_id")
		if err != nil {
			return nil, err
		}
		if run.Status != "queued" || seen[id] {
			return nil, fmt.Errorf("invalid queued run in pending-task response")
		}
		run.TaskID = id
		seen[id] = true
	}
	return pending, nil
}

func chatControlError(step, state string, err error) error {
	return cli.WithUserMessage(step+" failed. "+state+" Check chat queue before taking further action; this command did not retry.", err)
}

func requestChatCancel(ctx context.Context, client *cli.APIClient, path, taskID string) (map[string]any, error) {
	var result map[string]any
	if err := client.PostJSON(ctx, path, nil, &result); err != nil {
		return nil, err
	}
	id, err := chatControlID(strVal(result, "id"), "cancelled run id")
	if err != nil || id != taskID || strVal(result, "status") == "" {
		return nil, fmt.Errorf("cancellation response is missing a matching run id or status")
	}
	return result, nil
}

type chatControlRequest struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

func printChatControlPlan(sessionID string, pending *chatPendingRuns, requests []chatControlRequest) error {
	return cli.PrintJSON(os.Stdout, map[string]any{"dry_run": true, "session_id": sessionID, "snapshot": pending, "requests": requests})
}

func runChatQueue(cmd *cobra.Command, args []string) error {
	output, err := chatSessionOutput(cmd)
	if err != nil {
		return err
	}
	sessionID, _, err := chatControlIDs(args)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	pending, err := fetchChatPending(ctx, client, sessionID)
	if err != nil {
		return fmt.Errorf("read chat queue: %w", err)
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, pending)
	}
	rows := [][]string{}
	if pending.TaskID != "" {
		rows = append(rows, []string{"head", pending.TaskID, pending.Status, pending.CreatedAt, pending.WaitReason})
	}
	for _, run := range pending.QueuedTasks {
		rows = append(rows, []string{"follow-up", run.TaskID, run.Status, run.CreatedAt, run.Content})
	}
	cli.PrintTable(os.Stdout, []string{"POSITION", "RUN_ID", "STATUS", "CREATED", "CONTENT_OR_WAIT_REASON"}, rows)
	return nil
}

func runChatQueueClear(cmd *cobra.Command, args []string) error {
	sessionID, _, err := chatControlIDs(args)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	pending, err := fetchChatPending(ctx, client, sessionID)
	if err != nil {
		return err
	}
	if !pending.SupportsQueue {
		return fmt.Errorf("server does not report queue support")
	}
	path := chatSessionPath(sessionID) + "/queued-tasks"
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		return printChatControlPlan(sessionID, pending, []chatControlRequest{{"DELETE", path}})
	}
	if err := client.DeleteJSON(ctx, path); err != nil {
		return chatControlError("Clear queue", "Queued follow-ups may have been cancelled; the head was not targeted.", err)
	}
	return cli.PrintJSON(os.Stdout, map[string]any{"session_id": sessionID, "queue_cleared": true, "head_preserved": true})
}

func runChatQueueNow(cmd *cobra.Command, args []string) error {
	sessionID, taskID, err := chatControlIDs(args)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	path := chatSessionPath(sessionID) + "/queued-tasks/" + taskID + "/prioritize"
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		pending, err := fetchChatPending(ctx, client, sessionID)
		if err != nil {
			return err
		}
		if !pending.SupportsQueue || pending.TaskID == "" || pending.Status == "queued" || pending.Status == "deferred" {
			return fmt.Errorf("no active run eligible for Send now")
		}
		found := false
		for _, run := range pending.QueuedTasks {
			if run.TaskID == taskID {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("task-id is not a queued follow-up in this chat")
		}
		return printChatControlPlan(sessionID, pending, []chatControlRequest{{"POST", path}, {"POST", "/api/tasks/" + pending.TaskID + "/cancel"}})
	}
	var prioritized struct {
		TaskID       string `json:"task_id"`
		ActiveTaskID string `json:"active_task_id"`
	}
	if err := client.PostJSON(ctx, path, nil, &prioritized); err != nil {
		return chatControlError("Prioritize follow-up", "Priority may have changed; no cancel request was sent.", err)
	}
	returnedID, idErr := chatControlID(prioritized.TaskID, "prioritized task_id")
	activeID, activeErr := chatControlID(prioritized.ActiveTaskID, "active_task_id")
	if idErr != nil || returnedID != taskID || activeErr != nil || activeID == taskID {
		return fmt.Errorf("priority may have changed, but the response lacks matching task_id and a distinct valid active_task_id; no run was targeted for cancellation; check chat queue")
	}
	cancelled, err := requestChatCancel(ctx, client, "/api/tasks/"+activeID+"/cancel", activeID)
	if err != nil {
		return chatControlError("Interrupt current run", "The selected follow-up was prioritized; cancellation may or may not have applied.", err)
	}
	return cli.PrintJSON(os.Stdout, map[string]any{"session_id": sessionID, "task_id": taskID, "prioritized": true, "interrupt_requested_task_id": activeID, "cancel_result": cancelled})
}

func runChatQueueCancel(cmd *cobra.Command, args []string) error {
	sessionID, taskID, err := chatControlIDs(args)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	edit, _ := cmd.Flags().GetBool("edit")
	action := "remove"
	if edit {
		action = "edit"
	}
	query := url.Values{"expected_status": {"queued"}, "chat_session_id": {sessionID}, "queue_action": {action}}
	path := "/api/tasks/" + taskID + "/cancel?" + query.Encode()
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		pending, err := fetchChatPending(ctx, client, sessionID)
		if err != nil {
			return err
		}
		found := false
		for _, run := range pending.QueuedTasks {
			if run.TaskID == taskID {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("task-id is not a queued follow-up in this chat")
		}
		return printChatControlPlan(sessionID, pending, []chatControlRequest{{"POST", path}})
	}
	result, err := requestChatCancel(ctx, client, path, taskID)
	if err != nil {
		return chatControlError("Cancel queued follow-up", "Cancellation may or may not have applied.", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runChatStop(cmd *cobra.Command, args []string) error {
	sessionID, _, err := chatControlIDs(args)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	pending, err := fetchChatPending(ctx, client, sessionID)
	if err != nil {
		return err
	}
	clearQueue, _ := cmd.Flags().GetBool("clear-queue")
	if clearQueue && !pending.SupportsQueue {
		return fmt.Errorf("server does not report queue support")
	}
	requests := []chatControlRequest{}
	clearPath := chatSessionPath(sessionID) + "/queued-tasks"
	if clearQueue {
		requests = append(requests, chatControlRequest{"DELETE", clearPath})
	}
	if pending.TaskID != "" {
		requests = append(requests, chatControlRequest{"POST", "/api/tasks/" + pending.TaskID + "/cancel"})
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		return printChatControlPlan(sessionID, pending, requests)
	}
	if clearQueue {
		if err := client.DeleteJSON(ctx, clearPath); err != nil {
			return chatControlError("Clear queue", "Queue state is uncertain; no cancel request was sent.", err)
		}
		fresh, err := fetchChatPending(ctx, client, sessionID)
		if err != nil {
			return chatControlError("Recheck current run", "The queue was cleared; no cancel request was sent.", err)
		}
		if fresh.TaskID != "" && fresh.TaskID != pending.TaskID {
			return fmt.Errorf("queue was cleared but the head run changed; no cancel request was sent; check chat queue")
		}
		if fresh.TaskID == "" {
			pending.TaskID = ""
		}
	}
	result := map[string]any{"session_id": sessionID, "queue_cleared": clearQueue, "cancel_requested": false}
	if pending.TaskID != "" {
		cancelled, err := requestChatCancel(ctx, client, "/api/tasks/"+pending.TaskID+"/cancel", pending.TaskID)
		if err != nil {
			state := "Cancellation may or may not have applied."
			if clearQueue {
				state = "The queue was cleared; cancellation may or may not have applied."
			}
			return chatControlError("Stop current run", state, err)
		}
		result["cancel_requested"] = true
		result["task_id"] = pending.TaskID
		result["cancel_result"] = cancelled
	}
	return cli.PrintJSON(os.Stdout, result)
}
