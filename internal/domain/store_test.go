package domain

import (
	"path/filepath"
	"testing"
)

func TestReservationsSurviveRestartAndCancellation(t *testing.T) {
	c, file := fixture(t)
	dir := filepath.Dir(file)
	p, err := NewPlan(c, c.Hosts[1], map[string]bool{"one": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := SavePlan(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = SavePlan(dir, p); err == nil {
		t.Fatal("reserved session queued twice")
	}
	history, err := History(dir)
	if err != nil {
		t.Fatal(err)
	}
	Reserve(&c, history)
	if c.Sessions[0].ReservedBy != saved {
		t.Fatal("reservation missing after reopening history")
	}
	if err = RecordEvent(saved, Event{SessionID: "one", State: "cancelled"}); err != nil {
		t.Fatal(err)
	}
	p.ID = "second-plan"
	if _, err = SavePlan(dir, p); err != nil {
		t.Fatal("unstarted cancelled session should be selectable again:", err)
	}
}
func TestUncertainIntentCannotBeRetriedOrCancelled(t *testing.T) {
	c, file := fixture(t)
	p, err := NewPlan(c, c.Hosts[1], map[string]bool{"one": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteNewJSON(file, p); err != nil {
		t.Fatal(err)
	}
	if err = RecordEvent(file, Event{SessionID: "one", State: "intent"}); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"intent", "cancelled"} {
		if err = RecordEvent(file, Event{SessionID: "one", State: state}); err == nil {
			t.Fatalf("uncertain dispatch accepted %s", state)
		}
	}
	if err = RecordEvent(file, Event{SessionID: "one", State: "retryable", Evidence: "Native operation conclusively failed; source state and location checked"}); err != nil {
		t.Fatal(err)
	}
	if err = RecordEvent(file, Event{SessionID: "one", State: "intent"}); err != nil {
		t.Fatal(err)
	}
}
