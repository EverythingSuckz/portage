package apps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testApp(t *testing.T, key string) App {
	t.Helper()
	for _, a := range Known() {
		if a.key == key {
			a.dir = t.TempDir()
			return a
		}
	}
	t.Fatalf("no app with key %q", key)
	return App{}
}

func read(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAddCreatesConfig(t *testing.T) {
	a := testApp(t, "mcpServers")

	res, err := Add(a, "/bin/portage", []string{"serve"})
	if err != nil || res != Added {
		t.Fatalf("Add = %q, %v", res, err)
	}
	server := read(t, a.Path())["mcpServers"].(map[string]any)["portage"].(map[string]any)
	if server["command"] != "/bin/portage" {
		t.Errorf("entry = %v", server)
	}
	if _, err := os.Stat(a.Path() + ".bak"); err == nil {
		t.Error("made a backup of a file that didn't exist")
	}
}

func TestAddKeepsOtherSettingsAndBacksUp(t *testing.T) {
	a := testApp(t, "mcpServers")
	original := `{"theme":"dark","mcpServers":{"other":{"command":"x"}}}`
	os.WriteFile(a.Path(), []byte(original), 0o600)

	if _, err := Add(a, "/bin/portage", []string{"serve"}); err != nil {
		t.Fatal(err)
	}
	cfg := read(t, a.Path())
	servers := cfg["mcpServers"].(map[string]any)
	if cfg["theme"] != "dark" || servers["other"] == nil || servers["portage"] == nil {
		t.Errorf("config = %v", cfg)
	}
	if b, _ := os.ReadFile(a.Path() + ".bak"); string(b) != original {
		t.Errorf("backup = %q", b)
	}
}

func TestAddUpdatesThenReportsSame(t *testing.T) {
	a := testApp(t, "mcpServers")
	Add(a, "/old/portage", []string{"serve"})

	if res, _ := Add(a, "/new/portage", []string{"serve"}); res != Updated {
		t.Errorf("changed path = %q, want updated", res)
	}
	if res, _ := Add(a, "/new/portage", []string{"serve"}); res != Same {
		t.Errorf("same path = %q, want already set up", res)
	}
}

func TestAddRefusesJSONWithComments(t *testing.T) {
	a := testApp(t, "servers")
	original := "{\n  // my servers\n  \"servers\": {}\n}"
	os.WriteFile(a.Path(), []byte(original), 0o600)

	_, err := Add(a, "/bin/portage", []string{"serve"})
	if err == nil || !strings.Contains(err.Error(), "won't change it") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(a.Path()); string(b) != original {
		t.Error("file was changed")
	}
}

func TestVSCodeEntryHasType(t *testing.T) {
	a := testApp(t, "servers")
	Add(a, "/bin/portage", []string{"serve"})

	server := read(t, a.Path())["servers"].(map[string]any)["portage"].(map[string]any)
	if server["type"] != "stdio" {
		t.Errorf("entry = %v", server)
	}
}

func TestInstalled(t *testing.T) {
	a := testApp(t, "mcpServers")
	if !a.Installed() {
		t.Error("existing folder should count as installed")
	}
	a.dir = filepath.Join(a.dir, "missing")
	if a.Installed() {
		t.Error("missing folder should not count as installed")
	}
}
