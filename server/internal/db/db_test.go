package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer database.Close()

	for _, table := range []string{"vaults", "file_versions", "tokens", "github_configs"} {
		var name string
		if err := database.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}

	var pk int
	if err := database.QueryRow("SELECT pk FROM pragma_table_info('file_versions') WHERE name='id'").Scan(&pk); err != nil || pk != 1 {
		t.Fatalf("file_versions.id is not primary key: pk=%d err=%v", pk, err)
	}

	var uniqueCount int
	err = database.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_index_list('file_versions') il
		JOIN pragma_index_info(il.name) ii ON true
		WHERE il.[unique] = 1 AND ii.name IN ('vault_name', 'path', 'version')
	`).Scan(&uniqueCount)
	if err != nil || uniqueCount < 3 {
		t.Fatalf("missing unique index on vault_name/path/version: count=%d err=%v", uniqueCount, err)
	}
}

func TestOpenCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "test.db")

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer database.Close()

	if _, err := os.Stat(filepath.Join(dir, "sub")); os.IsNotExist(err) {
		t.Fatal("parent directory not created")
	}
}
