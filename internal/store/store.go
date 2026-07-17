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

type Store struct {
	db *sql.DB
}

type Item struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	Location  string `json:"location"`
	Quantity  int    `json:"quantity"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
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
	Name     string `json:"name"`
	Category string `json:"category"`
	Location string `json:"location"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) init() error {
	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
		`CREATE TABLE IF NOT EXISTS items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT '',
			location TEXT NOT NULL DEFAULT '',
			quantity INTEGER NOT NULL DEFAULT 0 CHECK (quantity >= 0),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
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
		`CREATE TABLE IF NOT EXISTS categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		)`,
		`CREATE TABLE IF NOT EXISTS locations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		)`,
		`CREATE TABLE IF NOT EXISTS batches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			item_id INTEGER NOT NULL,
			quantity INTEGER NOT NULL CHECK (quantity > 0),
			expiration_date TEXT,
			created_at TEXT NOT NULL,
			FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_movements_item_id_id ON movements(item_id, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_batches_item_id ON batches(item_id, expiration_date, created_at)`,
	
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("initialize database: %w", err)
		}
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

func scanItem(scanner interface{ Scan(...any) error }) (Item, error) {
	var item Item
	err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.Category,
		&item.Location,
		&item.Quantity,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

const itemColumns = `id, name, category, location, quantity, created_at, updated_at`
