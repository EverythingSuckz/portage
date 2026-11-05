package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"go.wrench.rocks/portage/internal/apps"
)

const claudeCode = "claude-code"

func installApps(args []string) error {
	dataDir, fs := flags("install", args, nil)
	dataSet := os.Getenv("PORTAGE_DATA") != ""
	fs.Visit(func(f *flag.Flag) { dataSet = dataSet || f.Name == "data" })

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if strings.Contains(exe, "go-build") {
		return errors.New("this copy of portage is temporary (from go run); build or install portage first, then run portage install from there")
	}

	serveArgs := []string{"serve"}
	if dataSet {
		abs, err := filepath.Abs(dataDir)
		if err != nil {
			return err
		}
		serveArgs = append(serveArgs, "-data", abs)
	}

	known := apps.Known()
	ids := []string{claudeCode}
	for _, a := range known {
		ids = append(ids, a.ID)
	}
	wanted := fs.Args()
	for _, id := range wanted {
		if strings.HasPrefix(id, "-") {
			return fmt.Errorf("put %s before the app names, for example: portage install -data <dir> claude-desktop", id)
		}
		if !slices.Contains(ids, id) {
			return fmt.Errorf("unknown app %q (choose from: %s)", id, strings.Join(ids, ", "))
		}
	}
	pick := func(id string) bool { return len(wanted) == 0 || slices.Contains(wanted, id) }

	done, failed := 0, 0
	for _, a := range known {
		if !pick(a.ID) {
			continue
		}
		if !a.Installed() {
			if len(wanted) > 0 {
				fmt.Fprintf(os.Stderr, "  - %s: not found on this computer\n", a.Name)
			}
			continue
		}
		res, err := apps.Add(a, exe, serveArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", a.Name, err)
			failed++
			continue
		}
		fmt.Fprintf(os.Stderr, "  ✓ %s: %s (%s)\n", a.Name, res, a.Path())
		done++
	}

	if pick(claudeCode) {
		switch claude, err := exec.LookPath("claude"); {
		case err != nil:
			if len(wanted) > 0 {
				fmt.Fprintln(os.Stderr, "  - Claude Code: not found on this computer")
			}
		default:
			exec.Command(claude, "mcp", "remove", "--scope", "user", "portage").Run()
			add := append([]string{"mcp", "add", "--scope", "user", "portage", "--", exe}, serveArgs...)
			if out, err := exec.Command(claude, add...).CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "  ✗ Claude Code: %v: %s\n", err, strings.TrimSpace(string(out)))
				failed++
			} else {
				fmt.Fprintln(os.Stderr, "  ✓ Claude Code: added")
				done++
			}
		}
	}

	if failed > 0 {
		fmt.Fprintln(os.Stderr, "\nSome apps couldn't be updated. Quit them completely and run portage install again.")
	}
	if done == 0 && failed == 0 {
		fmt.Fprintf(os.Stderr, "\nNo supported AI app was found. Add this server to your app's MCP settings by hand:\n  command: %s\n  args:    %s\n", exe, strings.Join(serveArgs, " "))
		return nil
	}
	if done > 0 {
		fmt.Fprintln(os.Stderr, "\nQuit and reopen the apps above, then ask: \"connect my bank\".")
	}
	return nil
}
