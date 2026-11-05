package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// App is an app that reads MCP servers from a JSON config file.
type App struct {
	ID   string
	Name string
	// dir must exist for the app to count as installed.
	dir  string
	file string
	// key is the top-level object that holds the servers.
	key   string
	entry func(command string, args []string) map[string]any
}

// config file where portage writes
func (a App) Path() string { return filepath.Join(a.dir, a.file) }

// Checks wherher the app installed or not.
func (a App) Installed() bool {
	info, err := os.Stat(a.dir)
	return err == nil && info.IsDir()
}

// Returns the apps portage can configure on this computer.
func Known() []App {
	config, _ := os.UserConfigDir()
	home, _ := os.UserHomeDir()

	stdio := func(command string, args []string) map[string]any {
		return map[string]any{"command": command, "args": args}
	}
	return []App{
		{
			ID: "claude-desktop", Name: "Claude Desktop",
			dir: claudeDesktopDir(config), file: "claude_desktop_config.json",
			key: "mcpServers", entry: stdio,
		},
		{
			ID: "cursor", Name: "Cursor",
			dir: filepath.Join(home, ".cursor"), file: "mcp.json",
			key: "mcpServers", entry: stdio,
		},
		{
			ID: "vscode", Name: "VS Code",
			dir: filepath.Join(config, "Code", "User"), file: "mcp.json",
			key: "servers",
			entry: func(command string, args []string) map[string]any {
				return map[string]any{"type": "stdio", "command": command, "args": args}
			},
		},
	}
}

// claudeDesktopDir handles the Microsoft Store version of Claude Desktop,
// which keeps its config inside a package folder.
func claudeDesktopDir(config string) string {
	dir := filepath.Join(config, "Claude")
	if runtime.GOOS != "windows" {
		return dir
	}
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	local := os.Getenv("LOCALAPPDATA")
	matches, _ := filepath.Glob(filepath.Join(local, "Packages", "Claude_*", "LocalCache", "Roaming", "Claude"))
	if len(matches) > 0 {
		return matches[0]
	}
	return dir
}

// describes what Add func did.
type Result string

const (
	Added   Result = "added"
	Updated Result = "updated"
	Same    Result = "already set up"
)

// This func puts a "portage" server entry in the app's config
func Add(a App, command string, args []string) (Result, error) {
	path := a.Path()
	config := map[string]any{}
	old, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return "", err
	case len(old) > 0:
		if err := json.Unmarshal(old, &config); err != nil {
			return "", fmt.Errorf("%s isn't plain JSON (it may contain comments), so portage won't change it: %w", path, err)
		}
	}

	servers, _ := config[a.key].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	want := a.entry(command, args)
	result := Added
	if existing, ok := servers["portage"]; ok {
		result = Updated
		if sameJSON(existing, want) {
			return Same, nil
		}
	}
	servers["portage"] = want
	config[a.key] = servers

	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}
	if old != nil {
		if err := os.WriteFile(path+".bak", old, 0o600); err != nil {
			return "", fmt.Errorf("back up %s: %w", path, err)
		}
	}
	out = append(out, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		// Windows refuses to replace a file another program has open.
		os.Remove(tmp)
		if err := os.WriteFile(path, out, 0o600); err != nil {
			return "", fmt.Errorf("couldn't update %s; quit %s and try again: %w", path, a.Name, err)
		}
	}
	return result, nil
}

func sameJSON(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}
