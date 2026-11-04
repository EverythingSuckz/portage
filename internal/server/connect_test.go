package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.wrench.rocks/portage/internal/bank"
	"go.wrench.rocks/portage/internal/setup"
)

type connectFixture struct {
	server  *Server
	page    *setup.Page
	banks   []string
	opened  []string
	reloads atomic.Int32
}

func newConnectFixture(t *testing.T, open func(f *connectFixture, url string) error) *connectFixture {
	t.Helper()
	f := &connectFixture{}
	load := func() ([]bank.Source, error) {
		f.reloads.Add(1)
		return nil, nil
	}
	start := func(b string) (*setup.Page, error) {
		f.banks = append(f.banks, b)
		p, err := setup.Start(t.TempDir(), b)
		if err != nil {
			return nil, err
		}
		t.Cleanup(p.Close)
		f.page = p
		return p, nil
	}
	opener := func(url string) error {
		f.opened = append(f.opened, url)
		if open == nil {
			return errors.New("no browser")
		}
		return open(f, url)
	}
	s, err := New(load, start, opener)
	if err != nil {
		t.Fatal(err)
	}
	f.server = s
	return f
}

func shortWait(t *testing.T) {
	t.Helper()
	old := connectWait
	connectWait = 50 * time.Millisecond
	t.Cleanup(func() { connectWait = old })
}

func signInSoon(f *connectFixture, _ string) error {
	go func() {
		time.Sleep(20 * time.Millisecond)
		f.page.Done <- nil
	}()
	return nil
}

func openOK(*connectFixture, string) error { return nil }

func connectClient(t *testing.T, s *Server, elicit func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error)) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.MCP().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	opts := &mcp.ClientOptions{}
	if elicit != nil {
		opts.ElicitationHandler = elicit
		opts.Capabilities = &mcp.ClientCapabilities{
			Elicitation: &mcp.ElicitationCapabilities{URL: &mcp.URLElicitationCapabilities{}},
		}
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, opts).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func connectBank(t *testing.T, cs *mcp.ClientSession, args map[string]any) (connectOutput, bool) {
	t.Helper()
	return call[connectOutput](t, cs, "connect_bank", args)
}

func TestConnectOpensBrowserAndWaits(t *testing.T) {
	f := newConnectFixture(t, signInSoon)
	cs := connectClient(t, f.server, nil)
	before := f.reloads.Load()

	out, isErr := connectBank(t, cs, map[string]any{"bank": "rogers"})
	if isErr || out.Status != "connected" {
		t.Fatalf("result = %+v (error=%v)", out, isErr)
	}
	if len(f.opened) != 1 || f.opened[0] != f.page.URL {
		t.Errorf("opened = %v, want the setup page", f.opened)
	}
	if f.reloads.Load() != before+1 {
		t.Error("sources were not reloaded after connecting")
	}
}

func TestConnectKeepsWaitingWithSetupID(t *testing.T) {
	shortWait(t)
	f := newConnectFixture(t, openOK)
	cs := connectClient(t, f.server, nil)

	out, _ := connectBank(t, cs, map[string]any{"bank": "rogers"})
	if out.Status != "waiting" || out.SetupID == "" {
		t.Fatalf("first result = %+v, want waiting with a setup_id", out)
	}
	again, _ := connectBank(t, cs, map[string]any{"bank": "rogers", "setup_id": out.SetupID})
	if again.Status != "waiting" || again.SetupID != out.SetupID {
		t.Fatalf("second result = %+v, want still waiting", again)
	}

	f.page.Done <- nil
	done, isErr := connectBank(t, cs, map[string]any{"bank": "rogers", "setup_id": out.SetupID})
	if isErr || done.Status != "connected" {
		t.Fatalf("final result = %+v (error=%v)", done, isErr)
	}
}

func TestConnectWithoutBrowserReturnsLink(t *testing.T) {
	f := newConnectFixture(t, nil)
	cs := connectClient(t, f.server, nil)

	out, isErr := connectBank(t, cs, map[string]any{"bank": "simplefin"})
	if isErr || out.Status != "open_link" || out.URL != f.page.URL || out.SetupID == "" {
		t.Fatalf("result = %+v (error=%v)", out, isErr)
	}

	f.page.Done <- nil
	done, isErr := connectBank(t, cs, map[string]any{"bank": "simplefin", "setup_id": out.SetupID})
	if isErr || done.Status != "connected" {
		t.Fatalf("after signing in = %+v (error=%v)", done, isErr)
	}
}

func TestConnectUnknownSetupID(t *testing.T) {
	f := newConnectFixture(t, openOK)
	cs := connectClient(t, f.server, nil)

	if _, isErr := connectBank(t, cs, map[string]any{"bank": "rogers", "setup_id": "nope"}); !isErr {
		t.Error("expected an error for an unknown setup_id")
	}
}

func TestConnectAppOpensLinkInsteadOfBrowser(t *testing.T) {
	f := newConnectFixture(t, openOK)
	var asked *mcp.ElicitParams
	cs := connectClient(t, f.server, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		asked = req.Params
		f.page.Done <- nil
		return &mcp.ElicitResult{Action: "accept"}, nil
	})

	out, isErr := connectBank(t, cs, map[string]any{"bank": "rogers"})
	if isErr || out.Status != "connected" {
		t.Fatalf("result = %+v (error=%v)", out, isErr)
	}
	if asked == nil || asked.Mode != "url" || asked.URL != f.page.URL {
		t.Errorf("elicitation = %+v", asked)
	}
	if len(f.opened) != 0 {
		t.Error("portage opened the browser even though the app can open the link")
	}
	if strings.Join(f.banks, ",") != "rogers" {
		t.Errorf("banks started = %v", f.banks)
	}
}

func TestConnectAppSlowSignInReturnsWaiting(t *testing.T) {
	shortWait(t)
	f := newConnectFixture(t, openOK)
	cs := connectClient(t, f.server, func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "accept"}, nil
	})

	out, isErr := connectBank(t, cs, map[string]any{"bank": "rogers"})
	if isErr || out.Status != "waiting" || out.SetupID == "" {
		t.Fatalf("result = %+v (error=%v)", out, isErr)
	}
	f.page.Done <- nil
	done, _ := connectBank(t, cs, map[string]any{"bank": "rogers", "setup_id": out.SetupID})
	if done.Status != "connected" {
		t.Errorf("after signing in = %+v", done)
	}
}

func TestConnectDeclinedClosesPage(t *testing.T) {
	f := newConnectFixture(t, openOK)
	cs := connectClient(t, f.server, func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "decline"}, nil
	})

	out, isErr := connectBank(t, cs, map[string]any{"bank": "rogers"})
	if isErr || out.Status != "cancelled" {
		t.Fatalf("result = %+v (error=%v)", out, isErr)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := http.Get(f.page.URL)
		if err != nil {
			break
		}
		resp.Body.Close()
		if time.Now().After(deadline) {
			t.Fatal("the setup page was left open after the user declined")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConnectSetupFails(t *testing.T) {
	f := newConnectFixture(t, func(f *connectFixture, _ string) error {
		f.page.Done <- errors.New("setup timed out")
		return nil
	})
	cs := connectClient(t, f.server, nil)

	if _, isErr := connectBank(t, cs, map[string]any{"bank": "rogers"}); !isErr {
		t.Error("expected an error when setup fails")
	}
}

func TestConnectUnknownBank(t *testing.T) {
	f := newConnectFixture(t, openOK)
	cs := connectClient(t, f.server, nil)

	if _, isErr := connectBank(t, cs, map[string]any{"bank": "nope"}); !isErr {
		t.Error("expected an error for an unknown bank")
	}
	if len(f.opened) != 0 {
		t.Error("opened a browser for an unknown bank")
	}
}

func TestConnectToolIsNotReadOnly(t *testing.T) {
	cs := connect(t, newServer(t))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "connect_bank" {
			if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
				t.Error("connect_bank changes saved logins, so it must not be marked read-only")
			}
			return
		}
	}
	t.Error("connect_bank tool not registered")
}
