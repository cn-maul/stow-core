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
		Name: "Rice", Category: "Food", Location: "Kitchen",
	})
	if err != nil {
		t.Fatalf("CreateItem() error = %v", err)
	}
	if item.Quantity != 0 {
		t.Fatalf("initial quantity = %d, want 0", item.Quantity)
	}

	item, err = s.StockIn(ctx, item.ID, 10, "initial stock", nil)
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
	item, err := s.CreateItem(ctx, ItemInput{Name: "Milk"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StockIn(ctx, item.ID, 2, "", nil); err != nil {
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
	item, err := s.CreateItem(ctx, ItemInput{Name: "Soap"})
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
	item, err := s.CreateItem(ctx, ItemInput{Name: "Flour"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StockIn(ctx, item.ID, 1, "", nil); err != nil {
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

func TestStockInCreatesBatch(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	item, err := s.CreateItem(ctx, ItemInput{Name: "Coke"})
	if err != nil {
		t.Fatal(err)
	}
	exp := "2027-06-01"
	if _, err := s.StockIn(ctx, item.ID, 12, "new batch", &exp); err != nil {
		t.Fatal(err)
	}
	batches, err := s.ListBatches(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 {
		t.Fatalf("batch count = %d, want 1", len(batches))
	}
	if batches[0].Quantity != 12 {
		t.Fatalf("batch quantity = %d, want 12", batches[0].Quantity)
	}
	if batches[0].ExpirationDate == nil || *batches[0].ExpirationDate != "2027-06-01" {
		t.Fatalf("batch expiration = %v, want 2027-06-01", batches[0].ExpirationDate)
	}
}

func TestStockOutConsumesFIFOByExpiration(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	item, err := s.CreateItem(ctx, ItemInput{Name: "Juice"})
	if err != nil {
		t.Fatal(err)
	}
	expFar := "2027-12-01"
	expNear := "2027-01-01"

	if _, err := s.StockIn(ctx, item.ID, 5, "", &expFar); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StockIn(ctx, item.ID, 3, "", &expNear); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Quantity != 8 {
		t.Fatalf("total quantity = %d, want 8", got.Quantity)
	}

	if _, err := s.StockOut(ctx, item.ID, 4, ""); err != nil {
		t.Fatal(err)
	}

	batches, err := s.ListBatches(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 {
		t.Fatalf("batch count = %d, want 1 (near batch consumed and deleted)", len(batches))
	}
	if batches[0].Quantity != 4 {
		t.Fatalf("remaining batch quantity = %d, want 4", batches[0].Quantity)
	}
}

func TestStockInWithoutExpiration(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	item, err := s.CreateItem(ctx, ItemInput{Name: "Water"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StockIn(ctx, item.ID, 24, "", nil); err != nil {
		t.Fatal(err)
	}
	batches, err := s.ListBatches(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 {
		t.Fatalf("batch count = %d, want 1", len(batches))
	}
	if batches[0].ExpirationDate != nil {
		t.Fatalf("expiration_date = %v, want nil", batches[0].ExpirationDate)
	}
}

func TestCategoryRenameCascadesToItems(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	c1, err := s.CreateCategory(ctx, "OldCat")
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.CreateItem(ctx, ItemInput{Name: "Rice", Category: "OldCat"})
	if err != nil {
		t.Fatal(err)
	}

	c1, err = s.UpdateCategory(ctx, c1.ID, "NewCat")
	if err != nil {
		t.Fatal(err)
	}
	if c1.Name != "NewCat" {
		t.Fatalf("category name = %s, want NewCat", c1.Name)
	}

	item, err = s.GetItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Category != "NewCat" {
		t.Fatalf("item category = %s, want NewCat", item.Category)
	}

	// Delete NewCat should fail because item references it
	if err := s.DeleteCategory(ctx, c1.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("DeleteCategory() error = %v, want ErrInUse", err)
	}
}

func TestLocationRenameCascadesToItems(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	l1, err := s.CreateLocation(ctx, "OldLoc")
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.CreateItem(ctx, ItemInput{Name: "Salt", Location: "OldLoc"})
	if err != nil {
		t.Fatal(err)
	}

	l1, err = s.UpdateLocation(ctx, l1.ID, "NewLoc")
	if err != nil {
		t.Fatal(err)
	}
	if l1.Name != "NewLoc" {
		t.Fatalf("location name = %s, want NewLoc", l1.Name)
	}

	item, err = s.GetItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Location != "NewLoc" {
		t.Fatalf("item location = %s, want NewLoc", item.Location)
	}

	if err := s.DeleteLocation(ctx, l1.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("DeleteLocation() error = %v, want ErrInUse", err)
	}
}
