package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type hostNames struct {
	Version   int                          `json:"version"`
	Providers map[string]map[string]string `json:"providers"`
}

func HostNamesPath(plansDir string) string {
	if plansDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(filepath.Clean(plansDir)), "host-names.json")
}
func readHostNames(file string) (hostNames, error) {
	v := hostNames{Version: 1, Providers: map[string]map[string]string{}}
	if file == "" {
		return v, nil
	}
	err := ReadJSON(file, &v)
	if os.IsNotExist(err) {
		return hostNames{Version: 1, Providers: map[string]map[string]string{}}, nil
	}
	if err != nil {
		return v, err
	}
	if v.Version != 1 {
		return v, fmt.Errorf("unsupported host-name settings version")
	}
	if v.Providers == nil {
		v.Providers = map[string]map[string]string{}
	}
	return v, nil
}

// ResolveHostNames changes presentation only. Native IDs remain authoritative.
func ResolveHostNames(c *Catalog, file string) error {
	v, err := readHostNames(file)
	for i := range c.Hosts {
		h := &c.Hosts[i]
		if name := v.Providers[c.ProviderID][h.ID]; name != "" {
			h.Name = name
			continue
		}
		if h.Name == "" || h.Name == h.ID {
			if h.ID == c.LocalHostID {
				h.Name = "This computer"
			} else {
				h.Name = "Remote computer"
			}
		}
	}
	return err
}

func SaveHostName(file, providerID, hostID, name string) error {
	name = strings.TrimSpace(name)
	if file == "" || providerID == "" || hostID == "" || name == "" || utf8.RuneCountInString(name) > 80 {
		return fmt.Errorf("enter a display name of 1–80 characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("display names must be a single line without control characters")
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(file+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("host-name settings are locked: %w", err)
	}
	lock.Close()
	defer os.Remove(file + ".lock")
	v, err := readHostNames(file)
	if err != nil {
		return err
	}
	if v.Providers[providerID] == nil {
		v.Providers[providerID] = map[string]string{}
	}
	v.Providers[providerID][hostID] = name
	tmp, err := os.CreateTemp(filepath.Dir(file), ".host-names-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err = enc.Encode(v); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(tmp.Name(), file); err != nil {
		return err
	}
	return syncDir(filepath.Dir(file))
}
