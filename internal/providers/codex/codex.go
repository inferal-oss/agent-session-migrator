// Package codex contains all knowledge of Codex desktop metadata and its
// experimental app-server project registry. It never writes Codex databases.
package codex

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
	"github.com/inferal-oss/agent-session-migrator/internal/gitwork"
	"github.com/inferal-oss/agent-session-migrator/internal/provider"
)

type Provider struct{}

func New() *Provider           { return &Provider{} }
func (*Provider) ID() string   { return "codex" }
func (*Provider) Name() string { return "Codex" }
func (*Provider) Workflow() provider.Workflow {
	return provider.Workflow{Label: "Codex-assisted handoff", Assisted: true, DefaultExecutable: "codex"}
}
func (*Provider) Instructions(plan string) string {
	binary, err := os.Executable()
	if err != nil {
		binary = "agent-session-migrator"
	}
	return "  In the selected coordinator conversation, ask:\n  Run " + gitwork.ShellQuote(binary) + " guide codex, then follow it for:\n  " + plan
}
func (*Provider) Discover(ctx context.Context, o provider.DiscoveryOptions) (domain.Catalog, string, error) {
	var c domain.Catalog
	if err := ctx.Err(); err != nil {
		return c, "", err
	}
	var warning string
	if o.CatalogPath != "" {
		if err := domain.ReadJSON(o.CatalogPath, &c); err != nil {
			return c, "", err
		}
		if c.ProviderID != "codex" || c.Version != 1 {
			return c, "", fmt.Errorf("catalog is not a supported Codex catalog")
		}
	} else {
		c = domain.Catalog{ProviderID: "codex", Version: 1, GeneratedAt: time.Now().UTC(), CoordinatorSessionID: o.CoordinatorID, LocalHostID: "local", Hosts: []domain.Host{{ID: "local", Name: "This computer"}}}
		if err := discoverProjects(&c, o.Home); err != nil {
			return c, "", err
		}
		if err := supplementLocal(&c, o.Home); err != nil {
			return c, "", err
		}
		if err := discoverHosts(&c, o.Home); err != nil {
			return c, "", err
		}
		warning = "Read-only Codex metadata discovery; host availability and runtime state are rechecked by the executor."
	}
	if o.CoordinatorID != "" {
		c.CoordinatorSessionID = o.CoordinatorID
	}
	if o.CatalogPath != "" && o.SupplementLocal {
		if err := supplementLocal(&c, o.Home); err != nil {
			warning = "Local supplement unavailable; showing exported catalog only: " + err.Error()
		}
	}
	if err := domain.ValidateCatalog(c); err != nil {
		return c, "", err
	}
	gitwork.InspectCatalog(&c)
	if len(c.Hosts) < 2 {
		warning += " No remote host is known. Connect a host in Codex or supply a fresh --catalog export."
	}
	return c, warning, nil
}
func (p *Provider) Prepare(ctx context.Context, planPath string, out io.Writer) error {
	plan, err := domain.LoadPlan(planPath)
	if err != nil {
		return err
	}
	if plan.ProviderID != p.ID() {
		return fmt.Errorf("plan belongs to provider %q", plan.ProviderID)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	fmt.Fprintln(out, "Preparing repositories for Codex. Native conversation handoff follows separately.")
	if err = verifyDestination(ctx, plan); err != nil {
		return err
	}
	return gitwork.PreparePlan(ctx, planPath, registerRemoteProject, out, nil)
}

func (p *Provider) PrepareWithProgress(ctx context.Context, file string, out io.Writer, observe func(domain.Progress)) error {
	plan, err := domain.LoadPlan(file)
	if err != nil {
		return err
	}
	if plan.ProviderID != p.ID() {
		return fmt.Errorf("wrong provider")
	}
	if err = verifyDestination(ctx, plan); err != nil {
		return err
	}
	return gitwork.PreparePlan(ctx, file, registerRemoteProject, out, observe)
}
func DefaultHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	u, _ := os.UserHomeDir()
	return filepath.Join(u, ".codex")
}

func discoverHosts(c *domain.Catalog, home string) error {
	var state struct {
		Connections map[string]bool `json:"remote-connection-auto-connect-by-host-id"`
	}
	if err := domain.ReadJSON(filepath.Join(home, ".codex-global-state.json"), &state); err != nil {
		return err
	}
	for id := range state.Connections {
		if strings.HasPrefix(id, "remote-control:") {
			c.Hosts = append(c.Hosts, domain.Host{ID: id, Name: id})
		}
	}
	return nil
}
