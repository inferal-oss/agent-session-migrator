package ui

import (
	"github.com/inferal-oss/agent-session-migrator/internal/gitwork"

	"context"
	"github.com/inferal-oss/agent-session-migrator/internal/domain"
	"github.com/inferal-oss/agent-session-migrator/internal/provider"

	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var accent = lipgloss.NewStyle().Foreground(lipgloss.Color("#A8E6CF")).Bold(true)
var muted = lipgloss.NewStyle().Foreground(lipgloss.Color("#8493A5"))
var warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F6CA82"))
var box = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#53647B")).Padding(0, 1)

type loadedMsg struct {
	catalog domain.Catalog
	warning string
	err     error
}
type tickMsg struct{}
type eventsMsg struct {
	events []domain.Event
	err    error
}
type preparedMsg struct{ err error }
type Options struct {
	Discovery  provider.DiscoveryOptions
	Out, Watch string
	Demo       bool
}

type model struct {
	backend              provider.Provider
	o                    Options
	c                    domain.Catalog
	stage                string
	hostIndex            int
	table                table.Model
	filter               textinput.Model
	visible              []domain.Session
	selected             map[string]bool
	interrupt, filtering bool
	width, height        int
	note                 string
	err                  error
	planPath             string
	plan                 domain.Plan
	events               []domain.Event
	sshInput, rootInput  textinput.Model
	prepField            int
}

func New(backend provider.Provider, o Options) tea.Model {
	f := textinput.New()
	f.Placeholder = "Search title, project, repo, branch, path…"
	f.SetWidth(70)
	t := table.New(table.WithFocused(true), table.WithHeight(12))
	styles := table.DefaultStyles()
	styles.Selected = lipgloss.NewStyle().Foreground(lipgloss.Color("#101820")).Background(lipgloss.Color("#A8E6CF"))
	t.SetStyles(styles)
	ssh := textinput.New()
	ssh.Placeholder = "SSH config alias or user@host"
	ssh.SetWidth(70)
	root := textinput.New()
	root.Placeholder = "Absolute checkout root on destination, e.g. /srv/projects"
	root.SetWidth(70)
	return model{backend: backend, o: o, stage: "loading", table: t, filter: f, selected: map[string]bool{}, width: 110, height: 36, sshInput: ssh, rootInput: root}
}
func (m model) Init() tea.Cmd {
	return func() tea.Msg {
		if m.o.Watch != "" {
			p, err := domain.LoadPlan(m.o.Watch)
			return struct {
				p   domain.Plan
				err error
			}{p, err}
		}
		c, w, err := m.backend.Discover(context.Background(), m.o.Discovery)
		return loadedMsg{c, w, err}
	}
}
func tick() tea.Cmd { return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} }) }
func (m model) host() domain.Host {
	if len(m.c.Hosts) == 0 {
		return domain.Host{}
	}
	return m.c.Hosts[m.hostIndex]
}
func (m *model) rebuild() {
	m.visible = nil
	rows := []table.Row{}
	q := strings.ToLower(m.filter.Value())
	for _, t := range m.c.Sessions {
		hay := strings.ToLower(strings.Join([]string{t.Title, t.ProjectName, t.Cwd, t.Repo.Origin, t.Repo.Branch}, " "))
		if !strings.Contains(hay, q) {
			continue
		}
		m.visible = append(m.visible, t)
		mark := "○"
		if m.selected[t.ID] {
			mark = "●"
		}
		state := t.Status
		if reason := domain.Blocked(t, m.c, m.host(), m.interrupt); reason != "" {
			mark = "–"
			state = "blocked"
		}
		branch := t.Repo.Branch
		if t.Repo.Dirty {
			branch += " *"
		}
		rows = append(rows, table.Row{mark, clean(t.Title), clean(t.ProjectName), clean(branch), state})
	}
	w := max(72, m.width-10)
	cols := []table.Column{{Title: "", Width: 2}, {Title: "Session", Width: max(24, w-66)}, {Title: "Local project", Width: 24}, {Title: "Branch", Width: 20}, {Title: "Status", Width: 10}}
	if m.width < 105 {
		cols = []table.Column{{Title: "", Width: 2}, {Title: "Session", Width: max(20, m.width-46)}, {Title: "Local project", Width: 18}, {Title: "Status", Width: 10}}
		for i, row := range rows {
			rows[i] = table.Row{row[0], row[1], row[2], row[4]}
		}
	}
	cursor := m.table.Cursor()
	m.table.SetRows(nil)
	m.table.SetColumns(cols)
	m.table.SetRows(rows)
	m.table.SetCursor(min(cursor, max(0, len(rows)-1)))
	m.table.SetWidth(max(1, m.width-4))
	m.table.SetHeight(max(3, m.height-21))
	m.filter.SetWidth(max(20, m.width-16))
}
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func (m *model) toggle() {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.visible) {
		return
	}
	t := m.visible[i]
	if reason := domain.Blocked(t, m.c, m.host(), m.interrupt); reason != "" {
		m.note = reason
		return
	}
	if m.selected[t.ID] {
		delete(m.selected, t.ID)
	} else {
		m.selected[t.ID] = true
	}
	m.rebuild()
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case preparedMsg:
		if v.err != nil {
			m.note = "Preparation stopped: " + v.err.Error()
		} else {
			m.note = "Repositories and saved projects prepared; ask the provider to execute the plan."
		}
		return m, nil
	case loadedMsg:
		m.c = v.catalog
		m.note = v.warning
		m.err = v.err
		if v.err != nil {
			m.stage = "error"
			return m, nil
		}
		m.stage = "hosts"
		m.rebuild()
		return m, nil
	case struct {
		p   domain.Plan
		err error
	}:
		if v.err != nil {
			m.err = v.err
			m.stage = "error"
			return m, nil
		}
		m.plan = v.p
		m.planPath = m.o.Watch
		m.stage = "queued"
		return m, tick()
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.rebuild()
		return m, nil
	case tickMsg:
		if m.stage == "queued" {
			path := m.planPath
			return m, func() tea.Msg { e, err := domain.LoadEvents(path + ".events.json"); return eventsMsg{e, err} }
		}
		return m, nil
	case eventsMsg:
		m.events = v.events
		if v.err != nil {
			m.note = "Journal: " + v.err.Error()
		}
		return m, tick()
	case tea.KeyPressMsg:
		key := v.String()
		if key == "ctrl+c" || (!m.filtering && m.stage != "connection" && key == "q") {
			return m, tea.Quit
		}
		if m.width < 70 || m.height < 24 {
			return m, nil
		}
		if m.filtering {
			if key == "esc" || key == "enter" {
				m.filtering = false
				m.filter.Blur()
				m.table.Focus()
				return m, nil
			}
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			m.rebuild()
			return m, cmd
		}
		if m.stage == "connection" {
			switch key {
			case "esc":
				m.sshInput.Blur()
				m.rootInput.Blur()
				m.stage = "hosts"
				return m, nil
			case "tab", "shift+tab":
				m.prepField = 1 - m.prepField
				if m.prepField == 0 {
					m.rootInput.Blur()
					return m, m.sshInput.Focus()
				}
				m.sshInput.Blur()
				return m, m.rootInput.Focus()
			case "enter":
				prep := domain.Preparation{SSHTarget: m.sshInput.Value(), Root: m.rootInput.Value(), Executable: m.backend.Workflow().DefaultExecutable}
				if !m.o.Demo {
					if err := gitwork.ValidatePreparation(prep); err != nil {
						m.note = err.Error()
						return m, nil
					}
				}
				m.sshInput.Blur()
				m.rootInput.Blur()
				m.stage = "tasks"
				m.selected = map[string]bool{}
				m.rebuild()
				return m, nil
			}
			var cmd tea.Cmd
			if m.prepField == 0 {
				m.sshInput, cmd = m.sshInput.Update(msg)
			} else {
				m.rootInput, cmd = m.rootInput.Update(msg)
			}
			return m, cmd
		}
		switch m.stage {
		case "hosts":
			switch key {
			case "up", "k":
				m.hostIndex = max(0, m.hostIndex-1)
			case "down", "j":
				m.hostIndex = min(len(m.c.Hosts)-1, m.hostIndex+1)
			case "enter":
				m.stage = "connection"
				m.prepField = 0
				return m, m.sshInput.Focus()
			}
			return m, nil
		case "tasks":
			switch key {
			case "esc":
				m.stage = "hosts"
				return m, nil
			case "/":
				m.filtering = true
				m.table.Blur()
				return m, m.filter.Focus()
			case "space":
				m.toggle()
				return m, nil
			case "a":
				for _, t := range m.visible {
					if domain.Blocked(t, m.c, m.host(), m.interrupt) == "" {
						m.selected[t.ID] = true
					}
				}
				m.rebuild()
				return m, nil
			case "n":
				m.selected = map[string]bool{}
				m.rebuild()
				return m, nil
			case "i":
				m.interrupt = !m.interrupt
				for _, t := range m.c.Sessions {
					if domain.Blocked(t, m.c, m.host(), m.interrupt) != "" {
						delete(m.selected, t.ID)
					}
				}
				m.rebuild()
				return m, nil
			case "enter":
				p, err := domain.NewPlan(m.c, m.host(), m.selected, m.interrupt)
				if err != nil {
					m.note = err.Error()
					return m, nil
				}
				m.plan = p
				m.plan.Preparation = domain.Preparation{SSHTarget: m.sshInput.Value(), Root: m.rootInput.Value(), Executable: m.backend.Workflow().DefaultExecutable}
				m.stage = "review"
				return m, nil
			}
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		case "review":
			if key == "esc" {
				m.stage = "tasks"
			}
			if key == "m" {
				if m.o.Demo {
					m.note = "Demo cannot queue real migrations."
					return m, nil
				}
				abs, err := filepath.Abs(filepath.Join(m.o.Out, time.Now().Format("20060102-150405")+"-"+m.plan.ID+".json"))
				if err == nil {
					err = domain.WriteNewJSON(abs, m.plan)
				}
				if err != nil {
					m.note = err.Error()
					return m, nil
				}
				m.planPath = abs
				m.stage = "queued"
				return m, tick()
			}
			return m, nil
		case "queued":
			if key == "p" {
				binary, err := os.Executable()
				if err != nil {
					m.note = err.Error()
					return m, nil
				}
				return m, tea.ExecProcess(exec.Command(binary, "prepare", "--plan", m.planPath), func(err error) tea.Msg { return preparedMsg{err} })
			}
			return m, nil
		}
	}
	return m, nil
}
func (m model) View() tea.View {
	var b strings.Builder
	if m.width < 70 || m.height < 24 {
		v := tea.NewView("Agent Session Migrator\n\nPlease resize this terminal to at least 70 columns × 24 rows.\nctrl+c exits; no new actions run on this screen.\n")
		v.AltScreen = true
		return v
	}
	b.WriteString(accent.Render("  AGENT SESSION MIGRATOR") + "\n" + muted.Render("  "+m.backend.Name()+" · Discover → select → prepare → verify → archive") + "\n\n")
	switch m.stage {
	case "loading":
		b.WriteString("  Discovering sessions and inspecting repositories…\n")
	case "error":
		b.WriteString(warnStyle.Render("  "+m.err.Error()) + "\n\n  q quit\n")
	case "hosts":
		b.WriteString("  Choose a destination discovered from provider\n\n")
		for i, h := range m.c.Hosts {
			line := "    " + clean(h.Name)
			if i == m.hostIndex {
				line = accent.Render("  › " + clean(h.Name))
			}
			b.WriteString(line + "\n" + muted.Render("      "+clean(h.ID)) + "\n")
		}
		b.WriteString("\n  ↑/↓ choose · enter tasks · q quit\n")
	case "connection":
		b.WriteString("  Destination: " + clean(m.host().Name) + "\n\n  SSH connection\n  " + m.sshInput.View() + "\n\n  Remote checkout root\n  " + m.rootInput.View() + "\n\n  Use the SSH alias for the selected provider host. The executor must\n  verify they refer to the same machine before moving any chats.\n\n  tab switch field · enter tasks · esc back · ctrl+c quit\n")
	case "tasks":
		b.WriteString(fmt.Sprintf("  Destination: %s     %d selected / %d shown\n", clean(m.host().Name), len(m.selected), len(m.visible)))
		b.WriteString("  " + m.filter.View() + "\n\n")
		b.WriteString("  " + m.table.View() + "\n")
		i := m.table.Cursor()
		if i >= 0 && i < len(m.visible) {
			t := m.visible[i]
			r := t.Repo.Origin
			if r == "" {
				r = t.Repo.Root
			}
			details := fmt.Sprintf("%s\nRepo   %s\nCwd    %s\nHEAD   %s", clean(t.Title), clean(r), clean(t.Cwd), clean(t.Repo.SHA))
			if reason := domain.Blocked(t, m.c, m.host(), m.interrupt); reason != "" {
				details += "\nBlocked: " + reason
			}
			b.WriteString(box.Width(max(40, m.width-10)).Render(details) + "\n")
		}
		b.WriteString(fmt.Sprintf("  space select · / filter · a all shown · n none · i interrupt running [%t]\n  enter review · esc destination · q quit\n", m.interrupt))
	case "review":
		b.WriteString(accent.Render(fmt.Sprintf("  Review %d task(s) → %s", len(m.plan.Jobs), clean(m.plan.Destination.Name))) + "\n\n")
		for i, j := range m.plan.Jobs {
			if i >= max(1, m.height-17) {
				b.WriteString(fmt.Sprintf("  … and %d more\n", len(m.plan.Jobs)-i))
				break
			}
			b.WriteString(fmt.Sprintf("  %2d  %s · %s\n", i+1, clean(j.Session.Title), clean(j.Session.ProjectName)))
		}
		b.WriteString("\n  SSH: " + clean(m.plan.Preparation.SSHTarget) + "  checkout root: " + clean(m.plan.Preparation.Root) + "\n  Preparation clones missing repos, copies missing commits with Git\n  bundles, and registers saved projects using the provider adapter.\n  Process one task at a time; stop on failure or uncertain outcome.\n  Archive only after verification; this hides the transferred chat.\n\n  m save queue · esc edit · q quit\n")
	case "queued":
		states := map[string]string{}
		for _, e := range m.events {
			states[e.SessionID] = e.State
		}
		b.WriteString(accent.Render("  Queue saved · "+m.backend.Workflow().Label) + "\n\n  " + m.planPath + "\n\n")
		for i, j := range m.plan.Jobs {
			if i >= max(1, m.height-18) {
				b.WriteString(fmt.Sprintf("  … %d more (use the status command)\n", len(m.plan.Jobs)-i))
				break
			}
			s := states[j.Session.ID]
			if s == "" {
				s = "queued"
			}
			b.WriteString(fmt.Sprintf("  %-10s %s\n", s, clean(j.Session.Title)))
		}
		b.WriteString("\n  p prepare repositories and saved projects over SSH\n\n" + m.backend.Instructions(m.planPath) + "\n\n  Saving a queue starts no transfers. q closes this viewer.\n")
	}
	if m.note != "" {
		b.WriteString("\n" + warnStyle.Width(max(30, m.width-6)).Render("  "+clean(m.note)) + "\n")
	}
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}
