package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"cashpulse/internal/model"
)

func (s *Store) migrateFeatures() error {
	const schema = `
CREATE TABLE IF NOT EXISTS budgets (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    month       TEXT    NOT NULL,          -- YYYY-MM or * for recurring default
    person_id   INTEGER REFERENCES people(id) ON DELETE CASCADE,
    kind        TEXT    NOT NULL DEFAULT 'consume', -- consume|all|transfer
    amount      REAL    NOT NULL,
    created_at  TEXT    NOT NULL,
    UNIQUE(month, person_id, kind)
);

CREATE TABLE IF NOT EXISTS label_rules (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL DEFAULT '',
    match_field  TEXT    NOT NULL, -- merchant_norm|merchant|category|kind|note
    match_op     TEXT    NOT NULL DEFAULT 'eq', -- eq|contains
    match_value  TEXT    NOT NULL,
    person_id    INTEGER REFERENCES people(id) ON DELETE SET NULL,
    tag_id       INTEGER REFERENCES tags(id) ON DELETE SET NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    priority     INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS goals (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    target      REAL    NOT NULL,
    created_at  TEXT    NOT NULL
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	return nil
}

// ---- Budgets ----

type Budget struct {
	ID         int64     `json:"id"`
	Month      string    `json:"month"`
	PersonID   *int64    `json:"person_id,omitempty"`
	PersonName string    `json:"person_name,omitempty"`
	Kind       string    `json:"kind"`
	Amount     float64   `json:"amount"`
	Spent      float64   `json:"spent"`
	Pct        float64   `json:"pct"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) ListBudgets(ctx context.Context, month string, loc *time.Location) ([]Budget, error) {
	if month == "" {
		if loc == nil {
			loc = time.Local
		}
		month = time.Now().In(loc).Format("2006-01")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT b.id, b.month, b.person_id, COALESCE(p.name,''), b.kind, b.amount, b.created_at
FROM budgets b
LEFT JOIN people p ON p.id = b.person_id
WHERE b.month = ? OR b.month = '*'
ORDER BY b.person_id IS NOT NULL, b.id`, month)
	if err != nil {
		return nil, err
	}

	// Read and close the result set before calculating spend. The store deliberately
	// uses one SQLite connection, so issuing nested queries while rows is open would
	// wait forever for that same connection.
	var out []Budget
	for rows.Next() {
		var b Budget
		var pid sql.NullInt64
		var pname, created string
		if err := rows.Scan(&b.ID, &b.Month, &pid, &pname, &b.Kind, &b.Amount, &created); err != nil {
			return nil, err
		}
		if pid.Valid {
			id := pid.Int64
			b.PersonID = &id
			b.PersonName = pname
		}
		b.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	start, _ := time.ParseInLocation("2006-01", month, loc)
	end := start.AddDate(0, 1, 0)
	for i := range out {
		spent, err := s.budgetSpent(ctx, start, end, out[i].PersonID, out[i].Kind)
		if err != nil {
			return nil, err
		}
		out[i].Spent = spent
		if out[i].Amount > 0 {
			out[i].Pct = round2(out[i].Spent / out[i].Amount * 100)
		}
	}
	if out == nil {
		out = []Budget{}
	}
	return out, nil
}

func (s *Store) budgetSpent(ctx context.Context, from, toExclusive time.Time, personID *int64, kind string) (float64, error) {
	q := `SELECT COALESCE(SUM(amount),0) FROM transactions WHERE direction='out' AND occurred_at >= ? AND occurred_at < ?`
	args := []any{from.UTC().Format(time.RFC3339Nano), toExclusive.UTC().Format(time.RFC3339Nano)}
	if kind != "" && kind != "all" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	if personID != nil {
		q += ` AND person_id = ?`
		args = append(args, *personID)
	}
	var sum float64
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&sum)
	return round2(sum), err
}

func (s *Store) UpsertBudget(ctx context.Context, month string, personID *int64, kind string, amount float64) (Budget, error) {
	if month == "" {
		return Budget{}, fmt.Errorf("month required")
	}
	if kind == "" {
		kind = "consume"
	}
	if amount <= 0 {
		return Budget{}, fmt.Errorf("amount must be > 0")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var pid any
	if personID != nil {
		pid = *personID
	}
	// A UNIQUE constraint treats NULL values as distinct in SQLite. Update with an
	// explicit NULL-safe predicate first so a global budget is a true upsert too.
	res, err := s.db.ExecContext(ctx, `
UPDATE budgets SET amount = ?
WHERE month = ? AND kind = ?
  AND ((person_id IS NULL AND ? IS NULL) OR person_id = ?)`,
		amount, month, kind, pid, pid)
	if err != nil {
		return Budget{}, err
	}
	if affected, _ := res.RowsAffected(); affected > 0 {
		var id int64
		err = s.db.QueryRowContext(ctx, `
SELECT id FROM budgets
WHERE month = ? AND kind = ?
  AND ((person_id IS NULL AND ? IS NULL) OR person_id = ?)
ORDER BY id LIMIT 1`, month, kind, pid, pid).Scan(&id)
		if err != nil {
			return Budget{}, err
		}
		return Budget{ID: id, Month: month, PersonID: personID, Kind: kind, Amount: amount}, nil
	}

	res, err = s.db.ExecContext(ctx, `
INSERT INTO budgets (month, person_id, kind, amount, created_at)
VALUES (?, ?, ?, ?, ?)`, month, pid, kind, amount, now)
	if err != nil {
		return Budget{}, err
	}
	id, _ := res.LastInsertId()
	b := Budget{ID: id, Month: month, PersonID: personID, Kind: kind, Amount: amount}
	return b, nil
}

func (s *Store) DeleteBudget(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM budgets WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("budget not found")
	}
	return nil
}

// ---- Label rules ----

type LabelRule struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	MatchField string    `json:"match_field"`
	MatchOp    string    `json:"match_op"`
	MatchValue string    `json:"match_value"`
	PersonID   *int64    `json:"person_id,omitempty"`
	PersonName string    `json:"person_name,omitempty"`
	TagID      *int64    `json:"tag_id,omitempty"`
	TagName    string    `json:"tag_name,omitempty"`
	Enabled    bool      `json:"enabled"`
	Priority   int       `json:"priority"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) ListRules(ctx context.Context) ([]LabelRule, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT r.id, r.name, r.match_field, r.match_op, r.match_value,
       r.person_id, COALESCE(p.name,''), r.tag_id, COALESCE(t.name,''),
       r.enabled, r.priority, r.created_at
FROM label_rules r
LEFT JOIN people p ON p.id = r.person_id
LEFT JOIN tags t ON t.id = r.tag_id
ORDER BY r.priority DESC, r.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LabelRule
	for rows.Next() {
		var r LabelRule
		var pid, tid sql.NullInt64
		var en int
		var created, pn, tn string
		if err := rows.Scan(&r.ID, &r.Name, &r.MatchField, &r.MatchOp, &r.MatchValue,
			&pid, &pn, &tid, &tn, &en, &r.Priority, &created); err != nil {
			return nil, err
		}
		if pid.Valid {
			id := pid.Int64
			r.PersonID = &id
			r.PersonName = pn
		}
		if tid.Valid {
			id := tid.Int64
			r.TagID = &id
			r.TagName = tn
		}
		r.Enabled = en != 0
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, r)
	}
	if out == nil {
		out = []LabelRule{}
	}
	return out, rows.Err()
}

func (s *Store) CreateRule(ctx context.Context, r LabelRule) (LabelRule, error) {
	if r.MatchField == "" || r.MatchValue == "" {
		return r, fmt.Errorf("match_field and match_value required")
	}
	if r.MatchOp == "" {
		r.MatchOp = "eq"
	}
	if r.PersonID == nil && r.TagID == nil {
		return r, fmt.Errorf("person_id or tag_id required")
	}
	now := time.Now().UTC()
	var pid, tid any
	if r.PersonID != nil {
		pid = *r.PersonID
	}
	if r.TagID != nil {
		tid = *r.TagID
	}
	en := 1
	if !r.Enabled {
		en = 0
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO label_rules (name, match_field, match_op, match_value, person_id, tag_id, enabled, priority, created_at)
VALUES (?,?,?,?,?,?,?,?,?)`,
		r.Name, r.MatchField, r.MatchOp, r.MatchValue, pid, tid, en, r.Priority, now.Format(time.RFC3339Nano))
	if err != nil {
		return r, err
	}
	r.ID, _ = res.LastInsertId()
	r.CreatedAt = now
	r.Enabled = en != 0
	return r, nil
}

func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM label_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("rule not found")
	}
	return nil
}

// ApplyRulesToTxn applies first matching enabled rules (by priority) to a txn id.
func (s *Store) ApplyRulesToTxn(ctx context.Context, txnID int64) error {
	txn, err := s.GetTransaction(ctx, txnID)
	if err != nil {
		return err
	}
	rules, err := s.ListRules(ctx)
	if err != nil {
		return err
	}
	var personSet bool
	var personID *int64
	tagIDs := map[int64]struct{}{}
	for _, tg := range txn.Tags {
		tagIDs[tg.ID] = struct{}{}
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if !ruleMatches(rule, txn) {
			continue
		}
		if rule.PersonID != nil && !personSet && txn.PersonID == nil {
			personSet = true
			personID = rule.PersonID
		}
		if rule.TagID != nil {
			tagIDs[*rule.TagID] = struct{}{}
		}
	}
	if !personSet && len(tagIDs) == len(txn.Tags) {
		return nil
	}
	ids := make([]int64, 0, len(tagIDs))
	for id := range tagIDs {
		ids = append(ids, id)
	}
	u := LabelUpdate{TagsSet: true, TagIDs: ids}
	if personSet {
		u.PersonSet = true
		u.PersonID = personID
	}
	return s.UpdateTransactionLabels(ctx, txnID, u)
}

func ruleMatches(rule LabelRule, txn model.Transaction) bool {
	var field string
	switch rule.MatchField {
	case "merchant_norm":
		field = txn.MerchantNorm
	case "merchant":
		field = txn.Merchant
	case "category":
		field = txn.Category
	case "kind":
		field = string(txn.Kind)
	case "note":
		field = txn.Note
	default:
		field = txn.MerchantNorm
	}
	v := rule.MatchValue
	switch rule.MatchOp {
	case "contains":
		return strings.Contains(field, v)
	default: // eq
		return field == v
	}
}

// ---- Goals ----

type Goal struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Target    float64   `json:"target"`
	Current   float64   `json:"current"`
	Pct       float64   `json:"pct"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) ListGoals(ctx context.Context) ([]Goal, error) {
	bal, _, ok, err := s.LatestBalance(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, target, created_at FROM goals ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Goal
	for rows.Next() {
		var g Goal
		var created string
		if err := rows.Scan(&g.ID, &g.Name, &g.Target, &created); err != nil {
			return nil, err
		}
		g.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if ok {
			g.Current = bal
			if g.Target > 0 {
				g.Pct = round2(g.Current / g.Target * 100)
			}
		}
		out = append(out, g)
	}
	if out == nil {
		out = []Goal{}
	}
	return out, rows.Err()
}

func (s *Store) CreateGoal(ctx context.Context, name string, target float64) (Goal, error) {
	if strings.TrimSpace(name) == "" || target <= 0 {
		return Goal{}, fmt.Errorf("name and positive target required")
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO goals (name, target, created_at) VALUES (?,?,?)`,
		name, target, now.Format(time.RFC3339Nano))
	if err != nil {
		return Goal{}, err
	}
	id, _ := res.LastInsertId()
	g := Goal{ID: id, Name: name, Target: target, CreatedAt: now}
	if bal, _, ok, _ := s.LatestBalance(ctx); ok {
		g.Current = bal
		g.Pct = round2(bal / target * 100)
	}
	return g, nil
}

func (s *Store) DeleteGoal(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM goals WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("goal not found")
	}
	return nil
}

// ---- Cards ----

type CardInfo struct {
	CardLast4 string  `json:"card_last4"`
	Bank      string  `json:"bank"`
	TxnCount  int     `json:"txn_count"`
	Expense   float64 `json:"expense"`
	Income    float64 `json:"income"`
}

func (s *Store) ListCards(ctx context.Context) ([]CardInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(NULLIF(card_last4,''),'未知'),
       COALESCE(MAX(bank),''),
       COUNT(*),
       SUM(CASE WHEN direction='out' THEN amount ELSE 0 END),
       SUM(CASE WHEN direction='in' THEN amount ELSE 0 END)
FROM transactions
GROUP BY COALESCE(NULLIF(card_last4,''),'未知')
ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CardInfo
	for rows.Next() {
		var c CardInfo
		if err := rows.Scan(&c.CardLast4, &c.Bank, &c.TxnCount, &c.Expense, &c.Income); err != nil {
			return nil, err
		}
		c.Expense = round2(c.Expense)
		c.Income = round2(c.Income)
		out = append(out, c)
	}
	if out == nil {
		out = []CardInfo{}
	}
	return out, rows.Err()
}

// ---- Digest ----

type Digest struct {
	Date           string              `json:"date"`
	TodayExpense   float64             `json:"today_expense"`
	TodayIncome    float64             `json:"today_income"`
	TodayTxnCount  int                 `json:"today_txn_count"`
	TodayConsume   float64             `json:"today_consume"`
	WeekExpense    float64             `json:"week_expense"`
	WeekConsume    float64             `json:"week_consume"`
	LatestBalance  float64             `json:"latest_balance,omitempty"`
	BalanceKnown   bool                `json:"balance_known"`
	DaysOfRunway   float64             `json:"days_of_runway"`
	UnlabeledToday int                 `json:"unlabeled_today"`
	UnlabeledWeek  int                 `json:"unlabeled_week"`
	TopToday       []model.Transaction `json:"top_today"`
	Anomalies      []string            `json:"anomalies"`
}

func (s *Store) Digest(ctx context.Context, loc *time.Location) (*Digest, error) {
	if loc == nil {
		loc = time.Local
	}
	now := time.Now().In(loc)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	weekStart := dayStart.AddDate(0, 0, -6)

	todayAll, err := s.rangeTotals(ctx, dayStart, dayEnd, loc, "all")
	if err != nil {
		return nil, err
	}
	todayConsume, err := s.rangeTotals(ctx, dayStart, dayEnd, loc, "consume")
	if err != nil {
		return nil, err
	}
	weekAll, err := s.rangeTotals(ctx, weekStart, dayEnd, loc, "all")
	if err != nil {
		return nil, err
	}
	weekConsume, err := s.rangeTotals(ctx, weekStart, dayEnd, loc, "consume")
	if err != nil {
		return nil, err
	}

	d := &Digest{
		Date:          dayStart.Format("2006-01-02"),
		TodayExpense:  todayAll.Expense,
		TodayIncome:   todayAll.Income,
		TodayTxnCount: todayAll.TxnCount,
		TodayConsume:  todayConsume.Expense,
		WeekExpense:   weekAll.Expense,
		WeekConsume:   weekConsume.Expense,
		Anomalies:     []string{},
	}
	if bal, _, ok, err := s.LatestBalance(ctx); err != nil {
		return nil, err
	} else if ok {
		d.BalanceKnown = true
		d.LatestBalance = bal
		if weekConsume.AvgDailyExpense > 0 {
			d.DaysOfRunway = round2(bal / weekConsume.AvgDailyExpense)
		}
	}

	_ = s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM transactions
WHERE direction='out' AND person_id IS NULL AND occurred_at >= ? AND occurred_at < ?`,
		dayStart.UTC().Format(time.RFC3339Nano), dayEnd.UTC().Format(time.RFC3339Nano)).Scan(&d.UnlabeledToday)
	_ = s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM transactions
WHERE direction='out' AND person_id IS NULL AND occurred_at >= ? AND occurred_at < ?`,
		weekStart.UTC().Format(time.RFC3339Nano), dayEnd.UTC().Format(time.RFC3339Nano)).Scan(&d.UnlabeledWeek)

	top, err := s.topExpenses(ctx, dayStart, dayEnd, 5)
	if err != nil {
		return nil, err
	}
	d.TopToday = top

	// simple anomalies
	if todayConsume.Expense > 0 && weekConsume.AvgDailyExpense > 0 && todayConsume.Expense > weekConsume.AvgDailyExpense*2 {
		d.Anomalies = append(d.Anomalies, fmt.Sprintf("今日消费 %.2f 超过近7日均消费的2倍", todayConsume.Expense))
	}
	if d.BalanceKnown && d.LatestBalance < 100 {
		d.Anomalies = append(d.Anomalies, fmt.Sprintf("余额偏低：%.2f", d.LatestBalance))
	}
	if d.UnlabeledToday > 0 {
		d.Anomalies = append(d.Anomalies, fmt.Sprintf("今日还有 %d 笔支出未打归属", d.UnlabeledToday))
	}
	return d, nil
}

// ExportTransactions returns rows for CSV.
func (s *Store) ExportTransactions(ctx context.Context, from, toExclusive time.Time) ([]model.Transaction, error) {
	rows, err := s.db.QueryContext(ctx, txnSelect+`
WHERE t.occurred_at >= ? AND t.occurred_at < ?
ORDER BY t.occurred_at ASC, t.id ASC`,
		from.UTC().Format(time.RFC3339Nano),
		toExclusive.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list, err := scanTxnList(rows)
	if err != nil {
		return nil, err
	}
	return s.EnrichTransactions(ctx, list)
}
