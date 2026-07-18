package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cashpulse/internal/parser"
	"cashpulse/internal/store"
)

func TestIngestSMSIdempotent(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	loc := time.FixedZone("CST", 8*3600)
	svc := New(st, parser.New(loc), loc)
	ctx := context.Background()
	text := "【邮储银行】26年07月18日10:00您尾号9653账户快捷支付-微信支付（财付通），支出金额100.50元，余额200.00元"
	r1, err := svc.IngestSMS(ctx, IngestSMSRequest{Text: text, Source: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Duplicate || r1.Transaction == nil {
		t.Fatalf("first: %+v", r1)
	}
	r2, err := svc.IngestSMS(ctx, IngestSMSRequest{Text: text, Source: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Duplicate {
		t.Fatalf("second not duplicate: %+v", r2)
	}
	if r2.Transaction == nil || r2.Transaction.ID != r1.Transaction.ID {
		t.Fatalf("txn mismatch: %v vs %v", r1.Transaction, r2.Transaction)
	}
	// only one txn in db
	n, err := st.CountTransactions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 txn, got %d", n)
	}
}
