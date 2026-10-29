package codex

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/inferal-oss/agent-session-migrator/internal/domain"
	"github.com/inferal-oss/agent-session-migrator/internal/gitwork"
	"github.com/inferal-oss/agent-session-migrator/internal/provider"
)

type appClient struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	scanner *bufio.Scanner
	seq     int
}

func (c *appClient) Close() { c.in.Close(); c.cmd.Process.Kill(); c.cmd.Wait() }
func newAppClient(ctx context.Context, executable, sshTarget, home string) (*appClient, error) {
	var cmd *exec.Cmd
	if sshTarget != "" {
		remote := gitwork.ShellQuote(executable) + " app-server --stdio"
		cmd = exec.CommandContext(ctx, "ssh", "-oBatchMode=yes", "-oConnectTimeout=15", "--", sshTarget, remote)
	} else {
		cmd = exec.CommandContext(ctx, executable, "app-server", "--stdio")
		if home != "" {
			cmd.Env = append(os.Environ(), "CODEX_HOME="+home)
		}
	}
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		return nil, err
	}
	c := &appClient{cmd: cmd, in: in, scanner: bufio.NewScanner(out)}
	c.scanner.Buffer(make([]byte, 4096), 16*1024*1024)
	var init json.RawMessage
	if err = c.Call("initialize", map[string]any{"clientInfo": map[string]string{"name": "agent_session_migrator", "version": "0.2.0"}, "capabilities": map[string]bool{"experimentalApi": true}}, &init); err != nil {
		c.Close()
		return nil, err
	}
	if _, err = io.WriteString(in, "{\"method\":\"initialized\"}\n"); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *appClient) Call(method string, params, result any) error {
	c.seq++
	id := c.seq
	b, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err = c.in.Write(append(b, '\n')); err != nil {
		return err
	}
	for c.scanner.Scan() {
		var r struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err = json.Unmarshal(c.scanner.Bytes(), &r); err != nil {
			return fmt.Errorf("unexpected app-server output: %w", err)
		}
		var responseID int
		if json.Unmarshal(r.ID, &responseID) != nil || responseID != id {
			continue
		}
		if len(r.Error) > 0 && string(r.Error) != "null" {
			return fmt.Errorf("app-server %s: %s", method, r.Error)
		}
		return json.Unmarshal(r.Result, result)
	}
	if err = c.scanner.Err(); err != nil {
		return err
	}
	return errors.New("app-server closed before responding")
}
func registerRemoteProject(parent context.Context, p domain.Preparation, name, root string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	client, err := newAppClient(ctx, p.Executable, p.SSHTarget, "")
	if err != nil {
		return "", err
	}
	defer client.Close()
	cursor := ""
	seen := map[string]bool{}
	for {
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var list struct {
			Data []struct {
				ID    string `json:"id"`
				Roots []struct {
					Path string `json:"path"`
				} `json:"roots"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err = client.Call("project/list", params, &list); err != nil {
			return "", err
		}
		for _, p := range list.Data {
			for _, r := range p.Roots {
				if r.Path == root && p.ID != "" {
					return p.ID, nil
				}
			}
		}
		if list.NextCursor == nil || *list.NextCursor == "" {
			break
		}
		if seen[*list.NextCursor] {
			return "", errors.New("project pagination repeated a cursor")
		}
		cursor = *list.NextCursor
		seen[cursor] = true
	}
	sum := sha256.Sum256([]byte(root))
	var created struct {
		Project struct {
			ID    string `json:"id"`
			Roots []struct {
				Path string `json:"path"`
			} `json:"roots"`
		} `json:"project"`
	}
	if err = client.Call("project/create", map[string]any{"idempotencyKey": "agent-session-migrator-" + hex.EncodeToString(sum[:]), "name": name, "roots": []map[string]string{{"path": root}}}, &created); err != nil {
		return "", err
	}
	for _, r := range created.Project.Roots {
		if r.Path == root && created.Project.ID != "" {
			return created.Project.ID, nil
		}
	}
	return "", errors.New("created project did not confirm the requested root")
}
func (p *Provider) CreateCoordinator(parent context.Context, o provider.DiscoveryOptions, name, dir string) (domain.Session, error) {
	var empty domain.Session
	if name == "" || !filepath.IsAbs(dir) {
		return empty, errors.New("coordinator requires a title and absolute working directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return empty, err
	}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	client, err := newAppClient(ctx, "codex", "", o.Home)
	if err != nil {
		return empty, err
	}
	defer client.Close()
	var started struct {
		Thread struct {
			ID  string `json:"id"`
			Cwd string `json:"cwd"`
		} `json:"thread"`
	}
	if err = client.Call("thread/start", map[string]any{"cwd": dir, "ephemeral": false, "developerInstructions": "This conversation coordinates reviewed agent-session-migrator plans. Wait for the human to provide a plan; verify destination and Git state before archival. Do not implement unrelated repository work."}, &started); err != nil {
		return empty, err
	}
	if started.Thread.ID == "" {
		return empty, errors.New("provider did not return a coordinator ID")
	}
	var result json.RawMessage
	if err = client.Call("thread/name/set", map[string]string{"threadId": started.Thread.ID, "name": name}, &result); err != nil {
		return empty, fmt.Errorf("coordinator %s created but naming failed: %w", started.Thread.ID, err)
	}
	return domain.Session{ID: started.Thread.ID, Title: name, Cwd: dir, HostID: "local", Status: "idle"}, nil
}
