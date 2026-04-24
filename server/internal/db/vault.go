package db

import (
	"database/sql"
	"time"
)

type Queries struct {
	db *sql.DB
}

func NewQueries(db *sql.DB) *Queries {
	return &Queries{db: db}
}

type Vault struct {
	Name       string
	InsertedAt string
	UpdatedAt  string
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
