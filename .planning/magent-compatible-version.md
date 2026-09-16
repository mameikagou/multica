# Magent-compatible Multica snapshot

This fork snapshot preserves Magent compatibility on top of the maintained
Multica v0.4.42 line at `dd64463b1`. It is not an official upstream release.

- Maintenance branch: `local/magent-compatible-v0.4.42`
- Source snapshot tag: `magent-compatible-v0.4.42-1`
- Existing local build label: `v0.4.42-magent.1`

## Compatibility changes

The existing Grok Build ACP backend accepts the runtime-advertised
`company.credential` authentication method. Executables identifying themselves
as Magent use a minimum version of 0.1.2; original Grok version checks and
authentication precedence remain unchanged.

Set `MULTICA_GROK_PATH` to the installed Magent executable in the environment of
the daemon that should use it. The registered provider remains `grok`. Magent
owns its model credentials; no credentials are included in this source snapshot.

## Validation

Run from `server/`:

```sh
go test ./pkg/agent -run 'TestGrok|TestCheckMinVersion' -count=1
go vet ./pkg/agent
```

The optional `TestMagentRealGrokACP` integration test requires the
`agentintegration` build tag and `MULTICA_RUN_REAL_AGENT_SMOKE=1`. It uses a
locally authenticated Magent installation and consumes model quota. It is not
part of the default checks.

## Updating

Retain these compatibility changes when updating the maintenance branch, rerun
the checks, then build a separate daemon binary. Publishing this branch or tag
does not replace or restart any running daemon. The snapshot tag intentionally
does not match the repository's official `v*.*.*` release workflow.
