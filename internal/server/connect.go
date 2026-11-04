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

var connectWait = 20 * time.Second

type connectInput struct {
	Bank string `json:"bank" jsonschema:"the bank to connect: simplefin or rogers"`
}

type connectOutput struct {
	Status  string `json:"status" jsonschema:"connected, waiting, cancelled, or open_link"`
	URL     string `json:"url,omitempty" jsonschema:"for open_link: the page the user must open"`
	Message string `json:"message"`
}

func (s *Server) addConnectTool(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "connect_bank",
		Description: "Connect a bank account. Opens a sign-in page in the user's browser; " +
			"passwords and codes are typed there, never in this chat. Use it when list_accounts " +
			"reports no connected banks or asks the user to log in again.",
		Annotations:  &mcp.ToolAnnotations{Title: "Connect a bank"},
		OutputSchema: outputSchema[connectOutput](),
	}, s.connectBank)
}

func (s *Server) connectBank(ctx context.Context, req *mcp.CallToolRequest, in connectInput) (*mcp.CallToolResult, connectOutput, error) {
	if answer, ok := req.Params.InputResponses[setupKey]; ok {
		return s.finishConnect(ctx, req.Params.RequestState, answer, in.Bank)
	}

	if s.start == nil {
		return nil, connectOutput{}, fmt.Errorf("connecting banks is not available")
	}
	page, err := s.start(in.Bank)
	if err != nil {
		return nil, connectOutput{}, err
	}

	if !canOpenLinks(req) {
		s.watch(page)
		return nil, connectOutput{
			Status:  "open_link",
			URL:     page.URL,
			Message: "Ask the user to open this link to sign in. Call list_accounts after they say they are done.",
		}, nil
	}

	id := rand.Text()
	s.pendingMutex.Lock()
	s.pending[id] = page
	s.pendingMutex.Unlock()
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

func (s *Server) finishConnect(ctx context.Context, id string, answer mcp.InputResponse, bank string) (*mcp.CallToolResult, connectOutput, error) {
	s.pendingMutex.Lock()
	page, ok := s.pending[id]
	delete(s.pending, id)
	s.pendingMutex.Unlock()
	if !ok {
		return nil, connectOutput{}, fmt.Errorf("this setup link expired; call connect_bank again")
	}

	if res, _ := answer.(*mcp.ElicitResult); res == nil || res.Action != "accept" {
		page.Close()
		return nil, connectOutput{Status: "cancelled", Message: "The user did not open the sign-in page."}, nil
	}

	select {
	case err := <-page.Done:
		if err != nil {
			return nil, connectOutput{}, fmt.Errorf("setup did not finish: %w", err)
		}
	case <-time.After(connectWait):
		s.watch(page)
		return nil, connectOutput{
			Status:  "waiting",
			Message: "The sign-in page is open in the user's browser. Call list_accounts after they say they are done.",
		}, nil
	case <-ctx.Done():
		s.watch(page)
		return nil, connectOutput{}, ctx.Err()
	}
	if err := s.reload(); err != nil {
		return nil, connectOutput{}, err
	}
	return nil, connectOutput{Status: "connected", Message: bank + " is connected. Call list_accounts to see it."}, nil
}

func (s *Server) watch(page *setup.Page) {
	go func() {
		if <-page.Done == nil {
			s.reload()
		}
	}()
}

func canOpenLinks(req *mcp.CallToolRequest) bool {
	caps := req.ClientCapabilities()
	return caps != nil && caps.Elicitation != nil && caps.Elicitation.URL != nil
}
