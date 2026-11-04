package setup

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.wrench.rocks/portage/internal/simplefin"
)

// These tests replace the package-level claim and timeout
// so they must not run in parallel.

// replace sets *ptr to v for the rest of the test, then restores it.
func replace[T any](t *testing.T, ptr *T, v T) {
	t.Helper()
	old := *ptr
	*ptr = v
	t.Cleanup(func() { *ptr = old })
}

// fakeClaim makes the SimpleFIN claim step return the given results in order.
// The last result repeats.
func fakeClaim(t *testing.T, results ...error) {
	t.Helper()
	replace(t, &claim, func(ctx context.Context, token string) (string, error) {
		err := results[0]
		if len(results) > 1 {
			results = results[1:]
		}
		if err != nil {
			return "", err
		}
		return "https://user:pass@bridge.example/simplefin", nil
	})
}

func startPage(t *testing.T, dataDir string) *Page {
	t.Helper()
	p, err := Start(dataDir, "simplefin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.finish(errors.New("test over")) })
	return p
}

func get(t *testing.T, u string) (int, string) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	return readResponse(t, resp)
}

func post(t *testing.T, u string, form url.Values) (int, string) {
	t.Helper()
	resp, err := http.PostForm(u, form)
	if err != nil {
		t.Fatal(err)
	}
	return readResponse(t, resp)
}

func readResponse(t *testing.T, resp *http.Response) (int, string) {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

// waitDone returns the page's result, failing the test if it takes too long.
func waitDone(t *testing.T, p *Page) error {
	t.Helper()
	select {
	case err := <-p.Done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("setup page never finished")
		return nil
	}
}

// assertOpen fails the test if the page has already reported a result.
func assertOpen(t *testing.T, p *Page) {
	t.Helper()
	select {
	case err := <-p.Done:
		t.Fatalf("setup page finished early: %v", err)
	default:
	}
}

func TestFormLoads(t *testing.T) {
	p := startPage(t, t.TempDir())

	code, body := get(t, p.URL)
	if code != http.StatusOK || !strings.Contains(body, "Setup token") {
		t.Errorf("GET form = %d, body:\n%s", code, body)
	}
}

func TestWrongIDIsNotFound(t *testing.T) {
	fakeClaim(t, nil)
	p := startPage(t, t.TempDir())
	wrong := strings.Replace(p.URL, p.ID, "not-the-id", 1)

	if code, _ := get(t, wrong); code != http.StatusNotFound {
		t.Errorf("GET with wrong id = %d, want 404", code)
	}
	if code, _ := post(t, wrong+"simplefin", url.Values{"token": {"x"}}); code != http.StatusNotFound {
		t.Errorf("POST with wrong id = %d, want 404", code)
	}
	assertOpen(t, p)
}

func TestBadTokenShowsErrorThenRetrySucceeds(t *testing.T) {
	fakeClaim(t, errors.New("token already used"), nil)
	dir := t.TempDir()
	p := startPage(t, dir)

	code, body := post(t, p.URL+"simplefin", url.Values{"token": {"bad"}})
	if code != http.StatusOK || !strings.Contains(body, "token already used") {
		t.Fatalf("bad token = %d, body:\n%s", code, body)
	}
	if strings.Contains(body, "Connected") {
		t.Error("a failed claim must not show the success page")
	}
	assertOpen(t, p)

	_, body = post(t, p.URL+"simplefin", url.Values{"token": {"good"}})
	if !strings.Contains(body, "Connected") {
		t.Fatalf("retry body:\n%s", body)
	}
	if err := waitDone(t, p); err != nil {
		t.Errorf("Done = %v, want nil", err)
	}
	if _, err := simplefin.Load(dir); err != nil {
		t.Errorf("access URL not saved: %v", err)
	}
}

func TestSuccessShutsDownPage(t *testing.T) {
	fakeClaim(t, nil)
	p := startPage(t, t.TempDir())

	post(t, p.URL+"simplefin", url.Values{"token": {"good"}})
	if err := waitDone(t, p); err != nil {
		t.Fatalf("Done = %v, want nil", err)
	}
	// Done is only sent after the server has shut down.
	if _, err := http.Get(p.URL); err == nil {
		t.Error("page still reachable after setup finished")
	}
}

func TestTimeout(t *testing.T) {
	replace(t, &timeout, 50*time.Millisecond)
	p := startPage(t, t.TempDir())

	err := waitDone(t, p)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("Done = %v, want a timeout error", err)
	}
}

func TestFinishOnlyReportsOnce(t *testing.T) {
	p := startPage(t, t.TempDir())

	// Several callers racing to finish must not block or send twice.
	for range 5 {
		go p.finish(errors.New("first wins"))
	}
	if err := waitDone(t, p); err == nil {
		t.Error("expected the finishing error")
	}
	select {
	case err := <-p.Done:
		t.Errorf("Done sent a second result: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUnknownBank(t *testing.T) {
	if _, err := Start(t.TempDir(), "notabank"); err == nil {
		t.Error("expected an error for an unknown bank")
	}
}
