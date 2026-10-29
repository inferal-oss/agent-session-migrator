// Package provider defines the boundary between the migration workflow and
// an agent's storage, project registry, and session transfer mechanism.
package provider

import (
	"context"
	"io"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
)

type DiscoveryOptions struct {
	CatalogPath     string
	Home            string
	CoordinatorID   string
	SupplementLocal bool
}

type Workflow struct {
	Label             string
	Assisted          bool
	DefaultExecutable string
}

// Provider implementations own discovery and destination project registration.
// Preparation must preserve existing checkouts. Assisted providers export a plan
// for their host application; they must not claim to have transferred a session.
type Provider interface {
	ID() string
	Name() string
	Discover(context.Context, DiscoveryOptions) (domain.Catalog, string, error)
	Prepare(context.Context, string, io.Writer) error
	Workflow() Workflow
	Instructions(planPath string) string
}

// Executor is the optional boundary for providers with a supported direct API.
// The Codex adapter intentionally does not implement it: its handoff API is
// exposed to the desktop agent, not to a standalone CLI client.
type Executor interface {
	Transfer(context.Context, domain.Plan, domain.Session) (operationID string, err error)
	Verify(context.Context, domain.Plan, domain.Session, string) (domain.Event, error)
	Archive(context.Context, domain.Plan, domain.Session) (domain.Event, error)
}
