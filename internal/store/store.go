package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cashpulse/internal/classify"
	"cashpulse/internal/model"

	_ "modernc.org/sqlite"
)

// Store wraps SQLite persistence.
type Store struct {
	db *sql.DB
}

// Open creates the database file if needed and applies schema.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS raw_sms (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    text       TEXT    NOT NULL,
    source     TEXT    NOT NULL DEFAULT '',
    status     TEXT    NOT NULL DEFAULT 'pending',
    error      TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS people (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    color      TEXT    NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    color      TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS transactions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    raw_sms_id    INTEGER NOT NULL REFERENCES raw_sms(id) ON DELETE CASCADE,
    amount        REAL    NOT NULL,
    currency      TEXT    NOT NULL DEFAULT 'CNY',
    direction     TEXT    NOT NULL,
    merchant      TEXT    NOT NULL DEFAULT '',
    card_last4    TEXT    NOT NULL DEFAULT '',
    occurred_at   TEXT    NOT NULL,
    category      TEXT    NOT NULL DEFAULT '',
    note          TEXT    NOT NULL DEFAULT '',
    bank          TEXT    NOT NULL DEFAULT '',
    balance_after REAL,
    balance_known INTEGER NOT NULL DEFAULT 0,
    person_id     INTEGER REFERENCES people(id) ON DELETE SET NULL,
    created_at    TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS transaction_tags (
    transaction_id INTEGER NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    tag_id         INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (transaction_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_txn_occurred_at ON transactions(occurred_at);
CREATE INDEX IF NOT EXISTS idx_txn_direction ON transactions(direction);
CREATE INDEX IF NOT EXISTS idx_raw_sms_status ON raw_sms(status);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Lightweight additive migrations for existing DBs.
	for _, col := range []struct {
		name string
		ddl  string
	}{
		{"balance_after", `ALTER TABLE transactions ADD COLUMN balance_after REAL`},
		{"balance_known", `ALTER TABLE transactions ADD COLUMN balance_known INTEGER NOT NULL DEFAULT 0`},
		{"person_id", `ALTER TABLE transactions ADD COLUMN person_id INTEGER REFERENCES people(id) ON DELETE SET NULL`},
		{"kind", `ALTER TABLE transactions ADD COLUMN kind TEXT NOT NULL DEFAULT ''`},
		{"merchant_norm", `ALTER TABLE transactions ADD COLUMN merchant_norm TEXT NOT NULL DEFAULT ''`},
	} {
		if !s.columnExists("transactions", col.name) {
			if _, err := s.db.Exec(col.ddl); err != nil {
				return fmt.Errorf("migrate add %s: %w", col.name, err)
			}
		}
	}

	// Indexes that depend on additive columns.
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_txn_person ON transactions(person_id)`); err != nil {
		return fmt.Errorf("migrate person index: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_txn_kind ON transactions(kind)`); err != nil {
		return fmt.Errorf("migrate kind index: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_txn_merchant_norm ON transactions(merchant_norm)`); err != nil {
		return fmt.Errorf("migrate merchant_norm index: %w", err)
	}
	// composite indexes for analytics filters
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_txn_kind_occurred ON transactions(kind, occurred_at)`); err != nil {
		return fmt.Errorf("migrate kind_occurred index: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_txn_dir_occurred ON transactions(direction, occurred_at)`); err != nil {
		return fmt.Errorf("migrate dir_occurred index: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_txn_person_occurred ON transactions(person_id, occurred_at)`); err != nil {
		return fmt.Errorf("migrate person_occurred index: %w", err)
	}

	if err := s.seedLabels(); err != nil {
		return fmt.Errorf("seed labels: %w", err)
	}
	if err := s.migrateFeatures(); err != nil {
		return fmt.Errorf("migrate features: %w", err)
	}

	// SMS idempotency fingerprint (exact body hash).
	if !s.columnExists("raw_sms", "fingerprint") {
		if _, err := s.db.Exec(`ALTER TABLE raw_sms ADD COLUMN fingerprint TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migrate fingerprint: %w", err)
		}
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_raw_sms_fingerprint ON raw_sms(fingerprint) WHERE fingerprint != ''`); err != nil {
		return fmt.Errorf("migrate fingerprint index: %w", err)
	}
	// one transaction per raw SMS
	var dup int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM (
		SELECT raw_sms_id FROM transactions GROUP BY raw_sms_id HAVING COUNT(*) > 1
	)`).Scan(&dup)
	if dup > 0 {
		return fmt.Errorf("migrate: found %d duplicate raw_sms_id in transactions; fix data before unique index", dup)
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_transactions_raw_sms_id ON transactions(raw_sms_id)`); err != nil {
		return fmt.Errorf("migrate txn raw_sms unique: %w", err)
	}
	if err := s.backfillKindAndNorm(); err != nil {
		return fmt.Errorf("backfill kind: %w", err)
	}
	return nil
}

func (s *Store) columnExists(table, col string) bool {
	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == col {
			return true
		}
	}
	return false
}

// InsertRawSMS stores the original SMS body and returns its id.
// SMSFingerprint is a stable hash of normalized SMS body for idempotency.
func SMSFingerprint(text string) string {
	norm := strings.TrimSpace(text)
	norm = strings.ReplaceAll(norm, "\r\n", "\n")
	norm = strings.ReplaceAll(norm, "\r", "\n")
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

// InsertRawSMSResult is the outcome of inserting (or finding) a raw SMS row.
type InsertRawSMSResult struct {
	ID        int64
	Duplicate bool
	Status    model.ParseStatus
	Error     string
}

// InsertRawSMS stores a raw SMS. Identical fingerprint returns the existing row as Duplicate.
func (s *Store) InsertRawSMS(ctx context.Context, text, source string) (InsertRawSMSResult, error) {
	fp := SMSFingerprint(text)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO raw_sms (text, source, status, created_at, fingerprint) VALUES (?, ?, ?, ?, ?)`,
		text, source, model.ParseStatusPending, now, fp,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			var out InsertRawSMSResult
			var status, errMsg string
			qerr := s.db.QueryRowContext(ctx, `
SELECT id, status, error FROM raw_sms WHERE fingerprint = ?`, fp).Scan(&out.ID, &status, &errMsg)
			if qerr != nil {
				return InsertRawSMSResult{}, err
			}
			out.Duplicate = true
			out.Status = model.ParseStatus(status)
			out.Error = errMsg
			return out, nil
		}
		return InsertRawSMSResult{}, err
	}
	id, _ := res.LastInsertId()
	return InsertRawSMSResult{ID: id, Status: model.ParseStatusPending}, nil
}

// TransactionByRawSMSID returns the transaction linked to a raw SMS, if any.
func (s *Store) TransactionByRawSMSID(ctx context.Context, rawID int64) (*model.Transaction, error) {
	row := s.db.QueryRowContext(ctx, txnSelect+` WHERE t.raw_sms_id = ?`, rawID)
	txn, err := scanTxn(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	list, err := s.EnrichTransactions(ctx, []model.Transaction{txn})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return &txn, nil
	}
	return &list[0], nil
}


// MarkRawSMS updates parse status for a raw SMS row.
func (s *Store) MarkRawSMS(ctx context.Context, id int64, status model.ParseStatus, errMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE raw_sms SET status = ?, error = ? WHERE id = ?`,
		status, errMsg, id,
	)
	return err
}

// InsertTransaction stores a structured transaction.
func (s *Store) InsertTransaction(ctx context.Context, t *model.Transaction) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var bal any
	if t.BalanceKnown {
		bal = t.BalanceAfter
	} else {
		bal = nil
	}
	known := 0
	if t.BalanceKnown {
		known = 1
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO transactions (
    raw_sms_id, amount, currency, direction, merchant, merchant_norm, card_last4,
    occurred_at, category, kind, note, bank, balance_after, balance_known, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.RawSMSID, t.Amount, t.Currency, t.Direction, t.Merchant, t.MerchantNorm, t.CardLast4,
		t.OccurredAt.UTC().Format(time.RFC3339Nano), t.Category, t.Kind, t.Note, t.Bank,
		bal, known, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const txnSelect = `
SELECT t.id, t.raw_sms_id, t.amount, t.currency, t.direction, t.merchant, t.merchant_norm, t.card_last4,
       t.occurred_at, t.category, t.kind, t.note, t.bank, t.balance_after, t.balance_known,
       t.person_id, p.name, p.color, t.created_at
FROM transactions t
LEFT JOIN people p ON p.id = t.person_id`

// ListTransactions returns recent transactions, optionally limited.
func (s *Store) ListTransactions(ctx context.Context, limit, offset int) ([]model.Transaction, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx, txnSelect+`
ORDER BY t.occurred_at DESC, t.id DESC
LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Transaction
	for rows.Next() {
		t, err := scanTxn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.Transaction{}
	}
	return s.EnrichTransactions(ctx, out)
}

// CountTransactions returns total transaction rows.
func (s *Store) CountTransactions(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transactions`).Scan(&n)
	return n, err
}

// CountUnparsed returns raw SMS that failed or are still pending (not ok/ignored).
func (s *Store) CountUnparsed(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM raw_sms WHERE status NOT IN (?, ?)`,
		model.ParseStatusOK, model.ParseStatusIgnored,
	).Scan(&n)
	return n, err
}

// ListUnparsed returns raw SMS that failed or are still pending.
func (s *Store) ListUnparsed(ctx context.Context, limit int) ([]model.RawSMS, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, text, source, status, error, created_at
FROM raw_sms
WHERE status NOT IN (?, ?)
ORDER BY id DESC
LIMIT ?`, model.ParseStatusOK, model.ParseStatusIgnored, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.RawSMS
	for rows.Next() {
		var r model.RawSMS
		var created string
		if err := rows.Scan(&r.ID, &r.Text, &r.Source, &r.Status, &r.Error, &created); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LatestBalance returns the most recent known balance.
func (s *Store) LatestBalance(ctx context.Context) (balance float64, at time.Time, ok bool, err error) {
	balance, at, _, ok, err = s.LatestBalanceDetail(ctx)
	return
}

// LatestBalanceDetail returns latest SMS balance plus card last4 for UI labeling.
func (s *Store) LatestBalanceDetail(ctx context.Context) (balance float64, at time.Time, cardLast4 string, ok bool, err error) {
	var bal sql.NullFloat64
	var occurred, card string
	err = s.db.QueryRowContext(ctx, `
SELECT balance_after, occurred_at, COALESCE(card_last4,'')
FROM transactions
WHERE balance_known = 1 AND balance_after IS NOT NULL
ORDER BY occurred_at DESC, id DESC
LIMIT 1`).Scan(&bal, &occurred, &card)
	if err == sql.ErrNoRows {
		return 0, time.Time{}, "", false, nil
	}
	if err != nil {
		return 0, time.Time{}, "", false, err
	}
	if !bal.Valid {
		return 0, time.Time{}, "", false, nil
	}
	at, _ = time.Parse(time.RFC3339Nano, occurred)
	if at.IsZero() {
		at, _ = time.Parse(time.RFC3339, occurred)
	}
	return bal.Float64, at, card, true, nil
}

// DailySummaries aggregates expense/income for the last nDays (including today), local dates.
func (s *Store) DailySummaries(ctx context.Context, nDays int, loc *time.Location) ([]model.DailySummary, error) {
	if nDays <= 0 {
		nDays = 7
	}
	if loc == nil {
		loc = time.Local
	}

	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(nDays - 1))
	startUTC := start.UTC().Format(time.RFC3339Nano)

	rows, err := s.db.QueryContext(ctx, `
SELECT amount, direction, occurred_at
FROM transactions
WHERE occurred_at >= ?
ORDER BY occurred_at ASC`, startUTC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDate := make(map[string]*model.DailySummary, nDays)
	order := make([]string, 0, nDays)
	for i := 0; i < nDays; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		byDate[d] = &model.DailySummary{Date: d}
		order = append(order, d)
	}

	for rows.Next() {
		var amount float64
		var direction string
		var occurred string
		if err := rows.Scan(&amount, &direction, &occurred); err != nil {
			return nil, err
		}
		ts, err := time.Parse(time.RFC3339Nano, occurred)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, occurred)
			if err != nil {
				continue
			}
		}
		day := ts.In(loc).Format("2006-01-02")
		sum, ok := byDate[day]
		if !ok {
			continue
		}
		sum.TxnCount++
		switch model.Direction(direction) {
		case model.DirectionOut:
			sum.Expense += amount
		case model.DirectionIn:
			sum.Income += amount
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]model.DailySummary, 0, len(order))
	for _, d := range order {
		sum := byDate[d]
		sum.Net = sum.Income - sum.Expense
		sum.Expense = round2(sum.Expense)
		sum.Income = round2(sum.Income)
		sum.Net = round2(sum.Net)
		out = append(out, *sum)
	}
	return out, nil
}

// DaySummary returns totals for a specific local calendar day.
func (s *Store) DaySummary(ctx context.Context, day time.Time, loc *time.Location) (model.TodaySummary, error) {
	if loc == nil {
		loc = time.Local
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)

	rows, err := s.db.QueryContext(ctx, `
SELECT amount, direction
FROM transactions
WHERE occurred_at >= ? AND occurred_at < ?`,
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return model.TodaySummary{}, err
	}
	defer rows.Close()

	var sum model.TodaySummary
	sum.Date = start.Format("2006-01-02")
	for rows.Next() {
		var amount float64
		var direction string
		if err := rows.Scan(&amount, &direction); err != nil {
			return model.TodaySummary{}, err
		}
		sum.TxnCount++
		switch model.Direction(direction) {
		case model.DirectionOut:
			sum.Expense += amount
		case model.DirectionIn:
			sum.Income += amount
		}
	}
	sum.Expense = round2(sum.Expense)
	sum.Income = round2(sum.Income)
	sum.Net = round2(sum.Income - sum.Expense)
	return sum, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanTxn(row scannable) (model.Transaction, error) {
	var t model.Transaction
	var occurred, created string
	var direction string
	var bal sql.NullFloat64
	var known int
	var personID sql.NullInt64
	var personName, personColor sql.NullString
	var kind string
	err := row.Scan(
		&t.ID, &t.RawSMSID, &t.Amount, &t.Currency, &direction, &t.Merchant, &t.MerchantNorm, &t.CardLast4,
		&occurred, &t.Category, &kind, &t.Note, &t.Bank, &bal, &known,
		&personID, &personName, &personColor, &created,
	)
	if err != nil {
		return t, err
	}
	t.Direction = model.Direction(direction)
	t.Kind = model.Kind(kind)
	t.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurred)
	if t.OccurredAt.IsZero() {
		t.OccurredAt, _ = time.Parse(time.RFC3339, occurred)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if known != 0 && bal.Valid {
		t.BalanceKnown = true
		t.BalanceAfter = bal.Float64
	}
	if personID.Valid {
		id := personID.Int64
		t.PersonID = &id
		if personName.Valid {
			t.PersonName = personName.String
		}
		if personColor.Valid {
			t.PersonColor = personColor.String
		}
	}
	return t, nil
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// SearchTransactions filters by optional merchant keyword.
func (s *Store) SearchTransactions(ctx context.Context, q string, limit, offset int) ([]model.Transaction, error) {
	return s.ListTransactionsFiltered(ctx, TxnFilter{Q: q, Limit: limit, Offset: offset})
}

// backfillKindAndNorm fills kind/merchant_norm for rows created before classification.
func (s *Store) backfillKindAndNorm() error {
	rows, err := s.db.Query(`
SELECT id, direction, merchant, category, note, COALESCE(kind,''), COALESCE(merchant_norm,'')
FROM transactions
WHERE kind = '' OR kind IS NULL OR merchant_norm = '' OR merchant_norm IS NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		id                         int64
		direction, merchant, cat, note, kind, norm string
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.direction, &r.merchant, &r.cat, &r.note, &r.kind, &r.norm); err != nil {
			return err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range list {
		t := &model.Transaction{
			Direction: model.Direction(r.direction),
			Merchant:  r.merchant,
			Category:  r.cat,
			Note:      r.note,
		}
		classify.Enrich(t)
		if _, err := s.db.Exec(`UPDATE transactions SET kind = ?, merchant_norm = ? WHERE id = ?`, t.Kind, t.MerchantNorm, r.id); err != nil {
			return err
		}
	}
	return nil
}


// IngestParse holds a fully decided parse outcome before DB write.
type IngestParse struct {
	Status       model.ParseStatus
	IgnoreReason string
	ParseError   string
	MatchedRule  string
	Transaction  *model.Transaction // nil unless StatusOK
}

// PersistIngestResult atomically stores raw SMS + optional transaction.
// Parse is done outside. Never leaves permanent pending for new inserts.
func (s *Store) PersistIngestResult(ctx context.Context, text, source string, parse IngestParse) (InsertRawSMSResult, *model.Transaction, error) {
	fp := SMSFingerprint(text)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if source == "" {
		source = "shortcut"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return InsertRawSMSResult{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
INSERT INTO raw_sms (text, source, status, error, created_at, fingerprint)
VALUES (?, ?, ?, ?, ?, ?)`,
		text, source, parse.Status, firstNonEmpty(parse.ParseError, parse.IgnoreReason), now, fp,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			// existing fingerprint — return committed final state
			var out InsertRawSMSResult
			var status, errMsg string
			qerr := tx.QueryRowContext(ctx, `SELECT id, status, error FROM raw_sms WHERE fingerprint = ?`, fp).
				Scan(&out.ID, &status, &errMsg)
			if qerr != nil {
				return InsertRawSMSResult{}, nil, err
			}
			out.Duplicate = true
			out.Status = model.ParseStatus(status)
			out.Error = errMsg
			// load txn if any (same connection via tx)
			var txnID sql.NullInt64
			_ = tx.QueryRowContext(ctx, `SELECT id FROM transactions WHERE raw_sms_id = ?`, out.ID).Scan(&txnID)
			if err := tx.Commit(); err != nil {
				return InsertRawSMSResult{}, nil, err
			}
			var txn *model.Transaction
			if txnID.Valid {
				t, err := s.GetTransaction(ctx, txnID.Int64)
				if err == nil {
					txn = &t
				}
			}
			return out, txn, nil
		}
		return InsertRawSMSResult{}, nil, err
	}
	rawID, _ := res.LastInsertId()

	var savedTxn *model.Transaction
	if parse.Status == model.ParseStatusOK && parse.Transaction != nil {
		txn := *parse.Transaction
		txn.RawSMSID = rawID
		if txn.OccurredAt.IsZero() {
			txn.OccurredAt = time.Now()
		}
		var bal any
		known := 0
		if txn.BalanceKnown {
			bal = txn.BalanceAfter
			known = 1
		}
		tres, err := tx.ExecContext(ctx, `
INSERT INTO transactions (
    raw_sms_id, amount, currency, direction, merchant, merchant_norm, card_last4,
    occurred_at, category, kind, note, bank, balance_after, balance_known, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rawID, txn.Amount, txn.Currency, txn.Direction, txn.Merchant, txn.MerchantNorm, txn.CardLast4,
			txn.OccurredAt.UTC().Format(time.RFC3339Nano), txn.Category, txn.Kind, txn.Note, txn.Bank,
			bal, known, now,
		)
		if err != nil {
			return InsertRawSMSResult{}, nil, err
		}
		tid, _ := tres.LastInsertId()
		txn.ID = tid
		txn.CreatedAt = time.Now().UTC()
		savedTxn = &txn
	}

	if err := tx.Commit(); err != nil {
		return InsertRawSMSResult{}, nil, err
	}

	// apply rules outside txn (best-effort) if we have a new txn
	if savedTxn != nil {
		_ = s.ApplyRulesToTxn(ctx, savedTxn.ID)
		if updated, err := s.GetTransaction(ctx, savedTxn.ID); err == nil {
			savedTxn = &updated
		}
	}
	return InsertRawSMSResult{ID: rawID, Status: parse.Status, Error: firstNonEmpty(parse.ParseError, parse.IgnoreReason)}, savedTxn, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
