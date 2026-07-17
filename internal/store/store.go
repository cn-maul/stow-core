package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound          = errors.New("item not found")
	ErrInvalidInput      = errors.New("invalid input")
	ErrInsufficientStock = errors.New("insufficient stock")
	ErrItemHasStock      = errors.New("item still has stock")
)

type Store struct {
	db *sql.DB
}

type Item struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	Unit      string `json:"unit"`
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
	Unit     string `json:"unit"`
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
			unit TEXT NOT NULL,
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
		`CREATE INDEX IF NOT EXISTS idx_movements_item_id_id ON movements(item_id, id DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("initialize database: %w", err)
		}
	}
	return nil
}

func validateItemInput(input ItemInput) error {
	if input.Name == "" || input.Unit == "" {
		return ErrInvalidInput
	}
	return nil
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func scanItem(scanner interface{ Scan(...any) error }) (Item, error) {
	var item Item
	err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.Category,
		&item.Unit,
		&item.Location,
		&item.Quantity,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

const itemColumns = `id, name, category, unit, location, quantity, created_at, updated_at`
