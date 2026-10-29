package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Host struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Project struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	HostID string `json:"hostId"`
}
type Repo struct {
	Root      string `json:"root"`
	CommonDir string `json:"commonDir"`
	Origin    string `json:"origin"`
	Branch    string `json:"branch"`
	SHA       string `json:"sha"`
	Dirty     bool   `json:"dirty"`
	Error     string `json:"error,omitempty"`
}
type Session struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Cwd           string `json:"cwd"`
	HostID        string `json:"hostId"`
	ProjectID     string `json:"projectId"`
	ProjectName   string `json:"projectName"`
	ProjectSubdir string `json:"projectSubdir"`
	Status        string `json:"status"`
	Pinned        bool   `json:"pinned"`
	Repo          Repo   `json:"repo"`
}
type Catalog struct {
	ProviderID           string    `json:"providerId"`
	Version              int       `json:"version"`
	GeneratedAt          time.Time `json:"generatedAt"`
	CoordinatorSessionID string    `json:"coordinatorSessionId"`
	LocalHostID          string    `json:"localHostId"`
	Hosts                []Host    `json:"hosts"`
	Projects             []Project `json:"projects"`
	Sessions             []Session `json:"sessions"`
}
type Job struct {
	Session Session `json:"session"`
}
type Plan struct {
	ProviderID               string      `json:"providerId"`
	Version                  int         `json:"version"`
	ID                       string      `json:"id"`
	CreatedAt                time.Time   `json:"createdAt"`
	CatalogGeneratedAt       time.Time   `json:"catalogGeneratedAt"`
	CoordinatorSessionID     string      `json:"coordinatorSessionId"`
	Destination              Host        `json:"destination"`
	ArchiveAfterVerification bool        `json:"archiveAfterVerification"`
	AllowInterrupt           bool        `json:"allowInterrupt"`
	Jobs                     []Job       `json:"jobs"`
	Preparation              Preparation `json:"preparation"`
}

// Events are append-only acknowledgements from the provider executor, not evidence
// synthesized by this selector. The executor must perform and verify each action.
type Event struct {
	Time        time.Time `json:"time"`
	SessionID   string    `json:"sessionId"`
	State       string    `json:"state"`
	OperationID string    `json:"operationId,omitempty"`
	HostID      string    `json:"hostId,omitempty"`
	Cwd         string    `json:"cwd,omitempty"`
	Evidence    string    `json:"evidence,omitempty"`
}

func ReadJSON(path string, out any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func WriteNewJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func NewPlan(c Catalog, host Host, selected map[string]bool, interrupt bool) (Plan, error) {
	if err := ValidateCatalog(c); err != nil {
		return Plan{}, err
	}
	known := false
	for _, h := range c.Hosts {
		if h == host {
			known = true
		}
	}
	if !known {
		return Plan{}, errors.New("destination is absent from the provider catalog")
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return Plan{}, err
	}
	p := Plan{ProviderID: c.ProviderID, Version: 1, ID: hex.EncodeToString(b), CreatedAt: time.Now().UTC(), CatalogGeneratedAt: c.GeneratedAt, CoordinatorSessionID: c.CoordinatorSessionID, Destination: host, ArchiveAfterVerification: true, AllowInterrupt: interrupt}
	for _, t := range c.Sessions {
		if selected[t.ID] {
			if reason := Blocked(t, c, host, interrupt); reason != "" {
				return Plan{}, fmt.Errorf("%s: %s", t.Title, reason)
			}
			p.Jobs = append(p.Jobs, Job{Session: t})
		}
	}
	for id, chosen := range selected {
		if !chosen {
			continue
		}
		found := false
		for _, t := range c.Sessions {
			if t.ID == id {
				found = true
				break
			}
		}
		if !found {
			return Plan{}, fmt.Errorf("selected session %q is not in the inventory", id)
		}
	}
	if len(p.Jobs) == 0 {
		return Plan{}, errors.New("select at least one eligible task")
	}
	return p, nil
}
func Blocked(t Session, c Catalog, h Host, interrupt bool) string {
	if t.ID == c.CoordinatorSessionID {
		return "coordinating chat"
	}
	if t.HostID == h.ID {
		return "already on destination"
	}
	if t.HostID != c.LocalHostID {
		return "inventory requires source-host inspection"
	}
	if t.Status == "active" && !interrupt {
		return "running; press i to allow interruption"
	}
	if t.Repo.Error != "" {
		return t.Repo.Error
	}
	if t.Repo.Root == "" {
		return "no Git repository"
	}
	return ""
}
func LoadPlan(path string) (Plan, error) {
	var p Plan
	err := ReadJSON(path, &p)
	if err != nil {
		return p, err
	}
	if p.ProviderID == "" || p.Version != 1 || p.ID == "" || len(p.Jobs) == 0 || p.Destination.ID == "" || p.CoordinatorSessionID == "" || !p.ArchiveAfterVerification {
		return p, errors.New("invalid or unsupported migration plan")
	}
	seen := map[string]bool{}
	for _, j := range p.Jobs {
		if j.Session.ID == "" || j.Session.ID == p.CoordinatorSessionID || seen[j.Session.ID] || j.Session.HostID == p.Destination.ID {
			return p, errors.New("invalid, duplicate, or protected task")
		}
		seen[j.Session.ID] = true
	}
	return p, nil
}

func LoadEvents(path string) ([]Event, error) {
	var e []Event
	err := ReadJSON(path, &e)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return e, err
}

// RecordEvent enforces ordered, serial completion. A verification acknowledgement
// must name the exact destination and a real destination working directory.
func RecordEvent(planPath string, e Event) error {
	p, err := LoadPlan(planPath)
	if err != nil {
		return err
	}
	path := planPath + ".events.json"
	lock, err := os.OpenFile(path+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("journal is locked: %w", err)
	}
	lock.Close()
	defer os.Remove(path + ".lock")
	events, err := LoadEvents(path)
	if err != nil {
		return err
	}
	states := map[string]string{}
	ops := map[string]string{}
	for _, v := range events {
		states[v.SessionID] = v.State
		if v.OperationID != "" {
			ops[v.SessionID] = v.OperationID
		}
	}
	found := false
	for _, j := range p.Jobs {
		if j.Session.ID == e.SessionID {
			found = true
			break
		}
		if states[j.Session.ID] != "archived" {
			return errors.New("finish or resolve earlier jobs before recording this task")
		}
	}
	if !found {
		return errors.New("thread is not in plan")
	}
	prev := states[e.SessionID]
	if existing := ops[e.SessionID]; existing != "" && e.OperationID != "" && e.OperationID != existing {
		return errors.New("operation ID differs from the dispatched transfer")
	}
	switch e.State {
	case "started":
		if prev != "" || e.OperationID == "" {
			return errors.New("started requires a new job and handoff operation ID")
		}
	case "verified":
		if (prev != "started" && !(prev == "failed" && ops[e.SessionID] != "")) || e.HostID != p.Destination.ID || !filepath.IsAbs(e.Cwd) || e.Evidence == "" {
			return errors.New("verification requires started handoff, destination host, absolute cwd, and evidence")
		}
	case "archived":
		if prev != "verified" || e.HostID != p.Destination.ID || e.Evidence == "" {
			return errors.New("archive acknowledgement requires verification and destination evidence")
		}
	case "failed":
		if prev == "archived" || e.Evidence == "" {
			return errors.New("failure requires explanation and an unfinished job")
		}
	default:
		return errors.New("state must be started, verified, archived, or failed")
	}
	if e.OperationID == "" {
		e.OperationID = ops[e.SessionID]
	}
	e.Time = time.Now().UTC()
	events = append(events, e)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".events-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	b, err := json.MarshalIndent(events, "", "  ")
	if err == nil {
		_, err = tmp.Write(append(b, '\n'))
	}
	if err == nil {
		err = tmp.Sync()
	}
	tmp.Close()
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

type Preparation struct {
	SSHTarget  string `json:"sshTarget"`
	Root       string `json:"root"`
	Executable string `json:"providerExecutable"`
}
type PreparedRepo struct {
	Origin      string `json:"origin"`
	Path        string `json:"path"`
	ProjectPath string `json:"projectPath"`
	ProjectID   string `json:"projectId"`
	SHA         string `json:"sha"`
	SessionID   string `json:"sessionId"`
}
type Prepared struct {
	PlanID            string         `json:"planId"`
	DestinationHostID string         `json:"destinationHostId"`
	SSH               Preparation    `json:"ssh"`
	CompletedAt       time.Time      `json:"completedAt"`
	Repositories      []PreparedRepo `json:"repositories"`
}
