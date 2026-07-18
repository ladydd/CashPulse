package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cashpulse/internal/model"
)

func TestSMSIdempotentInsert(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	text := "【邮储银行】26年07月18日10:00您尾号9653账户快捷支付-微信支付（财付通），支出金额1.00元，余额100.00元"
	a, err := s.InsertRawSMS(ctx, text, "test")
	if err != nil {
		t.Fatal(err)
	}
	if a.Duplicate || a.ID == 0 {
		t.Fatalf("first insert: %+v", a)
	}
	b, err := s.InsertRawSMS(ctx, text, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !b.Duplicate || b.ID != a.ID {
		t.Fatalf("second should duplicate first: first=%+v second=%+v", a, b)
	}
	// different text ok
	c, err := s.InsertRawSMS(ctx, text+" ", "test")
	if err != nil {
		t.Fatal(err)
	}
	// trim makes same fingerprint
	if !c.Duplicate {
		t.Fatalf("whitespace-normalized should duplicate: %+v", c)
	}
	_ = model.ParseStatusOK
	_ = time.Now()
}
