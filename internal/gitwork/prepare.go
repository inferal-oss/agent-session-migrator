package gitwork

import (
	"github.com/inferal-oss/agent-session-migrator/internal/domain"

	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func SSHCommand(ctx context.Context, target, script string, args ...string) *exec.Cmd {
	remote := "sh -c " + ShellQuote(script) + " sh"
	for _, a := range args {
		remote += " " + ShellQuote(a)
	}
	return exec.CommandContext(ctx, "ssh", "-oBatchMode=yes", "-oConnectTimeout=15", "--", target, remote)
}
func ValidatePreparation(p domain.Preparation) error {
	if p.SSHTarget == "" || strings.HasPrefix(p.SSHTarget, "-") || strings.ContainsAny(p.SSHTarget, " \t\r\n\x00") {
		return errors.New("enter a valid SSH config alias or user@host")
	}
	if !path.IsAbs(p.Root) || path.Clean(p.Root) != p.Root || p.Root == "/" || strings.ContainsAny(p.Root, "\r\n\x00") {
		return errors.New("checkout root must be a clean, absolute remote path other than /")
	}
	if p.Executable == "" {
		return errors.New("remote provider executable is required")
	}
	return nil
}
func OriginIdentity(raw string) (string, error) {
	if strings.ContainsAny(raw, "\r\n\x00") {
		return "", errors.New("invalid repository URL")
	}
	if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "ssh://") {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" {
			return "", errors.New("unsupported repository URL")
		}
		host := strings.ToLower(u.Hostname())
		if u.Port() != "" {
			host += ":" + u.Port()
		}
		repo := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
		if repo == "" {
			return "", errors.New("empty repository path")
		}
		return host + "/" + repo, nil
	}
	// SCP-style Git SSH URL. Relative and absolute local remotes are refused.
	if at := strings.Index(raw, "@"); at > 0 {
		if colon := strings.Index(raw[at+1:], ":"); colon > 0 {
			colon += at + 1
			host := raw[at+1 : colon]
			repo := strings.TrimSuffix(strings.Trim(raw[colon+1:], "/"), ".git")
			if host != "" && repo != "" {
				return strings.ToLower(host) + "/" + repo, nil
			}
		}
	}
	return "", errors.New("repository requires an HTTPS or SSH origin; local-path origins need an explicit mapping")
}
func RemoteRepoPath(root, origin string) (string, error) {
	id, err := OriginIdentity(origin)
	if err != nil {
		return "", err
	}
	base := path.Base(id)
	base = regexp.MustCompile(`[^a-zA-Z0-9._-]+`).ReplaceAllString(base, "-")
	sum := sha256.Sum256([]byte(id))
	return path.Join(root, base+"-"+hex.EncodeToString(sum[:4])), nil
}

const probeScript = `set -eu
repo=$1
if [ -e "$repo" ]; then
  root=$(git -C "$repo" rev-parse --show-toplevel) || exit 21
  [ "$root" = "$repo" ] || { echo 'Existing path is not the expected repository root' >&2; exit 22; }
  git -C "$repo" remote get-url origin
else
  echo __ASM_REPOSITORY_MISSING__
fi`
const cloneScript = `set -eu
repo=$1; origin=$2
[ ! -e "$repo" ] || { echo 'Destination appeared; refusing to overwrite it' >&2; exit 23; }
mkdir -p "$(dirname "$repo")"
git clone -- "$origin" "$repo"`
const hasCommitScript = `git -C "$1" cat-file -e "$2^{commit}"`
const receiveBundleScript = `set -eu
repo=$1; expected=$2; ref=$3
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
cat > "$tmp/transfer.bundle"
git -C "$repo" bundle verify "$tmp/transfer.bundle"
git -C "$repo" fetch --no-tags "$tmp/transfer.bundle" "HEAD:$ref"
git -C "$repo" cat-file -e "$expected^{commit}"`
const verifyProjectScript = `set -eu
mkdir -p "$1"
cd "$1"
[ "$(git rev-parse --show-toplevel)" = "$2" ] || { echo 'Project subdirectory does not belong to the expected repository' >&2; exit 24; }`

func RunSSH(parent context.Context, p domain.Preparation, script string, input io.Reader, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Minute)
	defer cancel()
	cmd := SSHCommand(ctx, p.SSHTarget, script, args...)
	cmd.Stdin = input
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("SSH operation failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
func PreparePlan(ctx context.Context, planPath string, register func(context.Context, domain.Preparation, string, string) (string, error), log io.Writer) error {
	p, err := domain.LoadPlan(planPath)
	if err != nil {
		return err
	}
	if err = ValidatePreparation(p.Preparation); err != nil {
		return err
	}
	lock, err := os.OpenFile(planPath+".prepare.lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("preparation is already running (or a stale .prepare.lock needs review)")
	}
	lock.Close()
	defer os.Remove(planPath + ".prepare.lock")
	if _, err = os.Stat(planPath + ".prepared.json"); err == nil {
		return errors.New("preparation already completed; review the existing receipt instead of repeating it")
	}
	result := domain.Prepared{PlanID: p.ID, DestinationHostID: p.Destination.ID, SSH: p.Preparation}
	visited := map[string]bool{}
	for i, j := range p.Jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		t := j.Session
		fmt.Fprintf(log, "[%d/%d] %s\n", i+1, len(p.Jobs), t.Title)
		if !regexp.MustCompile(`^[0-9a-fA-F]{40,64}$`).MatchString(t.Repo.SHA) {
			return errors.New("invalid source commit")
		}
		current := InspectRepo(t.Cwd)
		if current.Error != "" || current.SHA != t.Repo.SHA || current.CommonDir != t.Repo.CommonDir {
			return fmt.Errorf("source workspace changed for %s; generate a fresh plan", t.Title)
		}
		identity, err := OriginIdentity(t.Repo.Origin)
		if err != nil {
			return fmt.Errorf("%s: %w", t.Title, err)
		}
		currentIdentity, err := OriginIdentity(current.Origin)
		if err != nil || identity != currentIdentity {
			return errors.New("source repository origin changed; generate a fresh plan")
		}
		repo, err := RemoteRepoPath(p.Preparation.Root, t.Repo.Origin)
		if err != nil {
			return err
		}
		if !visited[repo] {
			fmt.Fprintln(log, "  Check repository:", repo)
			origin, err := RunSSH(ctx, p.Preparation, probeScript, nil, repo)
			if err != nil {
				return err
			}
			if origin == "__ASM_REPOSITORY_MISSING__" {
				fmt.Fprintln(log, "  Clone", t.Repo.Origin)
				if _, err = RunSSH(ctx, p.Preparation, cloneScript, nil, repo, t.Repo.Origin); err != nil {
					return err
				}
				origin, err = RunSSH(ctx, p.Preparation, probeScript, nil, repo)
				if err != nil {
					return err
				}
			}
			remoteIdentity, err := OriginIdentity(origin)
			if err != nil || remoteIdentity != identity {
				return fmt.Errorf("destination origin mismatch at %s; refusing to modify it", repo)
			}
			visited[repo] = true
		}
		if _, err = RunSSH(ctx, p.Preparation, hasCommitScript, nil, repo, t.Repo.SHA); err != nil {
			fmt.Fprintln(log, "  Copy unpushed history with a Git bundle")
			dir, err := os.MkdirTemp("", "agent-session-migrate-bundle-*")
			if err != nil {
				return err
			}
			err = func() error {
				defer os.RemoveAll(dir)
				bundle := filepath.Join(dir, "transfer.bundle")
				ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
				defer cancel()
				cmd := exec.CommandContext(ctx, "git", "-C", t.Cwd, "bundle", "create", bundle, "HEAD")
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					return err
				}
				head, err := Git(t.Cwd, "rev-parse", "HEAD")
				if err != nil || head != t.Repo.SHA {
					return errors.New("source HEAD changed while bundling; refusing transfer")
				}
				f, err := os.Open(bundle)
				if err != nil {
					return err
				}
				defer f.Close()
				sum := sha256.Sum256([]byte(t.ID))
				ref := "refs/agent-session-migrator/" + p.ID + "/" + hex.EncodeToString(sum[:8])
				_, err = RunSSH(ctx, p.Preparation, receiveBundleScript, f, repo, t.Repo.SHA, ref)
				return err
			}()
			if err != nil {
				return err
			}
		}
		if t.ProjectSubdir != "" && (path.Clean(t.ProjectSubdir) != strings.TrimSuffix(t.ProjectSubdir, "/") || t.ProjectSubdir == ".." || strings.HasPrefix(t.ProjectSubdir, "../") || path.IsAbs(t.ProjectSubdir)) {
			return errors.New("invalid project subdirectory")
		}
		projectPath := path.Join(repo, t.ProjectSubdir)
		if _, err = RunSSH(ctx, p.Preparation, verifyProjectScript, nil, projectPath, repo); err != nil {
			return fmt.Errorf("project subdirectory must exist in the remote checkout; native handoff has not run: %w", err)
		}
		fmt.Fprintln(log, "  Register saved project:", t.ProjectName, "at", projectPath)
		projectID, err := register(ctx, p.Preparation, t.ProjectName, projectPath)
		if err != nil {
			return fmt.Errorf("repository is prepared but project registration failed: %w", err)
		}
		result.Repositories = append(result.Repositories, domain.PreparedRepo{Origin: t.Repo.Origin, Path: repo, ProjectPath: projectPath, ProjectID: projectID, SHA: t.Repo.SHA, SessionID: t.ID})
	}
	result.CompletedAt = time.Now().UTC()
	if err = domain.WriteNewJSON(planPath+".prepared.json", result); err != nil {
		return err
	}
	fmt.Fprintln(log, "Repository preparation verified. Use the provider executor to transfer sessions.")
	return nil
}
