package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// StockIn adds a batch to the item's inventory.
func (s *Store) StockIn(ctx context.Context, itemID int64, quantity int, note string, expirationDate *string) (Item, error) {
	if quantity <= 0 {
		return Item{}, ErrInvalidInput
	}

	exp := expirationDate
	if exp != nil && *exp == "" {
		exp = nil
	}
	if exp != nil {
		_, err := time.Parse("2006-01-02", *exp)
		if err != nil {
			return Item{}, fmt.Errorf("%w: invalid expiration date format (expected YYYY-MM-DD)", ErrInvalidInput)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin stock-in tx: %w", err)
	}
	defer tx.Rollback()

	item, err := scanItemTx(tx.QueryRowContext(ctx, `SELECT `+itemColumnsShort+`
		FROM items i
		LEFT JOIN categories c ON i.category_id = c.id
		LEFT JOIN locations l ON i.location_id = l.id
		WHERE i.id = ?`, itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("load item: %w", err)
	}

	newQuantity := item.Quantity + quantity
	ts := now()

	_, err = tx.ExecContext(ctx, `
		UPDATE items SET quantity = ?, updated_at = ? WHERE id = ?`,
		newQuantity, ts, itemID)
	if err != nil {
		return Item{}, fmt.Errorf("update item quantity: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO batches (item_id, quantity, expiration_date, created_at)
		VALUES (?, ?, ?, ?)`,
		itemID, quantity, exp, ts)
	if err != nil {
		return Item{}, fmt.Errorf("create batch: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO movements (item_id, type, change, quantity_after, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		itemID, "stock_in", quantity, newQuantity, note, ts)
	if err != nil {
		return Item{}, fmt.Errorf("create movement: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit stock-in: %w", err)
	}

	item.Quantity = newQuantity
	item.UpdatedAt = ts
	return item, nil
}

// StockOut removes quantity from the item's inventory using FEFO (first-expired-first-out).
func (s *Store) StockOut(ctx context.Context, itemID int64, quantity int, note string) (Item, error) {
	if quantity <= 0 {
		return Item{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin stock-out tx: %w", err)
	}
	defer tx.Rollback()

	item, err := scanItemTx(tx.QueryRowContext(ctx, `SELECT `+itemColumnsShort+`
		FROM items i
		LEFT JOIN categories c ON i.category_id = c.id
		LEFT JOIN locations l ON i.location_id = l.id
		WHERE i.id = ?`, itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("load item: %w", err)
	}

	if item.Quantity < quantity {
		return Item{}, ErrInsufficientStock
	}

	newQuantity := item.Quantity - quantity
	ts := now()

	if err := consumeFromBatches(tx, itemID, quantity); err != nil {
		return Item{}, err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE items SET quantity = ?, updated_at = ? WHERE id = ?`,
		newQuantity, ts, itemID)
	if err != nil {
		return Item{}, fmt.Errorf("update item quantity: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO movements (item_id, type, change, quantity_after, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		itemID, "stock_out", -quantity, newQuantity, note, ts)
	if err != nil {
		return Item{}, fmt.Errorf("create movement: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit stock-out: %w", err)
	}

	item.Quantity = newQuantity
	item.UpdatedAt = ts
	return item, nil
}

// Adjust sets the item's quantity to the given value.
func (s *Store) Adjust(ctx context.Context, itemID int64, quantity int, note string) (Item, error) {
	if quantity < 0 {
		return Item{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin adjust tx: %w", err)
	}
	defer tx.Rollback()

	item, err := scanItemTx(tx.QueryRowContext(ctx, `SELECT `+itemColumnsShort+`
		FROM items i
		LEFT JOIN categories c ON i.category_id = c.id
		LEFT JOIN locations l ON i.location_id = l.id
		WHERE i.id = ?`, itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("load item: %w", err)
	}

	change := quantity - item.Quantity
	if change == 0 {
		return item, nil
	}

	ts := now()

	_, err = tx.ExecContext(ctx, `DELETE FROM batches WHERE item_id = ?`, itemID)
	if err != nil {
		return Item{}, fmt.Errorf("delete batches: %w", err)
	}

	if quantity > 0 {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO batches (item_id, quantity, expiration_date, created_at)
			VALUES (?, ?, NULL, ?)`,
			itemID, quantity, ts)
		if err != nil {
			return Item{}, fmt.Errorf("create batch for adjust: %w", err)
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE items SET quantity = ?, updated_at = ? WHERE id = ?`,
		quantity, ts, itemID)
	if err != nil {
		return Item{}, fmt.Errorf("update item quantity: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO movements (item_id, type, change, quantity_after, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		itemID, "adjust", change, quantity, note, ts)
	if err != nil {
		return Item{}, fmt.Errorf("create movement: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit adjust: %w", err)
	}

	item.Quantity = quantity
	item.UpdatedAt = ts
	return item, nil
}

// consumeFromBatches removes quantity from batches using FEFO ordering.
// NULL expiration_date batches are consumed last.
func consumeFromBatches(tx *sql.Tx, itemID int64, quantity int) error {
	// Sort: batches with expiration_date ASC first, then NULL last, then by created_at, then id
	rows, err := tx.Query(`
		SELECT id, quantity FROM batches
		WHERE item_id = ?
		ORDER BY
			CASE WHEN expiration_date IS NULL THEN 1 ELSE 0 END,
			expiration_date ASC,
			created_at ASC,
			id ASC`, itemID)
	if err != nil {
		return fmt.Errorf("query batches: %w", err)
	}
	defer rows.Close()

	remaining := quantity
	for rows.Next() {
		if remaining <= 0 {
			break
		}
		var batchID, batchQty int
		if err := rows.Scan(&batchID, &batchQty); err != nil {
			return fmt.Errorf("scan batch: %w", err)
		}
		consume := remaining
		if consume > batchQty {
			consume = batchQty
		}
		remaining -= consume
		newQty := batchQty - consume
		if newQty == 0 {
			_, err := tx.Exec(`DELETE FROM batches WHERE id = ?`, batchID)
			if err != nil {
				return fmt.Errorf("delete empty batch: %w", err)
			}
		} else {
			_, err := tx.Exec(`UPDATE batches SET quantity = ? WHERE id = ?`, newQty, batchID)
			if err != nil {
				return fmt.Errorf("update batch quantity: %w", err)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate batches: %w", err)
	}
	if remaining > 0 {
		return ErrInsufficientStock
	}
	return nil
}

// ListBatches returns all batches for the given item.
func (s *Store) ListBatches(ctx context.Context, itemID int64) ([]Batch, error) {
	if _, err := s.GetItem(ctx, itemID); err != nil {
		return nil, err
	}
	// Use same FEFO ordering as consumption
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, item_id, quantity, expiration_date, created_at
		FROM batches
		WHERE item_id = ?
		ORDER BY
			CASE WHEN expiration_date IS NULL THEN 1 ELSE 0 END,
			expiration_date ASC,
			created_at ASC,
			id ASC`, itemID)
	if err != nil {
		return nil, fmt.Errorf("list batches: %w", err)
	}
	defer rows.Close()

	batches := make([]Batch, 0)
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, fmt.Errorf("scan batch: %w", err)
		}
		batches = append(batches, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate batches: %w", err)
	}
	return batches, nil
}

// ListMovements returns all movements for the given item, ordered by id DESC.
func (s *Store) ListMovements(ctx context.Context, itemID int64) ([]Movement, error) {
	if _, err := s.GetItem(ctx, itemID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, item_id, type, change, quantity_after, note, created_at
		FROM movements WHERE item_id = ? ORDER BY id DESC`, itemID)
	if err != nil {
		return nil, fmt.Errorf("list movements: %w", err)
	}
	defer rows.Close()

	movements := make([]Movement, 0)
	for rows.Next() {
		var m Movement
		if err := rows.Scan(
			&m.ID,
			&m.ItemID,
			&m.Type,
			&m.Change,
			&m.QuantityAfter,
			&m.Note,
			&m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan movement: %w", err)
		}
		movements = append(movements, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate movements: %w", err)
	}
	return movements, nil
}
