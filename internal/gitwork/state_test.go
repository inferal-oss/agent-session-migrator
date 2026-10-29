package gitwork

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
)

func committedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitTest(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "data"), []byte("committed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", "data")
	gitTest(t, dir, "-c", "commit.gpgsign=false", "commit", "-m", "base")
	return dir
}
func TestCommonDirectoryIdentifiesRepositoriesAndWorktrees(t *testing.T) {
	a := committedRepo(t)
	b := committedRepo(t)
	ra, rb := InspectRepo(a), InspectRepo(b)
	if ra.Error != "" || rb.Error != "" || !filepath.IsAbs(ra.CommonDir) || ra.CommonDir == rb.CommonDir {
		t.Fatalf("invalid identities: %#v %#v", ra, rb)
	}
	worktree := filepath.Join(t.TempDir(), "linked")
	gitTest(t, a, "worktree", "add", "--detach", worktree)
	rw := InspectRepo(worktree)
	if rw.Error != "" || rw.CommonDir != ra.CommonDir {
		t.Fatalf("worktree identity mismatch: %#v", rw)
	}
}
func TestSnapshotDistinguishesStagedUnstagedAndUntrackedState(t *testing.T) {
	ctx := context.Background()
	dir := committedRepo(t)
	base, err := StateDigest(ctx, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "data")
	if err = os.WriteFile(file, []byte("modified\n"), 0644); err != nil {
		t.Fatal(err)
	}
	unstaged, err := StateDigest(ctx, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", "data")
	staged, err := StateDigest(ctx, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if base == unstaged || staged == unstaged {
		t.Fatal("snapshot lost index state")
	}
	untracked := filepath.Join(dir, "untracked\nname")
	if err = os.WriteFile(untracked, []byte("private\x00bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	one, err := StateDigest(ctx, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(untracked, []byte("changed\x00bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	two, err := StateDigest(ctx, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if staged == one || one == two {
		t.Fatal("snapshot lost untracked file content")
	}
}
func TestSnapshotIsIndependentOfCheckoutLocation(t *testing.T) {
	ctx := context.Background()
	source := committedRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")
	gitTest(t, source, "clone", source, dest)
	a, err := StateDigest(ctx, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := StateDigest(ctx, dest, nil)
	if err != nil || a != b {
		t.Fatalf("equivalent checkouts differ: %s %s %v", a, b, err)
	}
}

func fakeLocalSSH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
for arg do last=$arg; done
exec sh -c "$last"
`
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
func TestRemoteSnapshotMatchesSourceIncludingUntrackedFiles(t *testing.T) {
	fakeLocalSSH(t)
	ctx := context.Background()
	dir := committedRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "untracked\nfile"), []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	local, err := StateDigest(ctx, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := StateDigest(ctx, dir, &domain.Preparation{SSHTarget: "test"})
	if err != nil || local != remote {
		t.Fatalf("local=%s remote=%s err=%v", local, remote, err)
	}
}

func TestUnsupportedNestedAndFilteredDataStopsSnapshotting(t *testing.T) {
	ctx := context.Background()
	t.Run("filter", func(t *testing.T) {
		dir := committedRepo(t)
		if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("data filter=lfs\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := validateSnapshotCapabilities(ctx, dir); err == nil {
			t.Fatal("accepted LFS-filtered data without object preservation")
		}
	})
	t.Run("submodule", func(t *testing.T) {
		dir := committedRepo(t)
		head := gitTest(t, dir, "rev-parse", "HEAD")
		gitTest(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+head+",module")
		if err := validateSnapshotCapabilities(ctx, dir); err == nil {
			t.Fatal("accepted nested repository without data preservation")
		}
	})
}
