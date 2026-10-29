package demo

import (
	"time"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
)

func catalog() domain.Catalog {
	return domain.Catalog{ProviderID: "demo", Version: 1, GeneratedAt: time.Now().UTC(), CoordinatorSessionID: "coordinator", LocalHostID: "local", Hosts: []domain.Host{{ID: "local", Name: "This computer"}, {ID: "remote:example", Name: "Development server"}}, Sessions: []domain.Session{
		{ID: "task-a", Title: "Implement streaming output", HostID: "local", ProjectName: "API", Cwd: "/home/developer/projects/api", Status: "idle", Repo: domain.Repo{Root: "/home/developer/projects/api", Origin: "git@github.com:example/api.git", Branch: "streaming", SHA: "abc123"}},
		{ID: "task-b", Title: "Fix retry scheduling", HostID: "local", ProjectName: "Scheduler", Cwd: "/home/developer/worktrees/retries", Status: "active", Repo: domain.Repo{Root: "/home/developer/worktrees/retries", Origin: "git@github.com:example/scheduler.git", Branch: "fix/retries", SHA: "def456", Dirty: true}},
		{ID: "coordinator", Title: "Coordinate task migrations", HostID: "local", ProjectName: "Unassigned", Cwd: "/home/developer/migrations", Status: "active"},
	}}
}
