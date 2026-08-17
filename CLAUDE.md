# GoPM — working notes for AI agents

## Orientation

- Module `github.com/7c/gopm`. CLI + daemon over a Unix socket; the daemon owns all process state.
- `spec.md` is the design document, `README.md` the user manual, `gopm docs` the compiled-in reference.
- Build: `make build` or `go build ./cmd/gopm/`. Test: `go test ./...`.
- `GOPM_HOME` isolates an instance — tests rely on it, so never hardcode `~/.gopm`.

## Keep `gopm docs` in sync — every time

`gopm docs` is the capability reference AI agents ingest to learn what gopm can do. They act on it
without a human checking, so a stale reference is worse than none.

**Whenever you add a command, add or rename a flag, change a default, add a config key, change an
exit code or status value, or change observable behavior — update the docs topics in the same
commit.**

| File | Contents |
|------|----------|
| `internal/cli/docs.go` | Data model (`docTopic`, `docFlag`, `docExample`), topic assembly, renderer |
| `internal/cli/docs_guides.go` | Concept topics: overview, targets, restart policies, ecosystem, config, logging, files, environment, exit codes, JSON output, MCP, automation |
| `internal/cli/docs_commands_process.go` | One topic per process-management command |
| `internal/cli/docs_commands_daemon.go` | One topic per daemon-management command |
| `internal/cli/docs_commands_tools.go` | Configuration/state and tools commands |

`internal/cli/docs_test.go` enforces it: `go test ./internal/cli/` fails when a registered command
has no topic, when a visible flag is undocumented, when a documented flag no longer exists, when a
shorthand disagrees with cobra, or when a `See also` points at a missing topic. If that test fails,
the fix is to update the docs — not to loosen the test.

Also update `README.md` (user-facing) and `spec.md` §5 (CLI reference) when a command's contract
changes. The three are meant to agree.

## Conventions

- Commands live in `internal/cli/`, one file per command, registered in `newRootCommand()` with a
  `GroupID` — an ungrouped command falls into an "Additional Commands" bucket and fails a test.
- Every command that produces data honors the global `--json` flag; errors under `--json` go to
  stdout as `{"error":"..."}` with a non-zero exit.
- Colors come from `internal/display`. Output meant to be piped (like `gopm docs`) must degrade to
  plain text when stdout is not a terminal.
