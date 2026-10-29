package domain

import (
	"errors"
	"fmt"
)

func ValidateCatalog(c Catalog) error {
	if c.Version != 1 || c.ProviderID == "" || c.CoordinatorSessionID == "" || c.LocalHostID == "" || len(c.Hosts) == 0 {
		return errors.New("unsupported or incomplete provider catalog")
	}
	seen := map[string]bool{}
	for _, h := range c.Hosts {
		if h.ID == "" || seen[h.ID] {
			return errors.New("invalid or duplicate host")
		}
		seen[h.ID] = true
	}
	seen = map[string]bool{}
	for _, s := range c.Sessions {
		if s.ID == "" || seen[s.ID] {
			return fmt.Errorf("invalid or duplicate session %q", s.ID)
		}
		seen[s.ID] = true
	}
	return nil
}
