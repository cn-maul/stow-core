package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) StockIn(ctx context.Context, itemID int64, quantity int, note string) (Item, error) {
	if quantity <= 0 {
		return Item{}, ErrInvalidInput
	}
	return s.changeStock(ctx, itemID, "stock_in", quantity, note, nil)
}

func (s *Store) StockOut(ctx context.Context, itemID int64, quantity int, note string) (Item, error) {
	if quantity <= 0 {
		return Item{}, ErrInvalidInput
	}
	return s.changeStock(ctx, itemID, "stock_out", -quantity, note, nil)
}

func (s *Store) Adjust(ctx context.Context, itemID int64, quantity int, note string) (Item, error) {
	if quantity < 0 {
		return Item{}, ErrInvalidInput
	}
	return s.changeStock(ctx, itemID, "adjust", 0, note, &quantity)
}

func (s *Store) changeStock(
	ctx context.Context,
	itemID int64,
	movementType string,
	change int,
	note string,
	actualQuantity *int,
) (Item, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin inventory transaction: %w", err)
	}
	defer tx.Rollback()

	item, err := scanItem(tx.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items WHERE id = ?`, itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("load item for inventory change: %w", err)
	}

	newQuantity := item.Quantity + change
	if actualQuantity != nil {
		newQuantity = *actualQuantity
		change = newQuantity - item.Quantity
	}
	if newQuantity < 0 {
		return Item{}, ErrInsufficientStock
	}
	if change == 0 {
		return item, nil
	}

	timestamp := now()
	result, err := tx.ExecContext(ctx, `
		UPDATE items SET quantity = ?, updated_at = ?
		WHERE id = ? AND quantity = ?`,
		newQuantity, timestamp, itemID, item.Quantity,
	)
	if err != nil {
		return Item{}, fmt.Errorf("update inventory: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Item{}, fmt.Errorf("check inventory update: %w", err)
	}
	if rows != 1 {
		return Item{}, errors.New("inventory changed concurrently")
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO movements (item_id, type, change, quantity_after, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		itemID, movementType, change, newQuantity, note, timestamp,
	); err != nil {
		return Item{}, fmt.Errorf("create movement: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit inventory transaction: %w", err)
	}

	item.Quantity = newQuantity
	item.UpdatedAt = timestamp
	return item, nil
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
