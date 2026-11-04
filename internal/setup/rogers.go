package setup

import (
	"context"
	"errors"
	"strconv"

	"go.wrench.rocks/portage/internal/rogers"
)

// login is the Rogers sign-in flow.
var login = rogers.Login

// question is something the login needs to show the user next.
type question struct {
	Kind  string // "choose", "code" or "done"
	Prefs []rogers.TwoFactorPreference
	Note  string // e.g. "That code was rejected"
	Err   error  // only for "done"
}

// webPrompter answers rogers.Login's questions through the web page.
type webPrompter struct {
	ctx       context.Context // cancelled when the page finishes
	questions chan question   // login goroutine -> web handlers
	answers   chan string     // web handlers -> login goroutine
	note      string          // shown with the next question
}

// ask sends a question and waits for the answer.
func (w *webPrompter) ask(q question) (string, error) {
	q.Note, w.note = w.note, "" // show a pending note once, then clear it
	select {
	case w.questions <- q:
	case <-w.ctx.Done():
		return "", w.ctx.Err()
	}

	select {
	case ans := <-w.answers:
		return ans, nil
	case <-w.ctx.Done():
		return "", w.ctx.Err()
	}
}

func (w *webPrompter) Choose(prefs []rogers.TwoFactorPreference) (rogers.TwoFactorPreference, error) {
	ans, err := w.ask(question{Kind: "choose", Prefs: prefs})
	if err != nil {
		return rogers.TwoFactorPreference{}, err
	}
	i, err := strconv.Atoi(ans)
	if err != nil || i < 0 || i >= len(prefs) {
		return rogers.TwoFactorPreference{}, errors.New("invalid choice")
	}
	return prefs[i], nil
}

func (w *webPrompter) VerificationCode() (string, error) {
	return w.ask(question{Kind: "code"})
}

// notify saves a message to show with the next question.
func (w *webPrompter) Notify(msg string) { w.note = msg }
