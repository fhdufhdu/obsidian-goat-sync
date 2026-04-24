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

	for _, table := range []string{"vaults", "files", "tokens", "github_configs"} {
		var tableName string
		err = database.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&tableName)
		if err != nil {
			t.Fatalf("%s table not created: %v", table, err)
		}
	}

	var col string
	err = database.QueryRow("SELECT name FROM pragma_table_info('files') WHERE name='version'").Scan(&col)
	if err != nil {
		t.Fatal("files table missing 'version' column")
	}
	err = database.QueryRow("SELECT name FROM pragma_table_info('files') WHERE name='hash'").Scan(&col)
	if err != nil {
		t.Fatal("files table missing 'hash' column")
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
