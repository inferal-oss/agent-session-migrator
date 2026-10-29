# Agent Session Migrator

A terminal UI for choosing agent sessions, preparing their repositories on another machine, and tracking verified transfers. Built with Go, Bubble Tea, Bubbles, and Lip Gloss.

**Status: experimental.** Codex is the first provider. Repository preparation runs over SSH. Conversation handoff currently requires the Codex desktop agent; the standalone program does not have a supported Codex handoff API. The UI shows that boundary before any work starts.

```text
  AGENT SESSION MIGRATOR
  Discover → select → prepare → transfer → verify → archive

  Destination: Development server                 2 selected

    Session                         Local project   Branch          Status
  ● Implement streaming output     API             streaming       idle
  ● Fix retry scheduling           Scheduler       fix/retries *   active
  – Coordinate session migration   Unassigned                       blocked

  space select · / filter · a all shown · n none
  i allow interruption · enter review · esc destination · q quit
```

## Try the UI

Requires Go 1.26 or newer:

```sh
go run ./cmd/agent-session-migrator --demo
```

The demo uses synthetic data and cannot save a migration queue, prepare a repository, or transfer a session.

Build a binary:

```sh
go build -o bin/agent-session-migrator ./cmd/agent-session-migrator
./bin/agent-session-migrator --help
```

## Use with Codex

The source needs Git, `sqlite3`, and an SSH client. The destination needs Git and a compatible `codex` executable available to noninteractive SSH commands. SSH authentication and repository access must already work. Repository preparation targets macOS or Linux hosts with a POSIX shell.

1. Connect the destination host in Codex. Keep a separate chat open to coordinate migration; that chat is protected from selection.
2. Run the selector with that chat's ID:

   ```sh
   ./bin/agent-session-migrator --provider codex --coordinator SESSION_ID
   ```

   When launched by Codex, the adapter also accepts `CODEX_THREAD_ID` as the coordinator ID. `CODEX_HOME` controls the default provider data directory. Use `--provider-home` to override it.

3. Choose the destination discovered from Codex metadata. Enter its SSH alias and an absolute checkout root. The SSH alias and selected Codex host must refer to the same machine; the executor checks destination project IDs before transferring anything.
4. Search and select sessions. Inspect their project names, origins, branches, dirty status, working directories, and commits. Running sessions require an explicit interruption opt-in; unknown runtime state is checked by the executor.
5. Review and save the plan. Press **p** to prepare destination repositories and saved projects over SSH. This action can clone repositories, import missing Git objects under migration refs, and create saved projects. It does not move conversations.
6. Ask the coordinating Codex chat to execute the saved plan using [the Codex runner](docs/providers/codex-runner.md). It processes one session at a time, reports progress in that chat, and archives only after verification.

**Archiving hides the transferred chat on the destination.** It does not merely hide an old local copy. This behavior is shown in the review screen. Native handoff moves the existing chat.

An optional `--catalog /path/to/catalog.json` accepts a fresh provider catalog exported through Codex's app tools, including readable destination names and runtime status. Without an export, discovery reads local Codex metadata, shows known remote host IDs, and leaves runtime availability unconfirmed. Local discovery currently understands `state_5.sqlite` and the desktop global-state format; unsupported formats fail with an error. Uncertain host placement is omitted rather than assumed local. See [catalog format](docs/catalog.md).

## Repository preparation

Preparation compares repository identities across HTTPS and SSH URLs. Repositories with the same basename receive distinct destination paths. An existing checkout is reused only if its root and origin match; its checked-out branch and working files are preserved.

Missing repositories are cloned using the destination machine's Git authentication. Missing source commits are transferred in a Git bundle over SSH and fetched into `refs/agent-session-migrator/…`. Nothing is pushed to the repository's upstream remote. Multiple sessions for one repository reuse the same clone. The adapter registers the corresponding project and preserves a saved project's subdirectory relative to its repository root.

Uncommitted files and conversation state are handled by Codex's native handoff. Ignored files, local credentials, installed dependencies, running terminals, and services are not backed up by this tool. Submodules, Git LFS, local-path origins, SSH host aliases that cannot be matched to the source origin, and unusual project layouts may require manual preparation. A failed or uncertain check stops the workflow before archival.

## Plans and recovery

Plans live outside the source repository, under `$XDG_STATE_HOME/agent-session-migrator/plans` or `~/.local/state/agent-session-migrator/plans`. They contain private paths, repository URLs, session titles, and IDs, and are created with mode `0600`.

```sh
# Read-only inventory (JSON goes to stdout; warnings to stderr)
agent-session-migrator --provider codex --coordinator SESSION_ID --list

# Prepare a previously reviewed plan
agent-session-migrator prepare --plan /path/to/plan.json

# Inspect or watch executor acknowledgements
agent-session-migrator status /path/to/plan.json
agent-session-migrator --watch /path/to/plan.json
```

Each plan has a preparation receipt and an append-only event history. The journal enforces `started → verified → archived`, requires destination evidence, and prevents starting later sessions before earlier sessions finish. Journals are local audit records, not cryptographic proof; the executor is responsible for obtaining real tool results. Never manufacture acknowledgements to advance a queue.

An interrupted handoff with a recorded operation ID is reconciled using that operation; it is not submitted again. After failure, inspect source and destination before taking further action. Stale lock files require manual review. Preparation preserves successful clones and imported commits if a later step fails, so the original plan can be retried after resolving the cause. A completed preparation receipt is not overwritten.

## Providers and development

```sh
agent-session-migrator providers
go test ./...
go test -race ./...
go vet ./...
```

The shared domain, TUI, and Git/SSH code depend on a [provider interface](internal/provider/provider.go). Codex-specific metadata, project registration, and execution instructions are confined to its adapter. A separate optional `Executor` interface defines direct transfer, verification, and archival for providers that expose those APIs. Codex does not implement that interface today.

See [architecture](docs/architecture.md), [contributing](CONTRIBUTING.md), and the [MIT license](LICENSE). This project is independent software and is not affiliated with OpenAI or Charm.
