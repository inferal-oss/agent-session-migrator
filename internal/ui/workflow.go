package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/inferal-oss/agent-session-migrator/internal/domain"
	"github.com/inferal-oss/agent-session-migrator/internal/provider"
)

type savedMsg struct {
	plan domain.Plan
	file string
	err  error
}
type coordinatorMsg struct {
	session domain.Session
	err     error
}
type prepProgressMsg domain.Progress
type prepLogMsg string

func (m model) refresh() tea.Cmd {
	backend, o, ctx := m.backend, m.o.Discovery, m.o.Context
	return func() tea.Msg { c, w, err := backend.Discover(ctx, o); return loadedMsg{c, w, err} }
}
func (m model) workflowMessage(msg tea.Msg) (model, tea.Cmd, bool) {
	switch v := msg.(type) {
	case savedMsg:
		if v.err != nil {
			m.stage = "review"
			m.note = v.err.Error()
			return m, nil, true
		}
		m.plan = v.plan
		m.planPath = v.file
		m.stage = "queued"
		m.events = nil
		return m, tick(), true
	case coordinatorMsg:
		if v.err != nil {
			m.stage = "new-coordinator"
			m.note = v.err.Error()
			return m, m.coordinatorName.Focus(), true
		}
		m.c.Sessions = append(m.c.Sessions, v.session)
		m.c.CoordinatorSessionID = v.session.ID
		m.o.Discovery.CoordinatorID = v.session.ID
		m.coordinatorName.Blur()
		m.stage = "hosts"
		m.note = "Coordinator conversation created. Open it in the provider app to execute the saved plan; no implementation prompt was sent."
		m.rebuild()
		return m, nil, true
	case prepProgressMsg:
		m.transferProgress = domain.Progress(v)
		return m, listen(m.o.Context, m.progressCh), true
	case prepLogMsg:
		m.note = strings.TrimSpace(string(v))
		return m, listen(m.o.Context, m.progressCh), true
	case preparedMsg:
		m.stage = "queued"
		if m.prepareCancel != nil {
			m.prepareCancel()
			m.prepareCancel = nil
		}
		if v.err != nil {
			m.note = "Preparation stopped: " + v.err.Error()
		} else {
			m.note = "Repositories prepared. Use the selected coordinator for native handoff."
		}
		return m, tick(), true
	}
	return m, nil, false
}
func (m model) workflowKey(key tea.KeyPressMsg) (model, tea.Cmd, bool) {
	k := key.String()
	if (m.stage == "hosts" || m.stage == "queued" || m.stage == "tasks" || m.stage == "coordinator") && k == "h" {
		entries, err := domain.History(m.o.Out)
		if err != nil {
			m.note = err.Error()
			return m, nil, true
		}
		m.history = entries
		m.historyCursor = 0
		m.stage = "history"
		return m, nil, true
	}
	if m.stage == "hosts" && k == "c" {
		m.stage = "coordinator"
		return m, nil, true
	}
	if m.stage == "queued" && k == "n" {
		m.stage = "loading"
		m.selected = map[string]bool{}
		m.events = nil
		m.filter.SetValue("")
		m.o.Watch = ""
		m.o.Discovery.CoordinatorID = m.plan.CoordinatorSessionID
		return m, m.refresh(), true
	}
	switch m.stage {
	case "coordinator":
		switch k {
		case "up", "k":
			m.coordinatorChoice = 0
		case "down", "j":
			m.coordinatorChoice = 1
		case "enter":
			if m.coordinatorChoice == 0 {
				m.stage = "existing"
				m.filter.SetValue("")
				m.rebuild()
			} else {
				m.stage = "new-coordinator"
				return m, m.coordinatorName.Focus(), true
			}
		}
		return m, nil, true
	case "existing":
		if m.filtering {
			return m, nil, false
		}
		switch k {
		case "esc":
			m.stage = "coordinator"
			return m, nil, true
		case "/":
			m.filtering = true
			m.table.Blur()
			return m, m.filter.Focus(), true
		case "enter":
			i := m.table.Cursor()
			if i >= 0 && i < len(m.visible) {
				s := m.visible[i]
				if s.ReservedBy != "" {
					m.note = "This conversation is queued for migration; choose another coordinator."
					return m, nil, true
				}
				m.c.CoordinatorSessionID = s.ID
				m.o.Discovery.CoordinatorID = s.ID
				m.stage = "hosts"
				m.filter.SetValue("")
				m.note = "Coordinator selected: " + clean(s.Title)
				m.rebuild()
			}
			return m, nil, true
		}
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(key)
		return m, cmd, true
	case "new-coordinator":
		switch k {
		case "esc":
			m.coordinatorName.Blur()
			m.stage = "coordinator"
			return m, nil, true
		case "enter":
			name := strings.TrimSpace(m.coordinatorName.Value())
			if name == "" {
				m.note = "Enter a coordinator title."
				return m, nil, true
			}
			if m.o.Demo {
				s := domain.Session{ID: "demo-coordinator", Title: name, HostID: "local", Status: "idle"}
				return m, func() tea.Msg { return coordinatorMsg{session: s} }, true
			}
			creator, ok := m.backend.(provider.CoordinatorCreator)
			if !ok {
				m.note = "This provider cannot create coordinator conversations; choose an existing one."
				return m, nil, true
			}
			m.stage = "creating-coordinator"
			dir := filepath.Join(filepath.Dir(m.o.Out), "coordinators", time.Now().UTC().Format("20060102-150405.000000000"))
			o, ctx := m.o.Discovery, m.o.Context
			return m, func() tea.Msg { s, err := creator.CreateCoordinator(ctx, o, name, dir); return coordinatorMsg{s, err} }, true
		}
		var cmd tea.Cmd
		m.coordinatorName, cmd = m.coordinatorName.Update(key)
		return m, cmd, true
	case "history":
		switch k {
		case "up", "k":
			m.historyCursor = max(0, m.historyCursor-1)
		case "down", "j":
			m.historyCursor = min(max(0, len(m.history)-1), m.historyCursor+1)
		case "enter":
			if len(m.history) > 0 {
				h := m.history[m.historyCursor]
				m.plan = h.Plan
				m.planPath = h.Path
				m.events = nil
				m.o.Discovery.CoordinatorID = h.Plan.CoordinatorSessionID
				m.stage = "queued"
				return m, tick(), true
			}
		case "n", "esc":
			m.stage = "loading"
			return m, m.refresh(), true
		}
		return m, nil, true
	case "preparing":
		if k == "esc" && m.prepareCancel != nil {
			m.prepareCancel()
			m.note = "Cancelling preparation; partial remote artifacts remain available for inspection."
		}
		return m, nil, true
	case "creating-coordinator", "sealing":
		return m, nil, true
	}
	return m, nil, false
}
func (m model) workflowView() string {
	switch m.stage {
	case "coordinator":
		items := []string{"Select an existing conversation", "Create a new coordinator conversation"}
		var b strings.Builder
		b.WriteString("  Choose the conversation that will coordinate transfers\n\n")
		for i, item := range items {
			prefix := "    "
			if i == m.coordinatorChoice {
				prefix = "  › "
			}
			b.WriteString(prefix + item + "\n")
		}
		b.WriteString("\n  The coordinator is protected from migration.\n  ↑/↓ choose · enter continue · h history · q quit\n")
		return b.String()
	case "existing":
		return "  Select an existing coordinator (Git repositories are not required)\n\n  " + m.filter.View() + "\n\n" + m.table.View() + "\n\n  / filter · enter select · esc back\n"
	case "new-coordinator":
		return "  Create a coordinator conversation\n\n  " + m.coordinatorName.View() + "\n\n  Creates a saved conversation using the provider API.\n  Open it in the provider app to execute your reviewed plan.\n  No implementation prompt or automatic transfer is sent.\n\n  enter create · esc back · ctrl+c quit\n"
	case "creating-coordinator":
		return "  Creating the persistent coordinator conversation…\n"
	case "history":
		var b strings.Builder
		b.WriteString("  Migration history · choose a plan to resume\n\n")
		if len(m.history) == 0 {
			b.WriteString("  No plans yet.\n")
		}
		limit := max(1, m.height-14)
		start := max(0, m.historyCursor-limit+1)
		for i := start; i < min(len(m.history), start+limit); i++ {
			h := m.history[i]
			prefix := "    "
			if i == m.historyCursor {
				prefix = "  › "
			}
			b.WriteString(fmt.Sprintf("%s%s  %d/%d complete → %s\n", prefix, h.Plan.CreatedAt.Local().Format("Jan 02 15:04"), h.Completed, len(h.Plan.Jobs), clean(h.Plan.Destination.Name)))
		}
		b.WriteString("\n  enter resume · n transfer more · esc back · q quit\n")
		return b.String()
	case "sealing":
		return "  Capturing source Git state and reserving selected conversations…\n  The plan is published only after its snapshot is durable.\n"
	case "preparing":
		p := m.transferProgress
		overall := float64(p.Completed) / float64(max(1, p.Count))
		s := fmt.Sprintf("  Repository preparation\n\n  %s\n  %d/%d sessions prepared\n\n  %s\n", m.bar.ViewAs(overall), p.Completed, p.Count, clean(p.Stage))
		if p.Total > 0 {
			s += fmt.Sprintf("\n  %s\n  %d / %d Git bundle bytes sent\n", m.bar.ViewAs(float64(p.Bytes)/float64(p.Total)), p.Bytes, p.Total)
		}
		return s + "\n  esc cancel preparation · q quit\n"
	}
	return ""
}
func listen(ctx context.Context, ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		select {
		case m := <-ch:
			return m
		case <-ctx.Done():
			return preparedMsg{ctx.Err()}
		}
	}
}

type progressWriter struct{ ch chan tea.Msg }

func (w progressWriter) Write(b []byte) (int, error) {
	select {
	case w.ch <- prepLogMsg(string(b)):
	default:
	}
	return len(b), nil
}
func (m model) startPreparation() (tea.Model, tea.Cmd) {
	m.stage = "preparing"
	m.transferProgress = domain.Progress{Stage: "Starting", Count: len(m.plan.Jobs)}
	ctx, cancel := context.WithCancel(m.o.Context)
	m.prepareCancel = cancel
	ch := make(chan tea.Msg, 32)
	m.progressCh = ch
	backend, file, parent := m.backend, m.planPath, m.o.Context
	return m, func() tea.Msg {
		go func() {
			var err error
			out := progressWriter{ch}
			if rich, ok := backend.(provider.ProgressPreparer); ok {
				err = rich.PrepareWithProgress(ctx, file, out, func(p domain.Progress) {
					select {
					case ch <- prepProgressMsg(p):
					default:
					}
				})
			} else {
				err = backend.Prepare(ctx, file, out)
			}
			select {
			case ch <- preparedMsg{err}:
			case <-parent.Done():
			}
		}()
		select {
		case msg := <-ch:
			return msg
		case <-parent.Done():
			return preparedMsg{parent.Err()}
		}
	}
}
