package gitwork

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
)

func Git(cwd string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	argv := append([]string{"-C", cwd}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
func InspectRepo(cwd string) domain.Repo {
	r := domain.Repo{}
	if !filepath.IsAbs(cwd) {
		r.Error = "invalid workspace path"
		return r
	}
	var err error
	r.Root, err = Git(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		r.Error = "workspace missing or not a Git repository"
		return r
	}
	r.CommonDir, err = Git(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		r.Error = "cannot resolve Git common directory"
		return r
	}
	r.SHA, err = Git(cwd, "rev-parse", "HEAD")
	if err != nil {
		r.Error = "repository has no readable HEAD"
		return r
	}
	r.Branch, _ = Git(cwd, "symbolic-ref", "--short", "-q", "HEAD")
	if r.Branch == "" {
		r.Branch = "detached"
	}
	r.Origin, _ = Git(cwd, "remote", "get-url", "origin")
	r.Origin = RedactOrigin(r.Origin)
	status, err := Git(cwd, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		r.Error = "cannot inspect working tree"
		return r
	}
	r.Dirty = status != ""
	return r
}
func RedactOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" {
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		return u.String()
	}
	return raw
}
func InspectCatalog(c *domain.Catalog) {
	cache := map[string]domain.Repo{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	paths := map[string]bool{}
	for _, t := range c.Sessions {
		if t.HostID == c.LocalHostID {
			paths[t.Cwd] = true
		}
	}
	for _, p := range c.Projects {
		if p.HostID == c.LocalHostID {
			paths[p.Path] = true
		}
	}
	for path := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			sem <- struct{}{}
			r := InspectRepo(path)
			<-sem
			mu.Lock()
			cache[path] = r
			mu.Unlock()
		}(path)
	}
	wg.Wait()
	for i := range c.Sessions {
		t := &c.Sessions[i]
		if t.HostID == c.LocalHostID {
			t.Repo = cache[t.Cwd]
		}
		for _, p := range c.Projects {
			if p.HostID == t.HostID && p.ID == t.ProjectID {
				t.ProjectName = p.Name
				if t.HostID == c.LocalHostID {
					t.ProjectSubdir, _ = Git(p.Path, "rev-parse", "--show-prefix")
				}
				break
			}
		}
		if t.ProjectName == "" && t.Repo.CommonDir != "" {
			for _, p := range c.Projects {
				if p.HostID == t.HostID && cache[p.Path].CommonDir == t.Repo.CommonDir {
					if t.ProjectName != "" {
						t.ProjectName += " / "
					}
					t.ProjectName += p.Name
					t.ProjectSubdir, _ = Git(p.Path, "rev-parse", "--show-prefix")
				}
			}
		}
		if t.ProjectName == "" {
			t.ProjectName = "Unassigned"
		}
	}
}
