package server

import (
	"context"
	"errors"
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
	reloads atomic.Int32
}

func newConnectFixture(t *testing.T) *connectFixture {
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
	s, err := New(load, start)
	if err != nil {
		t.Fatal(err)
	}
	f.server = s
	return f
}

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

func connectBank(t *testing.T, cs *mcp.ClientSession, b string) (connectOutput, bool) {
	t.Helper()
	return call[connectOutput](t, cs, "connect_bank", map[string]any{"bank": b})
}

func waitForReload(t *testing.T, f *connectFixture, before int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for f.reloads.Load() == before {
		if time.Now().After(deadline) {
			t.Fatal("sources were not reloaded after the user finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConnectOpensLinkAndReloads(t *testing.T) {
	f := newConnectFixture(t)
	var asked *mcp.ElicitParams
	cs := connectClient(t, f.server, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		asked = req.Params
		f.page.Done <- nil
		return &mcp.ElicitResult{Action: "accept"}, nil
	})
	before := f.reloads.Load()

	out, isErr := connectBank(t, cs, "rogers")
	if isErr || out.Status != "connected" {
		t.Fatalf("result = %+v (error=%v)", out, isErr)
	}
	if asked == nil || asked.Mode != "url" || asked.URL != f.page.URL {
		t.Errorf("elicitation = %+v", asked)
	}
	if strings.Join(f.banks, ",") != "rogers" {
		t.Errorf("banks started = %v", f.banks)
	}
	if f.reloads.Load() != before+1 {
		t.Errorf("sources were not reloaded after connecting")
	}
}

func TestConnectSlowSignInReturnsWaitingThenReloads(t *testing.T) {
	old := connectWait
	connectWait = 50 * time.Millisecond
	t.Cleanup(func() { connectWait = old })

	f := newConnectFixture(t)
	cs := connectClient(t, f.server, func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "accept"}, nil
	})
	before := f.reloads.Load()

	out, isErr := connectBank(t, cs, "rogers")
	if isErr || out.Status != "waiting" {
		t.Fatalf("result = %+v (error=%v)", out, isErr)
	}
	f.page.Done <- nil
	waitForReload(t, f, before)
}

func TestConnectDeclinedClosesPage(t *testing.T) {
	f := newConnectFixture(t)
	cs := connectClient(t, f.server, func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "decline"}, nil
	})

	out, isErr := connectBank(t, cs, "rogers")
	if isErr || out.Status != "cancelled" {
		t.Errorf("result = %+v (error=%v)", out, isErr)
	}
	select {
	case err := <-f.page.Done:
		if err == nil {
			t.Error("a declined setup must not report success")
		}
	case <-time.After(2 * time.Second):
		t.Error("the setup page was left open after the user declined")
	}
}

func TestConnectSetupFails(t *testing.T) {
	f := newConnectFixture(t)
	cs := connectClient(t, f.server, func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		f.page.Done <- errors.New("setup timed out")
		return &mcp.ElicitResult{Action: "accept"}, nil
	})

	if _, isErr := connectBank(t, cs, "rogers"); !isErr {
		t.Error("expected an error when setup fails")
	}
}

func TestConnectUnknownBank(t *testing.T) {
	f := newConnectFixture(t)
	cs := connectClient(t, f.server, func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		t.Error("should not ask the user to open a link")
		return &mcp.ElicitResult{Action: "cancel"}, nil
	})

	if _, isErr := connectBank(t, cs, "nope"); !isErr {
		t.Error("expected an error for an unknown bank")
	}
}

func TestConnectFallbackReturnsLink(t *testing.T) {
	f := newConnectFixture(t)
	cs := connectClient(t, f.server, nil)
	before := f.reloads.Load()

	out, isErr := connectBank(t, cs, "simplefin")
	if isErr || out.Status != "open_link" || out.URL != f.page.URL {
		t.Fatalf("result = %+v (error=%v)", out, isErr)
	}
	f.page.Done <- nil
	waitForReload(t, f, before)
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
