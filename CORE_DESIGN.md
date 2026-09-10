# Core Design: Conversation Continuity

This is the maintenance contract for this fork's session and prompt handling.
Read it before rebasing, upgrading upstream, or changing daemon dispatch,
provider resume, prompt construction, session storage, or retry behavior.
The evidence below compares `local/native-context-chat-handoff` at
`5f32662be3db2578d20c89b61046ae86cf4405af` with the maintenance branch at
`6851caff182dc606c90df327ad97e3416645a2f0`.

## Native-history handoff is independent of prompt selection

The old fork's history-preservation design must survive removing the native-cwd
feature. Do not remove its session storage and reachability rules together with
its working-directory routing. These are separate responsibilities.

- **Environment ownership, all providers:** restore the old daemon's claim
  registry and one-shot release signals (`claimEnvRoot`, `lockEnvRootForReuse`,
  `releaseEnvRootClaim`). Release the OS lock before waking the successor;
  the successor must acquire the lock and revalidate the directory's ownership
  and identity. The old branch tip still uses a bounded busy wait, with polling
  for untracked holders; its release notification is not an unlimited-wait
  guarantee. Cancellation must not fall through into fresh preparation.
- **Codex history storage:** the old native-cwd path mounted a persistent store
  keyed by profile, agent and conversation. Managed chats now retain that policy
  without using native cwd. Newly prepared chat homes mount that store, so a
  new task ID or changed workdir does not imply a new native conversation.
  Existing task-local transcript directories remain intact; the requested
  rollout is also linked into the scoped persistent store on reuse.
- **Codex migration:** keep `CodexResumeSessionID` and the ownership-checked
  `CodexResumeSessionsSource` plumbing from the old fork. If an environment
  cannot be reused after claiming it, expose only the requested rollout from
  that same validated, still-locked managed root. Never scan arbitrary user
  directories or import another conversation's whole transcript collection.
- **Codex resume eligibility:** working-directory equality is not the criterion.
  Preserve the ID until the actual task `CODEX_HOME` has been checked for its
  rollout. Existing history must reach `thread/resume`; missing history must
  still be disclosed. This restores the old cwd-independent rule separately
  from its excluded native-directory feature.
- **Other providers:** retain Claude's `--resume`, Pi's `--session` file and
  Antigravity's `--conversation`, plus their existing provider-specific failure
  handling. Do not apply Codex's cwd-independent rule to cwd-keyed Claude stores.
  Keep the upstream Windows Pi sidecar-lock fix; reverting it to a mandatory
  lock on the transcript itself would prevent the CLI reading its own history.

This is an adapted migration, not a byte-for-byte restoration of native cwd.
The unchanged provider RPC fallback can still create a fresh session when the
provider rejects history that the daemon found. Do not claim that prompt tests,
filesystem tests or this migration prove every provider-side resume succeeds.

Regression coverage: `codex_chat_handoff_test.go` in `internal/daemon` and
`internal/daemon/execenv` covers old native store to managed chat, scoped legacy
rollout migration, preservation of original files, conversation isolation,
changed-cwd resume and honest disclosure when the rollout is absent. Existing
`leader_workdir_reuse_test.go` covers cancellation, busy holders, release and
ownership/identity revalidation. Test fixtures do not contact a real model.

## Three different kinds of context

1. **Provider session history:** the native Codex thread, Claude transcript,
   Pi session file, or Antigravity conversation. Successful resume makes its
   persisted context available to the next turn.
2. **Full current-turn prompt:** `BuildPrompt` combines the current input with
   platform/surface instructions and applicable task context. The runtime brief
   is another input, delivered through managed files or provider configuration.
3. **Multica chat transcript:** stored user/assistant chat messages, readable
   through `multica chat history`. This is not the complete provider transcript
   and does not reconstruct all prior tool execution or internal working state.

The full current-turn prompt does **not** automatically contain the entire
Multica chat transcript. The old branch's claim handler populates `ChatMessage`
from task-owned input or trailing unanswered user messages; `buildChatPrompt`
then inserts that value. Retaining the full prompt cannot itself repair a
missing provider session. Reading history is therefore not, by itself, proof
that this prompt contract regressed.

## Original Codex design and required behavior

The old maintenance branch already prepared two alternative strings:

```go
prompt := BuildPrompt(task, provider, promptOptions...)
resumedPrompt := buildResumedWebDirectPrompt(task, promptOptions...)
```

The second expression was conditional on a Codex web chat without a channel
binding and not an intro. `ExecOptions.ResumedPrompt` carried the alternative.
After `startOrResumeThread` returned, `codexPromptForThread` selected it only
when `resumed == true`; otherwise it returned `prompt`. Exactly one prompt was
sent to `turn/start`. These are alternatives, not two messages or two model runs.

Source at the old revision:

- `server/internal/daemon/daemon.go`: lines 7704–7708 and 7935–7937.
- `server/pkg/agent/agent.go`: lines 68–79.
- `server/pkg/agent/codex.go`: lines 1463–1466 and 1768–1772.
- `server/internal/handler/daemon.go`: lines 2659–2674 and 2768 explain input scope.

The original `OmitSystemPromptOnResume` additionally suppressed stable developer
instructions on resume while retaining them on cold start. Current Codex omits
the override on resume directly; preserving the behavior does not require
restoring the old flag.

| Actual execution outcome | Required input |
| --- | --- |
| Eligible private chat, confirmed native resume | New message plus turn-scoped attachments, selected skills, connected apps and applicable directory/conflict notices |
| First turn / cold start | Full current-turn prompt and applicable runtime brief |
| Codex resume rejected internally, then `thread/start` | Full current-turn prompt plus the applicable continuity notice, once |
| Resume rejected and daemon starts a fresh retry | Clear the resume ID, rebuild cold prompt/runtime context, disclose the gap once |
| Channel/group/intro turn | Preserve its full surface-specific instructions |

**A prior session ID is an intention to resume, not proof that resume succeeded.**
If a backend can create a new session inside `Execute`, it must retain the full
prompt until the actual outcome is known. Moving prompt selection above that
boundary without carrying a cold alternative violates this contract.

## Migration regression and scope of the repair

`90cfcc23a` generalized private-chat delta selection to all providers in the
daemon. Codex could subsequently reject `thread/resume` and fall back to
`thread/start` within the same `Execute`, receiving only the already-selected
delta. `6851caff1` restores the full alternative and makes Codex select after
the actual resume outcome. The current eligibility guard also requires a prior
session ID and a direct audience; it is not a byte-for-byte copy of the old code.

This repairs **cold fallback prompt completeness**. It neither prevents every
native resume failure nor proves why a particular production session failed.
The report that a model read `multica chat history` needs task-specific evidence:
requested/returned session IDs, resume outcome, workdir, reachable transcript,
and any fallback reason. Do not label a deployment a successful continuity
repair based only on a build, health response, or runtime registration.

## Provider differences and remaining limits

| Provider | Current continuation mechanism | Failure handling and evidence limits |
| --- | --- | --- |
| Codex | `thread/resume`; prompt chosen after returned `resumed` | Internal recoverable fallback uses the full alternative. Rollout presence and writer locking are separate requirements. |
| Claude Code | `--resume <id>` | Recognized rejection on a failed run produces `ResumeRejected`; daemon can rebuild cold context for a fresh retry. A completed run reporting a different ID is not rejected by the current predicate, so this is not a universal continuity guarantee. |
| Pi / Pi family | `--session <transcript-file>` | Daemon checks session reachability and recorded cwd; backend locks the file and detects known refusal. Fresh retry rebuilds the prompt. File presence does not prove every transcript is semantically loadable. |
| Antigravity | `--conversation <id>` | Adapter reads the resulting conversation ID but does not produce `ResumeRejected`. `ResumeRejectionUndetectable` uses a bounded failure heuristic in the daemon. Successful native history restoration is not confirmed before the prompt is sent. |

Claude, Pi and Antigravity currently retain the daemon's preselected private-chat
delta behavior. The Codex repair does not extend confirmed-resume selection to
them. All providers can lose native context if their session is missing,
inaccessible, incompatible or abandoned. Do not assert that an external CLI
cannot silently start fresh merely because its Multica adapter has no explicit
internal fallback. Any extension needs captured protocol/CLI evidence and a
regression test for the actual boundary.

Source entry points: `server/internal/daemon/prompt.go`
(`buildTaskExecutionPrompts`), `server/internal/daemon/daemon.go`
(`runTask`, `shouldRetryWithFreshSession`), and `server/pkg/agent/`
(`codex.go`, `claude.go`, `pi.go`, `antigravity.go`, `agent.go`).

## Upgrade acceptance

- Compare behavior against this contract, not just commit hashes or titles.
- Cover confirmed resume, cold start, rejected resume and internal fresh
  fallback. Preserve dynamic turn data and emit continuity notices only once.
- Keep writer exclusion, session reachability, rollout persistence, cancellation
  and retry bounds intact. They protect different failure boundaries.
- The local all-error execution retry remains limited to one daemon-level retry;
  cancellation must not launch another execution. Provider-internal retries are
  separate and must not be described as covered by that single shared budget.
- Do not restore `native_workdir`: the user explicitly excluded it from migration.
- Existing prompt/helper tests establish prompt selection; a protocol fixture
  capturing `turn/start.input` is stronger wiring evidence. Real continuity
  acceptance requires a multi-turn native-session smoke and session-ID/log
  verification, including an interrupted follow-up and a missing-session case.
- Markdown records intent; tests and runtime evidence establish behavior.
  Documentation alone cannot prevent a regression.
