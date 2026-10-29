# Current limits and review findings

The initial implementation is suitable for testing a reviewed, assisted migration workflow. It has not been validated with a real cross-host Codex handoff. Do that with a disposable repository and conversation before relying on bulk transfers.

## Remaining gaps

1. **No standalone native handoff executor.** Codex exposes the required desktop app tools to a coordinating chat; the program has no established public CLI equivalent. The optional `Executor` interface is not an automated execution engine. New coordinator creation creates a persistent conversation; the human still opens it in Codex and requests execution.
2. **No distributed atomic commit or automatic rollback.** Local manifests and journal updates are durable, sessions are reserved across queues, and destination Git state must match before archival. A crash around a native dispatch can still leave its outcome uncertain. Such intents remain blocked until native state is reconciled; a failed verification leaves the moved conversation unarchived. Archival is a separate, recoverable action.
3. **Git-visible data only.** Ignored files, credentials, dependencies, services, terminal processes, submodule repositories, LFS objects, and non-Git workspace data need explicit coverage. The adapter currently refuses sessions without a readable Git workspace and rejects submodules or working-tree filters such as LFS while sealing a plan. Untracked regular files and symlinks are fingerprinted; unsupported special file types stop snapshotting.
4. **Version-sensitive Codex integration.** Local metadata schemas and experimental app-server methods can change. A headless host whose native identity cannot be established is rejected before repository upload. Runtime state from local discovery is provisional and must be refreshed by the coordinator.
5. **Repository mapping and packaging need more work.** The UI chooses a checkout root and generates collision-resistant repository paths. It does not yet offer per-repository mapping to arbitrary existing checkouts. There are no tagged releases, signed binaries, installers, or compatibility matrix for Codex versions.

## Gaps closed in this pass

The review found an incorrectly capitalized Git common-directory flag that could collapse repository identities; it is corrected and covered by a real repository/worktree regression test. Verification previously recorded only an explanatory string; new plans now capture source state, and the `verify` command computes destination state before acknowledging it. Dispatch previously had a crash window with no prior intent; intent is now recorded before dispatch, and duplicate or uncertain jobs cannot be silently requeued.

Cobra command validation, coordinator selection/creation, measured Git bundle progress, persistent history, cancellation of undispatched plans, and cross-queue reservations are covered by tests. These checks do not replace a real native handoff integration test.
