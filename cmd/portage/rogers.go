package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"go.wrench.rocks/portage/internal/rogers"
)

func loginRogers(ctx context.Context, args []string) error {
	var name, username string
	var storePassword bool
	dataDir, _ := flags("rogers login", args, func(fs *flag.FlagSet) {
		fs.StringVar(&name, "name", "rogers", "name for this login, used as the account id prefix")
		fs.StringVar(&username, "username", os.Getenv("ROGERS_USERNAME"), "Rogers username")
		fs.BoolVar(&storePassword, "store-password", true, "save the password so syncs can log in; if false, set ROGERS_PASSWORD for the server")
	})

	in := bufio.NewReader(os.Stdin)
	if username == "" {
		username = prompt(in, "Rogers username: ")
	}
	password := os.Getenv("ROGERS_PASSWORD")
	if password == "" {
		fmt.Fprint(os.Stderr, "Rogers password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		password = string(b)
	}
	if username == "" || password == "" {
		return errors.New("username and password are required")
	}

	err := rogers.Login(ctx, rogers.LoginParams{
		DataDir:       dataDir,
		Name:          name,
		Username:      username,
		Password:      password,
		StorePassword: storePassword,
	}, terminal{in})
	if err != nil {
		return err
	}
	log.Printf("Rogers login saved as %q in %s", name, dataDir)
	return nil
}

func prompt(in *bufio.Reader, label string) string {
	fmt.Fprint(os.Stderr, label)
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

// terminal answers two-factor questions on stdin/stderr.
type terminal struct{ in *bufio.Reader }

func (t terminal) Choose(prefs []rogers.TwoFactorPreference) (rogers.TwoFactorPreference, error) {
	if len(prefs) == 1 {
		fmt.Fprintf(os.Stderr, "Sending a verification code by %s to %s.\n", prefs[0].Type, prefs[0].Value)
		return prefs[0], nil
	}
	fmt.Fprintln(os.Stderr, "Where should Rogers send the verification code?")
	for i, p := range prefs {
		fmt.Fprintf(os.Stderr, "  [%d] %s: %s\n", i+1, p.Type, p.Value)
	}
	for {
		n, err := strconv.Atoi(prompt(t.in, "Choice: "))
		if err == nil && n >= 1 && n <= len(prefs) {
			return prefs[n-1], nil
		}
		fmt.Fprintln(os.Stderr, "Enter one of the numbers above.")
	}
}

func (t terminal) VerificationCode() (string, error) {
	for {
		fmt.Fprint(os.Stderr, "Verification Code: ")
		line, err := t.in.ReadString('\n')
		if code := strings.TrimSpace(line); code != "" {
			return code, nil
		}
		if err != nil {
			return "", errors.New("no verification code entered")
		}
	}
}

func (t terminal) Notify(msg string) { fmt.Fprintln(os.Stderr, msg) }
