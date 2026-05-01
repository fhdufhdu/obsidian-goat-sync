package storage

import (
	"os"
	"path/filepath"
	"strings"
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

func TestStageWriteCommitAndCleanup(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	stage, err := s.StageWrite("personal", "notes/a.md", []byte("hello"))
	if err != nil {
		t.Fatalf("stage write: %v", err)
	}
	if _, err := os.Stat(stage.TempPath); err != nil {
		t.Fatalf("expected temp file: %v", err)
	}
	if err := stage.Commit(); err != nil {
		t.Fatalf("commit staged write: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "vaults", "personal", "notes", "a.md"))
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("content = %q", content)
	}
	info, err := os.Stat(filepath.Join(dir, "vaults", "personal", "notes", "a.md"))
	if err != nil {
		t.Fatalf("stat final file: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf("mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestStageWriteRollbackRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	stage, err := s.StageWrite("personal", "notes/a.md", []byte("hello"))
	if err != nil {
		t.Fatalf("stage write: %v", err)
	}
	tempPath := stage.TempPath
	if err := stage.Rollback(); err != nil {
		t.Fatalf("rollback staged write: %v", err)
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp file removed, got %v", err)
	}
}

func TestStageObjectWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	data := []byte("hello object")

	ref, op, err := s.StageObjectWrite(data)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "sha256:5cd9289a69664e69c5e2c3015062796590a2d6ed5f32fe9d4ec1f3c94636e457" {
		t.Fatalf("ref = %s", ref)
	}
	if err := op.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadObject(ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("object = %q", string(got))
	}
}

func TestStageObjectWriteIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	data := []byte("same")

	_, op1, err := s.StageObjectWrite(data)
	if err != nil {
		t.Fatal(err)
	}
	_, op2, err := s.StageObjectWrite(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := op1.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := op2.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestReadObjectRejectsInvalidRefs(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	tests := []struct {
		name string
		ref  string
	}{
		{
			name: "separator in digest",
			ref:  "sha256:" + strings.Repeat("a", 63) + "/",
		},
		{
			name: "non-hex digest",
			ref:  "sha256:" + strings.Repeat("g", 64),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.ReadObject(tt.ref)
			if err == nil {
				t.Fatal("expected invalid ref error")
			}
			if !strings.Contains(err.Error(), "invalid content ref") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestStageDeleteRestoreAndFinalize(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.WriteFile("personal", "notes/a.md", []byte("hello")); err != nil {
		t.Fatalf("write file: %v", err)
	}

	stage, err := s.StageDelete("personal", "notes/a.md")
	if err != nil {
		t.Fatalf("stage delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vaults", "personal", "notes", "a.md")); !os.IsNotExist(err) {
		t.Fatalf("expected original moved away, got %v", err)
	}
	if err := stage.Rollback(); err != nil {
		t.Fatalf("rollback delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vaults", "personal", "notes", "a.md")); err != nil {
		t.Fatalf("expected original restored: %v", err)
	}

	stage, err = s.StageDelete("personal", "notes/a.md")
	if err != nil {
		t.Fatalf("stage delete again: %v", err)
	}
	if err := stage.Commit(); err != nil {
		t.Fatalf("commit delete: %v", err)
	}
	if _, err := os.Stat(stage.TempPath); !os.IsNotExist(err) {
		t.Fatalf("expected trash removed, got %v", err)
	}
}

func TestStageDeleteMissingFileIsNoop(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	stage, err := s.StageDelete("personal", "notes/missing.md")
	if err != nil {
		t.Fatalf("stage delete missing file: %v", err)
	}
	if err := stage.Commit(); err != nil {
		t.Fatalf("commit missing delete: %v", err)
	}
	if err := stage.Rollback(); err != nil {
		t.Fatalf("rollback missing delete: %v", err)
	}
	trashDir := filepath.Join(dir, "vaults", "personal", "notes", ".goat-sync-trash")
	if _, err := os.Stat(trashDir); !os.IsNotExist(err) {
		t.Fatalf("expected no trash directory for missing file, got %v", err)
	}
}
