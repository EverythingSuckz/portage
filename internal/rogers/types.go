package rogers

import (
	"encoding/json"
	"fmt"
	"time"
)

// Rogers sends dates as "2006-01-02"
// but guess what, some fields have full timestamps
// Fire em.
type date struct {
	time.Time
	Valid bool
}

var dateLayouts = []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05.000Z07:00", "2006-01-02T15:04:05"}

func (d *date) UnmarshalJSON(b []byte) error {
	var s string
	if string(b) == "null" {
		return nil
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			d.Time, d.Valid = t.UTC(), true
			return nil
		}
	}
	return fmt.Errorf("unrecognized date %q", s)
}

func (d date) String() string {
	if !d.Valid {
		return ""
	}
	return d.Format("2006-01-02")
}

type amount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type activity struct {
	ReferenceNumber string `json:"referenceNumber"`
	ActivityID      string `json:"activityId"`
	ActivityType    string `json:"activityType"`
	ActivityStatus  string `json:"activityStatus"`
	Amount          amount `json:"amount"`
	Merchant        struct {
		Name     string `json:"name"`
		Category string `json:"category"`
	} `json:"merchant"`
	Date       date `json:"date"`
	PostedDate date `json:"postedDate"`
}

type account struct {
	AccountID       string `json:"accountId"`
	ProductName     string `json:"productName"`
	AccountCurrency string `json:"accountCurrency"`
	CycleDates      []date `json:"cycleDates"`
	CurrentBalance  amount `json:"currentBalance"`
	RealtimeBalance amount `json:"realtimeBalance"`
	Customer        struct {
		CustomerID string `json:"customerId"`
		CardLast4  string `json:"cardLast4"`
	} `json:"customer"`
}

type user struct {
	Accounts []account `json:"accounts"`
}

// TwoFactorPreference is one place a one-time passcode can be sent.
type TwoFactorPreference struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type twoFactorPreferences struct {
	Preferences []TwoFactorPreference `json:"preferences"`
	AccountID   string                `json:"accountId"`
	CustomerID  string                `json:"customerId"`
}
