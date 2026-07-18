package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) CreateLocation(ctx context.Context, name string) (Location, error) {
	if name == "" {
		return Location{}, ErrInvalidInput
	}
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO locations (name) VALUES (?)`, name,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return Location{}, ErrDuplicate
		}
		return Location{}, fmt.Errorf("create location: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Location{}, fmt.Errorf("get location id: %w", err)
	}
	return s.GetLocation(ctx, id)
}

func (s *Store) ListLocations(ctx context.Context) ([]Location, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM locations ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}
	defer rows.Close()
	locations := make([]Location, 0)
	for rows.Next() {
		l, err := scanLocation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan location: %w", err)
		}
		locations = append(locations, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locations: %w", err)
	}
	return locations, nil
}

func (s *Store) GetLocation(ctx context.Context, id int64) (Location, error) {
	l, err := scanLocation(s.db.QueryRowContext(ctx,
		`SELECT id, name FROM locations WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Location{}, ErrNotFound
	}
	if err != nil {
		return Location{}, fmt.Errorf("get location: %w", err)
	}
	return l, nil
}

func (s *Store) UpdateLocation(ctx context.Context, id int64, name string) (Location, error) {
	if name == "" {
		return Location{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Location{}, fmt.Errorf("begin update location tx: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `UPDATE locations SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		if isUniqueConstraint(err) {
			return Location{}, ErrDuplicate
		}
		return Location{}, fmt.Errorf("update location: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Location{}, fmt.Errorf("check location update: %w", err)
	}
	if rows == 0 {
		return Location{}, ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return Location{}, fmt.Errorf("commit update location: %w", err)
	}

	return s.GetLocation(ctx, id)
}

func (s *Store) DeleteLocation(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete location tx: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM locations WHERE id = ?`, id).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check location exists: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM locations WHERE id = ?`, id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			return ErrInUse
		}
		return fmt.Errorf("delete location: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete location: %w", err)
	}
	return nil
}
