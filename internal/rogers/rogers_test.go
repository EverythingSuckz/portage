package rogers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.wrench.rocks/portage/internal/bank"
)

// fakeRogers imitates the Rogers endpoints used by the client.
type fakeRogers struct {
	mu            sync.Mutex
	trusted       map[string]bool
	failActivity  int
	activityCalls int
	cycleStarts   []string
	user, acts    []byte
}

func newFake(t *testing.T) (*fakeRogers, string) {
	t.Helper()
	user, err := os.ReadFile("testdata/user.json")
	if err != nil {
		t.Fatal(err)
	}
	acts, err := os.ReadFile("testdata/activities.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRogers{trusted: map[string]bool{}, user: user, acts: acts}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("product") != "ROGERSBRAND" {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("POST /issuing/digital/authenticate/user", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if r.Header.Get("Brand_id") != "ROGERSBRAND" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		ok := f.trusted[body["deviceId"]]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"title":"Device Not Found","status":412}`))
			return
		}
		w.Write(f.user)
	})
	mux.HandleFunc("POST /issuing/digital/twofactorpasscode/preferences/user", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"preferences":[{"type":"email","value":"j***@example.com"}],"accountId":"ACCT-001","customerId":"CUST-001"}`))
	})
	mux.HandleFunc("POST /issuing/digital/twofactorpasscode/{acct}/customer/{cust}/generatecodefordevice", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true}`))
	})
	mux.HandleFunc("POST /issuing/digital/authenticate/validatepasscode", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["oneTimePassCode"] != "123456" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.mu.Lock()
		f.trusted[body["deviceId"]] = true
		f.mu.Unlock()
		w.Write(f.user)
	})
	mux.HandleFunc("GET /issuing/digital/account/{acct}/customer/{cust}/activity", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.activityCalls++
		fail := f.activityCalls <= f.failActivity
		if cs := r.URL.Query().Get("cycleStartDate"); cs != "" {
			f.cycleStarts = append(f.cycleStarts, cs)
		}
		f.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write(f.acts)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

type fakePrompter struct {
	codes    []string
	prompted bool
	notified int
}

func (p *fakePrompter) Choose(prefs []TwoFactorPreference) (TwoFactorPreference, error) {
	p.prompted = true
	return prefs[0], nil
}

func (p *fakePrompter) VerificationCode() (string, error) {
	code := p.codes[0]
	if len(p.codes) > 1 {
		p.codes = p.codes[1:]
	}
	return code, nil
}

func (p *fakePrompter) Notify(string) { p.notified++ }

func login(t *testing.T, base, dir string, pr Prompter) error {
	t.Helper()
	return Login(context.Background(), LoginParams{
		DataDir: dir, Name: "rogers", Username: "jane", Password: "pw", StorePassword: true, BaseURL: base,
	}, pr)
}

func fetch(t *testing.T, base, dir string) (bank.Snapshot, error) {
	t.Helper()
	sources, err := LoadAll(dir)
	if err != nil || len(sources) != 1 {
		t.Fatalf("LoadAll = %v, %v", sources, err)
	}
	sources[0].baseURL = base
	return sources[0].Fetch(context.Background())
}

func TestLoginThenFetch(t *testing.T) {
	f, base := newFake(t)
	dir := t.TempDir()

	pr := &fakePrompter{codes: []string{"123456"}}
	if err := login(t, base, dir, pr); err != nil {
		t.Fatal(err)
	}
	if !pr.prompted {
		t.Error("expected a two-factor prompt for a new device")
	}

	snap, err := fetch(t, base, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Warnings) != 0 {
		t.Errorf("warnings: %v", snap.Warnings)
	}
	acc := snap.Accounts[0]
	if acc.Name != "World Elite Mastercard ••4821" || acc.Balance != "-842.17" || acc.AvailableBalance != "14157.83" {
		t.Errorf("account = %+v", acc)
	}
	// 3 statement periods return the same 5 activities AND duplicates are dropped.
	if len(snap.Transactions) != 5 {
		t.Errorf("transactions = %d, want 5", len(snap.Transactions))
	}
	if strings.Join(f.cycleStarts, ",") != "2026-06-18,2026-05-18" {
		t.Errorf("cycle starts = %v", f.cycleStarts)
	}

	want := map[string]struct {
		amount, date string
		pending      bool
	}{
		"REF-777001": {"-82.19", "2026-07-11", false},  // purchase
		"REF-777002": {"1203.55", "2026-07-01", false}, // payment
		"REF-777005": {"26.81", "2026-07-13", false},   // refund
		"REF-777004": {"-27.31", "2026-07-09", false},  // foreign purchase
		"ACT-90003":  {"-56.40", "2026-07-15", true},   // pending
	}
	for _, tx := range snap.Transactions {
		w, ok := want[tx.ID]
		if !ok {
			t.Errorf("unexpected transaction %s", tx.ID)
			continue
		}
		if tx.Amount != w.amount || tx.Date != w.date || tx.Pending != w.pending {
			t.Errorf("%s = %+v, want %+v", tx.ID, tx, w)
		}
	}
}

func TestTrustedDeviceSkipsTwoFactor(t *testing.T) {
	_, base := newFake(t)
	dir := t.TempDir()
	if err := login(t, base, dir, &fakePrompter{codes: []string{"123456"}}); err != nil {
		t.Fatal(err)
	}
	pr := &fakePrompter{codes: []string{"123456"}}
	if err := login(t, base, dir, pr); err != nil {
		t.Fatal(err)
	}
	if pr.prompted {
		t.Error("a trusted device should not be asked for a passcode again")
	}
}

func TestWrongPasscode(t *testing.T) {
	_, base := newFake(t)

	pr := &fakePrompter{codes: []string{"000000", "123456"}}
	if err := login(t, base, t.TempDir(), pr); err != nil {
		t.Fatalf("retry should succeed: %v", err)
	}
	if pr.notified != 1 {
		t.Errorf("notified = %d, want 1", pr.notified)
	}

	err := login(t, base, t.TempDir(), &fakePrompter{codes: []string{"000000"}})
	if !errors.Is(err, ErrWrongVerificationCode) {
		t.Errorf("err = %v, want ErrWrongVerificationCode", err)
	}
}

func TestUntrustedDeviceAsksToLogInAgain(t *testing.T) {
	f, base := newFake(t)
	dir := t.TempDir()
	if err := login(t, base, dir, &fakePrompter{codes: []string{"123456"}}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.trusted = map[string]bool{}
	f.mu.Unlock()

	_, err := fetch(t, base, dir)
	if err == nil || !strings.Contains(err.Error(), "rogers login") {
		t.Errorf("err = %v, want a hint to log in again", err)
	}
}

func TestPasswordFromEnv(t *testing.T) {
	_, base := newFake(t)
	dir := t.TempDir()
	err := Login(context.Background(), LoginParams{
		DataDir: dir, Name: "rogers", Username: "jane", Password: "pw", BaseURL: base,
	}, &fakePrompter{codes: []string{"123456"}})
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("ROGERS_PASSWORD", "")
	if _, err := fetch(t, base, dir); err == nil {
		t.Error("expected an error without a password")
	}
	t.Setenv("ROGERS_PASSWORD", "pw")
	if _, err := fetch(t, base, dir); err != nil {
		t.Error(err)
	}
}

func TestActivityRetries(t *testing.T) {
	f, base := newFake(t)
	c := newClient(base)

	f.failActivity = 2
	if _, err := c.activities(context.Background(), "ACCT-001", "CUST-001", ""); err != nil {
		t.Errorf("should succeed on the third try: %v", err)
	}

	f.activityCalls, f.failActivity = 0, 3
	if _, err := c.activities(context.Background(), "ACCT-001", "CUST-001", ""); err == nil {
		t.Error("expected failure after 3 tries")
	}
}

func TestNegate(t *testing.T) {
	for in, want := range map[string]string{"12.50": "-12.50", "-3": "3", "0.00": "0.00", "-0.00": "0.00", "+4": "-4"} {
		if got := negate(in); got != want {
			t.Errorf("negate(%q) = %q, want %q", in, got, want)
		}
	}
}

func loginAs(t *testing.T, base, dir, name, username string, pr Prompter) {
	t.Helper()
	err := Login(context.Background(), LoginParams{
		DataDir: dir, Name: name, Username: username, Password: "pw", StorePassword: true, BaseURL: base,
	}, pr)
	if err != nil {
		t.Fatal(err)
	}
}

func savedNames(t *testing.T, dir string) string {
	t.Helper()
	sources, err := LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range sources {
		names = append(names, s.Name())
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

func TestReconnectReusesExistingLogin(t *testing.T) {
	_, base := newFake(t)
	dir := t.TempDir()
	loginAs(t, base, dir, "rogers-cc", "jane", &fakePrompter{codes: []string{"123456"}})

	pr := &fakePrompter{codes: []string{"123456"}}
	loginAs(t, base, dir, "", "JANE", pr)

	if got := savedNames(t, dir); got != "rogers-cc" {
		t.Errorf("saved logins = %q, want only rogers-cc", got)
	}
	if pr.prompted {
		t.Error("reconnecting should reuse the trusted device and skip the code")
	}
}

func TestNewUsernameGetsItsOwnLogin(t *testing.T) {
	_, base := newFake(t)
	dir := t.TempDir()
	loginAs(t, base, dir, "", "jane", &fakePrompter{codes: []string{"123456"}})
	loginAs(t, base, dir, "", "bob", &fakePrompter{codes: []string{"123456"}})

	if got := savedNames(t, dir); got != "rogers,rogers-2" {
		t.Errorf("saved logins = %q, want both rogers and rogers-2", got)
	}
}
