package db

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

func Open(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS vaults (
		name        TEXT PRIMARY KEY,
		inserted_at TEXT NOT NULL,
		updated_at  TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS files (
		vault_name  TEXT NOT NULL,
		path        TEXT NOT NULL,
		version     INTEGER NOT NULL DEFAULT 1,
		hash        TEXT NOT NULL,
		is_deleted  INTEGER NOT NULL DEFAULT 0,
		inserted_at TEXT NOT NULL,
		updated_at  TEXT NOT NULL,
		PRIMARY KEY (vault_name, path),
		FOREIGN KEY (vault_name) REFERENCES vaults(name) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS tokens (
		token       TEXT PRIMARY KEY,
		is_active   INTEGER NOT NULL DEFAULT 1,
		inserted_at TEXT NOT NULL,
		updated_at  TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS github_configs (
		vault_name    TEXT PRIMARY KEY,
		remote_url    TEXT NOT NULL,
		branch        TEXT NOT NULL DEFAULT 'main',
		interval      TEXT NOT NULL DEFAULT '1h',
		access_token  TEXT NOT NULL,
		author_name   TEXT NOT NULL,
		author_email  TEXT NOT NULL,
		enabled       INTEGER NOT NULL DEFAULT 1,
		inserted_at   TEXT NOT NULL,
		updated_at    TEXT NOT NULL,
		FOREIGN KEY (vault_name) REFERENCES vaults(name) ON DELETE CASCADE
	);`

	_, err := db.Exec(schema)
	return err
}
