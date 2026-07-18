package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const ExportSchemaVersion = 2

type ExportData struct {
	Version    int          `json:"version"`
	Categories []Category   `json:"categories"`
	Locations  []Location   `json:"locations"`
	Items      []ExportItem `json:"items"`
}

type ExportItem struct {
	Name       string        `json:"name"`
	CategoryID *int64        `json:"category_id"`
	LocationID *int64        `json:"location_id"`
	Batches    []ExportBatch `json:"batches"`
}

type ExportBatch struct {
	Quantity       int     `json:"quantity"`
	ExpirationDate *string `json:"expiration_date"`
}

func (s *Store) Export(ctx context.Context) (*ExportData, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin export tx: %w", err)
	}
	defer tx.Rollback()

	cats, err := listCategoriesTx(tx)
	if err != nil {
		return nil, fmt.Errorf("export categories: %w", err)
	}

	locs, err := listLocationsTx(tx)
	if err != nil {
		return nil, fmt.Errorf("export locations: %w", err)
	}

	itemsRows, err := tx.Query(`
		SELECT ` + itemColumnsShort + `
		FROM items i
		LEFT JOIN categories c ON i.category_id = c.id
		LEFT JOIN locations l ON i.location_id = l.id
		ORDER BY i.id`)
	if err != nil {
		return nil, fmt.Errorf("export items: %w", err)
	}
	defer itemsRows.Close()

	exportItems := make([]ExportItem, 0)
	for itemsRows.Next() {
		item, err := scanItem(itemsRows)
		if err != nil {
			return nil, fmt.Errorf("scan item for export: %w", err)
		}

		var batchSum int
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity), 0) FROM batches WHERE item_id = ?`, item.ID).Scan(&batchSum)
		if err != nil {
			return nil, fmt.Errorf("check batch sum for item %d: %w", item.ID, err)
		}
		if item.Quantity != batchSum {
			return nil, fmt.Errorf("%w: item %d quantity %d != batch sum %d",
				ErrExportError, item.ID, item.Quantity, batchSum)
		}

		batchRows, err := tx.Query(`
			SELECT id, item_id, quantity, expiration_date, created_at
			FROM batches WHERE item_id = ?
			ORDER BY
				CASE WHEN expiration_date IS NULL THEN 1 ELSE 0 END,
				expiration_date ASC,
				created_at ASC,
				id ASC`, item.ID)
		if err != nil {
			return nil, fmt.Errorf("export batches for item %d: %w", item.ID, err)
		}
		exportBatches := make([]ExportBatch, 0)
		for batchRows.Next() {
			b, err := scanBatch(batchRows)
			if err != nil {
				batchRows.Close()
				return nil, fmt.Errorf("scan batch: %w", err)
			}
			exportBatches = append(exportBatches, ExportBatch{
				Quantity:       b.Quantity,
				ExpirationDate: b.ExpirationDate,
			})
		}
		batchRows.Close()
		if err := batchRows.Err(); err != nil {
			return nil, fmt.Errorf("iterate batches for item %d: %w", item.ID, err)
		}

		exportItems = append(exportItems, ExportItem{
			Name:       item.Name,
			CategoryID: item.CategoryID,
			LocationID: item.LocationID,
			Batches:    exportBatches,
		})
	}
	if err := itemsRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate items for export: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit export: %w", err)
	}

	return &ExportData{
		Version:    ExportSchemaVersion,
		Categories: cats,
		Locations:  locs,
		Items:      exportItems,
	}, nil
}

func (s *Store) Import(ctx context.Context, data ExportData) error {
	if data.Version != ExportSchemaVersion {
		return fmt.Errorf("%w: unsupported export version %d (expected %d)",
			ErrInvalidInput, data.Version, ExportSchemaVersion)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import tx: %w", err)
	}
	defer tx.Rollback()

	// Read existing categories
	catNameToID := make(map[string]int64)
	rows, err := tx.Query(`SELECT id, name FROM categories`)
	if err != nil {
		return fmt.Errorf("import: read categories: %w", err)
	}
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			rows.Close()
			return fmt.Errorf("scan category: %w", err)
		}
		catNameToID[c.Name] = c.ID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate categories: %w", err)
	}

	// Read existing locations
	locNameToID := make(map[string]int64)
	rows, err = tx.Query(`SELECT id, name FROM locations`)
	if err != nil {
		return fmt.Errorf("import: read locations: %w", err)
	}
	for rows.Next() {
		l, err := scanLocation(rows)
		if err != nil {
			rows.Close()
			return fmt.Errorf("scan location: %w", err)
		}
		locNameToID[l.Name] = l.ID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate locations: %w", err)
	}

	// Map export category IDs to local IDs
	exportCatMap := make(map[int64]int64)
	for _, c := range data.Categories {
		if c.ID <= 0 || c.Name == "" {
			return ErrInvalidInput
		}
		newID, ok := catNameToID[c.Name]
		if !ok {
			_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO categories (name) VALUES (?)`, c.Name)
			if err != nil {
				return fmt.Errorf("insert category %q: %w", c.Name, err)
			}
			err = tx.QueryRowContext(ctx, `SELECT id FROM categories WHERE name = ?`, c.Name).Scan(&newID)
			if err != nil {
				return fmt.Errorf("lookup category %q: %w", c.Name, err)
			}
			catNameToID[c.Name] = newID
		}
		exportCatMap[c.ID] = newID
	}

	// Map export location IDs to local IDs
	exportLocMap := make(map[int64]int64)
	for _, l := range data.Locations {
		if l.ID <= 0 || l.Name == "" {
			return ErrInvalidInput
		}
		newID, ok := locNameToID[l.Name]
		if !ok {
			_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO locations (name) VALUES (?)`, l.Name)
			if err != nil {
				return fmt.Errorf("insert location %q: %w", l.Name, err)
			}
			err = tx.QueryRowContext(ctx, `SELECT id FROM locations WHERE name = ?`, l.Name).Scan(&newID)
			if err != nil {
				return fmt.Errorf("lookup location %q: %w", l.Name, err)
			}
			locNameToID[l.Name] = newID
		}
		exportLocMap[l.ID] = newID
	}

	ts := time.Now().UTC().Format(time.RFC3339Nano)

	for _, item := range data.Items {
		if item.Name == "" {
			return ErrInvalidInput
		}

		// Resolve category
		var catID *int64
		if item.CategoryID != nil && *item.CategoryID > 0 {
			newID, ok := exportCatMap[*item.CategoryID]
			if !ok {
				return fmt.Errorf("%w: unknown category id %d for item %q", ErrInvalidInput, *item.CategoryID, item.Name)
			}
			catID = &newID
		} else if item.CategoryID != nil {
			return fmt.Errorf("%w: invalid category id for item %q", ErrInvalidInput, item.Name)
		}

		// Resolve location
		var locID *int64
		if item.LocationID != nil && *item.LocationID > 0 {
			newID, ok := exportLocMap[*item.LocationID]
			if !ok {
				return fmt.Errorf("%w: unknown location id %d for item %q", ErrInvalidInput, *item.LocationID, item.Name)
			}
			locID = &newID
		} else if item.LocationID != nil {
			return fmt.Errorf("%w: invalid location id for item %q", ErrInvalidInput, item.Name)
		}

		// Validate and sum batches
		totalQty := 0
		for _, b := range item.Batches {
			if b.Quantity <= 0 {
				return fmt.Errorf("%w: batch quantity must be greater than zero for item %q", ErrInvalidInput, item.Name)
			}
			totalQty += b.Quantity
			if b.ExpirationDate != nil {
				_, err := time.Parse("2006-01-02", *b.ExpirationDate)
				if err != nil {
					return fmt.Errorf("%w: invalid expiration date %q for item %q",
						ErrInvalidInput, *b.ExpirationDate, item.Name)
				}
			}
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO items (name, category_id, location_id, quantity, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			item.Name, catID, locID, totalQty, ts, ts)
		if err != nil {
			return fmt.Errorf("create item %q: %w", item.Name, err)
		}
		itemID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("get item id for %q: %w", item.Name, err)
		}

		for _, batch := range item.Batches {
			if batch.Quantity <= 0 {
				continue
			}
			_, err := tx.ExecContext(ctx, `
				INSERT INTO batches (item_id, quantity, expiration_date, created_at)
				VALUES (?, ?, ?, ?)`,
				itemID, batch.Quantity, batch.ExpirationDate, ts)
			if err != nil {
				return fmt.Errorf("create batch for item %d: %w", itemID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import: %w", err)
	}
	return nil
}

func listCategoriesTx(tx *sql.Tx) ([]Category, error) {
	rows, err := tx.Query(`SELECT id, name FROM categories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cats := make([]Category, 0)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

func listLocationsTx(tx *sql.Tx) ([]Location, error) {
	rows, err := tx.Query(`SELECT id, name FROM locations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	locs := make([]Location, 0)
	for rows.Next() {
		l, err := scanLocation(rows)
		if err != nil {
			return nil, err
		}
		locs = append(locs, l)
	}
	return locs, rows.Err()
}
