package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.wrench.rocks/portage/internal/setup"
)

const setupKey = "setup"

var connectWait = 40 * time.Second

type connectInput struct {
	Bank    string `json:"bank" jsonschema:"the bank to connect: simplefin or rogers"`
	SetupID string `json:"setup_id,omitempty" jsonschema:"from an earlier waiting or open_link result: keep waiting for that sign-in"`
}

type connectOutput struct {
	Status  string `json:"status" jsonschema:"connected, waiting, cancelled, or open_link"`
	SetupID string `json:"setup_id,omitempty" jsonschema:"pass back to connect_bank to keep waiting"`
	URL     string `json:"url,omitempty" jsonschema:"for open_link: the page the user must open"`
	Message string `json:"message"`
}

type pendingSetup struct {
	bank string
	page *setup.Page
	done chan struct{}
	err  error
}

func (s *Server) addConnectTool(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "connect_bank",
		Description: "Connect a bank account. Opens a sign-in page in the user's browser; " +
			"passwords and codes are typed there, never in this chat. Use it when list_accounts " +
			"reports no connected banks or asks the user to log in again. " +
			"If the result is waiting, call connect_bank again right away with the same setup_id; " +
			"don't ask the user to confirm they are done.",
		Annotations:  &mcp.ToolAnnotations{Title: "Connect a bank"},
		OutputSchema: outputSchema[connectOutput](),
	}, s.connectBank)
}

func (s *Server) connectBank(ctx context.Context, req *mcp.CallToolRequest, in connectInput) (*mcp.CallToolResult, connectOutput, error) {
	if answer, ok := req.Params.InputResponses[setupKey]; ok {
		return s.answered(ctx, req.Params.RequestState, answer)
	}
	if in.SetupID != "" {
		p, ok := s.lookup(in.SetupID)
		if !ok {
			return nil, connectOutput{}, fmt.Errorf("no sign-in is waiting under that setup_id; call list_accounts to check, or connect_bank without setup_id to start again")
		}
		return s.await(ctx, in.SetupID, p)
	}

	if s.start == nil {
		return nil, connectOutput{}, fmt.Errorf("connecting banks is not available")
	}
	page, err := s.start(in.Bank)
	if err != nil {
		return nil, connectOutput{}, err
	}
	id, p := s.track(in.Bank, page)

	if canOpenLinks(req) {
		return &mcp.CallToolResult{
			InputRequests: mcp.InputRequestMap{
				setupKey: &mcp.ElicitParams{
					Mode:          "url",
					URL:           page.URL,
					Message:       "Sign in to " + in.Bank + " in your browser to connect it to portage.",
					ElicitationID: id,
				},
			},
			RequestState: id,
		}, connectOutput{}, nil
	}

	if s.open != nil && s.open(page.URL) == nil {
		return s.await(ctx, id, p)
	}

	return nil, connectOutput{
		Status:  "open_link",
		SetupID: id,
		URL:     page.URL,
		Message: "Show the user this link to sign in, then call connect_bank with this setup_id to wait for them to finish.",
	}, nil
}

func (s *Server) answered(ctx context.Context, id string, answer mcp.InputResponse) (*mcp.CallToolResult, connectOutput, error) {
	p, ok := s.lookup(id)
	if !ok {
		return nil, connectOutput{}, fmt.Errorf("this setup link expired; call connect_bank again")
	}
	if res, _ := answer.(*mcp.ElicitResult); res == nil || res.Action != "accept" {
		s.forget(id)
		p.page.Close()
		return nil, connectOutput{Status: "cancelled", Message: "The user did not open the sign-in page."}, nil
	}
	return s.await(ctx, id, p)
}

func (s *Server) await(ctx context.Context, id string, p *pendingSetup) (*mcp.CallToolResult, connectOutput, error) {
	select {
	case <-p.done:
		s.forget(id)
		if p.err != nil {
			return nil, connectOutput{}, fmt.Errorf("setup did not finish: %w", p.err)
		}
		return nil, connectOutput{Status: "connected", Message: p.bank + " is connected. Call list_accounts to see it."}, nil
	case <-time.After(connectWait):
		return nil, connectOutput{
			Status:  "waiting",
			SetupID: id,
			Message: "The user is still signing in. Call connect_bank again with this setup_id to keep waiting.",
		}, nil
	case <-ctx.Done():
		return nil, connectOutput{}, ctx.Err()
	}
}

func (s *Server) track(bank string, page *setup.Page) (string, *pendingSetup) {
	p := &pendingSetup{bank: bank, page: page, done: make(chan struct{})}
	id := rand.Text()
	s.pendingMutex.Lock()
	s.pending[id] = p
	s.pendingMutex.Unlock()
	go func() {
		p.err = <-page.Done
		if p.err == nil {
			s.reload()
		}
		close(p.done)
	}()
	return id, p
}

func (s *Server) lookup(id string) (*pendingSetup, bool) {
	s.pendingMutex.Lock()
	defer s.pendingMutex.Unlock()
	p, ok := s.pending[id]
	return p, ok
}

func (s *Server) forget(id string) {
	s.pendingMutex.Lock()
	delete(s.pending, id)
	s.pendingMutex.Unlock()
}

func canOpenLinks(req *mcp.CallToolRequest) bool {
	caps := req.ClientCapabilities()
	return caps != nil && caps.Elicitation != nil && caps.Elicitation.URL != nil
}
