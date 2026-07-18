package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"cashpulse/internal/model"
)

var defaultPeople = []struct {
	Name  string
	Color string
}{
	{"我", "#3987e5"},
	{"老婆", "#d55181"},
	{"孩子", "#c98500"},
}

var defaultTags = []struct {
	Name  string
	Color string
}{
	{"餐饮", "#d95926"},
	{"日用", "#199e70"},
	{"交通", "#9085e9"},
	{"人情", "#e66767"},
}

func (s *Store) seedLabels() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM people`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for i, p := range defaultPeople {
			if _, err := s.db.Exec(
				`INSERT INTO people (name, color, sort_order, created_at) VALUES (?, ?, ?, ?)`,
				p.Name, p.Color, i, now,
			); err != nil {
				return err
			}
		}
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tags`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for _, t := range defaultTags {
			if _, err := s.db.Exec(
				`INSERT INTO tags (name, color, created_at) VALUES (?, ?, ?)`,
				t.Name, t.Color, now,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListPeople returns all people ordered for the UI.
func (s *Store) ListPeople(ctx context.Context) ([]model.Person, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, color, sort_order, created_at
FROM people
ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Person
	for rows.Next() {
		var p model.Person
		var created string
		if err := rows.Scan(&p.ID, &p.Name, &p.Color, &p.SortOrder, &created); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, p)
	}
	if out == nil {
		out = []model.Person{}
	}
	return out, rows.Err()
}

// CreatePerson adds a new person.
func (s *Store) CreatePerson(ctx context.Context, name, color string) (model.Person, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Person{}, fmt.Errorf("name is required")
	}
	if color == "" {
		color = "#9085e9"
	}
	var maxSort sql.NullInt64
	_ = s.db.QueryRowContext(ctx, `SELECT MAX(sort_order) FROM people`).Scan(&maxSort)
	sort := 0
	if maxSort.Valid {
		sort = int(maxSort.Int64) + 1
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO people (name, color, sort_order, created_at) VALUES (?, ?, ?, ?)`,
		name, color, sort, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return model.Person{}, fmt.Errorf("person already exists")
		}
		return model.Person{}, err
	}
	id, _ := res.LastInsertId()
	return model.Person{ID: id, Name: name, Color: color, SortOrder: sort, CreatedAt: now}, nil
}

// DeletePerson removes a person and clears assignments (does not delete txns).
func (s *Store) DeletePerson(ctx context.Context, id int64) error {
	_, _ = s.db.ExecContext(ctx, `UPDATE transactions SET person_id = NULL WHERE person_id = ?`, id)
	res, err := s.db.ExecContext(ctx, `DELETE FROM people WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("person not found")
	}
	return nil
}

// ListTags returns all tags.
func (s *Store) ListTags(ctx context.Context) ([]model.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, color, created_at FROM tags ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Tag
	for rows.Next() {
		var t model.Tag
		var created string
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &created); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, t)
	}
	if out == nil {
		out = []model.Tag{}
	}
	return out, rows.Err()
}

// CreateTag adds a tag.
func (s *Store) CreateTag(ctx context.Context, name, color string) (model.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Tag{}, fmt.Errorf("name is required")
	}
	if color == "" {
		color = "#8a897c"
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO tags (name, color, created_at) VALUES (?, ?, ?)`,
		name, color, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return model.Tag{}, fmt.Errorf("tag already exists")
		}
		return model.Tag{}, err
	}
	id, _ := res.LastInsertId()
	return model.Tag{ID: id, Name: name, Color: color, CreatedAt: now}, nil
}

// DeleteTag removes a tag and its links.
func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM transaction_tags WHERE tag_id = ?`, id)
	res, err := s.db.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("tag not found")
	}
	return nil
}

// LabelUpdate is a partial update for a transaction's human labels.
type LabelUpdate struct {
	// PersonID: nil pointer field means "don't change";
	// pointer to nil means clear; pointer to id means set.
	PersonSet bool
	PersonID  *int64
	// TagIDs: if TagsSet, replace all tags with this list.
	TagsSet bool
	TagIDs  []int64
}

// UpdateTransactionLabels sets person and/or tags on a transaction.
func (s *Store) UpdateTransactionLabels(ctx context.Context, txnID int64, u LabelUpdate) error {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transactions WHERE id = ?`, txnID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("transaction not found")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if u.PersonSet {
		if u.PersonID != nil {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM people WHERE id = ?`, *u.PersonID).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("person not found")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE transactions SET person_id = ? WHERE id = ?`, *u.PersonID, txnID); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE transactions SET person_id = NULL WHERE id = ?`, txnID); err != nil {
				return err
			}
		}
	}

	if u.TagsSet {
		if _, err := tx.ExecContext(ctx, `DELETE FROM transaction_tags WHERE transaction_id = ?`, txnID); err != nil {
			return err
		}
		for _, tid := range u.TagIDs {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO transaction_tags (transaction_id, tag_id) VALUES (?, ?)`,
				txnID, tid,
			); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// GetTransaction returns one transaction with person + tags.
func (s *Store) GetTransaction(ctx context.Context, id int64) (model.Transaction, error) {
	row := s.db.QueryRowContext(ctx, txnSelect+` WHERE t.id = ?`, id)
	t, err := scanTxn(row)
	if err == sql.ErrNoRows {
		return t, fmt.Errorf("transaction not found")
	}
	if err != nil {
		return t, err
	}
	if err := s.attachTags(ctx, []model.Transaction{t}, func(i int, tags []model.Tag) {
		// no-op placeholder
	}); err != nil {
		return t, err
	}
	// re-fetch tags into t
	tags, err := s.tagsForTxn(ctx, id)
	if err != nil {
		return t, err
	}
	t.Tags = tags
	return t, nil
}

func (s *Store) tagsForTxn(ctx context.Context, txnID int64) ([]model.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT tg.id, tg.name, tg.color, tg.created_at
FROM transaction_tags tt
JOIN tags tg ON tg.id = tt.tag_id
WHERE tt.transaction_id = ?
ORDER BY tg.id`, txnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Tag
	for rows.Next() {
		var t model.Tag
		var created string
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &created); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, t)
	}
	if out == nil {
		out = []model.Tag{}
	}
	return out, rows.Err()
}

// attachTags loads tags for a slice of transactions in one query.
func (s *Store) attachTags(ctx context.Context, txns []model.Transaction, _ func(int, []model.Tag)) error {
	if len(txns) == 0 {
		return nil
	}
	ids := make([]any, len(txns))
	index := map[int64]int{}
	for i, t := range txns {
		ids[i] = t.ID
		index[t.ID] = i
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	q := fmt.Sprintf(`
SELECT tt.transaction_id, tg.id, tg.name, tg.color, tg.created_at
FROM transaction_tags tt
JOIN tags tg ON tg.id = tt.tag_id
WHERE tt.transaction_id IN (%s)
ORDER BY tg.id`, placeholders)
	rows, err := s.db.QueryContext(ctx, q, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()

	buckets := map[int64][]model.Tag{}
	for rows.Next() {
		var txnID int64
		var t model.Tag
		var created string
		if err := rows.Scan(&txnID, &t.ID, &t.Name, &t.Color, &created); err != nil {
			return err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		buckets[txnID] = append(buckets[txnID], t)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range txns {
		if tags, ok := buckets[txns[i].ID]; ok {
			txns[i].Tags = tags
		} else {
			txns[i].Tags = []model.Tag{}
		}
	}
	return nil
}

// EnrichTransactions fills tags for the given slice (mutates).
func (s *Store) EnrichTransactions(ctx context.Context, txns []model.Transaction) ([]model.Transaction, error) {
	if len(txns) == 0 {
		return txns, nil
	}
	// copy to avoid confusion - we mutate in place via indices
	if err := s.attachTags(ctx, txns, nil); err != nil {
		return nil, err
	}
	return txns, nil
}

// CountUnlabeled returns expense transactions without a person.
func (s *Store) CountUnlabeled(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM transactions
WHERE direction = 'out' AND person_id IS NULL`).Scan(&n)
	return n, err
}

// ListTransactionsFiltered supports search + unlabeled + person + date range.
type TxnFilter struct {
	Q         string
	Unlabeled bool
	PersonID  *int64
	// From / ToExclusive are optional UTC bounds on occurred_at.
	From        *time.Time
	ToExclusive *time.Time
	Limit       int
	Offset      int
}

func (s *Store) ListTransactionsFiltered(ctx context.Context, f TxnFilter) ([]model.Transaction, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 500 {
		f.Limit = 500
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	var where []string
	var args []any
	if q := strings.TrimSpace(f.Q); q != "" {
		like := "%" + q + "%"
		where = append(where, `(t.merchant LIKE ? OR t.note LIKE ? OR t.bank LIKE ? OR t.category LIKE ? OR p.name LIKE ?)`)
		args = append(args, like, like, like, like, like)
	}
	if f.Unlabeled {
		where = append(where, `t.person_id IS NULL`)
	}
	if f.PersonID != nil {
		where = append(where, `t.person_id = ?`)
		args = append(args, *f.PersonID)
	}
	if f.From != nil {
		where = append(where, `t.occurred_at >= ?`)
		args = append(args, f.From.UTC().Format(time.RFC3339Nano))
	}
	if f.ToExclusive != nil {
		where = append(where, `t.occurred_at < ?`)
		args = append(args, f.ToExclusive.UTC().Format(time.RFC3339Nano))
	}
	clause := ""
	if len(where) > 0 {
		clause = "WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, f.Limit, f.Offset)

	q := txnSelect + " " + clause + `
ORDER BY t.occurred_at DESC, t.id DESC
LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, q, args...)
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

// BulkLabelFilter describes which transactions to label in bulk.
type BulkLabelFilter struct {
	// IDs takes precedence when non-empty.
	IDs []int64
	// Merchant exact match (from SMS parsed merchant field).
	Merchant string
	// Category exact match.
	Category string
	// Direction optional: out|in|""
	Direction string
	// OnlyUnlabeled only rows with person_id IS NULL.
	OnlyUnlabeled bool
	// OnlyWithoutTag only rows that do not yet have TagID (when adding a tag).
	// Not used for person bulk.
}

// MerchantBucket is unlabeled volume grouped by merchant for bulk UI.
type MerchantBucket struct {
	Merchant string  `json:"merchant"`
	Expense  float64 `json:"expense"`
	Income   float64 `json:"income"`
	TxnCount int     `json:"txn_count"`
}

// BulkUpdatePerson sets person_id for all rows matching filter. Returns affected count.
// CountTransactionsFiltered counts rows matching filter (ignores limit/offset).
func (s *Store) CountTransactionsFiltered(ctx context.Context, f TxnFilter) (int, error) {
	var where []string
	var args []any
	if q := strings.TrimSpace(f.Q); q != "" {
		like := "%" + q + "%"
		where = append(where, `(t.merchant LIKE ? OR t.note LIKE ? OR t.bank LIKE ? OR t.category LIKE ? OR p.name LIKE ?)`)
		args = append(args, like, like, like, like, like)
	}
	if f.Unlabeled {
		where = append(where, `t.person_id IS NULL`)
	}
	if f.PersonID != nil {
		where = append(where, `t.person_id = ?`)
		args = append(args, *f.PersonID)
	}
	if f.From != nil {
		where = append(where, `t.occurred_at >= ?`)
		args = append(args, f.From.UTC().Format(time.RFC3339Nano))
	}
	if f.ToExclusive != nil {
		where = append(where, `t.occurred_at < ?`)
		args = append(args, f.ToExclusive.UTC().Format(time.RFC3339Nano))
	}
	clause := ""
	if len(where) > 0 {
		clause = "WHERE " + strings.Join(where, " AND ")
	}
	q := `SELECT COUNT(*) FROM transactions t LEFT JOIN people p ON p.id = t.person_id ` + clause
	var n int
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}


func (s *Store) BulkUpdatePerson(ctx context.Context, personID *int64, f BulkLabelFilter) (int64, error) {
	if personID != nil {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM people WHERE id = ?`, *personID).Scan(&n); err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, fmt.Errorf("person not found")
		}
	}

	where, args, err := buildBulkWhere(f)
	if err != nil {
		return 0, err
	}
	var res sql.Result
	if personID != nil {
		q := `UPDATE transactions SET person_id = ? WHERE ` + where
		all := append([]any{*personID}, args...)
		res, err = s.db.ExecContext(ctx, q, all...)
	} else {
		q := `UPDATE transactions SET person_id = NULL WHERE ` + where
		res, err = s.db.ExecContext(ctx, q, args...)
	}
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// BulkAddTag adds a tag to all matching transactions (does not remove other tags).
func (s *Store) BulkAddTag(ctx context.Context, tagID int64, f BulkLabelFilter) (int64, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags WHERE id = ?`, tagID).Scan(&n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, fmt.Errorf("tag not found")
	}
	where, args, err := buildBulkWhere(f)
	if err != nil {
		return 0, err
	}
	// Insert for matching txn ids
	q := `
INSERT OR IGNORE INTO transaction_tags (transaction_id, tag_id)
SELECT id, ? FROM transactions WHERE ` + where
	all := append([]any{tagID}, args...)
	res, err := s.db.ExecContext(ctx, q, all...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func buildBulkWhere(f BulkLabelFilter) (string, []any, error) {
	var parts []string
	var args []any
	if len(f.IDs) > 0 {
		ph := strings.TrimRight(strings.Repeat("?,", len(f.IDs)), ",")
		parts = append(parts, `id IN (`+ph+`)`)
		for _, id := range f.IDs {
			args = append(args, id)
		}
	} else {
		// require at least one selector so we never update whole table by accident
		if strings.TrimSpace(f.Merchant) == "" && strings.TrimSpace(f.Category) == "" {
			return "", nil, fmt.Errorf("bulk label requires ids, merchant, or category")
		}
		if m := strings.TrimSpace(f.Merchant); m != "" {
			parts = append(parts, `merchant = ?`)
			args = append(args, m)
		}
		if c := strings.TrimSpace(f.Category); c != "" {
			parts = append(parts, `category = ?`)
			args = append(args, c)
		}
	}
	if f.Direction == "out" || f.Direction == "in" {
		parts = append(parts, `direction = ?`)
		args = append(args, f.Direction)
	}
	if f.OnlyUnlabeled {
		parts = append(parts, `person_id IS NULL`)
	}
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("empty bulk filter")
	}
	return strings.Join(parts, " AND "), args, nil
}

// UnlabeledByMerchant returns top merchants among unlabeled expense rows.
func (s *Store) UnlabeledByMerchant(ctx context.Context, limit int) ([]MerchantBucket, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(NULLIF(merchant, ''), '未知') AS merchant,
       SUM(CASE WHEN direction = 'out' THEN amount ELSE 0 END) AS expense,
       SUM(CASE WHEN direction = 'in' THEN amount ELSE 0 END) AS income,
       COUNT(*) AS cnt
FROM transactions
WHERE person_id IS NULL
GROUP BY merchant
ORDER BY cnt DESC, expense DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MerchantBucket
	for rows.Next() {
		var b MerchantBucket
		if err := rows.Scan(&b.Merchant, &b.Expense, &b.Income, &b.TxnCount); err != nil {
			return nil, err
		}
		b.Expense = round2(b.Expense)
		b.Income = round2(b.Income)
		out = append(out, b)
	}
	if out == nil {
		out = []MerchantBucket{}
	}
	return out, rows.Err()
}

// PersonStats aggregates by person in [from, toExclusive).
// kind: all|consume|transfer|... empty/all means no kind filter.
func (s *Store) PersonStats(ctx context.Context, from, toExclusive time.Time, totalExpense float64, kind string) ([]model.PersonStat, error) {
	q := `
SELECT t.person_id, COALESCE(p.name, '未标记') AS person_name, COALESCE(p.color, '#8a897c') AS color,
       SUM(CASE WHEN t.direction = 'out' THEN t.amount ELSE 0 END) AS expense,
       SUM(CASE WHEN t.direction = 'in' THEN t.amount ELSE 0 END) AS income,
       COUNT(*) AS cnt
FROM transactions t
LEFT JOIN people p ON p.id = t.person_id
WHERE t.occurred_at >= ? AND t.occurred_at < ?`
	args := []any{from.UTC().Format(time.RFC3339Nano), toExclusive.UTC().Format(time.RFC3339Nano)}
	if kind != "" && kind != "all" {
		q += ` AND t.kind = ?`
		args = append(args, kind)
	}
	q += `
GROUP BY t.person_id, p.name, p.color
ORDER BY expense DESC, income DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.PersonStat
	for rows.Next() {
		var st model.PersonStat
		var pid sql.NullInt64
		if err := rows.Scan(&pid, &st.PersonName, &st.Color, &st.Expense, &st.Income, &st.TxnCount); err != nil {
			return nil, err
		}
		if pid.Valid {
			id := pid.Int64
			st.PersonID = &id
		}
		st.Expense = round2(st.Expense)
		st.Income = round2(st.Income)
		if totalExpense > 0 {
			st.Pct = round2(st.Expense / totalExpense * 100)
		}
		out = append(out, st)
	}
	if out == nil {
		out = []model.PersonStat{}
	}
	return out, rows.Err()
}
