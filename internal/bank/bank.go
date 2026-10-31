package bank

import "context"

// Amounts and balances are decimal strings (e.g. "-12.34")
// These are never converted to floats.
// Positive means money into the account.
// a credit card balance is negative when money is owed.

type Account struct {
	ID               string `json:"id" jsonschema:"use this id to filter get_transactions"`
	Name             string `json:"name"`
	Institution      string `json:"institution"`
	Currency         string `json:"currency"`
	Balance          string `json:"balance"`
	AvailableBalance string `json:"available_balance,omitempty"`
	BalanceDate      string `json:"balance_date" jsonschema:"YYYY-MM-DD"`
}

type Transaction struct {
	ID          string `json:"id"`
	AccountID   string `json:"account_id"`
	Date        string `json:"date" jsonschema:"posted date, or transaction date when pending (YYYY-MM-DD)"`
	Amount      string `json:"amount"`
	Description string `json:"description"`
	Category    string `json:"category,omitempty"`
	Pending     bool   `json:"pending,omitempty"`
}

// Snapshot is everything a source knows at one point in time
type Snapshot struct {
	Accounts     []Account
	Transactions []Transaction
	// Warnings are non-fatal problems, such as one account failing to load.
	Warnings []string
}

// Source is one connection to a bank, either through SimpleFIN or a direct adapter.
type Source interface {
	Name() string
	Fetch(ctx context.Context) (Snapshot, error)
}
