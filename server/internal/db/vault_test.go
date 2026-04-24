package db

import (
	"path/filepath"
	"testing"
)

func setupTestDB(t *testing.T) *Queries {
	t.Helper()
	dir := t.TempDir()
	database, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewQueries(database)
}

func TestCreateVault(t *testing.T) {
	q := setupTestDB(t)

	err := q.CreateVault("personal")
	if err != nil {
		t.Fatalf("failed to create vault: %v", err)
	}

	vaults, err := q.ListVaults()
	if err != nil {
		t.Fatalf("failed to list vaults: %v", err)
	}
	if len(vaults) != 1 {
		t.Fatalf("expected 1 vault, got %d", len(vaults))
	}
	if vaults[0].Name != "personal" {
		t.Errorf("expected vault name=personal, got %s", vaults[0].Name)
	}
	if vaults[0].InsertedAt == "" {
		t.Error("expected inserted_at to be set")
	}
}

func TestCreateVaultDuplicate(t *testing.T) {
	q := setupTestDB(t)

	q.CreateVault("personal")
	err := q.CreateVault("personal")
	if err == nil {
		t.Fatal("expected error on duplicate vault")
	}
}

func TestDeleteVault(t *testing.T) {
	q := setupTestDB(t)

	q.CreateVault("personal")
	err := q.DeleteVault("personal")
	if err != nil {
		t.Fatalf("failed to delete vault: %v", err)
	}

	vaults, _ := q.ListVaults()
	if len(vaults) != 0 {
		t.Fatalf("expected 0 vaults, got %d", len(vaults))
	}
}
