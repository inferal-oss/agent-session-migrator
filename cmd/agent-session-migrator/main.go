package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
	"github.com/inferal-oss/agent-session-migrator/internal/provider"
	"github.com/inferal-oss/agent-session-migrator/internal/providers/codex"
	"github.com/inferal-oss/agent-session-migrator/internal/providers/demo"
	"github.com/inferal-oss/agent-session-migrator/internal/ui"
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
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agent-session-migrator:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version":
			fmt.Println("agent-session-migrator", version)
			return nil
		case "providers":
			fmt.Println("codex  Codex desktop · SSH repository preparation · assisted native handoff\ndemo   Synthetic inventory · read-only UI preview")
			return nil
		case "prepare":
			return prepare(ctx, args[1:])
		case "record":
			return record(args[1:])
		case "status":
			if len(args) != 2 {
				return errors.New("usage: agent-session-migrator status PLAN")
			}
			return status(args[1])
		}
	}
	f := flag.NewFlagSet("agent-session-migrator", flag.ContinueOnError)
	f.Usage = func() {
		fmt.Fprint(f.Output(), "Select and prepare agent sessions for migration.\n\nUsage: agent-session-migrator [options]\n       agent-session-migrator providers|prepare|status|record|version\n\nCodex conversation handoff is assisted by the desktop agent.\n\n")
		f.PrintDefaults()
	}
	id := f.String("provider", "codex", "session provider (see providers command)")
	catalog := f.String("catalog", "", "optional provider catalog JSON exported by the host app")
	home := f.String("provider-home", "", "provider data directory (defaults to provider environment)")
	coordinator := f.String("coordinator", "", "protected coordinator session ID (Codex: CODEX_THREAD_ID by default)")
	out := f.String("out", defaultStateDir(), "directory for private plans, journals, and preparation receipts")
	watch := f.String("watch", "", "watch an existing plan and its executor journal")
	supplement := f.Bool("local-inventory", true, "supplement exported catalogs with read-only local metadata")
	list := f.Bool("list", false, "print inspected provider inventory as JSON; never prepare or transfer")
	demoMode := f.Bool("demo", false, "preview synthetic sessions without reading provider state")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", f.Arg(0))
	}
	if *demoMode {
		*id = "demo"
	}
	if *watch != "" {
		p, err := domain.LoadPlan(*watch)
		if err != nil {
			return err
		}
		*id = p.ProviderID
	}
	backend, err := resolve(*id)
	if err != nil {
		return err
	}
	if *home == "" && backend.ID() == "codex" {
		*home = codex.DefaultHome()
	}
	if *coordinator == "" && backend.ID() == "codex" {
		*coordinator = os.Getenv("CODEX_THREAD_ID")
	}
	discovery := provider.DiscoveryOptions{CatalogPath: *catalog, Home: *home, CoordinatorID: *coordinator, SupplementLocal: *supplement}
	if *list {
		c, w, err := backend.Discover(ctx, discovery)
		if err != nil {
			return err
		}
		if w != "" {
			fmt.Fprintln(os.Stderr, w)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(c)
	}
	_, err = tea.NewProgram(ui.New(backend, ui.Options{Discovery: discovery, Out: *out, Watch: *watch, Demo: *demoMode}), tea.WithContext(ctx)).Run()
	return err
}
func defaultStateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "agent-session-migrator", "plans")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "agent-session-migrator", "plans")
}
func prepare(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("prepare", flag.ContinueOnError)
	file := f.String("plan", "", "plan file saved by the selector")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--plan is required")
	}
	p, err := domain.LoadPlan(*file)
	if err != nil {
		return err
	}
	backend, err := resolve(p.ProviderID)
	if err != nil {
		return err
	}
	return backend.Prepare(ctx, *file, os.Stdout)
}
func status(file string) error {
	p, err := domain.LoadPlan(file)
	if err != nil {
		return err
	}
	events, err := domain.LoadEvents(file + ".events.json")
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
		fmt.Printf("%-10s %s  %s\n", s, j.Session.ID, j.Session.Title)
	}
	return nil
}
func record(args []string) error {
	f := flag.NewFlagSet("record", flag.ContinueOnError)
	file := f.String("plan", "", "plan file")
	var e domain.Event
	f.StringVar(&e.SessionID, "session", "", "provider session ID")
	f.StringVar(&e.State, "state", "", "started, verified, archived, or failed")
	f.StringVar(&e.OperationID, "operation", "", "provider handoff operation ID")
	f.StringVar(&e.HostID, "host", "", "observed destination host ID")
	f.StringVar(&e.Cwd, "cwd", "", "observed absolute destination cwd")
	f.StringVar(&e.Evidence, "evidence", "", "verification/tool result summary; no secrets")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *file == "" || e.SessionID == "" {
		return errors.New("--plan and --session are required")
	}
	return domain.RecordEvent(*file, e)
}
