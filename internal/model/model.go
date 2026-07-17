package model

import "time"

// Direction indicates money flow relative to the card holder.
type Direction string

const (
	DirectionOut Direction = "out" // 支出
	DirectionIn  Direction = "in"  // 收入/入账
)

// ParseStatus is the parsing result of a raw SMS.
type ParseStatus string

const (
	ParseStatusPending ParseStatus = "pending"
	ParseStatusOK      ParseStatus = "ok"
	ParseStatusFailed  ParseStatus = "failed"
	ParseStatusIgnored ParseStatus = "ignored" // e.g. OTP / non-transaction
)

// Person is a spend owner the user defines (我 / 老婆 / 孩子 / …).
type Person struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color,omitempty"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

// Tag is a free-form label the user defines.
type Tag struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// RawSMS is the original message received from the iPhone shortcut.
type RawSMS struct {
	ID        int64       `json:"id"`
	Text      string      `json:"text"`
	Source    string      `json:"source,omitempty"`
	Status    ParseStatus `json:"status"`
	Error     string      `json:"error,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

// Transaction is a structured bank transaction extracted from an SMS.
type Transaction struct {
	ID         int64     `json:"id"`
	RawSMSID   int64     `json:"raw_sms_id"`
	Amount     float64   `json:"amount"`   // always positive
	Currency   string    `json:"currency"` // e.g. CNY
	Direction  Direction `json:"direction"`
	Merchant   string    `json:"merchant,omitempty"`
	// MerchantNorm is a stable channel/counterparty label for analytics.
	MerchantNorm string    `json:"merchant_norm,omitempty"`
	CardLast4    string    `json:"card_last4,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
	Category     string    `json:"category,omitempty"`
	// Kind: consume | transfer | refund | fee | income | other
	Kind         Kind      `json:"kind,omitempty"`
	Note         string    `json:"note,omitempty"`
	Bank         string    `json:"bank,omitempty"`
	// BalanceAfter is the account balance reported in the SMS, if any.
	BalanceAfter float64   `json:"balance_after,omitempty"`
	BalanceKnown bool      `json:"balance_known"`
	PersonID     *int64    `json:"person_id,omitempty"`
	PersonName   string    `json:"person_name,omitempty"`
	PersonColor  string    `json:"person_color,omitempty"`
	Tags         []Tag     `json:"tags,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// DailySummary is the aggregated spend for one calendar day.
type DailySummary struct {
	Date     string  `json:"date"` // YYYY-MM-DD
	Expense  float64 `json:"expense"`
	Income   float64 `json:"income"`
	Net      float64 `json:"net"` // income - expense
	TxnCount int     `json:"txn_count"`
}

// TodaySummary is a quick snapshot for the dashboard header.
type TodaySummary struct {
	Date     string  `json:"date"`
	Expense  float64 `json:"expense"`
	Income   float64 `json:"income"`
	Net      float64 `json:"net"`
	TxnCount int     `json:"txn_count"`
}

// DashboardStats is the payload for the home page.
type DashboardStats struct {
	Today           TodaySummary   `json:"today"`
	Daily           []DailySummary `json:"daily"`
	UnparsedCount   int            `json:"unparsed_count"`
	TotalTxnCount   int            `json:"total_txn_count"`
	LatestBalance   float64        `json:"latest_balance,omitempty"`
	LatestBalanceAt *time.Time     `json:"latest_balance_at,omitempty"`
	BalanceKnown    bool           `json:"balance_known"`
	UnlabeledCount  int            `json:"unlabeled_count"` // out txns without person
}

// PersonStat is spend/income for one person in a range.
type PersonStat struct {
	PersonID   *int64  `json:"person_id"` // null = 未标记
	PersonName string  `json:"person_name"`
	Color      string  `json:"color,omitempty"`
	Expense    float64 `json:"expense"`
	Income     float64 `json:"income"`
	TxnCount   int     `json:"txn_count"`
	Pct        float64 `json:"pct"` // of total expense in range
}
