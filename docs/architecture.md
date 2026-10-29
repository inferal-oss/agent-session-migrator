# Architecture

The program distinguishes preparation from session transfer. A provider must state whether transfer is assisted by its host application or available through a direct API. The UI must never infer that preparing a checkout moved a session.

```text
cmd/agent-session-migrator   CLI parsing and provider registration
internal/ui                 Bubble Tea model and selection workflow
internal/domain             Catalogs, plans, receipts, event transitions
internal/provider           Provider contract and optional Executor
internal/gitwork            Repository inspection, identity, bundles, SSH
internal/providers/codex     Codex metadata and app-server project adapter
internal/providers/demo      Synthetic inventory with writes disabled
```

## Provider contract

`Provider.Discover` returns normalized sessions, hosts, and project identities. It must keep uncertainty visible and return warnings separately from JSON inventory. Provider data may be inspected read-only, but discovery must not rewrite storage or resume sessions.

`Provider.Prepare` prepares repositories and registers projects using the provider's supported management surface. It uses a reviewed plan and writes a receipt with provider project IDs. The shared Git implementation preserves existing checkouts and imports objects into a dedicated ref namespace.

`Provider.Workflow` describes execution capability and the destination executable. `Provider.Instructions` explains how an assisted plan is executed. No Codex tool names or storage schemas are needed by the UI.

Providers with a direct transfer API can additionally implement `Executor`: `Transfer`, `Verify`, and `Archive`. Wiring an automated execution engine is future work; adding the interface does not imply direct execution is available. The initial adapter uses the desktop agent as executor.

## Codex adapter

Local discovery reads a known SQLite schema using `sqlite3 -readonly` and a narrowly interpreted desktop JSON state file. It excludes archived sessions, spawned agents, unknown host placement, and data assigned to other hosts. A host-app catalog can override cached runtime status and host labels. Metadata compatibility is version-sensitive; an exported catalog is the fallback.

Destination saved projects are listed or created over an SSH stdio connection to `codex app-server`. `project/list` and `project/create` are experimental and may require compatible Codex versions. Their schemas were checked against the installed CLI's generated protocol. An existing project at the requested root is reused; creation uses a stable idempotency key.

Native handoff is deliberately outside that client: no standalone endpoint was established for transferring desktop chats. The [assisted runner](providers/codex-runner.md) uses Codex app tools, checks the preparation receipt against the selected native host, and records actual completion evidence.

## Failure boundaries

Plans are immutable after selection and include source commits. Preparation checks the source repository and commit before importing objects. A source change requires a fresh plan. A failed clone is preserved for inspection; an existing directory with an unexpected repository is never overwritten.

The journal serializes writers with a lock and replaces its file atomically. It gates archive acknowledgements on prior verification. The executor must also apply these checks before calling the archive API; recording an event after the fact is not a substitute for checking first. A failure stops the queue; no later session is started automatically.
