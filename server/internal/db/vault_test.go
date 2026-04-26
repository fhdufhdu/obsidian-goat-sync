package db

import (
	"errors"
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

func TestEnsureVaultCreatesAndIgnoresExisting(t *testing.T) {
	q := setupTestDB(t)

	if err := q.EnsureVault("personal"); err != nil {
		t.Fatalf("ensure first vault: %v", err)
	}
	if err := q.EnsureVault("personal"); err != nil {
		t.Fatalf("ensure existing vault: %v", err)
	}

	vaults, err := q.ListVaults()
	if err != nil {
		t.Fatalf("list vaults: %v", err)
	}
	if len(vaults) != 1 || vaults[0].Name != "personal" {
		t.Fatalf("expected one personal vault, got %#v", vaults)
	}
}

func TestEnsureVaultRejectsBlankName(t *testing.T) {
	q := setupTestDB(t)

	if err := q.EnsureVault(" "); err == nil {
		t.Fatal("expected blank vault name to fail")
	} else if !errors.Is(err, ErrInvalidVaultName) {
		t.Fatalf("expected ErrInvalidVaultName, got %v", err)
	}
}

func TestInTxRollsBackOnPanic(t *testing.T) {
	q := setupTestDB(t)

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("expected panic")
			}
		}()
		_ = q.InTx(func(txq *Queries) error {
			if err := txq.EnsureVault("panic-rolled-back"); err != nil {
				return err
			}
			panic("stop")
		})
	}()

	exists, err := q.VaultExists("panic-rolled-back")
	if err != nil {
		t.Fatalf("vault exists: %v", err)
	}
	if exists {
		t.Fatal("expected transaction rollback after panic to remove vault")
	}
}

func TestInTxRollsBackOnError(t *testing.T) {
	q := setupTestDB(t)

	err := q.InTx(func(txq *Queries) error {
		if err := txq.EnsureVault("rolled-back"); err != nil {
			return err
		}
		return assertErr("stop")
	})
	if err == nil {
		t.Fatal("expected transaction error")
	}

	exists, err := q.VaultExists("rolled-back")
	if err != nil {
		t.Fatalf("vault exists: %v", err)
	}
	if exists {
		t.Fatal("expected transaction rollback to remove vault")
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
