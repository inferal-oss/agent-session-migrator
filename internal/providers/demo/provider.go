// Package demo supplies a synthetic, read-only provider for trying the UI.
package demo

import (
	"context"
	"errors"
	"io"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
	"github.com/inferal-oss/agent-session-migrator/internal/provider"
)

type Provider struct{}

func New() *Provider           { return &Provider{} }
func (*Provider) ID() string   { return "demo" }
func (*Provider) Name() string { return "Demo" }
func (*Provider) Discover(context.Context, provider.DiscoveryOptions) (domain.Catalog, string, error) {
	return catalog(), "Synthetic data. Preparation and transfer are disabled.", nil
}
func (*Provider) Prepare(context.Context, string, io.Writer) error {
	return errors.New("demo cannot prepare repositories")
}
func (*Provider) Workflow() provider.Workflow {
	return provider.Workflow{Label: "read-only demo", Assisted: true, DefaultExecutable: "agent"}
}
func (*Provider) Instructions(string) string { return "Demo cannot transfer sessions." }
