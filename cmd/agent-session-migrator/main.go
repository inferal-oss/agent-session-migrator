package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"github.com/inferal-oss/agent-session-migrator/internal/domain"
	"github.com/inferal-oss/agent-session-migrator/internal/gitwork"
	"github.com/inferal-oss/agent-session-migrator/internal/provider"
	"github.com/inferal-oss/agent-session-migrator/internal/providers/codex"
	"github.com/inferal-oss/agent-session-migrator/internal/providers/demo"
	"github.com/inferal-oss/agent-session-migrator/internal/ui"
	"github.com/spf13/cobra"
)

var version = "dev"

func resolve(id string) (provider.Provider, error) {
	switch id {
	case "codex":
		return codex.New(), nil
	case "demo":
		return demo.New(), nil
	default:
		return nil, fmt.Errorf("unknown provider %q; run agent-session-migrator providers", id)
	}
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := newRoot().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "agent-session-migrator:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	cmd := newRoot()
	cmd.SetArgs(args)
	return cmd.ExecuteContext(context.Background())
}
func defaultStateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "agent-session-migrator", "plans")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "agent-session-migrator", "plans")
}

type config struct {
	id, home, catalog, coordinator, out, watch string
	list, demo, supplement                     bool
}

func (c config) backend() (provider.Provider, provider.DiscoveryOptions, error) {
	id := c.id
	if c.demo {
		id = "demo"
	}
	if c.watch != "" {
		p, err := domain.LoadPlan(c.watch)
		if err != nil {
			return nil, provider.DiscoveryOptions{}, err
		}
		id = p.ProviderID
	}
	p, err := resolve(id)
	if err != nil {
		return nil, provider.DiscoveryOptions{}, err
	}
	home := c.home
	if home == "" && id == "codex" {
		home = codex.DefaultHome()
	}
	return p, provider.DiscoveryOptions{Home: home, CatalogPath: c.catalog, CoordinatorID: c.coordinator, SupplementLocal: c.supplement}, nil
}
func newRoot() *cobra.Command {
	c := config{}
	root := &cobra.Command{Use: "agent-session-migrator", Short: "Select, prepare, and track agent session migrations", Long: "A Charm terminal workflow for coordinator selection, repository preparation, and verified session migration. Codex handoff is currently assisted by the desktop agent.", Version: version, Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&c.id, "provider", "codex", "session provider")
	root.PersistentFlags().StringVar(&c.home, "provider-home", "", "provider data directory")
	root.PersistentFlags().StringVar(&c.out, "out", defaultStateDir(), "private plan and history directory")
	root.Flags().StringVar(&c.catalog, "catalog", "", "provider catalog JSON exported by the host app")
	root.Flags().StringVar(&c.coordinator, "coordinator", "", "use this coordinator ID instead of the selection wizard")
	root.Flags().StringVar(&c.watch, "watch", "", "resume an existing plan viewer")
	root.Flags().BoolVar(&c.supplement, "local-inventory", true, "supplement exported catalogs with read-only local metadata")
	root.Flags().BoolVar(&c.list, "list", false, "print read-only inspected inventory as JSON")
	root.Flags().BoolVar(&c.demo, "demo", false, "preview synthetic data without provider access or writes")
	root.MarkFlagsMutuallyExclusive("watch", "list")
	root.MarkFlagsMutuallyExclusive("watch", "demo")
	root.RegisterFlagCompletionFunc("provider", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"codex", "demo"}, cobra.ShellCompDirectiveNoFileComp
	})
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		backend, o, err := c.backend()
		if err != nil {
			return err
		}
		if c.list {
			catalog, w, err := backend.Discover(cmd.Context(), o)
			if err != nil {
				return err
			}
			if backend.ID() != "demo" {
				if err := domain.ResolveHostNames(&catalog, domain.HostNamesPath(c.out)); err != nil {
					return err
				}
			}
			if w != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), w)
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(catalog)
		}
		_, err = tea.NewProgram(ui.New(backend, ui.Options{Context: cmd.Context(), Discovery: o, Out: c.out, Watch: c.watch, Demo: c.demo || backend.ID() == "demo"}), tea.WithContext(cmd.Context())).Run()
		return err
	}
	root.AddCommand(&cobra.Command{Use: "providers", Short: "List available providers and execution capabilities", Args: cobra.NoArgs, Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintln(cmd.OutOrStdout(), "codex  SSH preparation · coordinator creation · assisted native handoff\ndemo   Synthetic inventory · read-only preview")
	}})
	root.AddCommand(&cobra.Command{Use: "version", Short: "Print version", Args: cobra.NoArgs, Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintln(cmd.OutOrStdout(), "agent-session-migrator", version)
	}})
	prepare := &cobra.Command{Use: "prepare", Short: "Prepare repositories and saved projects from a reviewed plan", Args: cobra.NoArgs}
	var prepareFile string
	prepare.Flags().StringVar(&prepareFile, "plan", "", "reviewed plan file")
	prepare.MarkFlagRequired("plan")
	prepare.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := domain.LoadPlan(prepareFile)
		if err != nil {
			return err
		}
		backend, err := resolve(p.ProviderID)
		if err != nil {
			return err
		}
		if rich, ok := backend.(provider.ProgressPreparer); ok {
			bar := progress.New(progress.WithDefaultBlend())
			return rich.PrepareWithProgress(cmd.Context(), prepareFile, cmd.OutOrStdout(), func(p domain.Progress) {
				if p.Total > 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "\r%s %d / %d bytes sent", bar.ViewAs(float64(p.Bytes)/float64(p.Total)), p.Bytes, p.Total)
					if p.Bytes == p.Total {
						fmt.Fprintln(cmd.ErrOrStderr())
					}
				}
			})
		}
		return backend.Prepare(cmd.Context(), prepareFile, cmd.OutOrStdout())
	}
	root.AddCommand(prepare)
	root.AddCommand(&cobra.Command{Use: "host-name HOST_ID NAME", Short: "Save a friendly destination label without changing its identity", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		backend, err := resolve(c.id)
		if err != nil {
			return err
		}
		if backend.ID() == "demo" {
			return fmt.Errorf("demo cannot save host names")
		}
		return domain.SaveHostName(domain.HostNamesPath(c.out), backend.ID(), args[0], args[1])
	}})
	root.AddCommand(&cobra.Command{Use: "guide PROVIDER", Short: "Print the embedded provider execution guide for the coordinator", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		backend, err := resolve(args[0])
		if err != nil {
			return err
		}
		guide, ok := backend.(provider.GuideProvider)
		if !ok {
			return fmt.Errorf("provider has no assisted execution guide")
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), guide.ExecutionGuide())
		return err
	}})
	root.AddCommand(&cobra.Command{Use: "status PLAN", Short: "Show each session's durable transaction state", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, err := domain.LoadPlan(args[0])
		if err != nil {
			return err
		}
		events, err := domain.LoadEvents(args[0] + ".events.json")
		if err != nil {
			return err
		}
		states := map[string]string{}
		for _, e := range events {
			states[e.SessionID] = e.State
		}
		for _, j := range p.Jobs {
			s := states[j.Session.ID]
			if s == "" {
				s = "queued"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-10s %s  %s\n", s, j.Session.ID, j.Session.Title)
		}
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "history", Short: "List past plans so you can resume or transfer more sessions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		entries, err := domain.History(c.out)
		if err != nil {
			return err
		}
		for _, h := range entries {
			fmt.Fprintf(cmd.OutOrStdout(), "%d/%d complete · %s · %s\n", h.Completed, len(h.Plan.Jobs), h.Plan.Destination.Name, h.Path)
		}
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "cancel PLAN", Short: "Cancel only jobs that have not been dispatched", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, err := domain.LoadPlan(args[0])
		if err != nil {
			return err
		}
		events, err := domain.LoadEvents(args[0] + ".events.json")
		if err != nil {
			return err
		}
		states := map[string]string{}
		for _, e := range events {
			states[e.SessionID] = e.State
		}
		for _, j := range p.Jobs {
			state := states[j.Session.ID]
			if domain.Terminal(state) {
				continue
			}
			if state != "" && state != "retryable" {
				return fmt.Errorf("session %s has an uncertain or dispatched transfer; reconcile it before cancellation", j.Session.ID)
			}
			if err = domain.RecordEvent(args[0], domain.Event{SessionID: j.Session.ID, State: "cancelled"}); err != nil {
				return err
			}
		}
		return nil
	}})
	root.AddCommand(recordCommand(), sourceCommand(), verifyCommand())
	return root
}
func recordCommand() *cobra.Command {
	var file string
	var e domain.Event
	cmd := &cobra.Command{Use: "record", Short: "Record an executor acknowledgement (never performs a transfer)", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		if e.State == "verified" {
			return fmt.Errorf("use verify to compare destination working state before verification")
		}
		return domain.RecordEvent(file, e)
	}}
	cmd.Flags().StringVar(&file, "plan", "", "plan file")
	cmd.Flags().StringVar(&e.SessionID, "session", "", "provider session ID")
	cmd.Flags().StringVar(&e.State, "state", "", "intent, started, archived, failed, retryable, or cancelled")
	cmd.Flags().StringVar(&e.OperationID, "operation", "", "native operation ID")
	cmd.Flags().StringVar(&e.HostID, "host", "", "observed native destination host")
	cmd.Flags().StringVar(&e.Cwd, "cwd", "", "observed destination cwd")
	cmd.Flags().StringVar(&e.Evidence, "evidence", "", "real provider result summary; no secrets")
	for _, name := range []string{"plan", "session", "state"} {
		cmd.MarkFlagRequired(name)
	}
	return cmd
}
func planSession(file, id string) (domain.Plan, domain.Session, error) {
	p, err := domain.LoadPlan(file)
	if err != nil {
		return p, domain.Session{}, err
	}
	for _, j := range p.Jobs {
		if j.Session.ID == id {
			return p, j.Session, nil
		}
	}
	return p, domain.Session{}, fmt.Errorf("session is not in the plan")
}
func sourceCommand() *cobra.Command {
	var file, id string
	cmd := &cobra.Command{Use: "check-source", Short: "Check source HEAD and working-state snapshot before dispatch", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, s, err := planSession(file, id)
		if err != nil {
			return err
		}
		return gitwork.CheckSource(cmd.Context(), s)
	}}
	cmd.Flags().StringVar(&file, "plan", "", "plan file")
	cmd.Flags().StringVar(&id, "session", "", "session ID")
	cmd.MarkFlagRequired("plan")
	cmd.MarkFlagRequired("session")
	return cmd
}
func verifyCommand() *cobra.Command {
	var file, id, cwd, host, operation string
	cmd := &cobra.Command{Use: "verify", Short: "Compare remote Git state to the sealed source before archival", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		p, s, err := planSession(file, id)
		if err != nil {
			return err
		}
		if host != p.Destination.ID || !filepath.IsAbs(cwd) {
			return fmt.Errorf("verification requires the selected native host and an absolute destination worktree")
		}
		if s.Repo.StateDigest == "" {
			return fmt.Errorf("plan has no sealed source state")
		}
		backend, err := resolve(p.ProviderID)
		if err != nil {
			return err
		}
		if verifier, ok := backend.(provider.DestinationVerifier); ok {
			if err = verifier.VerifyDestination(cmd.Context(), p); err != nil {
				return err
			}
		}
		var receipt domain.Prepared
		if err = domain.ReadJSON(file+".prepared.json", &receipt); err != nil {
			return err
		}
		if receipt.PlanID != p.ID || receipt.DestinationHostID != host {
			return fmt.Errorf("preparation receipt does not match the plan")
		}
		covered := false
		for _, r := range receipt.Repositories {
			if r.SessionID == id && r.SHA == s.Repo.SHA {
				covered = true
			}
		}
		if !covered {
			return fmt.Errorf("preparation receipt does not cover this session and commit")
		}
		origin, err := gitwork.RunSSH(cmd.Context(), p.Preparation, `git -C "$1" remote get-url origin`, nil, cwd)
		if err != nil {
			return err
		}
		expected, err := gitwork.OriginIdentity(s.Repo.Origin)
		if err != nil {
			return err
		}
		actual, err := gitwork.OriginIdentity(origin)
		if err != nil || actual != expected {
			return fmt.Errorf("destination repository identity differs; session remains unarchived")
		}
		got, err := gitwork.StateDigest(cmd.Context(), cwd, &p.Preparation)
		if err != nil {
			return err
		}
		if got != s.Repo.StateDigest {
			return fmt.Errorf("destination staged, unstaged, or untracked state differs; session remains unarchived")
		}
		return domain.RecordEvent(file, domain.Event{SessionID: id, State: "verified", OperationID: operation, HostID: host, Cwd: cwd, StateDigest: got, Evidence: "Remote HEAD and staged/unstaged/untracked Git state match sealed source snapshot"})
	}}
	cmd.Flags().StringVar(&file, "plan", "", "plan file")
	cmd.Flags().StringVar(&id, "session", "", "session ID")
	cmd.Flags().StringVar(&cwd, "cwd", "", "native destination worktree")
	cmd.Flags().StringVar(&host, "host", "", "native destination host ID")
	cmd.Flags().StringVar(&operation, "operation", "", "completed native operation ID")
	for _, name := range []string{"plan", "session", "cwd", "host", "operation"} {
		cmd.MarkFlagRequired(name)
	}
	return cmd
}
