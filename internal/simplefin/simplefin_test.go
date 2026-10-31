package simplefin

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const bridgeResponse = `{
  "errors": ["Tangerine: login required"],
  "accounts": [{
    "org": {"name": "RBC Royal Bank"},
    "id": "ACT-1", "name": "Chequing", "currency": "CAD",
    "balance": "1520.33", "available-balance": "1500.00", "balance-date": 1785000000,
    "transactions": [
      {"id": "T1", "posted": 1784900000, "amount": "-42.10", "description": "METRO"},
      {"id": "T2", "posted": 0, "transacted_at": 1784990000, "amount": "-5.00", "description": "TIM HORTONS", "pending": true}
    ]
  }]
}`

func TestClaimAndFetch(t *testing.T) {
	var gotAuth, gotQuery string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/claim/abc":
			w.Write([]byte("https://user:secret@" + r.Host + "/simplefin"))
		case r.URL.Path == "/simplefin/accounts":
			user, pass, _ := r.BasicAuth()
			gotAuth, gotQuery = user+":"+pass, r.URL.RawQuery
			w.Write([]byte(bridgeResponse))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	httpClient = srv.Client()

	token := base64.StdEncoding.EncodeToString([]byte(srv.URL + "/claim/abc"))
	accessURL, err := Claim(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := Save(dir, accessURL); err != nil {
		t.Fatal(err)
	}
	src, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	src.now = func() time.Time { return time.Unix(1785000000, 0) }

	snap, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "user:secret" {
		t.Errorf("basic auth = %q", gotAuth)
	}
	if !strings.Contains(gotQuery, "pending=1") || !strings.Contains(gotQuery, "start-date=1777224000") {
		t.Errorf("query = %q", gotQuery)
	}
	if len(snap.Warnings) != 1 {
		t.Errorf("warnings = %v", snap.Warnings)
	}
	a := snap.Accounts[0]
	if a.Institution != "RBC Royal Bank" || a.Balance != "1520.33" || a.BalanceDate != "2026-07-25" {
		t.Errorf("account = %+v", a)
	}
	if len(snap.Transactions) != 2 || snap.Transactions[0].Date != "2026-07-24" || !snap.Transactions[1].Pending {
		t.Errorf("transactions = %+v", snap.Transactions)
	}
}

func TestClaimRejectsBadToken(t *testing.T) {
	for _, tok := range []string{"not base64!", base64.StdEncoding.EncodeToString([]byte("http://insecure/claim"))} {
		if _, err := Claim(context.Background(), tok); err == nil {
			t.Errorf("Claim(%q) should fail", tok)
		}
	}
}
