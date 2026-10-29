package gitwork

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
)

const untrackedArchiveScript = `set -eu
cd "$1"
list=$(mktemp)
trap 'rm -f "$list"' EXIT HUP INT TERM
git ls-files --others --exclude-standard -z > "$list"
tar --null -T "$list" -cf -`

// StateDigest identifies Git-visible staged, unstaged, and untracked state.
// Ignored files are intentionally outside native Git session migration.
func StateDigest(ctx context.Context, cwd string, remote *domain.Preparation) (string, error) {
	command := func(args ...string) *exec.Cmd {
		var cmd *exec.Cmd
		if remote == nil {
			cmd = exec.CommandContext(ctx, args[0], args[1:]...)
			cmd.Dir = cwd
			cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
		} else {
			cmd = SSHCommand(ctx, remote.SSHTarget, `cd "$1"; shift; export GIT_OPTIONAL_LOCKS=0; exec "$@"`, append([]string{cwd}, args...)...)
		}
		return cmd
	}
	root, err := command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	cwd = strings.TrimSpace(string(root))
	headCmd := func() (string, error) {
		b, err := command("git", "rev-parse", "HEAD").Output()
		return strings.TrimSpace(string(b)), err
	}
	head, err := headCmd()
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	fmt.Fprintf(sum, "head:%s\n", head)
	for _, cached := range []bool{true, false} {
		args := []string{"git", "-c", "core.quotePath=true", "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--binary", "--no-renames", "--diff-algorithm=myers", "--src-prefix=a/", "--dst-prefix=b/"}
		if cached {
			args = append(args, "--cached")
		}
		cmd := command(args...)
		part := sha256.New()
		cmd.Stdout = part
		if err = cmd.Run(); err != nil {
			return "", fmt.Errorf("snapshot diff: %w", err)
		}
		fmt.Fprintf(sum, "diff:%t:%x\n", cached, part.Sum(nil))
	}
	var archive *exec.Cmd
	if remote == nil {
		archive = exec.CommandContext(ctx, "sh", "-c", untrackedArchiveScript, "sh", cwd)
		archive.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	} else {
		archive = SSHCommand(ctx, remote.SSHTarget, untrackedArchiveScript, cwd)
	}
	stream, err := archive.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err = archive.Start(); err != nil {
		return "", err
	}
	readErr := func() error {
		reader := tar.NewReader(stream)
		for {
			hdr, err := reader.Next()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			switch hdr.Typeflag {
			case tar.TypeReg, tar.TypeRegA, tar.TypeSymlink:
			default:
				return fmt.Errorf("unsupported untracked file type for %q", hdr.Name)
			}
			content := sha256.New()
			if _, err = io.Copy(content, reader); err != nil {
				return err
			}
			fmt.Fprintf(sum, "file:%d:%s:%d:%d:%d:%s:%x\n", len(hdr.Name), hdr.Name, hdr.Typeflag, hdr.Mode, len(hdr.Linkname), hdr.Linkname, content.Sum(nil))
		}
	}()
	if readErr != nil {
		archive.Process.Kill()
	}
	waitErr := archive.Wait()
	if readErr != nil {
		return "", readErr
	}
	if waitErr != nil {
		return "", fmt.Errorf("snapshot untracked files: %w", waitErr)
	}
	after, err := headCmd()
	if err != nil || after != head {
		return "", errors.New("HEAD changed during snapshot")
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
func SealPlan(ctx context.Context, p domain.Plan) (domain.Plan, error) {
	for i := range p.Jobs {
		s := &p.Jobs[i].Session
		if err := validateSnapshotCapabilities(ctx, s.Repo.Root); err != nil {
			return p, err
		}
		current := InspectRepo(s.Cwd)
		if current.Error != "" || current.SHA != s.Repo.SHA || current.CommonDir != s.Repo.CommonDir {
			return p, fmt.Errorf("source changed for %s; refresh inventory", s.Title)
		}
		digest, err := StateDigest(ctx, s.Repo.Root, nil)
		if err != nil {
			return p, err
		}
		again, err := StateDigest(ctx, s.Repo.Root, nil)
		if err != nil || again != digest {
			return p, errors.New("working state changed during snapshot; retry when the session is idle")
		}
		s.Repo.StateDigest = digest
	}
	p.Version = 2
	return p, nil
}

func validateSnapshotCapabilities(ctx context.Context, root string) error {
	stages, err := Git(root, "ls-files", "--stage", "-z")
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(stages, "\x00") {
		if strings.HasPrefix(entry, "160000 ") {
			return errors.New("submodule workspaces need explicit preservation support before automated migration")
		}
	}
	files, err := Git(root, "ls-files", "-z")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "check-attr", "-z", "--stdin", "filter")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = strings.NewReader(files)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	fields := strings.Split(string(out), "\x00")
	for i := 2; i < len(fields); i += 3 {
		if fields[i] != "unspecified" && fields[i] != "unset" && fields[i] != "" {
			return fmt.Errorf("working-tree filter %q requires explicit data preservation support", fields[i])
		}
	}
	return nil
}
func CheckSource(ctx context.Context, s domain.Session) error {
	if s.Repo.StateDigest == "" {
		return errors.New("plan has no source working-state snapshot")
	}
	current := InspectRepo(s.Cwd)
	if current.Error != "" || current.SHA != s.Repo.SHA || current.CommonDir != s.Repo.CommonDir {
		return errors.New("source repository or HEAD changed")
	}
	got, err := StateDigest(ctx, s.Repo.Root, nil)
	if err != nil {
		return err
	}
	if got != s.Repo.StateDigest {
		return errors.New("staged, unstaged, or untracked state changed; create a fresh plan")
	}
	return nil
}
