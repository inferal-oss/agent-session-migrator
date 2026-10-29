# Contributing

Use Go 1.26 or newer. Start with `go run ./cmd/agent-session-migrator --demo`; it needs no provider account or SSH connection.

Before proposing a change, run:

```sh
gofmt -w ./cmd ./internal
go test ./...
go test -race ./...
go vet ./...
```

Keep provider storage and API knowledge under `internal/providers/<name>`. Implement `provider.Provider`, register it in the CLI composition root, and document its execution mode. Share repository preparation through `internal/gitwork`. A provider that needs a host agent should expose that limitation instead of pretending to support autonomous transfer.

Test meaningful preservation and recovery behavior: repository identity, existing checkouts, private plan storage, interruption policy, duplicate dispatch prevention, destination readback, and archive ordering. Use temporary repositories and synthetic metadata. Tests must never move real sessions, use live credentials, or connect to a personal remote host.

Do not commit real catalogs, session titles, SSH aliases, private repository URLs, plans, transcripts, credentials, Git bundles, or built binaries. Read provider state through supported APIs where available; a read-only, version-sensitive metadata adapter must fail when it cannot establish identity. Never modify a provider database as a migration shortcut.
