package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cashpulse/internal/model"
)

func TestRangeTotalsRefundNetsExpenseNotIncome(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	loc := time.FixedZone("CST", 8*3600)
	day := time.Date(2026, 7, 10, 12, 0, 0, 0, loc)

	// 消费 100 out + 退款 30 in(refund) + 工资 200 in
	mustInsertTxn(t, st, day, 100, model.DirectionOut, model.KindConsume, "咖啡")
	mustInsertTxn(t, st, day.Add(time.Hour), 30, model.DirectionIn, model.KindRefund, "退货")
	mustInsertTxn(t, st, day.Add(2*time.Hour), 200, model.DirectionIn, model.KindIncome, "工资")

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, loc)
	toEx := time.Date(2026, 8, 1, 0, 0, 0, 0, loc)
	tot, err := st.rangeTotals(ctx, from, toEx, loc, "all")
	if err != nil {
		t.Fatal(err)
	}
	if tot.ExpenseGross != 100 {
		t.Fatalf("gross out want 100 got %v", tot.ExpenseGross)
	}
	if tot.Refund != 30 {
		t.Fatalf("refund want 30 got %v", tot.Refund)
	}
	if tot.Expense != 70 {
		t.Fatalf("net expense want 70 got %v", tot.Expense)
	}
	if tot.Income != 200 {
		t.Fatalf("income (ex-refund) want 200 got %v", tot.Income)
	}
	if tot.Net != 130 { // 200 - 70
		t.Fatalf("net want 130 got %v", tot.Net)
	}
}

func mustInsertTxn(t *testing.T, st *Store, at time.Time, amount float64, dir model.Direction, kind model.Kind, merchant string) {
	t.Helper()
	ctx := context.Background()
	now := at.UTC().Format(time.RFC3339Nano)
	res, err := st.db.ExecContext(ctx, `
INSERT INTO raw_sms (text, source, status, error, created_at, fingerprint)
VALUES (?, 'test', 'ok', '', ?, ?)`,
		"test "+merchant+" "+now, now, "fp-"+now+"-"+merchant)
	if err != nil {
		t.Fatal(err)
	}
	rawID, _ := res.LastInsertId()
	_, err = st.db.ExecContext(ctx, `
INSERT INTO transactions (
  raw_sms_id, amount, currency, direction, merchant, merchant_norm, card_last4,
  occurred_at, category, kind, note, bank, balance_after, balance_known, created_at
) VALUES (?, ?, 'CNY', ?, ?, ?, '', ?, '', ?, '', 'PSBC', 0, 0, ?)`,
		rawID, amount, string(dir), merchant, merchant, now, string(kind), now)
	if err != nil {
		t.Fatal(err)
	}
}
