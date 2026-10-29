package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
	"github.com/inferal-oss/agent-session-migrator/internal/gitwork"
)

func matchHost(expected, environment string) error {
	if environment == "" {
		return fmt.Errorf("destination did not provide a native environment ID")
	}
	if expected != "remote-control:"+environment {
		return fmt.Errorf("SSH target is a different Codex host; no repository data was sent")
	}
	return nil
}

func (*Provider) VerifyDestination(ctx context.Context, p domain.Plan) error {
	return verifyDestination(ctx, p)
}

// Bind the SSH transport to the selected native host before sending Git data.
// A headless host without readable native identity must fail closed.
func verifyDestination(parent context.Context, p domain.Plan) error {
	if !strings.HasPrefix(p.Destination.ID, "remote-control:") {
		return fmt.Errorf("Codex SSH preparation requires a remote-control destination")
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	client, err := newAppClient(ctx, p.Preparation.Executable, p.Preparation.SSHTarget, "")
	if err == nil {
		var status struct {
			EnvironmentID string `json:"environmentId"`
		}
		err = client.Call("remoteControl/status/read", nil, &status)
		client.Close()
		if err == nil && status.EnvironmentID != "" {
			return matchHost(p.Destination.ID, status.EnvironmentID)
		}
	}
	// Desktop metadata is the compatibility fallback when a fresh stdio server
	// has no remote-control connection. Never print or persist the raw document.
	raw, err := gitwork.RunSSH(ctx, p.Preparation, `set -eu; root=${CODEX_HOME:-"$HOME/.codex"}; cat "$root/.codex-global-state.json"`, nil)
	if err != nil {
		return fmt.Errorf("cannot bind SSH alias to selected Codex host before upload: %w", err)
	}
	var state struct {
		EnvironmentID string `json:"electron-local-remote-control-environment-id"`
	}
	if err = json.Unmarshal([]byte(raw), &state); err != nil {
		return fmt.Errorf("unsupported remote host identity metadata: %w", err)
	}
	return matchHost(p.Destination.ID, state.EnvironmentID)
}
