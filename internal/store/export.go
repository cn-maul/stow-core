package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ExportData struct {
	Version    string       `json:"version"`
	Categories []Category   `json:"categories"`
	Locations  []Location   `json:"locations"`
	Items      []ExportItem `json:"items"`
}

type ExportItem struct {
	Name      string       `json:"name"`
	Category  string       `json:"category"`
	Location  string       `json:"location"`
	Batches   []ExportBatch `json:"batches"`
}

type ExportBatch struct {
	Quantity       int     `json:"quantity"`
	ExpirationDate *string `json:"expiration_date"`
}

func (s *Store) Export(ctx context.Context) (*ExportData, error) {
	categories, err := s.ListCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("export categories: %w", err)
	}
	locations, err := s.ListLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("export locations: %w", err)
	}

	items, err := s.ListItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("export items: %w", err)
	}

	exportItems := make([]ExportItem, 0, len(items))
	for _, item := range items {
		batches, err := s.ListBatches(ctx, item.ID)
		if err != nil {
			return nil, fmt.Errorf("export batches for item %d: %w", item.ID, err)
		}
		exportBatches := make([]ExportBatch, len(batches))
		for i, b := range batches {
			exportBatches[i] = ExportBatch{
				Quantity:       b.Quantity,
				ExpirationDate: b.ExpirationDate,
			}
		}
		exportItems = append(exportItems, ExportItem{
			Name:     item.Name,
			Category: item.Category,
			Location: item.Location,
			Batches:  exportBatches,
		})
	}

	return &ExportData{
		Version:    "1.0.2",
		Categories: categories,
		Locations:  locations,
		Items:      exportItems,
	}, nil
}

func (s *Store) Import(ctx context.Context, data ExportData) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import transaction: %w", err)
	}
	defer tx.Rollback()

	// Get existing category and location IDs for fast lookup
	catNameToID := make(map[string]int64)
	locNameToID := make(map[string]int64)

	// Create or find categories
	for _, c := range data.Categories {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM categories WHERE name = ?`, c.Name).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			result, err := tx.ExecContext(ctx, `INSERT INTO categories (name) VALUES (?)`, c.Name)
			if err != nil {
				return fmt.Errorf("create category: %w", err)
			}
			id, _ = result.LastInsertId()
		} else if err != nil {
			return fmt.Errorf("lookup category: %w", err)
		}
		catNameToID[c.Name] = id
	}

	// Create or find locations
	for _, l := range data.Locations {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM locations WHERE name = ?`, l.Name).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			result, err := tx.ExecContext(ctx, `INSERT INTO locations (name) VALUES (?)`, l.Name)
			if err != nil {
				return fmt.Errorf("create location: %w", err)
			}
			id, _ = result.LastInsertId()
		} else if err != nil {
			return fmt.Errorf("lookup location: %w", err)
		}
		locNameToID[l.Name] = id
	}

	timestamp := time.Now().UTC().Format(time.RFC3339Nano)

	// Create items and their batches
	for _, item := range data.Items {
		totalQty := 0
		for _, b := range item.Batches {
			totalQty += b.Quantity
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO items (name, category, location, quantity, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			item.Name, item.Category, item.Location, totalQty, timestamp, timestamp,
		)
		if err != nil {
			return fmt.Errorf("create item %q: %w", item.Name, err)
		}
		itemID, _ := result.LastInsertId()

		for _, batch := range item.Batches {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO batches (item_id, quantity, expiration_date, created_at)
				VALUES (?, ?, ?, ?)`,
				itemID, batch.Quantity, batch.ExpirationDate, timestamp,
			); err != nil {
				return fmt.Errorf("create batch for item %d: %w", itemID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import: %w", err)
	}
	return nil
}
