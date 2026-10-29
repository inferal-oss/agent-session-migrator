# Provider catalogs

The selector can use a JSON catalog exported by a provider's host application. Catalogs contain metadata, not conversation history or credentials. Keep real catalogs outside the source repository.

```json
{
  "version": 1,
  "providerId": "codex",
  "generatedAt": "2026-10-29T00:00:00Z",
  "coordinatorSessionId": "coordinator-session-id",
  "localHostId": "local",
  "hosts": [
    {"id": "local", "name": "This computer"},
    {"id": "remote-control:example", "name": "Development server"}
  ],
  "projects": [
    {"id": "project-id", "name": "API", "path": "/home/developer/projects/api", "hostId": "local"}
  ],
  "sessions": [
    {
      "id": "session-id",
      "title": "Implement streaming output",
      "cwd": "/home/developer/projects/api",
      "hostId": "local",
      "projectId": "project-id",
      "status": "idle",
      "pinned": false
    }
  ]
}
```

For Codex, a coordinating chat can export `list_projects` and `list_threads` tool results into this shape. Preserve exact IDs and source titles, deduplicate pinned and ordinary sessions by ID, include only Codex sessions, and distinguish native host IDs from SSH aliases. Include only hosts returned by native tools; do not invent destination IDs. Respect tool limits: an export may cover recent sessions only, so state its coverage. The local supplement can add explicitly local top-level sessions from read-only metadata.

The selector inspects each local working directory using Git. It resolves worktrees through their common Git directory and matches local project names using project IDs, then repository identity. Remote workspaces are not inspected through local paths. Every runtime status, project binding, and destination is checked again by the executor before mutation.
