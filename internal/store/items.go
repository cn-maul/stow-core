package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) CreateItem(ctx context.Context, input ItemInput) (Item, error) {
	if err := validateItemInput(input); err != nil {
		return Item{}, err
	}
	timestamp := now()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO items (name, category, unit, location, quantity, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)`,
		input.Name, input.Category, input.Unit, input.Location, timestamp, timestamp,
	)
	if err != nil {
		return Item{}, fmt.Errorf("create item: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Item{}, fmt.Errorf("get item id: %w", err)
	}
	return s.GetItem(ctx, id)
}

func (s *Store) ListItems(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColumns+` FROM items ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()

	items := make([]Item, 0)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate items: %w", err)
	}
	return items, nil
}

func (s *Store) GetItem(ctx context.Context, id int64) (Item, error) {
	item, err := scanItem(s.db.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("get item: %w", err)
	}
	return item, nil
}

func (s *Store) UpdateItem(ctx context.Context, id int64, input ItemInput) (Item, error) {
	if err := validateItemInput(input); err != nil {
		return Item{}, err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE items
		SET name = ?, category = ?, unit = ?, location = ?, updated_at = ?
		WHERE id = ?`,
		input.Name, input.Category, input.Unit, input.Location, now(), id,
	)
	if err != nil {
		return Item{}, fmt.Errorf("update item: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Item{}, fmt.Errorf("check updated item: %w", err)
	}
	if rows == 0 {
		return Item{}, ErrNotFound
	}
	return s.GetItem(ctx, id)
}

func (s *Store) DeleteItem(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete: %w", err)
	}
	defer tx.Rollback()

	var quantity int
	if err := tx.QueryRowContext(ctx, `SELECT quantity FROM items WHERE id = ?`, id).Scan(&quantity); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("load item for delete: %w", err)
	}
	if quantity != 0 {
		return ErrItemHasStock
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}
	return nil
}
