package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
	"github.com/inferal-oss/agent-session-migrator/internal/gitwork"
	"github.com/inferal-oss/agent-session-migrator/internal/provider"
)

var _ provider.Provider = New()

func TestEmbeddedGuideMatchesPublishedDocumentation(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "providers", "codex-runner.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != New().ExecutionGuide() {
		t.Fatal("embedded guide differs from published documentation")
	}
}

func TestDestinationBindingRejectsWrongHostBeforeGitUpload(t *testing.T) {
	if err := matchHost("remote-control:expected", "other"); err == nil {
		t.Fatal("accepted wrong SSH target")
	}
	if err := matchHost("remote-control:expected", ""); err == nil {
		t.Fatal("accepted absent native identity")
	}
	if err := matchHost("remote-control:expected", "expected"); err != nil {
		t.Fatal(err)
	}
}

func TestLocalInventorySkipsUnknownAndRemotePlacement(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	home := t.TempDir()
	db := filepath.Join(home, "state_5.sqlite")
	query := `CREATE TABLE threads(id TEXT,name TEXT,title TEXT,cwd TEXT,project_id TEXT,is_pinned INTEGER,archived INTEGER,has_user_event INTEGER,attached_thread_parent_id TEXT,source TEXT,recency_at_ms INTEGER,updated_at INTEGER);INSERT INTO threads VALUES ('local',NULL,'Local session','/repo','project',1,0,0,NULL,'vscode',3,3),('remote',NULL,'Remote session','/repo','project',0,0,1,NULL,'vscode',2,2),('unknown',NULL,'Unknown session','/repo','project',0,0,1,NULL,'vscode',1,1);`
	if out, err := exec.Command("sqlite3", db, query).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	state := map[string]any{"thread-project-membership-host-ids": map[string]string{"local": "local", "remote": "remote-control:example"}}
	b, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(home, ".codex-global-state.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	c := domain.Catalog{LocalHostID: "local"}
	if err := supplementLocal(&c, home); err != nil {
		t.Fatal(err)
	}
	if len(c.Sessions) != 1 || c.Sessions[0].ID != "local" || !c.Sessions[0].Pinned {
		t.Fatalf("unexpected inventory: %#v", c.Sessions)
	}
}
func TestProviderRejectsCatalogFromAnotherProvider(t *testing.T) {
	file := filepath.Join(t.TempDir(), "catalog.json")
	if err := domain.WriteNewJSON(file, domain.Catalog{Version: 1, ProviderID: "other"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := New().Discover(context.Background(), provider.DiscoveryOptions{CatalogPath: file}); err == nil {
		t.Fatal("foreign provider catalog accepted")
	}
}
func TestProjectRegistrationProtocol(t *testing.T) {
	if os.Getenv("ASM_TEST_RPC_HELPER") == "1" {
		dec := json.NewDecoder(os.Stdin)
		enc := json.NewEncoder(os.Stdout)
		for {
			var r struct {
				ID     int             `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := dec.Decode(&r); err != nil {
				os.Exit(0)
			}
			if r.ID == 0 {
				continue
			}
			var result any
			switch r.Method {
			case "initialize":
				result = map[string]any{"userAgent": "test"}
			case "project/list":
				result = map[string]any{"data": []any{}, "nextCursor": nil}
			case "project/create":
				var params struct {
					Name  string `json:"name"`
					Roots []struct {
						Path string `json:"path"`
					} `json:"roots"`
				}
				if err := json.Unmarshal(r.Params, &params); err != nil || params.Name != "Example" || len(params.Roots) != 1 || params.Roots[0].Path != "/srv/repos/example" {
					os.Exit(3)
				}
				result = map[string]any{"project": map[string]any{"id": "new-project", "roots": params.Roots}}
			default:
				os.Exit(4)
			}
			if err := enc.Encode(map[string]any{"id": r.ID, "result": result}); err != nil {
				os.Exit(5)
			}
		}
	}
	// The helper is this Go test binary, reached through a fake SSH command.
	// No Python, network, credentials, or live provider database are needed.
	dir := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec " + gitwork.ShellQuote(binary) + " '-test.run=^TestProjectRegistrationProtocol$'\n"
	if err = os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASM_TEST_RPC_HELPER", "1")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	id, err := registerRemoteProject(context.Background(), domain.Preparation{SSHTarget: "example", Executable: "codex"}, "Example", "/srv/repos/example")
	if err != nil || id != "new-project" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}
