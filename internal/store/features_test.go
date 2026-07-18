package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestBudgetUpsertAndListWithSingleConnection(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "cashpulse.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	first, err := s.UpsertBudget(ctx, "2026-07", nil, "consume", 5000)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.UpsertBudget(ctx, "2026-07", nil, "consume", 6500)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("global budget was duplicated: first id %d, second id %d", first.ID, second.ID)
	}

	items, err := s.ListBudgets(ctx, "2026-07", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d budgets, want 1", len(items))
	}
	if items[0].Amount != 6500 {
		t.Fatalf("amount = %v, want 6500", items[0].Amount)
	}
}
