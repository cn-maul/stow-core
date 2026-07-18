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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin create item tx: %w", err)
	}
	defer tx.Rollback()

	catID, locID, err := s.resolveCategoryLocation(ctx, tx, input)
	if err != nil {
		return Item{}, err
	}

	timestamp := now()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO items (name, category_id, location_id, quantity, created_at, updated_at)
		VALUES (?, ?, ?, 0, ?, ?)`,
		input.Name, catID, locID, timestamp, timestamp,
	)
	if err != nil {
		return Item{}, fmt.Errorf("create item: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Item{}, fmt.Errorf("get item id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit create item: %w", err)
	}

	return s.GetItem(ctx, id)
}

func (s *Store) ListItems(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+itemColumns+`
		FROM items i
		LEFT JOIN categories c ON i.category_id = c.id
		LEFT JOIN locations l ON i.location_id = l.id
		ORDER BY i.id DESC`)
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
	item, err := scanItem(s.db.QueryRowContext(ctx, `
		SELECT `+itemColumns+`
		FROM items i
		LEFT JOIN categories c ON i.category_id = c.id
		LEFT JOIN locations l ON i.location_id = l.id
		WHERE i.id = ?`, id))
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin update item tx: %w", err)
	}
	defer tx.Rollback()

	catID, locID, err := s.resolveCategoryLocation(ctx, tx, input)
	if err != nil {
		return Item{}, err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE items
		SET name = ?, category_id = ?, location_id = ?, updated_at = ?
		WHERE id = ?`,
		input.Name, catID, locID, now(), id,
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

	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit update item: %w", err)
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

	var batchCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM batches WHERE item_id = ?`, id).Scan(&batchCount); err != nil {
		return fmt.Errorf("check batches: %w", err)
	}
	if batchCount > 0 {
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

// resolveCategoryLocation resolves category/location from either ID or name.
func (s *Store) resolveCategoryLocation(ctx context.Context, tx *sql.Tx, input ItemInput) (*int64, *int64, error) {
	var catID, locID *int64
	var err error

	// Resolve category
	if input.CategoryID != nil {
		var exists int
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM categories WHERE id = ?`, *input.CategoryID).Scan(&exists)
		if err != nil {
			return nil, nil, fmt.Errorf("lookup category: %w", err)
		}
		if exists == 0 {
			return nil, nil, ErrNotFound
		}
		catID = input.CategoryID
	} else if input.Category != "" {
		catID, err = resolveOrCreateName(ctx, tx, `categories`, input.Category)
		if err != nil {
			return nil, nil, err
		}
	}

	// Resolve location
	if input.LocationID != nil {
		var exists int
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM locations WHERE id = ?`, *input.LocationID).Scan(&exists)
		if err != nil {
			return nil, nil, fmt.Errorf("lookup location: %w", err)
		}
		if exists == 0 {
			return nil, nil, ErrNotFound
		}
		locID = input.LocationID
	} else if input.Location != "" {
		locID, err = resolveOrCreateName(ctx, tx, `locations`, input.Location)
		if err != nil {
			return nil, nil, err
		}
	}

	return catID, locID, nil
}

func resolveOrCreateName(ctx context.Context, tx *sql.Tx, table, name string) (*int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT id FROM %s WHERE name = ?`, table), name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		result, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (name) VALUES (?)`, table), name)
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", table, err)
		}
		id, err = result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("get %s id: %w", table, err)
		}
		return &id, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup %s: %w", table, err)
	}
	return &id, nil
}
