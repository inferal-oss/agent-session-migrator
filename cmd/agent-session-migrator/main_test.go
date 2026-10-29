package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCobraCommandsAndRequiredFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
		fail bool
	}{{[]string{"--help"}, "completion", false}, {[]string{"prepare", "--help"}, "--plan", false}, {[]string{"prepare"}, "required flag", true}, {[]string{"status"}, "accepts 1 arg", true}, {[]string{"providers"}, "codex", false}, {[]string{"completion", "zsh"}, "#compdef", false}, {[]string{"record", "--plan", "x", "--session", "s", "--state", "verified"}, "use verify", true}} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cmd := newRoot()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if tc.fail != (err != nil) {
				t.Fatalf("error=%v output=%s", err, out.String())
			}
			text := out.String()
			if err != nil {
				text += err.Error()
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("expected %q in %q", tc.want, text)
			}
		})
	}
}
func TestListDoesNotRequireCoordinator(t *testing.T) {
	cmd := newRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--demo", "--list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"providerId": "demo"`) {
		t.Fatal(out.String())
	}
}
