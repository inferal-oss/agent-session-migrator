package domain

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T) (Catalog, string) {
	t.Helper()
	c := Catalog{ProviderID: "test", Version: 1, GeneratedAt: time.Now(), CoordinatorSessionID: "coordinator", LocalHostID: "local", Hosts: []Host{{ID: "local", Name: "Local"}, {ID: "remote", Name: "Remote"}}, Sessions: []Session{{ID: "one", Title: "First", HostID: "local", Status: "idle", Repo: Repo{Root: "/repo"}}, {ID: "two", Title: "Second", HostID: "local", Status: "idle", Repo: Repo{Root: "/repo"}}, {ID: "coordinator", HostID: "local", Status: "idle", Repo: Repo{Root: "/repo"}}}}
	return c, filepath.Join(t.TempDir(), "plan.json")
}
func TestProtectedAndRunningSessions(t *testing.T) {
	c, _ := fixture(t)
	h := c.Hosts[1]
	if _, err := NewPlan(c, h, map[string]bool{"coordinator": true}, true); err == nil {
		t.Fatal("coordinator must remain protected even with interruption enabled")
	}
	c.Sessions[0].Status = "active"
	if _, err := NewPlan(c, h, map[string]bool{"one": true}, false); err == nil {
		t.Fatal("running session allowed without opt-in")
	}
	if _, err := NewPlan(c, h, map[string]bool{"one": true}, true); err != nil {
		t.Fatal(err)
	}
}
func TestNoArchiveBeforeVerifiedAndSerialOrder(t *testing.T) {
	c, file := fixture(t)
	p, err := NewPlan(c, c.Hosts[1], map[string]bool{"one": true, "two": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteNewJSON(file, p); err != nil {
		t.Fatal(err)
	}
	if err = RecordEvent(file, Event{SessionID: "one", State: "archived", HostID: "remote", Evidence: "claimed"}); err == nil {
		t.Fatal("archived without verification")
	}
	if err = RecordEvent(file, Event{SessionID: "two", State: "started", OperationID: "op2"}); err == nil {
		t.Fatal("second session started before first completed")
	}
	if err = RecordEvent(file, Event{SessionID: "one", State: "started", OperationID: "op1"}); err != nil {
		t.Fatal(err)
	}
	if err = RecordEvent(file, Event{SessionID: "one", State: "verified", HostID: "wrong-host", Cwd: "/repo", Evidence: "success"}); err == nil {
		t.Fatal("accepted wrong destination")
	}
	if err = RecordEvent(file, Event{SessionID: "one", State: "verified", HostID: "remote", Cwd: "/repo", Evidence: "native completion and destination readback"}); err != nil {
		t.Fatal(err)
	}
	if err = RecordEvent(file, Event{SessionID: "one", State: "archived", HostID: "remote", Evidence: "archive tool succeeded"}); err != nil {
		t.Fatal(err)
	}
	if err = RecordEvent(file, Event{SessionID: "two", State: "started", OperationID: "op2"}); err != nil {
		t.Fatal(err)
	}
	events, err := LoadEvents(file + ".events.json")
	if err != nil || len(events) != 4 {
		t.Fatalf("events=%v err=%v", events, err)
	}
	if events[2].OperationID != "op1" {
		t.Fatal("lost operation identity")
	}
}
func TestPlansArePrivateAndNeverOverwritten(t *testing.T) {
	c, file := fixture(t)
	p, err := NewPlan(c, c.Hosts[1], map[string]bool{"one": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteNewJSON(file, p); err != nil {
		t.Fatal(err)
	}
	if err = WriteNewJSON(file, p); err == nil {
		t.Fatal("overwrote existing plan")
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("plan mode %v", info.Mode())
	}
}
func TestPlanRejectsDuplicateSessions(t *testing.T) {
	c, file := fixture(t)
	p, err := NewPlan(c, c.Hosts[1], map[string]bool{"one": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	p.Jobs = append(p.Jobs, p.Jobs[0])
	if err = WriteNewJSON(file, p); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadPlan(file); err == nil {
		t.Fatal("duplicate session accepted")
	}
}
