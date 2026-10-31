// SimpleFIN Bridge (https://www.simplefin.org/protocol.html)
package simplefin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"go.wrench.rocks/portage/internal/bank"
)

// historyDays is how far back transactions are requested.
// The Bridge caps requests at 90 days.
const historyDays = 90

var httpClient = &http.Client{Timeout: 60 * time.Second}

type Source struct {
	accessURL string
	now       func() time.Time
}

func New(accessURL string) *Source {
	return &Source{accessURL: accessURL, now: time.Now}
}

func (s *Source) Name() string { return "simplefin" }

func path(dataDir string) string { return filepath.Join(dataDir, "simplefin.json") }

type saved struct {
	AccessURL string `json:"access_url"`
}

// Load returns the saved source, or an error matching os.ErrNotExist
func Load(dataDir string) (*Source, error) {
	var s saved
	if err := bank.LoadSecret(path(dataDir), &s); err != nil {
		return nil, err
	}
	return New(s.AccessURL), nil
}

func Save(dataDir, accessURL string) error {
	return bank.SaveSecret(path(dataDir), saved{AccessURL: accessURL})
}

// Claim exchanges a one-time setup token for a long-lived Access URL.
// The token is the base64-encoded claim URL.
func Claim(ctx context.Context, setupToken string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(setupToken))
	if err != nil {
		return "", errors.New("setup token is not valid base64")
	}
	claimURL, err := url.Parse(string(raw))
	if err != nil || claimURL.Scheme != "https" {
		return "", errors.New("setup token does not contain an https URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claimURL.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claim failed with status %d (tokens can only be claimed once)", resp.StatusCode)
	}
	return strings.TrimSpace(string(body)), nil
}

type accountSet struct {
	Errors  []string `json:"errors"`
	Errlist []struct {
		Msg string `json:"msg"`
	} `json:"errlist"`
	Connections []struct {
		ConnID string `json:"conn_id"`
		Name   string `json:"name"`
	} `json:"connections"`
	Accounts []struct {
		Org struct {
			Name string `json:"name"`
		} `json:"org"`
		ConnID           string `json:"conn_id"`
		ID               string `json:"id"`
		Name             string `json:"name"`
		Currency         string `json:"currency"`
		Balance          string `json:"balance"`
		AvailableBalance string `json:"available-balance"`
		BalanceDate      int64  `json:"balance-date"`
		Transactions     []struct {
			ID           string `json:"id"`
			Posted       int64  `json:"posted"`
			Amount       string `json:"amount"`
			Description  string `json:"description"`
			TransactedAt int64  `json:"transacted_at"`
			Pending      bool   `json:"pending"`
		} `json:"transactions"`
	} `json:"accounts"`
}

func (s *Source) Fetch(ctx context.Context) (bank.Snapshot, error) {
	u, err := url.Parse(s.accessURL)
	if err != nil || u.User == nil {
		return bank.Snapshot{}, errors.New("saved SimpleFIN access URL is invalid; run `portage simplefin claim` again")
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	u.User = nil
	u.Path = strings.TrimRight(u.Path, "/") + "/accounts"
	start := s.now().AddDate(0, 0, -historyDays).Unix()
	u.RawQuery = url.Values{"start-date": {fmt.Sprint(start)}, "pending": {"1"}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return bank.Snapshot{}, err
	}
	req.SetBasicAuth(user, pass)
	resp, err := httpClient.Do(req)
	if err != nil {
		return bank.Snapshot{}, fmt.Errorf("simplefin: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return bank.Snapshot{}, errors.New("simplefin: access was revoked; claim a new setup token")
	}
	if resp.StatusCode != http.StatusOK {
		return bank.Snapshot{}, fmt.Errorf("simplefin: status %d", resp.StatusCode)
	}
	var set accountSet
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&set); err != nil {
		return bank.Snapshot{}, fmt.Errorf("simplefin: decode response: %w", err)
	}

	conns := map[string]string{}
	for _, c := range set.Connections {
		conns[c.ConnID] = c.Name
	}
	snap := bank.Snapshot{Warnings: set.Errors}
	for _, e := range set.Errlist {
		snap.Warnings = append(snap.Warnings, e.Msg)
	}
	for _, a := range set.Accounts {
		institution := a.Org.Name
		if institution == "" {
			institution = conns[a.ConnID]
		}
		snap.Accounts = append(snap.Accounts, bank.Account{
			ID:               a.ID,
			Name:             a.Name,
			Institution:      institution,
			Currency:         a.Currency,
			Balance:          a.Balance,
			AvailableBalance: a.AvailableBalance,
			BalanceDate:      day(a.BalanceDate),
		})
		for _, t := range a.Transactions {
			when := t.Posted
			if when == 0 {
				when = t.TransactedAt
			}
			snap.Transactions = append(snap.Transactions, bank.Transaction{
				ID:          t.ID,
				AccountID:   a.ID,
				Date:        day(when),
				Amount:      t.Amount,
				Description: t.Description,
				Pending:     t.Pending || t.Posted == 0,
			})
		}
	}
	return snap, nil
}

func day(unix int64) string {
	if unix == 0 {
		return ""
	}
	return time.Unix(unix, 0).UTC().Format("2006-01-02")
}
