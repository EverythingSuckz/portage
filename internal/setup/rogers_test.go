package setup

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.wrench.rocks/portage/internal/rogers"
)

// fakeLogin stands in for rogers.Login. It rejects any password but "right",
// asks where to send a code, then accepts only the code "123". It records the
// params it was given and closes stopped when it returns.
type fakeLogin struct {
	params  rogers.LoginParams
	stopped chan struct{}
}

func useFakeLogin(t *testing.T) *fakeLogin {
	t.Helper()
	f := &fakeLogin{stopped: make(chan struct{})}
	replace(t, &login, func(ctx context.Context, p rogers.LoginParams, pr rogers.Prompter) error {
		f.params = p
		if p.Password != "right" {
			return errors.New("wrong password")
		}
		defer close(f.stopped)
		prefs := []rogers.TwoFactorPreference{{Type: "email", Value: "j***@example.com"}, {Type: "text", Value: "***-1234"}}
		if _, err := pr.Choose(prefs); err != nil {
			return err
		}
		for {
			code, err := pr.VerificationCode()
			if err != nil {
				return err
			}
			if code == "123" {
				return nil
			}
			pr.Notify("That verification code was rejected.")
		}
	})
	return f
}

func startRogers(t *testing.T, dataDir string) *Page {
	t.Helper()
	p, err := Start(dataDir, "rogers")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.finish(errors.New("test over")) })
	return p
}

// step posts a form and fails the test unless the reply contains want.
func step(t *testing.T, p *Page, path string, form url.Values, want string) {
	t.Helper()
	_, body := post(t, p.URL+path, form)
	if !strings.Contains(body, want) {
		t.Fatalf("POST %s: want %q in page:\n%s", path, want, body)
	}
}

func signIn(password string) url.Values {
	return url.Values{"username": {"jane"}, "password": {password}}
}

func TestRogersFormLoads(t *testing.T) {
	p := startRogers(t, t.TempDir())
	if _, body := get(t, p.URL); !strings.Contains(body, "Connect Rogers Bank") {
		t.Errorf("form page:\n%s", body)
	}
}

func TestRogersHappyPath(t *testing.T) {
	f := useFakeLogin(t)
	dir := t.TempDir()
	p := startRogers(t, dir)

	step(t, p, "rogers", signIn("right"), "send your code")
	step(t, p, "answer", url.Values{"answer": {"1"}}, "Enter your verification code")
	step(t, p, "answer", url.Values{"answer": {"123"}}, "Connected")

	if err := waitDone(t, p); err != nil {
		t.Fatalf("Done = %v, want nil", err)
	}
	if f.params.Username != "jane" || f.params.DataDir != dir || !f.params.StorePassword {
		t.Errorf("login params = %+v", f.params)
	}
}

func TestRogersWrongPasswordThenRetry(t *testing.T) {
	useFakeLogin(t)
	p := startRogers(t, t.TempDir())

	step(t, p, "rogers", signIn("wrong"), "wrong password")
	assertOpen(t, p)
	step(t, p, "rogers", signIn("right"), "send your code")
}

func TestRogersWrongCodeThenRetry(t *testing.T) {
	useFakeLogin(t)
	p := startRogers(t, t.TempDir())

	step(t, p, "rogers", signIn("right"), "send your code")
	step(t, p, "answer", url.Values{"answer": {"0"}}, "verification code")
	step(t, p, "answer", url.Values{"answer": {"999"}}, "rejected")
	assertOpen(t, p)
	step(t, p, "answer", url.Values{"answer": {"123"}}, "Connected")
}

func TestRogersInvalidChoiceShowsError(t *testing.T) {
	useFakeLogin(t)
	p := startRogers(t, t.TempDir())

	step(t, p, "rogers", signIn("right"), "send your code")
	step(t, p, "answer", url.Values{"answer": {"7"}}, "invalid choice")
	assertOpen(t, p)
}

func TestRogersSecondSignInWhileRunning(t *testing.T) {
	useFakeLogin(t)
	p := startRogers(t, t.TempDir())

	step(t, p, "rogers", signIn("right"), "send your code")
	step(t, p, "rogers", signIn("right"), "already in progress")
}

func TestRogersTimeoutStopsLogin(t *testing.T) {
	replace(t, &timeout, 200*time.Millisecond)
	f := useFakeLogin(t)
	p := startRogers(t, t.TempDir())

	// Leave the login waiting for a choice, then let the page time out.
	step(t, p, "rogers", signIn("right"), "send your code")
	if err := waitDone(t, p); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Done = %v, want a timeout error", err)
	}
	select {
	case <-f.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("login goroutine kept waiting after the page timed out")
	}
}
