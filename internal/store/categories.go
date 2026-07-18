package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) CreateCategory(ctx context.Context, name string) (Category, error) {
	if name == "" {
		return Category{}, ErrInvalidInput
	}
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO categories (name) VALUES (?)`, name,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return Category{}, ErrDuplicate
		}
		return Category{}, fmt.Errorf("create category: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Category{}, fmt.Errorf("get category id: %w", err)
	}
	return s.GetCategory(ctx, id)
}

func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM categories ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()
	categories := make([]Category, 0)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		categories = append(categories, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}
	return categories, nil
}

func (s *Store) GetCategory(ctx context.Context, id int64) (Category, error) {
	c, err := scanCategory(s.db.QueryRowContext(ctx,
		`SELECT id, name FROM categories WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Category{}, ErrNotFound
	}
	if err != nil {
		return Category{}, fmt.Errorf("get category: %w", err)
	}
	return c, nil
}

func (s *Store) UpdateCategory(ctx context.Context, id int64, name string) (Category, error) {
	if name == "" {
		return Category{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Category{}, fmt.Errorf("begin update category tx: %w", err)
	}
	defer tx.Rollback()

	// Conditionally update to detect concurrent modification
	result, err := tx.ExecContext(ctx, `UPDATE categories SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		if isUniqueConstraint(err) {
			return Category{}, ErrDuplicate
		}
		return Category{}, fmt.Errorf("update category: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Category{}, fmt.Errorf("check category update: %w", err)
	}
	if rows == 0 {
		return Category{}, ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return Category{}, fmt.Errorf("commit update category: %w", err)
	}

	return s.GetCategory(ctx, id)
}

func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	// Use transaction to be safe; FK RESTRICT is the real guard
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete category tx: %w", err)
	}
	defer tx.Rollback()

	// Check if item exists
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM categories WHERE id = ?`, id).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check category exists: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			return ErrInUse
		}
		return fmt.Errorf("delete category: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete category: %w", err)
	}
	return nil
}
