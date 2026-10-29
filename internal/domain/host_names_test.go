package domain

import (
	"path/filepath"
	"testing"
)

func TestFriendlyHostNamePersistsWithoutChangingIdentity(t *testing.T) {
	file := HostNamesPath(filepath.Join(t.TempDir(), "plans"))
	id := "remote-control:example"
	if err := SaveHostName(file, "codex", id, "Build server"); err != nil {
		t.Fatal(err)
	}
	c := Catalog{ProviderID: "codex", LocalHostID: "local", Hosts: []Host{{ID: id, Name: id}}}
	if err := ResolveHostNames(&c, file); err != nil {
		t.Fatal(err)
	}
	if c.Hosts[0].Name != "Build server" || c.Hosts[0].ID != id {
		t.Fatal(c.Hosts[0])
	}
	c.ProviderID = "other"
	c.Hosts[0].Name = id
	if err := ResolveHostNames(&c, file); err != nil {
		t.Fatal(err)
	}
	if c.Hosts[0].Name != "Remote computer" {
		t.Fatal("provider labels leaked across namespaces")
	}
}
func TestNativeHostNamesAndReadableFallback(t *testing.T) {
	c := Catalog{ProviderID: "codex", LocalHostID: "local", Hosts: []Host{{ID: "local", Name: "local"}, {ID: "remote-control:one", Name: "Development server"}, {ID: "remote-control:two", Name: "remote-control:two"}}}
	if err := ResolveHostNames(&c, ""); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"This computer", "Development server", "Remote computer"} {
		if c.Hosts[i].Name != want {
			t.Fatal(c.Hosts[i])
		}
	}
}
func TestHostNameRejectsControlCharacters(t *testing.T) {
	if err := SaveHostName(filepath.Join(t.TempDir(), "names.json"), "codex", "remote-control:example", "Bad\x1bname"); err == nil {
		t.Fatal("accepted terminal control characters")
	}
}
