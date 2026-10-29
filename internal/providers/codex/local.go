package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
)

func supplementLocal(c *domain.Catalog, home string) error {
	db := filepath.Join(home, "state_5.sqlite")
	if _, err := os.Stat(db); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	query := `SELECT id, coalesce(nullif(name,''),title) AS title,cwd,coalesce(project_id,'') AS projectId,is_pinned AS pinned FROM threads WHERE archived=0 AND attached_thread_parent_id IS NULL AND source IN ('cli','vscode','appServer') ORDER BY recency_at_ms DESC,updated_at DESC`
	cmd := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json", db, query)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("read-only local inventory: %w", err)
	}
	if len(out) == 0 {
		return nil
	}
	var rows []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Cwd       string `json:"cwd"`
		ProjectID string `json:"projectId"`
		Pinned    int    `json:"pinned"`
	}
	if err = json.Unmarshal(out, &rows); err != nil {
		return err
	}
	// The native catalog wins for runtime status and host placement. Old local
	// rows can survive a handoff, so supplement only with explicitly local state.
	var state struct {
		Membership  map[string]string `json:"thread-project-membership-host-ids"`
		Assignments map[string]struct {
			ProjectID string `json:"projectId"`
		} `json:"thread-project-assignments"`
	}
	if err = domain.ReadJSON(filepath.Join(home, ".codex-global-state.json"), &state); err != nil {
		return fmt.Errorf("host placement state: %w", err)
	}
	seen := map[string]bool{}
	for _, t := range c.Sessions {
		seen[t.ID] = true
	}
	for _, r := range rows {
		if seen[r.ID] {
			continue
		}
		host, ok := state.Membership[r.ID]
		if !ok || host != c.LocalHostID {
			continue
		}
		pid := r.ProjectID
		if pid == "" {
			pid = state.Assignments[r.ID].ProjectID
		}
		c.Sessions = append(c.Sessions, domain.Session{ID: r.ID, Title: r.Title, Cwd: r.Cwd, HostID: host, ProjectID: pid, Status: "unknown", Pinned: r.Pinned != 0})
	}
	return nil
}

func discoverProjects(c *domain.Catalog, home string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	query := `SELECT p.id,p.name,r.path,'local' AS hostId FROM projects p JOIN project_roots r ON p.id=r.project_id ORDER BY p.position,r.position`
	cmd := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json", filepath.Join(home, "state_5.sqlite"), query)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("unsupported Codex project metadata: %w (use an exported --catalog)", err)
	}
	if len(out) == 0 {
		return nil
	}
	return json.Unmarshal(out, &c.Projects)
}
