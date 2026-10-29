package gitwork

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
)

func TestRepositoryIdentityAcrossTransports(t *testing.T) {
	want := "github.com/team/repo"
	for _, in := range []string{"git@github.com:team/repo.git", "https://github.com/team/repo.git", "ssh://git@github.com/team/repo.git"} {
		got, err := OriginIdentity(in)
		if err != nil || got != want {
			t.Fatalf("%q => %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"/tmp/repo", "../repo", "file:///tmp/repo", "https://github.com/", "git@github.com:repo\nmalicious"} {
		if _, err := OriginIdentity(in); err == nil {
			t.Fatalf("accepted %q", in)
		}
	}
}
func TestRemotePathsSeparateSameNamedRepositories(t *testing.T) {
	a, err := RemoteRepoPath("/srv/repos", "git@github.com:team-a/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	b, err := RemoteRepoPath("/srv/repos", "git@github.com:team-b/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !strings.HasPrefix(a, "/srv/repos/repo-") {
		t.Fatalf("ambiguous remote paths %q %q", a, b)
	}
}
func TestShellQuoteNeverEvaluatesInput(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "injected")
	payload := "a'$(touch " + marker + ")`touch " + marker + "`\nend"
	cmd := exec.Command("sh", "-c", "printf %s "+ShellQuote(payload))
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != payload {
		t.Fatalf("payload changed: %q", out)
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("shell injection executed")
	}
}
func TestPreparationValidation(t *testing.T) {
	valid := domain.Preparation{SSHTarget: "devbox", Root: "/srv/repos", Executable: "codex"}
	if err := ValidatePreparation(valid); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"/", "relative", "/srv/../repos", "/srv/repos\nother"} {
		v := valid
		v.Root = root
		if err := ValidatePreparation(v); err == nil {
			t.Fatalf("accepted root %q", root)
		}
	}
	v := valid
	v.SSHTarget = "-oProxyCommand=bad"
	if err := ValidatePreparation(v); err == nil {
		t.Fatal("accepted SSH option injection")
	}
}
func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}
func runScript(t *testing.T, script string, input []byte, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"-c", script, "sh"}, args...)...)
	cmd.Stdin = bytes.NewReader(input)
	return cmd.CombinedOutput()
}
func TestBundleTransfersUnpushedCommitWithoutChangingDestinationWorktree(t *testing.T) {
	source := t.TempDir()
	dest := filepath.Join(t.TempDir(), "remote")
	gitTest(t, source, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(source, "data"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, source, "add", "data")
	gitTest(t, source, "-c", "commit.gpgsign=false", "commit", "-m", "base")
	gitTest(t, source, "clone", source, dest)
	base := gitTest(t, dest, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(source, "data"), []byte("unpushed"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, source, "add", "data")
	gitTest(t, source, "-c", "commit.gpgsign=false", "commit", "-m", "unpushed")
	head := gitTest(t, source, "rev-parse", "HEAD")
	bundle := filepath.Join(t.TempDir(), "transfer.bundle")
	gitTest(t, source, "bundle", "create", bundle, "HEAD")
	b, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := runScript(t, receiveBundleScript, b, dest, head, "refs/agent-session-migrator/test/session"); err != nil {
		t.Fatalf("bundle receive: %s: %v", out, err)
	}
	if got := gitTest(t, dest, "rev-parse", "HEAD"); got != base {
		t.Fatal("destination HEAD moved")
	}
	if got := gitTest(t, dest, "show", head+":data"); got != "unpushed" {
		t.Fatal("unpushed commit missing")
	}
	if b, err = os.ReadFile(filepath.Join(dest, "data")); err != nil || string(b) != "base" {
		t.Fatal("destination working tree changed")
	}
}
func TestCloneScriptRefusesExistingPath(t *testing.T) {
	dest := t.TempDir()
	marker := filepath.Join(dest, "keep")
	if err := os.WriteFile(marker, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := runScript(t, cloneScript, nil, dest, "https://example.invalid/repo.git"); err == nil {
		t.Fatalf("existing destination accepted: %s", out)
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "preserve" {
		t.Fatal("existing files modified")
	}
}
