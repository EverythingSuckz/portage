package rogers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.wrench.rocks/portage/internal/bank"
)

// cycles is how many statement periods are fetched, including the current one.
const cycles = 3

type Source struct {
	name    string
	dataDir string
	baseURL string
}

func (s *Source) Name() string { return s.name }

func (s *Source) Fetch(ctx context.Context) (bank.Snapshot, error) {
	sess, err := s.loadSession()
	if err != nil {
		return bank.Snapshot{}, err
	}
	c := newClient(s.baseURL)
	if err := c.start(ctx); err != nil {
		return bank.Snapshot{}, err
	}
	u, challenge, err := c.authenticate(ctx, sess.Username, sess.Password, sess.DeviceID)
	if err != nil {
		return bank.Snapshot{}, err
	}
	if challenge {
		return bank.Snapshot{}, fmt.Errorf("Rogers no longer trusts this device; run `portage rogers login -name %s`", s.name)
	}

	var snap bank.Snapshot
	for _, a := range u.Accounts {
		var acts []activity
		for n := range cycles {
			start := ""
			if n > 0 {
				if n > len(a.CycleDates) {
					break
				}
				start = a.CycleDates[n-1].String()
			}
			list, err := c.activities(ctx, a.AccountID, a.Customer.CustomerID, start)
			if err != nil {
				snap.Warnings = append(snap.Warnings, fmt.Sprintf("%s: statement period %d: %v", accountName(a), n, err))
				continue
			}
			acts = append(acts, list...)
		}
		mapAccount(&snap, a, acts, time.Now())
	}
	return snap, nil
}

// mapAccount converts one Rogers account. Rogers reports amounts as their
// effect on the amount owed (a purchase is positive), so every amount and the
// balance are negated to get the usual sign where money in is positive.
func mapAccount(snap *bank.Snapshot, a account, acts []activity, now time.Time) {
	acc := bank.Account{
		ID:          a.AccountID,
		Name:        accountName(a),
		Institution: "Rogers Bank",
		Currency:    a.AccountCurrency,
		Balance:     negate(a.CurrentBalance.Value),
		BalanceDate: now.UTC().Format("2006-01-02"),
	}
	if validAmount(a.RealtimeBalance.Value) {
		acc.AvailableBalance = a.RealtimeBalance.Value
	}
	if !validAmount(acc.Balance) {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("%s: unreadable balance %q", acc.Name, a.CurrentBalance.Value))
	}
	snap.Accounts = append(snap.Accounts, acc)

	seen := map[string]bool{}
	for _, act := range acts {
		amt := negate(act.Amount.Value)
		if !validAmount(amt) {
			snap.Warnings = append(snap.Warnings, fmt.Sprintf("%s: skipped a transaction with amount %q", acc.Name, act.Amount.Value))
			continue
		}
		pending := act.ActivityStatus == "PENDING" || act.ActivityType == "AUTH" || !act.PostedDate.Valid
		id := transactionID(a.AccountID, act, pending)
		if seen[id] {
			continue
		}
		seen[id] = true
		when := act.PostedDate
		if pending {
			when = act.Date
		}
		snap.Transactions = append(snap.Transactions, bank.Transaction{
			ID:          id,
			AccountID:   a.AccountID,
			Date:        when.String(),
			Amount:      amt,
			Description: act.Merchant.Name,
			Category:    act.Merchant.Category,
			Pending:     pending,
		})
	}
}

func accountName(a account) string {
	if a.Customer.CardLast4 == "" {
		return a.ProductName
	}
	return a.ProductName + " ••" + a.Customer.CardLast4
}

// transactionID returns a unique ID for one transaction. Guess someone was lazy at their j*b.
func transactionID(accountID string, a activity, pending bool) string {
	switch {
	case !pending && a.ReferenceNumber != "":
		return a.ReferenceNumber
	case a.ActivityID != "":
		return a.ActivityID
	case a.ReferenceNumber != "":
		return a.ReferenceNumber
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{accountID, a.Date.String(), a.Amount.Value, a.Merchant.Name}, "\x00")))
	return hex.EncodeToString(sum[:])
}

var amountRe = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`) // please work

func validAmount(s string) bool { return amountRe.MatchString(s) }

// negate flips the sign of a decimal string without converting it to a float.
// Zero stays unsigned.
func negate(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "+")
	if strings.HasPrefix(s, "-") {
		return s[1:]
	}
	if strings.Trim(s, "0.") == "" {
		return s
	}
	return "-" + s
}
