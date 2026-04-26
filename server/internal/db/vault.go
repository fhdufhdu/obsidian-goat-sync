package db

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

type Queries struct {
	db    dbtx
	begin func() (*sql.Tx, error)
}

type dbtx interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func NewQueries(db *sql.DB) *Queries {
	return &Queries{
		db: db,
		begin: func() (*sql.Tx, error) {
			return db.Begin()
		},
	}
}

func newTxQueries(tx *sql.Tx) *Queries {
	return &Queries{db: tx}
}

func (q *Queries) InTx(fn func(*Queries) error) error {
	if q.begin == nil {
		return fn(q)
	}

	tx, err := q.begin()
	if err != nil {
		return err
	}

	txq := newTxQueries(tx)
	if err := fn(txq); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type Vault struct {
	Name       string
	InsertedAt string
	UpdatedAt  string
}

var ErrInvalidVaultName = errors.New("invalid vault name")

func (q *Queries) EnsureVault(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrInvalidVaultName
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := q.db.Exec(
		"INSERT OR IGNORE INTO vaults (name, inserted_at, updated_at) VALUES (?, ?, ?)",
		name, now, now,
	)
	return err
}

func (q *Queries) CreateVault(name string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := q.db.Exec(
		"INSERT INTO vaults (name, inserted_at, updated_at) VALUES (?, ?, ?)",
		name, now, now,
	)
	return err
}

func (q *Queries) ListVaults() ([]Vault, error) {
	rows, err := q.db.Query("SELECT name, inserted_at, updated_at FROM vaults ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vaults []Vault
	for rows.Next() {
		var v Vault
		if err := rows.Scan(&v.Name, &v.InsertedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		vaults = append(vaults, v)
	}
	return vaults, rows.Err()
}

func (q *Queries) DeleteVault(name string) error {
	_, err := q.db.Exec("DELETE FROM vaults WHERE name = ?", name)
	return err
}

func (q *Queries) VaultExists(name string) (bool, error) {
	var count int
	err := q.db.QueryRow("SELECT COUNT(*) FROM vaults WHERE name = ?", name).Scan(&count)
	return count > 0, err
}
