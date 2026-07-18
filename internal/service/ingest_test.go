package service

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cashpulse/internal/model"
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
	if r1.Duplicate || r1.Transaction == nil || r1.Status != model.ParseStatusOK {
		t.Fatalf("first: %+v", r1)
	}
	r2, err := svc.IngestSMS(ctx, IngestSMSRequest{Text: text, Source: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Duplicate || r2.Transaction == nil || r2.Transaction.ID != r1.Transaction.ID {
		t.Fatalf("second: %+v", r2)
	}
	if r2.Status == model.ParseStatusOK && r2.Transaction == nil {
		t.Fatal("ok without transaction")
	}
	n, _ := st.CountTransactions(ctx)
	if n != 1 {
		t.Fatalf("want 1 txn got %d", n)
	}
}

func TestIngestSMSConcurrentSameBody(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	loc := time.FixedZone("CST", 8*3600)
	svc := New(st, parser.New(loc), loc)
	text := "【邮储银行】26年07月18日11:00您尾号9653账户快捷支付-微信支付（财付通），支出金额12.34元，余额500.00元"

	const N = 20
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	done.Add(N)
	results := make([]*IngestSMSResponse, N)
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer done.Done()
			start.Wait()
			results[i], errs[i] = svc.IngestSMS(context.Background(), IngestSMSRequest{Text: text, Source: "c"})
		}(i)
	}
	start.Done()
	done.Wait()

	var okID int64
	for i := 0; i < N; i++ {
		if errs[i] != nil {
			t.Fatalf("err %d: %v", i, errs[i])
		}
		r := results[i]
		if r.Status == model.ParseStatusOK && r.Transaction == nil {
			t.Fatalf("ok without txn at %d", i)
		}
		if r.Transaction != nil {
			if okID == 0 {
				okID = r.Transaction.ID
			} else if r.Transaction.ID != okID {
				t.Fatalf("multiple txn ids %d vs %d", okID, r.Transaction.ID)
			}
		}
	}
	n, _ := st.CountTransactions(context.Background())
	if n != 1 {
		t.Fatalf("want 1 txn, got %d", n)
	}
	if okID == 0 {
		t.Fatal("no successful transaction")
	}
}
