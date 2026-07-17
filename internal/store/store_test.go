package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return s
}

func TestInventoryLifecycle(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	item, err := s.CreateItem(ctx, ItemInput{
		Name: "Rice", Category: "Food", Unit: "bag", Location: "Kitchen",
	})
	if err != nil {
		t.Fatalf("CreateItem() error = %v", err)
	}
	if item.Quantity != 0 {
		t.Fatalf("initial quantity = %d, want 0", item.Quantity)
	}

	item, err = s.StockIn(ctx, item.ID, 10, "initial stock")
	if err != nil {
		t.Fatalf("StockIn() error = %v", err)
	}
	if item.Quantity != 10 {
		t.Fatalf("quantity after stock in = %d, want 10", item.Quantity)
	}

	item, err = s.StockOut(ctx, item.ID, 3, "used")
	if err != nil {
		t.Fatalf("StockOut() error = %v", err)
	}
	if item.Quantity != 7 {
		t.Fatalf("quantity after stock out = %d, want 7", item.Quantity)
	}

	item, err = s.Adjust(ctx, item.ID, 5, "counted")
	if err != nil {
		t.Fatalf("Adjust() error = %v", err)
	}
	if item.Quantity != 5 {
		t.Fatalf("quantity after adjust = %d, want 5", item.Quantity)
	}

	movements, err := s.ListMovements(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListMovements() error = %v", err)
	}
	if len(movements) != 3 {
		t.Fatalf("movement count = %d, want 3", len(movements))
	}
	if movements[0].Type != "adjust" || movements[0].Change != -2 || movements[0].QuantityAfter != 5 {
		t.Fatalf("latest movement = %+v, want adjust -2 -> 5", movements[0])
	}
}

func TestStockOutInsufficientLeavesInventoryUnchanged(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	item, err := s.CreateItem(ctx, ItemInput{Name: "Milk", Unit: "bottle"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StockIn(ctx, item.ID, 2, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := s.StockOut(ctx, item.ID, 3, "too much"); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("StockOut() error = %v, want ErrInsufficientStock", err)
	}
	item, err = s.GetItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Quantity != 2 {
		t.Fatalf("quantity = %d, want 2", item.Quantity)
	}
	movements, err := s.ListMovements(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(movements) != 1 {
		t.Fatalf("movement count = %d, want 1", len(movements))
	}
}

func TestAdjustNoChangeDoesNotCreateMovement(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	item, err := s.CreateItem(ctx, ItemInput{Name: "Soap", Unit: "bar"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Adjust(ctx, item.ID, 0, "same"); err != nil {
		t.Fatalf("Adjust() error = %v", err)
	}
	movements, err := s.ListMovements(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(movements) != 0 {
		t.Fatalf("movement count = %d, want 0", len(movements))
	}
}

func TestDeleteRequiresZeroStock(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	item, err := s.CreateItem(ctx, ItemInput{Name: "Flour", Unit: "bag"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StockIn(ctx, item.ID, 1, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteItem(ctx, item.ID); !errors.Is(err, ErrItemHasStock) {
		t.Fatalf("DeleteItem() error = %v, want ErrItemHasStock", err)
	}
	if _, err := s.StockOut(ctx, item.ID, 1, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteItem(ctx, item.ID); err != nil {
		t.Fatalf("DeleteItem() error = %v", err)
	}
	if _, err := s.GetItem(ctx, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetItem() error = %v, want ErrNotFound", err)
	}
}
