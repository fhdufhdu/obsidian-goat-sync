package db

import (
	"database/sql"
	"testing"
)

func TestCreateFile(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")

	f, err := q.CreateFile("personal", "notes/hello.md", "abc123")
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if f.Version != 1 {
		t.Errorf("expected version=1, got %d", f.Version)
	}
	if f.Hash != "abc123" {
		t.Errorf("expected hash=abc123, got %s", f.Hash)
	}
	if f.IsDeleted {
		t.Error("expected is_deleted=false")
	}
}

func TestUpdateFile(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")
	q.CreateFile("personal", "notes/hello.md", "abc123")

	f, err := q.UpdateFile("personal", "notes/hello.md", "def456")
	if err != nil {
		t.Fatalf("failed to update file: %v", err)
	}
	if f.Version != 2 {
		t.Errorf("expected version=2, got %d", f.Version)
	}
	if f.Hash != "def456" {
		t.Errorf("expected hash=def456, got %s", f.Hash)
	}
}

func TestDeleteFile(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")
	q.CreateFile("personal", "notes/hello.md", "abc123")

	f, err := q.DeleteFile("personal", "notes/hello.md")
	if err != nil {
		t.Fatalf("failed to delete file: %v", err)
	}
	if !f.IsDeleted {
		t.Error("expected is_deleted=true")
	}
	if f.Version != 2 {
		t.Errorf("expected version=2 after delete, got %d", f.Version)
	}
}

func TestCreateFileFromTombstone(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")
	q.CreateFile("personal", "notes/hello.md", "abc123")
	q.DeleteFile("personal", "notes/hello.md")

	tombstone, _ := q.GetFile("personal", "notes/hello.md")
	if !tombstone.IsDeleted {
		t.Fatal("expected tombstone")
	}

	f, err := q.CreateFileFromTombstone("personal", "notes/hello.md", "newHash", tombstone.Version)
	if err != nil {
		t.Fatalf("failed to recreate from tombstone: %v", err)
	}
	if f.IsDeleted {
		t.Error("expected is_deleted=false")
	}
	if f.Version != tombstone.Version+1 {
		t.Errorf("expected version=%d, got %d", tombstone.Version+1, f.Version)
	}
	if f.Hash != "newHash" {
		t.Errorf("expected hash=newHash, got %s", f.Hash)
	}
}

func TestGetFileNotFound(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")

	_, err := q.GetFile("personal", "nonexistent.md")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestListActiveFiles(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")
	q.CreateFile("personal", "notes/a.md", "hash1")
	q.CreateFile("personal", "notes/b.md", "hash2")
	q.CreateFile("personal", "notes/c.md", "hash3")
	q.DeleteFile("personal", "notes/c.md")

	files, err := q.ListActiveFiles("personal")
	if err != nil {
		t.Fatalf("failed to list files: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 active files, got %d", len(files))
	}
}
