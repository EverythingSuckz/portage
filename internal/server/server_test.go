package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.wrench.rocks/portage/internal/bank"
)

type fakeSource struct {
	name  string
	snap  bank.Snapshot
	err   error
	calls int
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Fetch(context.Context) (bank.Snapshot, error) {
	f.calls++
	return f.snap, f.err
}

var visa = &fakeSource{name: "rogers", snap: bank.Snapshot{
	Accounts: []bank.Account{{ID: "1", Name: "Mastercard", Balance: "-100.00"}},
	Transactions: []bank.Transaction{
		{ID: "a", AccountID: "1", Date: "2026-07-01", Amount: "-10.00", Description: "LOBLAWS", Category: "groceries"},
		{ID: "b", AccountID: "1", Date: "2026-07-03", Amount: "-20.00", Description: "SHELL"},
		{ID: "c", AccountID: "1", Date: "2026-07-02", Amount: "50.00", Description: "PAYMENT"},
	},
}}

func connect(t *testing.T, s *Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.MCP().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call[T any](t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (T, bool) {
	t.Helper()
	var out T
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		return out, true
	}
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out, false
}

func newServer(t *testing.T, sources ...bank.Source) *Server {
	t.Helper()
	server, err := New(func() ([]bank.Source, error) { return sources, nil }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestTools(t *testing.T) {
	broken := &fakeSource{name: "simplefin", err: errors.New("offline")}
	cs := connect(t, newServer(t, visa, broken))

	accts, _ := call[accountsOutput](t, cs, "list_accounts", nil)
	if len(accts.Accounts) != 1 || accts.Accounts[0].ID != "rogers:1" {
		t.Errorf("accounts = %+v", accts.Accounts)
	}
	if len(accts.Warnings) != 1 || accts.Warnings[0] != "simplefin: offline" {
		t.Errorf("warnings = %v", accts.Warnings)
	}

	all, _ := call[transactionsOutput](t, cs, "get_transactions", nil)
	if len(all.Transactions) != 3 || all.Transactions[0].ID != "b" || all.Transactions[2].ID != "a" {
		t.Errorf("want newest first, got %+v", all.Transactions)
	}

	tests := []struct {
		args map[string]any
		want int
	}{
		{map[string]any{"account_id": "rogers:1"}, 3},
		{map[string]any{"start_date": "2026-07-02"}, 2},
		{map[string]any{"end_date": "2026-07-02"}, 2},
		{map[string]any{"search": "grocer"}, 1},
		{map[string]any{"limit": 1}, 1},
	}
	for _, tt := range tests {
		got, isErr := call[transactionsOutput](t, cs, "get_transactions", tt.args)
		if isErr || len(got.Transactions) != tt.want {
			t.Errorf("%v: got %d transactions (error=%v), want %d", tt.args, len(got.Transactions), isErr, tt.want)
		}
	}

	for _, args := range []map[string]any{{"start_date": "July 1"}, {"account_id": "nope"}} {
		if _, isErr := call[transactionsOutput](t, cs, "get_transactions", args); !isErr {
			t.Errorf("%v should be an error", args)
		}
	}
}

func TestCache(t *testing.T) {
	src := &fakeSource{name: "rogers", snap: visa.snap}
	s := newServer(t, src)
	now := time.Now()
	s.now = func() time.Time { return now }

	s.snapshot(context.Background())
	s.snapshot(context.Background())
	if src.calls != 1 {
		t.Errorf("calls = %d, want 1 while cached", src.calls)
	}
	now = now.Add(cacheTTL + time.Second)
	s.snapshot(context.Background())
	if src.calls != 2 {
		t.Errorf("calls = %d, want 2 after the cache expires", src.calls)
	}
}

func TestReadOnlyTools(t *testing.T) {
	cs := connect(t, newServer(t, visa))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "connect_bank" {
			continue // it saves logins; see TestConnectToolIsNotReadOnly
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
	}
}
