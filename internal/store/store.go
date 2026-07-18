package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound          = errors.New("item not found")
	ErrInvalidInput      = errors.New("invalid input")
	ErrInsufficientStock = errors.New("insufficient stock")
	ErrItemHasStock      = errors.New("item still has stock")
	ErrInUse             = errors.New("resource is in use")
	ErrDuplicate         = errors.New("resource already exists")
	ErrExportError       = errors.New("export failed")
)

type Category struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Location struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Batch struct {
	ID             int64   `json:"id"`
	ItemID         int64   `json:"item_id"`
	Quantity       int     `json:"quantity"`
	ExpirationDate *string `json:"expiration_date"`
	CreatedAt      string  `json:"created_at"`
}

type Item struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Category   string `json:"category"` // name, populated by JOIN
	CategoryID *int64 `json:"category_id"`
	Location   string `json:"location"` // name, populated by JOIN
	LocationID *int64 `json:"location_id"`
	Quantity   int    `json:"quantity"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type Movement struct {
	ID            int64  `json:"id"`
	ItemID        int64  `json:"item_id"`
	Type          string `json:"type"`
	Change        int    `json:"change"`
	QuantityAfter int    `json:"quantity_after"`
	Note          string `json:"note"`
	CreatedAt     string `json:"created_at"`
}

type ItemInput struct {
	Name       string `json:"name"`
	Category   string `json:"category"`    // legacy name or resolved name
	CategoryID *int64 `json:"category_id"` // preferred, ID-based
	Location   string `json:"location"`    // legacy name or resolved name
	LocationID *int64 `json:"location_id"` // preferred, ID-based
}

// SchemaVersion is the current supported export/import schema version.
const SchemaVersion = 2

const (
	userVersionCol = 2 // target version
)

// Store holds the database connection.
type Store struct {
	db *sql.DB
}

// Open opens a SQLite database and runs schema initialization + migration.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db}
	if err := s.ensureFK(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// ensureFK enables foreign keys on the connection.
func (s *Store) ensureFK() error {
	// Enable foreign keys on the initial connection.
	// Since MaxOpenConns=1, we also set it per-connection via DSN if the driver supports it.
	_, err := s.db.Exec(`PRAGMA foreign_keys = ON`)
	if err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	// Verify it was actually set.
	var val int
	err = s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&val)
	if err != nil {
		return fmt.Errorf("check foreign keys pragma: %w", err)
	}
	if val != 1 {
		return fmt.Errorf("foreign keys pragma not enabled (got %d)", val)
	}
	return nil
}

func (s *Store) init() error {
	statements := []string{
		`PRAGMA busy_timeout = 5000`,
		`CREATE TABLE IF NOT EXISTS categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		)`,
		`CREATE TABLE IF NOT EXISTS locations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		)`,
		`CREATE TABLE IF NOT EXISTS items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			category_id INTEGER,
			location_id INTEGER,
			quantity INTEGER NOT NULL DEFAULT 0 CHECK (quantity >= 0),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY (category_id) REFERENCES categories(id),
			FOREIGN KEY (location_id) REFERENCES locations(id)
		)`,
		`CREATE TABLE IF NOT EXISTS batches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			item_id INTEGER NOT NULL,
			quantity INTEGER NOT NULL CHECK (quantity > 0),
			expiration_date TEXT,
			created_at TEXT NOT NULL,
			FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS movements (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			item_id INTEGER NOT NULL,
			type TEXT NOT NULL CHECK (type IN ('stock_in', 'stock_out', 'adjust')),
			change INTEGER NOT NULL CHECK (change != 0),
			quantity_after INTEGER NOT NULL CHECK (quantity_after >= 0),
			note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_batches_item_id ON batches(item_id, expiration_date, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_movements_item_id_id ON movements(item_id, id DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("initialize database: %w", err)
		}
	}
	return nil
}

// migrate migrates the database schema to the latest version.
func (s *Store) migrate() error {
	ctx := context.Background()

	// Check current user_version
	var version int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&version)

	// Check current items columns
	var colNames []string
	rows, err := s.db.Query(`PRAGMA table_info(items)`)
	if err != nil {
		return fmt.Errorf("migrate: table_info items: %w", err)
	}
	for rows.Next() {
		var cid, notNull, hasDefault, pk int
		var name, colType string
		rows.Scan(&cid, &name, &colType, &notNull, &hasDefault, &pk)
		colNames = append(colNames, name)
	}
	rows.Close()

	hasCatID := containsStr(colNames, "category_id")

	// Already at target schema
	if hasCatID {
		// Make sure user_version is set correctly
		if version != userVersionCol {
			_, err := s.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, userVersionCol))
			if err != nil {
				return fmt.Errorf("migrate: set user_version: %w", err)
			}
		}
		return nil
	}

	// Need migration
	return s.migrateToV2(ctx)
}

// migrateToV2 migrates from pre-FK schema (v1) to FK-based schema (v2).
func (s *Store) migrateToV2(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Get existing items
	itemsRows, err := tx.QueryContext(ctx, `SELECT id, name, category, location, quantity, created_at, updated_at FROM items`)
	if err != nil {
		return fmt.Errorf("migrate: query items: %w", err)
	}

	type legacyItem struct {
		id, quantity                                   int
		name, category, location, createdAt, updatedAt string
	}
	var legacyItems []legacyItem
	for itemsRows.Next() {
		var li legacyItem
		if err := itemsRows.Scan(&li.id, &li.name, &li.category, &li.location, &li.quantity, &li.createdAt, &li.updatedAt); err != nil {
			itemsRows.Close()
			return fmt.Errorf("migrate: scan item: %w", err)
		}
		legacyItems = append(legacyItems, li)
	}
	itemsRows.Close()

	// Build category mapping (ensure all legacy category strings exist as entities)
	catMap := make(map[string]int64)
	for _, li := range legacyItems {
		if li.category == "" {
			continue
		}
		if _, ok := catMap[li.category]; ok {
			continue
		}
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO categories (name) VALUES (?)`, li.category)
		if err != nil {
			return fmt.Errorf("migrate: insert category: %w", err)
		}
		var id int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM categories WHERE name = ?`, li.category).Scan(&id)
		if err != nil {
			return fmt.Errorf("migrate: lookup category: %w", err)
		}
		catMap[li.category] = id
	}

	// Build location mapping
	locMap := make(map[string]int64)
	for _, li := range legacyItems {
		if li.location == "" {
			continue
		}
		if _, ok := locMap[li.location]; ok {
			continue
		}
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO locations (name) VALUES (?)`, li.location)
		if err != nil {
			return fmt.Errorf("migrate: insert location: %w", err)
		}
		var id int64
		_ = result
		err = tx.QueryRowContext(ctx, `SELECT id FROM locations WHERE name = ?`, li.location).Scan(&id)
		if err != nil {
			return fmt.Errorf("migrate: lookup location: %w", err)
		}
		locMap[li.location] = id
	}

	// Check if batches table already has data
	var hasExistingBatches bool
	tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM batches`).Scan(&hasExistingBatches)
	// hasExistingBatches will be 0 or 1 from COUNT(*)

	// Create new_items table with FK columns
	_, err = tx.ExecContext(ctx, `CREATE TABLE new_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		category_id INTEGER,
		location_id INTEGER,
		quantity INTEGER NOT NULL DEFAULT 0 CHECK (quantity >= 0),
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY (category_id) REFERENCES categories(id),
		FOREIGN KEY (location_id) REFERENCES locations(id)
	)`)
	if err != nil {
		return fmt.Errorf("migrate: create new_items: %w", err)
	}

	// Copy items to new table with FK references
	for _, li := range legacyItems {
		var catID, locID *int64
		if id, ok := catMap[li.category]; ok {
			catID = &id
		}
		if id, ok := locMap[li.location]; ok {
			locID = &id
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO new_items (id, name, category_id, location_id, quantity, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			li.id, li.name, catID, locID, li.quantity, li.createdAt, li.updatedAt)
		if err != nil {
			return fmt.Errorf("migrate: insert item %d: %w", li.id, err)
		}
	}

	// If no existing batches, populate from legacy inventory
	var existingBatchCount int
	tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM batches`).Scan(&existingBatchCount)
	if existingBatchCount == 0 {
		for _, li := range legacyItems {
			if li.quantity > 0 {
				_, err = tx.ExecContext(ctx, `INSERT INTO batches (item_id, quantity, expiration_date, created_at) VALUES (?, ?, NULL, ?)`,
					li.id, li.quantity, li.updatedAt)
				if err != nil {
					return fmt.Errorf("migrate: create batch for item %d: %w", li.id, err)
				}
			}
		}
	}

	// Drop old items table and rename
	_, err = tx.ExecContext(ctx, `DROP TABLE items`)
	if err != nil {
		return fmt.Errorf("migrate: drop old items: %w", err)
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE new_items RENAME TO items`)
	if err != nil {
		return fmt.Errorf("migrate: rename new_items: %w", err)
	}

	// Set user_version
	_, err = tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, userVersionCol))
	if err != nil {
		return fmt.Errorf("migrate: set user_version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: commit: %w", err)
	}
	return nil
}

func validateItemInput(input ItemInput) error {
	if input.Name == "" {
		return ErrInvalidInput
	}
	return nil
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func isUniqueConstraint(err error) bool {
	return err != nil && (errors.Is(err, ErrDuplicate) || strings.Contains(err.Error(), "UNIQUE constraint failed"))
}

// HasColumn checks if a table has a given column.
func (s *Store) HasColumn(ctx context.Context, table, column string) bool {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, hasDefault, pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &hasDefault, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

func containsStr(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// scanItem scans an item with category/location JOIN.
func scanItem(scanner interface{ Scan(...any) error }) (Item, error) {
	var item Item
	err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.CategoryID,
		&item.Category,
		&item.LocationID,
		&item.Location,
		&item.Quantity,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func scanItemTx(scanner interface{ Scan(...any) error }) (Item, error) {
	var item Item
	err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.CategoryID,
		&item.Category,
		&item.LocationID,
		&item.Location,
		&item.Quantity,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

const itemColumns = `i.id, i.name, i.category_id, COALESCE(c.name, '') AS category, i.location_id, COALESCE(l.name, '') AS location, i.quantity, i.created_at, i.updated_at`

const itemColumnsShort = `i.id, i.name, i.category_id, COALESCE(c.name, '') AS category, i.location_id, COALESCE(l.name, '') AS location, i.quantity, i.created_at, i.updated_at`

func scanBatch(scanner interface{ Scan(...any) error }) (Batch, error) {
	var b Batch
	err := scanner.Scan(&b.ID, &b.ItemID, &b.Quantity, &b.ExpirationDate, &b.CreatedAt)
	return b, err
}

func scanCategory(scanner interface{ Scan(...any) error }) (Category, error) {
	var c Category
	err := scanner.Scan(&c.ID, &c.Name)
	return c, err
}

func scanLocation(scanner interface{ Scan(...any) error }) (Location, error) {
	var l Location
	err := scanner.Scan(&l.ID, &l.Name)
	return l, err
}
