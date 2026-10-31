package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.wrench.rocks/portage/internal/bank"
	"go.wrench.rocks/portage/internal/rogers"
	"go.wrench.rocks/portage/internal/server"
	"go.wrench.rocks/portage/internal/simplefin"
)

const usage = `Portage is a read-only MCP server for Canadian bank accounts

Usage:
  portage serve                      run the MCP server over stdio
  portage simplefin claim <token>    connect a SimpleFIN Bridge account using a setup token
  portage rogers login               sign in to Rogers Bank (asks for a one-time verification code)

Every command accepts -data <dir> (default: $PORTAGE_DATA or the user config dir).`

func main() {
	log.SetFlags(0)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"serve"}
	}
	var err error
	switch {
	case args[0] == "serve":
		err = serve(ctx, args[1:])
	case len(args) >= 2 && args[0] == "simplefin" && args[1] == "claim":
		err = claimSimpleFIN(ctx, args[2:])
	case len(args) >= 2 && args[0] == "rogers" && args[1] == "login":
		err = loginRogers(ctx, args[2:])
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// parses all the flags
func flags(name string, args []string, setup func(*flag.FlagSet)) (string, *flag.FlagSet) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	data := fs.String("data", defaultDataDir(), "directory holding saved logins")
	if setup != nil {
		setup(fs)
	}
	fs.Parse(args)
	return *data, fs
}

func defaultDataDir() string {
	if d := os.Getenv("PORTAGE_DATA"); d != "" {
		return d
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "portage")
	}
	return "data"
}

func loadSources(dataDir string) ([]bank.Source, error) {
	var sources []bank.Source
	if sf, err := simplefin.Load(dataDir); err == nil {
		sources = append(sources, sf)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("load simplefin: %w", err)
	}
	rs, err := rogers.LoadAll(dataDir)
	if err != nil {
		return nil, fmt.Errorf("load rogers: %w", err)
	} else {
		for _, r := range rs {
			sources = append(sources, r)
		}
	}
	log.Printf("portage: %d bank source(s) loaded from %s", len(sources), dataDir)
	return sources, nil
}

// serve speaks MCP over stdin/stdout, so all logging must go to stderr.
func serve(ctx context.Context, args []string) error {
	dataDir, _ := flags("serve", args, nil)

	load := func() ([]bank.Source, error) {
		return loadSources(dataDir)
	}
	srv, err := server.New(load)
	if err != nil {
		return err
	}
	return srv.MCP().Run(ctx, &mcp.StdioTransport{})
}

func claimSimpleFIN(ctx context.Context, args []string) error {
	dataDir, fs := flags("simplefin claim", args, nil)
	if fs.NArg() != 1 {
		return errors.New("usage: portage simplefin claim <setup-token>")
	}
	accessURL, err := simplefin.Claim(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if err := simplefin.Save(dataDir, accessURL); err != nil {
		return err
	}
	log.Printf("SimpleFIN connected. Saved to %s", dataDir)
	return nil
}
