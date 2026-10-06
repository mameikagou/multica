# Runtimes and repos

A runtime is the execution target behind an agent. A daemon owns local runtime
processes and claims queued tasks from the server.

- [Core model](#core-model)
- [CLI](#cli)
- [Task CLI boundary](#task-cli-boundary)
- [Chat session sends](#chat-session-sends)
- [Debugging an agent that did not run](#debugging-an-agent-that-did-not-run)
- [Repos](#repos)

## Core model

The chain is:

1. a user action creates or updates a queued task;
2. the task points at an agent and runtime;
3. the server wakes the runtime over the daemon websocket when possible;
4. the daemon polls and claims the task;
5. the server returns task context, repos, project resources, prior
   session/workdir hints, and a task token;
6. the daemon prepares a workdir and launches the provider CLI;
7. `multica repo checkout` talks to the local daemon, not directly to GitHub.

## CLI

```bash
multica runtime list --output json
multica runtime usage <runtime-id> --output json
multica runtime activity <runtime-id> --output json
multica runtime update <runtime-id> --target-version <version> --output json
multica runtime delete <runtime-id>
multica repo checkout <url>
multica repo checkout <url> --ref <branch-or-sha>
```

Runtime and repo commands affect active agent execution. Do not restart daemons,
update runtimes, or check out arbitrary repos just to test.

`runtime update` and `runtime delete` are writes. Starting a runtime update is
limited to its owner or a workspace owner/admin; the original initiator may keep
polling that specific in-flight request if their admin role changes.

`runtime delete` removes a runtime registration; if active agents are still
bound, it refuses unless the user explicitly passes `--cascade`, which unbinds
those agents and cancels their queued/running tasks before deleting the runtime.
Unbinding keeps the agents and everything they own — instructions, skills,
chats, labels, channel installations, autopilots and task history — and only
clears the agent's runtime binding; an unbound agent cannot run until it is
bound again (`multica agent update <id> --runtime-id <runtime-id>`), and every
trigger path refuses it with `agent_runtime_required`.

`repo checkout` creates a dedicated branch in the task working directory. Most
runtimes use a linked worktree; Linux and Windows Codex use task-local Git
metadata so a task can stage and commit without making the shared repository
cache writable.

`repo checkout` requires both `MULTICA_DAEMON_PORT` and the injected task-scoped
`MULTICA_TOKEN`; it is intended to run inside the active daemon task and from
that task's workdir (or a descendant). The local daemon authenticates the token
against its active-task registry, derives workspace/task/agent identity itself,
and rejects a caller-supplied workdir outside that task. If either variable is
absent, you are not in the normal agent checkout path. When a project
`github_repo` resource has `resource_ref.ref`, `repo checkout <url>` uses that
ref by default for the current task; an explicit
`repo checkout <url> --ref <branch-or-sha>` overrides it.

## Chat session sends

```bash
multica chat list --title "Review" --output json
multica chat send <session-id> --body "Please review the proposed change."
```

`chat list` is read-only. It lists accessible active sessions owned by the
authenticated user in the current workspace; inside a task, that user is the
runtime owner bound to the `mat_` token, not necessarily the task initiator.
`--title` filters by exact title, which need not be unique. Select a real full
session UUID from the result; table output also preserves full UUIDs.

`chat send` is a real write that enqueues an agent run and consumes budget.
Use it only when the user requested sending to that destination. It reuses the
web composer's direct-chat message endpoint and existing membership, creator,
agent-access and invocation gates. A private session belonging to another
member is not an allowed destination. A `mat_` credential never falls back to
a saved member credential. The message is normal chat input, not a structured
agent-authored card or a new issue.

The response includes `message_id`, `task_id` (run ID), and `queued`. A busy
session queues the new turn; acknowledgement is not completion. The command
does not create sessions, send attachments, wait for replies, forward replies,
or retry. After a server error, timeout or lost response, check the target
conversation before sending again: the original send may already have committed.
Do not assume an error means nothing was sent or turn it into an agent-to-agent
auto-reply loop.

`chat history` and `chat thread` remain scoped to the caller's current
conversation/channel; neither becomes a cross-session transcript reader.

## Chat queue controls

```bash
multica chat queue <session-id> --output table
multica chat queue clear <session-id> --dry-run
multica chat queue now <session-id> <task-id> --dry-run
multica chat queue cancel <session-id> <task-id> --dry-run
multica chat queue cancel <session-id> <task-id> --edit --dry-run
multica chat stop <session-id> --dry-run
multica chat stop <session-id> --clear-queue --dry-run
```

Use full UUIDs from `chat list` and `chat queue`. `queue` is read-only;
all other commands are real writes unless `--dry-run` is present. Dry runs
read a snapshot and print planned requests as JSON. Remove `--dry-run` only
when the user has authorized the corresponding action. Existing session
ownership, workspace and task credential checks apply without profile fallback.

`queue clear` cancels queued follow-ups while preserving the head run, even if
that head is still queued. `queue now` promotes the selected follow-up and then
requests cancellation of the active run returned by the server. This follows
the web Send now flow; dispatch remains asynchronous. `queue cancel` uses
server-side queued-status and session-ID guards; `--edit` restores the message
and attachments as a draft and returns the native restore payload. It does not
open an editor or resubmit the message.

`stop` cancels only the captured head; remaining follow-ups may start next.
`stop --clear-queue` clears follow-ups first and rechecks the head before
cancelling. If the head has changed, it refuses to cancel the new run.
No command freezes a session or resumes a suspended process. New messages can
still enqueue runs. Cancellation does not undo actions already completed.

The Send now and stop-with-clear flows are separate requests, not transactions;
concurrent sends may change the queue. Commands never automatically retry a
write and report partial progress on failure. After a timeout or error, inspect
`chat queue` and the conversation before acting again. Do not cancel the
current agent's own run as a smoke test or mutate real user chats for testing.

## Task CLI boundary

The daemon injects a task-scoped `mat_` credential for Multica API commands and
a private task-local Multica configuration root. Inside that managed task
context:

- The `multica` command remains available in every platform-context mode.
  Official binaries are exposed from their install directory; a locally
  renamed daemon gets a task-private `multica` alias without requiring a
  machine-wide symlink.
- API commands such as `issue list`, `issue get`, and `issue runs` use the
  injected task identity and never fall back to the daemon Owner's saved Multica
  profile.
- `config show` and `config set` operate only on task-local Multica state. A
  missing task config root fails closed.
- `auth status` may verify the task identity but omits all token material from
  its output.
- `daemon status` and `daemon disk-usage` report on the runtime hosting this
  task: `status` probes the daemon-injected health port, and `disk-usage` scans
  the daemon-injected workspaces root. Both refuse `--profile`, `disk-usage`
  also refuses `--all-profiles` and `--workspaces-root`, and its STATUS column
  stays blank because filling it would spend the Owner's credential.
- Human/local profile and daemon commands — including `login`, `logout`,
  `setup`, `workspace switch`, local runtime profile path mutation,
  `daemon start` / `stop` / `restart`, `daemon logs`, and
  `daemon probe-runtimes` — are unavailable. `daemon stop` in particular would
  terminate the daemon running this task and every sibling task on it.

`MULTICA_DAEMON_PORT` alone is a weak, defense-in-depth signal for task-safe API
and profile resolution, not task identity for human/local command rejection.
Genuine tasks also carry `MULTICA_AGENT_ID` / `MULTICA_TASK_ID`,
`MULTICA_TASK_CONFIG_ROOT`, or a daemon-managed workdir marker. When the port is
the only signal, `daemon status` uses the selected host profile's derived health
port; ordinary API and profile-resolving commands still fail closed. Host
operators should not export `MULTICA_DAEMON_PORT`; the daemon derives its host
health port from the selected profile and injects the variable into tasks
itself.

The daemon still preserves the real `HOME` and XDG variables for provider tools
such as `gh`, `aws`, `kubectl`, and npm. This is CLI resolution hardening, not
hard filesystem confidentiality: a process under the same OS user can still open
an explicitly known Owner path. Dedicated Unix users, containers, VMs, or an
equivalent OS boundary are required for that stronger isolation.

Host operators can set `platform_context_mode` in the human profile (or start
the daemon with `--platform-context` / `MULTICA_PLATFORM_CONTEXT`). `full`
preserves the historical workflow brief; `minimal` keeps only runtime facts and
a neutral catalog of task-bound skills, so server-provided skills stay visible
without activation rules; `off` removes the brief and server-provided skills.
`--no-platform-context` remains an alias for `off`. User-installed and
workspace-assigned skills remain available in every mode. This is a host-profile
setting: task-local config cannot reconfigure the daemon.

## Debugging an agent that did not run

Check in this order:

1. Was a task supposed to be created? Inspect issue/comment/autopilot context.
2. Is the assignee an agent or squad? A squad routes to its leader.
3. Is the agent archived or bound to a runtime the actor cannot use?
4. Is the runtime online? `multica runtime list --output json`.
5. Did the daemon heartbeat recently? Runtime `last_seen_at` is the visible clue.
6. Did the task get claimed or is it stuck pending/running/waiting for local directory?
7. If repo checkout failed, classify it after checking whether repo context was
   present in the task/project context.

## Repos

The runtime brief lists repos available to this task. Treat that list as the
authority for agent checkout unless the user explicitly asks to bind a new
project resource.

Workspace repos and project resources are not the same thing:

- workspace repo metadata can appear in workspace context;
- `github_repo` project resources are durable project context and can affect
  future tasks; optional `resource_ref.ref` pins the default checkout ref for
  tasks in that project;
- `local_directory` resources point at a path owned by a daemon and carry
  local-machine assumptions.

Do not add a project resource just because `repo checkout` failed. First
determine whether the user asked for durable project context or just a task
checkout.
