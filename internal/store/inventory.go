package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) StockIn(ctx context.Context, itemID int64, quantity int, note string, expirationDate *string) (Item, error) {
	if quantity <= 0 {
		return Item{}, ErrInvalidInput
	}
	if expirationDate != nil && *expirationDate == "" {
		expirationDate = nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin stock-in transaction: %w", err)
	}
	defer tx.Rollback()

	item, err := scanItem(tx.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items WHERE id = ?`, itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("load item: %w", err)
	}

	newQuantity := item.Quantity + quantity
	timestamp := now()

	if _, err := tx.ExecContext(ctx, `
		UPDATE items SET quantity = ?, updated_at = ? WHERE id = ?`,
		newQuantity, timestamp, itemID,
	); err != nil {
		return Item{}, fmt.Errorf("update item quantity: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO batches (item_id, quantity, expiration_date, created_at)
		VALUES (?, ?, ?, ?)`,
		itemID, quantity, expirationDate, timestamp,
	); err != nil {
		return Item{}, fmt.Errorf("create batch: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO movements (item_id, type, change, quantity_after, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		itemID, "stock_in", quantity, newQuantity, note, timestamp,
	); err != nil {
		return Item{}, fmt.Errorf("create movement: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit stock-in: %w", err)
	}

	item.Quantity = newQuantity
	item.UpdatedAt = timestamp
	return item, nil
}

func (s *Store) StockOut(ctx context.Context, itemID int64, quantity int, note string) (Item, error) {
	if quantity <= 0 {
		return Item{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin stock-out transaction: %w", err)
	}
	defer tx.Rollback()

	item, err := scanItem(tx.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items WHERE id = ?`, itemID))
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
	timestamp := now()

	if err := consumeFromBatches(tx, itemID, quantity); err != nil {
		return Item{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE items SET quantity = ?, updated_at = ? WHERE id = ?`,
		newQuantity, timestamp, itemID,
	); err != nil {
		return Item{}, fmt.Errorf("update item quantity: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO movements (item_id, type, change, quantity_after, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		itemID, "stock_out", -quantity, newQuantity, note, timestamp,
	); err != nil {
		return Item{}, fmt.Errorf("create movement: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit stock-out: %w", err)
	}

	item.Quantity = newQuantity
	item.UpdatedAt = timestamp
	return item, nil
}

func (s *Store) Adjust(ctx context.Context, itemID int64, quantity int, note string) (Item, error) {
	if quantity < 0 {
		return Item{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin adjust transaction: %w", err)
	}
	defer tx.Rollback()

	item, err := scanItem(tx.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items WHERE id = ?`, itemID))
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

	timestamp := now()

	if _, err := tx.ExecContext(ctx, `DELETE FROM batches WHERE item_id = ?`, itemID); err != nil {
		return Item{}, fmt.Errorf("delete batches: %w", err)
	}
	if quantity > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO batches (item_id, quantity, expiration_date, created_at)
			VALUES (?, ?, NULL, ?)`,
			itemID, quantity, timestamp,
		); err != nil {
			return Item{}, fmt.Errorf("create batch for adjust: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE items SET quantity = ?, updated_at = ? WHERE id = ?`,
		quantity, timestamp, itemID,
	); err != nil {
		return Item{}, fmt.Errorf("update item quantity: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO movements (item_id, type, change, quantity_after, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		itemID, "adjust", change, quantity, note, timestamp,
	); err != nil {
		return Item{}, fmt.Errorf("create movement: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit adjust: %w", err)
	}

	item.Quantity = quantity
	item.UpdatedAt = timestamp
	return item, nil
}

func consumeFromBatches(tx *sql.Tx, itemID int64, quantity int) error {
	rows, err := tx.Query(`
		SELECT id, quantity FROM batches
		WHERE item_id = ?
		ORDER BY expiration_date ASC, created_at ASC`, itemID)
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
			if _, err := tx.Exec(`DELETE FROM batches WHERE id = ?`, batchID); err != nil {
				return fmt.Errorf("delete empty batch: %w", err)
			}
		} else {
			if _, err := tx.Exec(`UPDATE batches SET quantity = ? WHERE id = ?`, newQty, batchID); err != nil {
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

func (s *Store) ListBatches(ctx context.Context, itemID int64) ([]Batch, error) {
	if _, err := s.GetItem(ctx, itemID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, item_id, quantity, expiration_date, created_at
		FROM batches WHERE item_id = ? ORDER BY expiration_date ASC, created_at ASC`, itemID)
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
		var movement Movement
		if err := rows.Scan(
			&movement.ID,
			&movement.ItemID,
			&movement.Type,
			&movement.Change,
			&movement.QuantityAfter,
			&movement.Note,
			&movement.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan movement: %w", err)
		}
		movements = append(movements, movement)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate movements: %w", err)
	}
	return movements, nil
}
