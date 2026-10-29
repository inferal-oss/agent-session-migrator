package domain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type HistoryEntry struct {
	Path      string            `json:"path"`
	Plan      Plan              `json:"plan"`
	States    map[string]string `json:"states"`
	Completed int               `json:"completed"`
	Problem   string            `json:"problem,omitempty"`
}

func History(dir string) ([]HistoryEntry, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var result []HistoryEntry
	for _, file := range files {
		if strings.HasSuffix(file, ".events.json") || strings.HasSuffix(file, ".prepared.json") {
			continue
		}
		p, err := LoadPlan(file)
		if err != nil {
			return nil, fmt.Errorf("history plan %s: %w", file, err)
		}
		events, err := LoadEvents(file + ".events.json")
		if err != nil {
			return nil, fmt.Errorf("history journal %s: %w", file, err)
		}
		entry := HistoryEntry{Path: file, Plan: p, States: map[string]string{}}
		for _, e := range events {
			entry.States[e.SessionID] = e.State
		}
		for _, j := range p.Jobs {
			if Terminal(entry.States[j.Session.ID]) {
				entry.Completed++
			}
		}
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Plan.CreatedAt.After(result[j].Plan.CreatedAt) })
	return result, nil
}
func Terminal(state string) bool { return state == "archived" || state == "cancelled" }
func Reserve(c *Catalog, history []HistoryEntry) {
	for i := range c.Sessions {
		c.Sessions[i].ReservedBy = ""
		for _, h := range history {
			if h.Plan.ProviderID != c.ProviderID {
				continue
			}
			for _, j := range h.Plan.Jobs {
				if j.Session.ID == c.Sessions[i].ID && !Terminal(h.States[j.Session.ID]) {
					c.Sessions[i].ReservedBy = h.Path
				}
			}
		}
	}
}
func SavePlan(dir string, p Plan) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".queue.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", errors.New("another queue writer is active; inspect stale locks before retrying")
	}
	lock.Close()
	defer os.Remove(filepath.Join(dir, ".queue.lock"))
	history, err := History(dir)
	if err != nil {
		return "", err
	}
	for _, h := range history {
		if h.Plan.ProviderID != p.ProviderID {
			continue
		}
		for _, a := range h.Plan.Jobs {
			for _, b := range p.Jobs {
				if a.Session.ID == b.Session.ID && !Terminal(h.States[a.Session.ID]) {
					return "", fmt.Errorf("session already reserved by %s", h.Path)
				}
			}
		}
	}
	file, err := filepath.Abs(filepath.Join(dir, p.CreatedAt.Format("20060102-150405")+"-"+p.ID+".json"))
	if err != nil {
		return "", err
	}
	return file, WriteNewJSON(file, p)
}
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
