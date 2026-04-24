package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	err := s.WriteFile("personal", "notes/hello.md", []byte("# Hello"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "vaults", "personal", "notes", "hello.md"))
	if err != nil {
		t.Fatalf("file not on disk: %v", err)
	}
	if string(content) != "# Hello" {
		t.Errorf("expected '# Hello', got '%s'", string(content))
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	s.WriteFile("personal", "notes/hello.md", []byte("# Hello"))

	content, err := s.ReadFile("personal", "notes/hello.md")
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "# Hello" {
		t.Errorf("expected '# Hello', got '%s'", string(content))
	}
}

func TestDeleteFile(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	s.WriteFile("personal", "notes/hello.md", []byte("# Hello"))

	err := s.DeleteFile("personal", "notes/hello.md")
	if err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	_, err = s.ReadFile("personal", "notes/hello.md")
	if err == nil {
		t.Fatal("expected error reading deleted file")
	}
}

func TestWriteBinaryFile(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	err := s.WriteFile("personal", "attachments/image.png", data)
	if err != nil {
		t.Fatalf("failed to write binary: %v", err)
	}

	content, _ := s.ReadFile("personal", "attachments/image.png")
	if len(content) != 8 {
		t.Errorf("expected 8 bytes, got %d", len(content))
	}
}

func TestWriteHiddenFile(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	err := s.WriteFile("personal", ".obsidian/app.json", []byte(`{"theme":"dark"}`))
	if err != nil {
		t.Fatalf("failed to write hidden file: %v", err)
	}

	content, _ := s.ReadFile("personal", ".obsidian/app.json")
	if string(content) != `{"theme":"dark"}` {
		t.Errorf("unexpected content: %s", string(content))
	}
}

func TestVaultSize(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	s.WriteFile("personal", "a.md", []byte("hello"))
	s.WriteFile("personal", "b.md", []byte("world"))

	count, size, err := s.VaultStats("personal")
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 files, got %d", count)
	}
	if size != 10 {
		t.Errorf("expected 10 bytes, got %d", size)
	}
}

func TestCreateVaultDir(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	err := s.CreateVaultDir("newvault")
	if err != nil {
		t.Fatalf("failed to create vault dir: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "vaults", "newvault"))
	if err != nil {
		t.Fatalf("vault dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory")
	}
}

func TestDeleteVaultDir(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	s.CreateVaultDir("delvault")
	s.WriteFile("delvault", "test.md", []byte("x"))

	err := s.DeleteVaultDir("delvault")
	if err != nil {
		t.Fatalf("failed to delete vault dir: %v", err)
	}

	_, err = os.Stat(filepath.Join(dir, "vaults", "delvault"))
	if !os.IsNotExist(err) {
		t.Fatal("expected vault dir removed")
	}
}
