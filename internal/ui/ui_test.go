package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/inferal-oss/agent-session-migrator/internal/domain"
	"github.com/inferal-oss/agent-session-migrator/internal/provider"
	"github.com/inferal-oss/agent-session-migrator/internal/providers/demo"
)

func demoModel(t *testing.T) model {
	t.Helper()
	p := demo.New()
	c, w, err := p.Discover(context.Background(), provider.DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	m := New(p, Options{Demo: true}).(model)
	next, _ := m.Update(loadedMsg{catalog: c, warning: w})
	return next.(model)
}
func key(m model, code rune, text string) model {
	next, _ := m.Update(tea.KeyPressMsg{Code: code, Text: text})
	return next.(model)
}
func TestDemoSelectionAndProtectedSession(t *testing.T) {
	m := demoModel(t)
	m.hostIndex = 1
	m.stage = "tasks"
	m.rebuild()
	m.toggle()
	if len(m.selected) != 1 {
		t.Fatal("idle session not selected")
	}
	m.table.SetCursor(1)
	m.toggle()
	if len(m.selected) != 1 {
		t.Fatal("running session selected without opt-in")
	}
	m = key(m, 'i', "i")
	m.toggle()
	if len(m.selected) != 2 {
		t.Fatal("running opt-in failed")
	}
	m.table.SetCursor(2)
	m.toggle()
	if len(m.selected) != 2 {
		t.Fatal("coordinator selected")
	}
}
func TestDemoCannotSavePlan(t *testing.T) {
	m := demoModel(t)
	m.hostIndex = 1
	m.stage = "tasks"
	m.rebuild()
	m.toggle()
	m = key(m, tea.KeyEnter, "")
	if m.stage != "review" {
		t.Fatalf("stage %s", m.stage)
	}
	m = key(m, 'm', "m")
	if m.stage != "review" || m.planPath != "" {
		t.Fatal("demo saved a migration plan")
	}
}
func TestSSHFieldAcceptsLetterQ(t *testing.T) {
	m := demoModel(t)
	m.stage = "connection"
	m.sshInput.Focus()
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = next.(model)
	if m.sshInput.Value() != "q" {
		t.Fatal("q quit rather than entering SSH alias")
	}
	_ = cmd
}
func TestSelectionSurvivesFilter(t *testing.T) {
	m := demoModel(t)
	m.hostIndex = 1
	m.stage = "tasks"
	m.rebuild()
	m.toggle()
	m.filter.SetValue("nonexistent")
	m.rebuild()
	if len(m.visible) != 0 || len(m.selected) != 1 {
		t.Fatal("filter lost selection")
	}
	m.filter.SetValue("")
	m.rebuild()
	if len(m.visible) != 3 || !m.selected["task-a"] {
		t.Fatal("selection not restored")
	}
}

func TestTableRendersSessionRows(t *testing.T) {
	m := demoModel(t)
	m.hostIndex = 1
	m.stage = "tasks"
	m.rebuild()
	if !strings.Contains(m.table.View(), "Implement streaming output") {
		t.Fatalf("table rows are invisible: %q", m.table.View())
	}
}

func TestCoordinatorWizardAllowsExistingNonGitConversation(t *testing.T) {
	m := demoModel(t)
	m.c.CoordinatorSessionID = ""
	m.stage = "coordinator"
	m = key(m, tea.KeyEnter, "")
	if m.stage != "existing" {
		t.Fatal(m.stage)
	}
	m.table.SetCursor(2)
	m = key(m, tea.KeyEnter, "")
	if m.stage != "hosts" || m.c.CoordinatorSessionID != "coordinator" {
		t.Fatal("failed to select non-Git coordinator")
	}
}
func TestCoordinatorCreationIsExplicitAndDemoDoesNotUseProvider(t *testing.T) {
	m := demoModel(t)
	m.stage = "coordinator"
	m = key(m, tea.KeyDown, "")
	m = key(m, tea.KeyEnter, "")
	if m.stage != "new-coordinator" {
		t.Fatal(m.stage)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	if cmd == nil {
		t.Fatal("no creation action")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.c.CoordinatorSessionID != "demo-coordinator" || m.stage != "hosts" {
		t.Fatal("coordinator not selected")
	}
}
func TestProgressViewShowsActualByteCount(t *testing.T) {
	m := demoModel(t)
	m.stage = "preparing"
	m.transferProgress = domain.Progress{Stage: "Sending Git bundle", Bytes: 50, Total: 100, Count: 2}
	if !strings.Contains(m.View().Content, "50 / 100 Git bundle bytes sent") {
		t.Fatal(m.View().Content)
	}
}
func TestNarrowWindowAndCleanLabels(t *testing.T) {
	m := demoModel(t)
	m.hostIndex = 1
	m.stage = "tasks"
	m.c.Sessions[0].Title = "hello\x1b[31m\nworld"
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = next.(model)
	view := m.View().Content
	if strings.Contains(view, "domain.Session") || strings.Contains(view, "\x1b[31m") {
		t.Fatal("implementation label or raw title escape leaked")
	}
	if strings.Contains(clean("a\nb\x1b"), "\n") {
		t.Fatal("control characters were not removed")
	}
}
