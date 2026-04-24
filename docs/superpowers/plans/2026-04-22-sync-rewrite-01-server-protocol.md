# Sync Rewrite — Plan 1: 서버 프로토콜 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Go 서버의 DB 스키마, WebSocket 메시지 프로토콜, 동기화/충돌 판정 로직을 `(version, hash)` 낙관적 락 모델로 전면 교체한다.

**Architecture:** 기존 mtime 기반 비교 로직을 제거하고 클라이언트가 전송한 `prevServerVersion`/`prevServerHash`/`currentClientHash`를 서버 DB 현재값과 비교해 분류. 모든 쓰기 메시지는 낙관적 락 검증을 통과해야 커밋. `remote_change` 폐기.

**Tech Stack:** Go 1.25, SQLite (mattn/go-sqlite3), gorilla/websocket, 표준 `testing`.

**상위 스펙:** [`docs/superpowers/specs/2026-04-22-sync-rewrite-design.md`](../specs/2026-04-22-sync-rewrite-design.md)

---

## 전체 파일 구조

| 파일 | 역할 | 상태 |
|---|---|---|
| `server/internal/db/db.go` | 스키마 마이그레이션 | 재작성 |
| `server/internal/db/vault.go` | vaults 테이블 CRUD | 수정 (inserted_at/updated_at) |
| `server/internal/db/file.go` | files 테이블 CRUD (version/hash) | 재작성 |
| `server/internal/db/token.go` | tokens 테이블 CRUD | 수정 (inserted_at/updated_at) |
| `server/internal/db/github_config.go` | github_configs CRUD (access_token, author_*) | 확장 |
| `server/internal/db/file_test.go` | files CRUD 단위 테스트 | 재작성 |
| `server/internal/db/github_config_test.go` | github_configs 단위 테스트 | 수정 |
| `server/internal/db/vault_test.go` | vault 단위 테스트 | 수정 |
| `server/internal/db/token_test.go` | token 단위 테스트 | 수정 |
| `server/internal/sync/classify.go` | 판정표 구현 (sync_init/file_check) | 신규 |
| `server/internal/sync/classify_test.go` | 판정표 테스트 | 신규 |
| `server/internal/sync/conflict.go` | `makeConflictPath` 외 구버전 로직 | 축소 (makeConflictPath만 유지) |
| `server/internal/sync/conflict_test.go` | 충돌 경로 테스트 | 유지 |
| `server/internal/ws/messages.go` | 새 메시지 타입 정의 | 재작성 |
| `server/internal/ws/handler.go` | 신규 메시지 타입별 핸들러 | 재작성 |
| `server/internal/ws/handler_test.go` | 핸들러 통합 테스트 | 재작성 |

## 개발 환경

서버 디렉토리에서 작업:

```bash
cd server
go test ./...   # 모든 테스트
go build ./...  # 빌드
```

기존 `sync.db`는 마이그레이션 없이 폐기. 테스트는 `t.TempDir()` 기반 임시 DB.

---

## Task 1: DB 스키마 전면 교체

**Files:**
- Modify: `server/internal/db/db.go`
- Create: `server/internal/db/db_test.go` (신규 스키마 검증)

- [ ] **Step 1: Write the failing test**

`server/internal/db/db_test.go`:

```go
package db

import (
	"path/filepath"
	"testing"
)

func TestMigrateCreatesNewSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sync.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	tables := []string{"vaults", "files", "tokens", "github_configs"}
	for _, tbl := range tables {
		var name string
		err := database.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing: %v", tbl, err)
		}
	}

	columnChecks := map[string][]string{
		"vaults":         {"name", "inserted_at", "updated_at"},
		"files":          {"vault_name", "path", "version", "hash", "is_deleted", "inserted_at", "updated_at"},
		"tokens":         {"token", "is_active", "inserted_at", "updated_at"},
		"github_configs": {"vault_name", "remote_url", "branch", "interval", "access_token", "author_name", "author_email", "enabled", "inserted_at", "updated_at"},
	}
	for table, cols := range columnChecks {
		rows, err := database.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			t.Fatalf("PRAGMA for %s: %v", table, err)
		}
		existing := map[string]bool{}
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt interface{}
			rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk)
			existing[name] = true
		}
		rows.Close()
		for _, col := range cols {
			if !existing[col] {
				t.Errorf("table %s missing column %s", table, col)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/db/ -run TestMigrateCreatesNewSchema -v
```

Expected: FAIL. `files` 테이블에 `version`, `hash`, `inserted_at`, `updated_at` 누락. `github_configs`에 `access_token`, `author_name`, `author_email`, `inserted_at`, `updated_at` 누락. `tokens`에 `updated_at` 누락. `vaults`에 `inserted_at`, `updated_at` 누락.

- [ ] **Step 3: Write minimal implementation**

`server/internal/db/db.go`:

```go
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
		access_token  TEXT NOT NULL DEFAULT '',
		author_name   TEXT NOT NULL DEFAULT '',
		author_email  TEXT NOT NULL DEFAULT '',
		enabled       INTEGER NOT NULL DEFAULT 1,
		inserted_at   TEXT NOT NULL,
		updated_at    TEXT NOT NULL,
		FOREIGN KEY (vault_name) REFERENCES vaults(name) ON DELETE CASCADE
	);`

	_, err := db.Exec(schema)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/db/ -run TestMigrateCreatesNewSchema -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/estsoft/project/other/obsidian-goat-sync
git add server/internal/db/db.go server/internal/db/db_test.go
git commit -m "feat(db): migrate to version/hash schema with inserted_at/updated_at"
```

---

## Task 2: Vault CRUD inserted_at/updated_at 반영

**Files:**
- Modify: `server/internal/db/vault.go`
- Modify: `server/internal/db/vault_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/db/vault_test.go` 전체 교체:

```go
package db

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestQueries(t *testing.T) *Queries {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sync.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewQueries(database)
}

func TestCreateVaultStoresTimestamps(t *testing.T) {
	q := newTestQueries(t)
	before := time.Now().UTC().Add(-time.Second).Format(time.RFC3339)

	if err := q.CreateVault("personal"); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}

	v, err := q.GetVault("personal")
	if err != nil {
		t.Fatalf("GetVault: %v", err)
	}
	if v.Name != "personal" {
		t.Errorf("name = %q, want personal", v.Name)
	}
	if v.InsertedAt < before {
		t.Errorf("inserted_at %q < before %q", v.InsertedAt, before)
	}
	if v.UpdatedAt != v.InsertedAt {
		t.Errorf("updated_at %q != inserted_at %q on create", v.UpdatedAt, v.InsertedAt)
	}
}

func TestListVaultsReturnsAll(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("a")
	q.CreateVault("b")

	vaults, err := q.ListVaults()
	if err != nil {
		t.Fatalf("ListVaults: %v", err)
	}
	if len(vaults) != 2 {
		t.Fatalf("want 2 vaults, got %d", len(vaults))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/db/ -run TestCreateVaultStoresTimestamps -v
```

Expected: FAIL. `Vault.InsertedAt`, `UpdatedAt` 필드 없음. `GetVault` 함수 없음.

- [ ] **Step 3: Write minimal implementation**

`server/internal/db/vault.go`:

```go
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

func (q *Queries) GetVault(name string) (Vault, error) {
	var v Vault
	err := q.db.QueryRow(
		"SELECT name, inserted_at, updated_at FROM vaults WHERE name = ?",
		name,
	).Scan(&v.Name, &v.InsertedAt, &v.UpdatedAt)
	return v, err
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
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/db/ -run "TestCreateVaultStoresTimestamps|TestListVaultsReturnsAll" -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/db/vault.go server/internal/db/vault_test.go
git commit -m "refactor(db): vaults use inserted_at/updated_at"
```

---

## Task 3: Token CRUD inserted_at/updated_at 반영

**Files:**
- Modify: `server/internal/db/token.go`
- Modify: `server/internal/db/token_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/db/token_test.go` 전체 교체:

```go
package db

import (
	"testing"
	"time"
)

func TestGenerateTokenStoresTimestamps(t *testing.T) {
	q := newTestQueries(t)
	before := time.Now().UTC().Add(-time.Second).Format(time.RFC3339)

	tok, err := q.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(tok) != 64 {
		t.Errorf("token length = %d, want 64", len(tok))
	}

	list, err := q.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 token, got %d", len(list))
	}
	entry := list[0]
	if entry.InsertedAt < before {
		t.Errorf("inserted_at %q < before %q", entry.InsertedAt, before)
	}
	if entry.UpdatedAt != entry.InsertedAt {
		t.Error("updated_at must equal inserted_at on create")
	}
}

func TestDeactivateTokenUpdatesTimestamp(t *testing.T) {
	q := newTestQueries(t)
	tok, _ := q.GenerateToken()
	// 1초 간격 보장 (RFC3339 초단위)
	time.Sleep(1100 * time.Millisecond)
	if err := q.DeactivateToken(tok); err != nil {
		t.Fatalf("DeactivateToken: %v", err)
	}

	list, _ := q.ListTokens()
	if len(list) != 1 {
		t.Fatalf("want 1 token, got %d", len(list))
	}
	if list[0].IsActive {
		t.Error("expected is_active=false after deactivate")
	}
	if list[0].UpdatedAt <= list[0].InsertedAt {
		t.Errorf("updated_at %q must be later than inserted_at %q", list[0].UpdatedAt, list[0].InsertedAt)
	}
}

func TestValidateToken(t *testing.T) {
	q := newTestQueries(t)
	tok, _ := q.GenerateToken()

	valid, _ := q.ValidateToken(tok)
	if !valid {
		t.Error("expected active token to be valid")
	}
	q.DeactivateToken(tok)
	valid, _ = q.ValidateToken(tok)
	if valid {
		t.Error("expected deactivated token to be invalid")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/db/ -run "TestGenerateTokenStoresTimestamps|TestDeactivateTokenUpdatesTimestamp|TestValidateToken" -v
```

Expected: FAIL. `Token.InsertedAt`, `UpdatedAt` 필드 없음.

- [ ] **Step 3: Write minimal implementation**

`server/internal/db/token.go`:

```go
package db

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type Token struct {
	Token      string
	IsActive   bool
	InsertedAt string
	UpdatedAt  string
}

func (q *Queries) GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := q.db.Exec(
		"INSERT INTO tokens (token, is_active, inserted_at, updated_at) VALUES (?, 1, ?, ?)",
		token, now, now,
	)
	return token, err
}

func (q *Queries) ValidateToken(token string) (bool, error) {
	var count int
	err := q.db.QueryRow(
		"SELECT COUNT(*) FROM tokens WHERE token = ? AND is_active = 1",
		token,
	).Scan(&count)
	return count > 0, err
}

func (q *Queries) DeactivateToken(token string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := q.db.Exec(
		"UPDATE tokens SET is_active = 0, updated_at = ? WHERE token = ?",
		now, token,
	)
	return err
}

func (q *Queries) RegenerateToken(oldToken string) (string, error) {
	if err := q.DeactivateToken(oldToken); err != nil {
		return "", err
	}
	return q.GenerateToken()
}

func (q *Queries) ListTokens() ([]Token, error) {
	rows, err := q.db.Query(
		"SELECT token, is_active, inserted_at, updated_at FROM tokens ORDER BY inserted_at DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.Token, &t.IsActive, &t.InsertedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/db/ -run "TestGenerateTokenStoresTimestamps|TestDeactivateTokenUpdatesTimestamp|TestValidateToken" -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/db/token.go server/internal/db/token_test.go
git commit -m "refactor(db): tokens use inserted_at/updated_at"
```

---

## Task 4: Files CRUD 낙관적 락 (version/hash) 재작성

**Files:**
- Modify: `server/internal/db/file.go`
- Modify: `server/internal/db/file_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/db/file_test.go` 전체 교체:

```go
package db

import (
	"database/sql"
	"testing"
)

func TestCreateFileNewRecord(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")

	f, err := q.CreateFile("v", "notes/hello.md", "hashA")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if f.Version != 1 {
		t.Errorf("version = %d, want 1", f.Version)
	}
	if f.Hash != "hashA" {
		t.Errorf("hash = %q, want hashA", f.Hash)
	}
	if f.IsDeleted {
		t.Error("is_deleted should be false")
	}
}

func TestCreateFileRejectsActiveDuplicate(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")
	q.CreateFile("v", "a.md", "h1")

	_, err := q.CreateFile("v", "a.md", "h2")
	if err != ErrFileConflict {
		t.Errorf("want ErrFileConflict, got %v", err)
	}
}

func TestCreateFileReusesTombstone(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")
	q.CreateFile("v", "a.md", "h1")           // v=1
	q.DeleteFile("v", "a.md", 1)              // v=2 tombstone

	f, err := q.CreateFile("v", "a.md", "h3")
	if err != nil {
		t.Fatalf("CreateFile after tombstone: %v", err)
	}
	if f.Version != 3 {
		t.Errorf("version = %d, want 3 (tombstone+1)", f.Version)
	}
	if f.Hash != "h3" {
		t.Errorf("hash = %q, want h3", f.Hash)
	}
	if f.IsDeleted {
		t.Error("resurrected file must not be deleted")
	}
}

func TestUpdateFileOptimisticLockSuccess(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")
	q.CreateFile("v", "a.md", "h1")

	f, err := q.UpdateFile("v", "a.md", 1, "h2")
	if err != nil {
		t.Fatalf("UpdateFile: %v", err)
	}
	if f.Version != 2 {
		t.Errorf("version = %d, want 2", f.Version)
	}
	if f.Hash != "h2" {
		t.Errorf("hash = %q, want h2", f.Hash)
	}
}

func TestUpdateFileConflictStaleVersion(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")
	q.CreateFile("v", "a.md", "h1")
	q.UpdateFile("v", "a.md", 1, "h2")

	_, err := q.UpdateFile("v", "a.md", 1, "h3")
	if err != ErrFileConflict {
		t.Errorf("want ErrFileConflict, got %v", err)
	}
}

func TestUpdateFileOnTombstoneFails(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")
	q.CreateFile("v", "a.md", "h1")
	q.DeleteFile("v", "a.md", 1)

	_, err := q.UpdateFile("v", "a.md", 2, "h2")
	if err != ErrFileNotFound {
		t.Errorf("want ErrFileNotFound, got %v", err)
	}
}

func TestDeleteFileOptimisticLockSuccess(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")
	q.CreateFile("v", "a.md", "h1")

	newVersion, err := q.DeleteFile("v", "a.md", 1)
	if err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if newVersion != 2 {
		t.Errorf("newVersion = %d, want 2", newVersion)
	}

	f, err := q.GetFile("v", "a.md")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if !f.IsDeleted {
		t.Error("expected tombstone")
	}
}

func TestDeleteFileConflictStaleVersion(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")
	q.CreateFile("v", "a.md", "h1")
	q.UpdateFile("v", "a.md", 1, "h2")

	_, err := q.DeleteFile("v", "a.md", 1)
	if err != ErrFileConflict {
		t.Errorf("want ErrFileConflict, got %v", err)
	}
}

func TestDeleteFileIdempotentOnTombstone(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")
	q.CreateFile("v", "a.md", "h1")
	q.DeleteFile("v", "a.md", 1)

	_, err := q.DeleteFile("v", "a.md", 2)
	if err != ErrFileNotFound {
		t.Errorf("want ErrFileNotFound, got %v", err)
	}
}

func TestGetFileReturnsTombstone(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")
	q.CreateFile("v", "a.md", "h1")
	q.DeleteFile("v", "a.md", 1)

	f, err := q.GetFile("v", "a.md")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if !f.IsDeleted {
		t.Error("expected is_deleted=true")
	}
	if f.Version != 2 {
		t.Errorf("version = %d, want 2", f.Version)
	}
}

func TestGetFileNotFound(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")

	_, err := q.GetFile("v", "missing.md")
	if err != sql.ErrNoRows {
		t.Errorf("want sql.ErrNoRows, got %v", err)
	}
}

func TestListActiveFiles(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")
	q.CreateFile("v", "a.md", "h1")
	q.CreateFile("v", "b.md", "h2")
	q.DeleteFile("v", "b.md", 1)

	files, err := q.ListActiveFiles("v")
	if err != nil {
		t.Fatalf("ListActiveFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("want 1 active file, got %d", len(files))
	}
	if files[0].Path != "a.md" {
		t.Errorf("path = %q, want a.md", files[0].Path)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/db/ -run "TestCreateFile|TestUpdateFile|TestDeleteFile|TestGetFile|TestListActiveFiles" -v
```

Expected: FAIL. `CreateFile`, `UpdateFile`, `DeleteFile(ver)`, `ListActiveFiles`, `ErrFileConflict`, `ErrFileNotFound` 모두 없음.

- [ ] **Step 3: Write minimal implementation**

`server/internal/db/file.go` 전체 교체:

```go
package db

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrFileConflict = errors.New("file version conflict")
	ErrFileNotFound = errors.New("file not found or tombstoned")
)

type File struct {
	VaultName  string
	Path       string
	Version    int64
	Hash       string
	IsDeleted  bool
	InsertedAt string
	UpdatedAt  string
}

// CreateFile 새 파일 저장. 기존 활성 레코드가 있으면 ErrFileConflict.
// tombstone이 있으면 version+1로 재활용.
func (q *Queries) CreateFile(vaultName, path, hash string) (File, error) {
	tx, err := q.db.Begin()
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback()

	var existing File
	row := tx.QueryRow(
		"SELECT vault_name, path, version, hash, is_deleted, inserted_at, updated_at FROM files WHERE vault_name = ? AND path = ?",
		vaultName, path,
	)
	err = row.Scan(&existing.VaultName, &existing.Path, &existing.Version, &existing.Hash, &existing.IsDeleted, &existing.InsertedAt, &existing.UpdatedAt)

	now := time.Now().UTC().Format(time.RFC3339)

	switch {
	case err == sql.ErrNoRows:
		_, err = tx.Exec(
			"INSERT INTO files (vault_name, path, version, hash, is_deleted, inserted_at, updated_at) VALUES (?, ?, 1, ?, 0, ?, ?)",
			vaultName, path, hash, now, now,
		)
		if err != nil {
			return File{}, err
		}
		if err := tx.Commit(); err != nil {
			return File{}, err
		}
		return File{VaultName: vaultName, Path: path, Version: 1, Hash: hash, InsertedAt: now, UpdatedAt: now}, nil

	case err != nil:
		return File{}, err

	case existing.IsDeleted:
		newVersion := existing.Version + 1
		_, err = tx.Exec(
			"UPDATE files SET version = ?, hash = ?, is_deleted = 0, updated_at = ? WHERE vault_name = ? AND path = ?",
			newVersion, hash, now, vaultName, path,
		)
		if err != nil {
			return File{}, err
		}
		if err := tx.Commit(); err != nil {
			return File{}, err
		}
		return File{VaultName: vaultName, Path: path, Version: newVersion, Hash: hash, InsertedAt: existing.InsertedAt, UpdatedAt: now}, nil

	default:
		return File{}, ErrFileConflict
	}
}

// UpdateFile prevVersion이 DB 현재 version과 일치하면 version+1로 저장.
func (q *Queries) UpdateFile(vaultName, path string, prevVersion int64, newHash string) (File, error) {
	tx, err := q.db.Begin()
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback()

	var existing File
	err = tx.QueryRow(
		"SELECT vault_name, path, version, hash, is_deleted, inserted_at, updated_at FROM files WHERE vault_name = ? AND path = ?",
		vaultName, path,
	).Scan(&existing.VaultName, &existing.Path, &existing.Version, &existing.Hash, &existing.IsDeleted, &existing.InsertedAt, &existing.UpdatedAt)
	if err == sql.ErrNoRows {
		return File{}, ErrFileNotFound
	}
	if err != nil {
		return File{}, err
	}
	if existing.IsDeleted {
		return File{}, ErrFileNotFound
	}
	if existing.Version != prevVersion {
		return File{}, ErrFileConflict
	}

	now := time.Now().UTC().Format(time.RFC3339)
	newVersion := existing.Version + 1
	_, err = tx.Exec(
		"UPDATE files SET version = ?, hash = ?, updated_at = ? WHERE vault_name = ? AND path = ?",
		newVersion, newHash, now, vaultName, path,
	)
	if err != nil {
		return File{}, err
	}
	if err := tx.Commit(); err != nil {
		return File{}, err
	}
	return File{
		VaultName: vaultName, Path: path,
		Version: newVersion, Hash: newHash,
		InsertedAt: existing.InsertedAt, UpdatedAt: now,
	}, nil
}

// DeleteFile prevVersion이 DB 현재 version과 일치하면 tombstone으로 전환.
// 이미 tombstone이거나 레코드가 없으면 ErrFileNotFound.
func (q *Queries) DeleteFile(vaultName, path string, prevVersion int64) (int64, error) {
	tx, err := q.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var existing File
	err = tx.QueryRow(
		"SELECT version, is_deleted FROM files WHERE vault_name = ? AND path = ?",
		vaultName, path,
	).Scan(&existing.Version, &existing.IsDeleted)
	if err == sql.ErrNoRows {
		return 0, ErrFileNotFound
	}
	if err != nil {
		return 0, err
	}
	if existing.IsDeleted {
		return 0, ErrFileNotFound
	}
	if existing.Version != prevVersion {
		return 0, ErrFileConflict
	}

	now := time.Now().UTC().Format(time.RFC3339)
	newVersion := existing.Version + 1
	_, err = tx.Exec(
		"UPDATE files SET version = ?, is_deleted = 1, updated_at = ? WHERE vault_name = ? AND path = ?",
		newVersion, now, vaultName, path,
	)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newVersion, nil
}

// GetFile tombstone 포함 모든 레코드 반환. 없으면 sql.ErrNoRows.
func (q *Queries) GetFile(vaultName, path string) (File, error) {
	var f File
	err := q.db.QueryRow(
		"SELECT vault_name, path, version, hash, is_deleted, inserted_at, updated_at FROM files WHERE vault_name = ? AND path = ?",
		vaultName, path,
	).Scan(&f.VaultName, &f.Path, &f.Version, &f.Hash, &f.IsDeleted, &f.InsertedAt, &f.UpdatedAt)
	return f, err
}

// ListActiveFiles is_deleted=0 레코드만 반환 (대시보드 표시용).
func (q *Queries) ListActiveFiles(vaultName string) ([]File, error) {
	rows, err := q.db.Query(
		"SELECT vault_name, path, version, hash, is_deleted, inserted_at, updated_at FROM files WHERE vault_name = ? AND is_deleted = 0 ORDER BY path",
		vaultName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.VaultName, &f.Path, &f.Version, &f.Hash, &f.IsDeleted, &f.InsertedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// ListAllFiles tombstone 포함 모든 레코드. sync_init 판정에 사용.
func (q *Queries) ListAllFiles(vaultName string) ([]File, error) {
	rows, err := q.db.Query(
		"SELECT vault_name, path, version, hash, is_deleted, inserted_at, updated_at FROM files WHERE vault_name = ? ORDER BY path",
		vaultName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.VaultName, &f.Path, &f.Version, &f.Hash, &f.IsDeleted, &f.InsertedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/db/ -v
```

Expected: PASS (모든 file/vault/token/db 테스트).

- [ ] **Step 5: Commit**

```bash
git add server/internal/db/file.go server/internal/db/file_test.go
git commit -m "feat(db): files CRUD with version/hash optimistic locking"
```

---

## Task 5: GitHubConfig access_token/author 필드 추가

**Files:**
- Modify: `server/internal/db/github_config.go`
- Modify: `server/internal/db/github_config_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/db/github_config_test.go` 전체 교체:

```go
package db

import (
	"testing"
)

func TestSetGitHubConfigFull(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")

	cfg := GitHubConfig{
		VaultName:    "v",
		RemoteURL:    "https://github.com/acme/repo.git",
		Branch:       "main",
		Interval:     "2h",
		AccessToken:  "ghp_abcd1234abcd1234",
		AuthorName:   "Alice",
		AuthorEmail:  "alice@example.com",
		Enabled:      true,
	}
	if err := q.SetGitHubConfig(cfg); err != nil {
		t.Fatalf("SetGitHubConfig: %v", err)
	}

	got, err := q.GetGitHubConfig("v")
	if err != nil {
		t.Fatalf("GetGitHubConfig: %v", err)
	}
	if got.AccessToken != cfg.AccessToken {
		t.Errorf("access_token = %q, want %q", got.AccessToken, cfg.AccessToken)
	}
	if got.AuthorName != cfg.AuthorName {
		t.Errorf("author_name = %q, want %q", got.AuthorName, cfg.AuthorName)
	}
	if got.AuthorEmail != cfg.AuthorEmail {
		t.Errorf("author_email = %q, want %q", got.AuthorEmail, cfg.AuthorEmail)
	}
	if got.RemoteURL != cfg.RemoteURL || got.Branch != cfg.Branch ||
		got.Interval != cfg.Interval || !got.Enabled {
		t.Errorf("other fields mismatch: %+v", got)
	}
}

func TestUpdateGitHubConfigPreservesTokenIfEmpty(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")

	q.SetGitHubConfig(GitHubConfig{
		VaultName: "v", RemoteURL: "u", Branch: "main", Interval: "1h",
		AccessToken: "secret", AuthorName: "A", AuthorEmail: "a@x", Enabled: true,
	})

	// 빈 토큰 업데이트 = 기존 값 유지
	err := q.UpdateGitHubConfigPreservingToken(GitHubConfig{
		VaultName: "v", RemoteURL: "u2", Branch: "main", Interval: "1h",
		AccessToken: "", AuthorName: "B", AuthorEmail: "b@x", Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpdateGitHubConfigPreservingToken: %v", err)
	}

	got, _ := q.GetGitHubConfig("v")
	if got.AccessToken != "secret" {
		t.Errorf("access_token = %q, want secret (preserved)", got.AccessToken)
	}
	if got.AuthorName != "B" {
		t.Errorf("author_name = %q, want B", got.AuthorName)
	}
	if got.RemoteURL != "u2" {
		t.Errorf("remote_url = %q, want u2", got.RemoteURL)
	}
}

func TestDeleteGitHubConfig(t *testing.T) {
	q := newTestQueries(t)
	q.CreateVault("v")
	q.SetGitHubConfig(GitHubConfig{
		VaultName: "v", RemoteURL: "u", Branch: "main", Interval: "1h",
		AccessToken: "t", AuthorName: "n", AuthorEmail: "e@x", Enabled: true,
	})

	if err := q.DeleteGitHubConfig("v"); err != nil {
		t.Fatalf("DeleteGitHubConfig: %v", err)
	}
	_, err := q.GetGitHubConfig("v")
	if err == nil {
		t.Error("expected error after delete")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/db/ -run TestSetGitHubConfigFull -v
```

Expected: FAIL. `AccessToken`, `AuthorName`, `AuthorEmail` 필드 없음. `UpdateGitHubConfigPreservingToken` 없음.

- [ ] **Step 3: Write minimal implementation**

`server/internal/db/github_config.go` 전체 교체:

```go
package db

import (
	"database/sql"
	"time"
)

type GitHubConfig struct {
	VaultName    string
	RemoteURL    string
	Branch       string
	Interval     string
	AccessToken  string
	AuthorName   string
	AuthorEmail  string
	Enabled      bool
	InsertedAt   string
	UpdatedAt    string
}

func (q *Queries) SetGitHubConfig(cfg GitHubConfig) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := q.db.Exec(`
		INSERT INTO github_configs (vault_name, remote_url, branch, interval, access_token, author_name, author_email, enabled, inserted_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (vault_name) DO UPDATE SET
			remote_url = excluded.remote_url,
			branch = excluded.branch,
			interval = excluded.interval,
			access_token = excluded.access_token,
			author_name = excluded.author_name,
			author_email = excluded.author_email,
			enabled = excluded.enabled,
			updated_at = excluded.updated_at`,
		cfg.VaultName, cfg.RemoteURL, cfg.Branch, cfg.Interval,
		cfg.AccessToken, cfg.AuthorName, cfg.AuthorEmail, cfg.Enabled,
		now, now,
	)
	return err
}

// UpdateGitHubConfigPreservingToken AccessToken이 빈 문자열이면 기존 값 유지.
// 대시보드에서 마스킹된 값으로 PUT할 때 사용.
func (q *Queries) UpdateGitHubConfigPreservingToken(cfg GitHubConfig) error {
	if cfg.AccessToken == "" {
		existing, err := q.GetGitHubConfig(cfg.VaultName)
		if err == nil {
			cfg.AccessToken = existing.AccessToken
		}
	}
	return q.SetGitHubConfig(cfg)
}

func (q *Queries) GetGitHubConfig(vaultName string) (GitHubConfig, error) {
	var cfg GitHubConfig
	err := q.db.QueryRow(`
		SELECT vault_name, remote_url, branch, interval, access_token, author_name, author_email, enabled, inserted_at, updated_at
		FROM github_configs WHERE vault_name = ?`,
		vaultName,
	).Scan(
		&cfg.VaultName, &cfg.RemoteURL, &cfg.Branch, &cfg.Interval,
		&cfg.AccessToken, &cfg.AuthorName, &cfg.AuthorEmail, &cfg.Enabled,
		&cfg.InsertedAt, &cfg.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return cfg, sql.ErrNoRows
	}
	return cfg, err
}

func (q *Queries) DeleteGitHubConfig(vaultName string) error {
	_, err := q.db.Exec("DELETE FROM github_configs WHERE vault_name = ?", vaultName)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/db/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/db/github_config.go server/internal/db/github_config_test.go
git commit -m "feat(db): github_configs store access_token and author fields"
```

---

## Task 6: Sync 판정 로직 (classify.go) 신규 구현

**Files:**
- Create: `server/internal/sync/classify.go`
- Create: `server/internal/sync/classify_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/sync/classify_test.go`:

```go
package sync

import "testing"

func TestClassifyNoPrevNoServerWithHashIsUpload(t *testing.T) {
	got := Classify(FileClassifyInput{
		HasPrev:           false,
		ServerExists:      false,
		CurrentClientHash: "h1",
	})
	if got.Action != ActionUpload {
		t.Errorf("action = %v, want upload", got.Action)
	}
}

func TestClassifyNoPrevServerTombstoneIsUpload(t *testing.T) {
	got := Classify(FileClassifyInput{
		HasPrev:           false,
		ServerExists:      true,
		ServerIsDeleted:   true,
		ServerVersion:     3,
		CurrentClientHash: "h1",
	})
	if got.Action != ActionUpload {
		t.Errorf("action = %v, want upload", got.Action)
	}
}

func TestClassifyNoPrevServerActiveSameHashIsUpdateMeta(t *testing.T) {
	got := Classify(FileClassifyInput{
		HasPrev:           false,
		ServerExists:      true,
		ServerVersion:     5,
		ServerHash:        "same",
		CurrentClientHash: "same",
	})
	if got.Action != ActionUpdateMeta {
		t.Errorf("action = %v, want updateMeta", got.Action)
	}
}

func TestClassifyNoPrevServerActiveDifferentHashIsConflict(t *testing.T) {
	got := Classify(FileClassifyInput{
		HasPrev:           false,
		ServerExists:      true,
		ServerVersion:     5,
		ServerHash:        "server",
		CurrentClientHash: "local",
	})
	if got.Action != ActionConflict {
		t.Errorf("action = %v, want conflict", got.Action)
	}
}

func TestClassifyPrevServerMissingIsUpload(t *testing.T) {
	got := Classify(FileClassifyInput{
		HasPrev:           true,
		PrevServerVersion: 3,
		PrevServerHash:    "x",
		ServerExists:      false,
		CurrentClientHash: "h",
	})
	if got.Action != ActionUpload {
		t.Errorf("action = %v, want upload", got.Action)
	}
}

func TestClassifyPrevTombstoneServerAheadIsDelete(t *testing.T) {
	got := Classify(FileClassifyInput{
		HasPrev:           true,
		PrevServerVersion: 3,
		ServerExists:      true,
		ServerIsDeleted:   true,
		ServerVersion:     4,
	})
	if got.Action != ActionDelete {
		t.Errorf("action = %v, want delete", got.Action)
	}
}

func TestClassifyPrevActiveSameVersionSameHashIsSkip(t *testing.T) {
	got := Classify(FileClassifyInput{
		HasPrev:           true,
		PrevServerVersion: 5,
		PrevServerHash:    "h",
		ServerExists:      true,
		ServerVersion:     5,
		ServerHash:        "h",
		CurrentClientHash: "h",
	})
	if got.Action != ActionSkip {
		t.Errorf("action = %v, want skip", got.Action)
	}
}

func TestClassifyPrevActiveSameVersionDifferentHashIsUpdate(t *testing.T) {
	got := Classify(FileClassifyInput{
		HasPrev:           true,
		PrevServerVersion: 5,
		PrevServerHash:    "h",
		ServerExists:      true,
		ServerVersion:     5,
		ServerHash:        "h",
		CurrentClientHash: "h2",
	})
	if got.Action != ActionUpdate {
		t.Errorf("action = %v, want update", got.Action)
	}
}

func TestClassifyPrevBehindSameHashIsUpdateMeta(t *testing.T) {
	got := Classify(FileClassifyInput{
		HasPrev:           true,
		PrevServerVersion: 3,
		PrevServerHash:    "old",
		ServerExists:      true,
		ServerVersion:     5,
		ServerHash:        "new",
		CurrentClientHash: "new",
	})
	if got.Action != ActionUpdateMeta {
		t.Errorf("action = %v, want updateMeta", got.Action)
	}
}

func TestClassifyPrevBehindClientUnchangedIsDownload(t *testing.T) {
	got := Classify(FileClassifyInput{
		HasPrev:           true,
		PrevServerVersion: 3,
		PrevServerHash:    "old",
		ServerExists:      true,
		ServerVersion:     5,
		ServerHash:        "new",
		CurrentClientHash: "old",
	})
	if got.Action != ActionDownload {
		t.Errorf("action = %v, want download", got.Action)
	}
}

func TestClassifyPrevBehindBothDivergedIsConflict(t *testing.T) {
	got := Classify(FileClassifyInput{
		HasPrev:           true,
		PrevServerVersion: 3,
		PrevServerHash:    "old",
		ServerExists:      true,
		ServerVersion:     5,
		ServerHash:        "serverNew",
		CurrentClientHash: "clientNew",
	})
	if got.Action != ActionConflict {
		t.Errorf("action = %v, want conflict", got.Action)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/sync/ -run TestClassify -v
```

Expected: FAIL. `FileClassifyInput`, `Classify`, `Action*` 상수 모두 없음.

- [ ] **Step 3: Write minimal implementation**

`server/internal/sync/classify.go`:

```go
package sync

type Action int

const (
	ActionSkip Action = iota
	ActionUpload
	ActionUpdate
	ActionDownload
	ActionDelete
	ActionUpdateMeta
	ActionConflict
)

func (a Action) String() string {
	return [...]string{"skip", "upload", "update", "download", "delete", "updateMeta", "conflict"}[a]
}

type FileClassifyInput struct {
	HasPrev           bool
	PrevServerVersion int64
	PrevServerHash    string
	ServerExists      bool
	ServerIsDeleted   bool
	ServerVersion     int64
	ServerHash        string
	CurrentClientHash string
}

type FileClassifyOutput struct {
	Action Action
}

// Classify 설계 스펙의 "서버 충돌 감지 판정표" 구현.
func Classify(in FileClassifyInput) FileClassifyOutput {
	if !in.HasPrev {
		switch {
		case !in.ServerExists:
			return FileClassifyOutput{Action: ActionUpload}
		case in.ServerIsDeleted:
			return FileClassifyOutput{Action: ActionUpload}
		case in.ServerHash == in.CurrentClientHash:
			return FileClassifyOutput{Action: ActionUpdateMeta}
		default:
			return FileClassifyOutput{Action: ActionConflict}
		}
	}

	// HasPrev = true
	if !in.ServerExists {
		return FileClassifyOutput{Action: ActionUpload}
	}
	if in.ServerIsDeleted {
		if in.PrevServerVersion <= in.ServerVersion {
			return FileClassifyOutput{Action: ActionDelete}
		}
		return FileClassifyOutput{Action: ActionUpload}
	}

	// 활성 서버 레코드
	if in.PrevServerVersion == in.ServerVersion {
		if in.CurrentClientHash == in.ServerHash {
			return FileClassifyOutput{Action: ActionSkip}
		}
		return FileClassifyOutput{Action: ActionUpdate}
	}

	// prev.v < server.v (서버가 더 앞섬)
	if in.PrevServerVersion < in.ServerVersion {
		if in.CurrentClientHash == in.ServerHash {
			return FileClassifyOutput{Action: ActionUpdateMeta}
		}
		if in.PrevServerHash == in.CurrentClientHash {
			return FileClassifyOutput{Action: ActionDownload}
		}
		return FileClassifyOutput{Action: ActionConflict}
	}

	// prev.v > server.v : 서버 DB 유실 상태. 안전하게 업로드로 취급.
	return FileClassifyOutput{Action: ActionUpload}
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/sync/ -run TestClassify -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/sync/classify.go server/internal/sync/classify_test.go
git commit -m "feat(sync): classify optimistic locking judgment table"
```

---

## Task 7: 기존 conflict.go 축소 (makeConflictPath 유지, mtime 로직 제거)

**Files:**
- Modify: `server/internal/sync/conflict.go`
- Modify: `server/internal/sync/conflict_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/sync/conflict_test.go` 전체 교체:

```go
package sync

import (
	"regexp"
	"strings"
	"testing"
)

func TestMakeConflictPathKeepsExtension(t *testing.T) {
	got := MakeConflictPath("notes/hello.md")
	if !strings.HasPrefix(got, "notes/hello.conflict-") {
		t.Errorf("prefix missing: %s", got)
	}
	if !strings.HasSuffix(got, ".md") {
		t.Errorf("extension missing: %s", got)
	}
}

func TestMakeConflictPathNoExtension(t *testing.T) {
	got := MakeConflictPath("notes/noext")
	if !strings.HasPrefix(got, "notes/noext.conflict-") {
		t.Errorf("prefix wrong: %s", got)
	}
	if strings.Contains(got[len("notes/noext.conflict-"):], ".") {
		t.Errorf("unexpected dot in suffix: %s", got)
	}
}

func TestMakeConflictPathTimestampFormat(t *testing.T) {
	got := MakeConflictPath("a.txt")
	re := regexp.MustCompile(`\.conflict-\d{8}T\d{6}Z`)
	if !re.MatchString(got) {
		t.Errorf("timestamp format: %s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/sync/ -run TestMakeConflictPath -v
```

Expected: FAIL. 현재 함수는 `makeConflictPath` (비공개). 공개 함수 `MakeConflictPath` 없음.

- [ ] **Step 3: Write minimal implementation**

`server/internal/sync/conflict.go` 전체 교체:

```go
package sync

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// MakeConflictPath "notes/hello.md" → "notes/hello.conflict-20260422T114750Z.md"
func MakeConflictPath(path string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	ts := time.Now().UTC().Format("20060102T150405Z")
	if ext == "" {
		return fmt.Sprintf("%s.conflict-%s", base, ts)
	}
	return fmt.Sprintf("%s.conflict-%s%s", base, ts, ext)
}
```

`CheckCreateConflict`, `CheckUpdateConflict`, `makeConflictPath` (소문자), `ConflictResult` 전부 삭제.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/sync/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/sync/conflict.go server/internal/sync/conflict_test.go
git commit -m "refactor(sync): remove mtime-based conflict helpers, keep path builder"
```

---

## Task 8: 신규 WebSocket 메시지 타입 정의

**Files:**
- Modify: `server/internal/ws/messages.go`
- Create: `server/internal/ws/messages_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/ws/messages_test.go`:

```go
package ws

import (
	"encoding/json"
	"testing"
)

func TestUnmarshalSyncInit(t *testing.T) {
	raw := []byte(`{"type":"sync_init","vault":"v","files":[
		{"path":"a.md","prevServerVersion":5,"prevServerHash":"abc","currentClientHash":"xyz"},
		{"path":"b.md","currentClientHash":"aaa"}
	]}`)
	msg, err := UnmarshalMessage(raw)
	if err != nil {
		t.Fatalf("UnmarshalMessage: %v", err)
	}
	if msg.Type != "sync_init" {
		t.Errorf("type = %q", msg.Type)
	}
	if len(msg.Files) != 2 {
		t.Fatalf("files len = %d", len(msg.Files))
	}
	if msg.Files[0].PrevServerVersion == nil || *msg.Files[0].PrevServerVersion != 5 {
		t.Errorf("PrevServerVersion mismatch")
	}
	if msg.Files[1].PrevServerVersion != nil {
		t.Errorf("PrevServerVersion should be nil for fresh entry")
	}
}

func TestUnmarshalFileUpdate(t *testing.T) {
	raw := []byte(`{"type":"file_update","vault":"v","path":"a.md","content":"body",
		"prevServerVersion":5,"currentClientHash":"h"}`)
	msg, _ := UnmarshalMessage(raw)
	if msg.PrevServerVersion == nil || *msg.PrevServerVersion != 5 {
		t.Error("prevServerVersion not parsed")
	}
	if msg.CurrentClientHash != "h" {
		t.Errorf("currentClientHash = %q", msg.CurrentClientHash)
	}
}

func TestMarshalSyncResultOmitsEmpty(t *testing.T) {
	msg := OutgoingMessage{
		Type:     "sync_result",
		Vault:    "v",
		ToUpload: []string{"a.md"},
	}
	data, err := MarshalMessage(msg)
	if err != nil {
		t.Fatalf("MarshalMessage: %v", err)
	}
	var got map[string]any
	json.Unmarshal(data, &got)
	if _, ok := got["toDownload"]; ok {
		t.Error("toDownload should be omitted when empty")
	}
	if _, ok := got["conflicts"]; ok {
		t.Error("conflicts should be omitted when empty")
	}
}

func TestMarshalConflictEntry(t *testing.T) {
	msg := OutgoingMessage{
		Type:  "sync_result",
		Vault: "v",
		Conflicts: []ConflictEntry{{
			Path:                 "a.md",
			CurrentServerVersion: 7,
			CurrentServerHash:    "sh",
			CurrentServerContent: "body",
		}},
	}
	data, _ := MarshalMessage(msg)
	var got struct {
		Conflicts []ConflictEntry `json:"conflicts"`
	}
	json.Unmarshal(data, &got)
	if len(got.Conflicts) != 1 {
		t.Fatalf("conflicts missing")
	}
	if got.Conflicts[0].CurrentServerVersion != 7 {
		t.Errorf("currentServerVersion = %d", got.Conflicts[0].CurrentServerVersion)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ws/ -run "TestUnmarshalSyncInit|TestUnmarshalFileUpdate|TestMarshalSyncResultOmitsEmpty|TestMarshalConflictEntry" -v
```

Expected: FAIL. 새 필드(`PrevServerVersion`, `CurrentClientHash` 등)와 `ConflictEntry` 타입 모두 없음.

- [ ] **Step 3: Write minimal implementation**

`server/internal/ws/messages.go` 전체 교체:

```go
package ws

import "encoding/json"

// ClientFileEntry sync_init에 포함되는 파일 메타.
// prev* 필드는 선택 (fresh install에서 누락 가능).
type ClientFileEntry struct {
	Path              string `json:"path"`
	PrevServerVersion *int64 `json:"prevServerVersion,omitempty"`
	PrevServerHash    string `json:"prevServerHash,omitempty"`
	CurrentClientHash string `json:"currentClientHash,omitempty"`
}

// ServerFileEntry 서버 → 클라 전송 파일 (content 포함).
type ServerFileEntry struct {
	Path                 string `json:"path"`
	Content              string `json:"content,omitempty"`
	Encoding             string `json:"encoding,omitempty"`
	CurrentServerVersion int64  `json:"currentServerVersion"`
	CurrentServerHash    string `json:"currentServerHash"`
}

// MetaEntry 메타만 업데이트.
type MetaEntry struct {
	Path                 string `json:"path"`
	CurrentServerVersion int64  `json:"currentServerVersion"`
	CurrentServerHash    string `json:"currentServerHash"`
}

// ConflictEntry 클라에 내려주는 충돌 정보.
type ConflictEntry struct {
	Path                 string `json:"path"`
	PrevServerVersion    *int64 `json:"prevServerVersion,omitempty"`
	CurrentClientHash    string `json:"currentClientHash,omitempty"`
	CurrentServerVersion int64  `json:"currentServerVersion"`
	CurrentServerHash    string `json:"currentServerHash"`
	CurrentServerContent string `json:"currentServerContent,omitempty"`
	Encoding             string `json:"encoding,omitempty"`
	Kind                 string `json:"kind,omitempty"` // "modify" | "delete"
}

// IncomingMessage 공용 입력 구조.
// 필드가 메시지 타입마다 다르지만 JSON 역직렬화 편의를 위해 하나로 합침.
type IncomingMessage struct {
	Type  string `json:"type"`
	Vault string `json:"vault,omitempty"`

	// sync_init
	Files []ClientFileEntry `json:"files,omitempty"`

	// 단건 메시지 공용
	Path     string `json:"path,omitempty"`
	Content  string `json:"content,omitempty"`
	Encoding string `json:"encoding,omitempty"`

	PrevServerVersion *int64 `json:"prevServerVersion,omitempty"`
	PrevServerHash    string `json:"prevServerHash,omitempty"`
	CurrentClientHash string `json:"currentClientHash,omitempty"`

	// conflict_resolve
	Resolution string `json:"resolution,omitempty"` // "local" (server는 클라 로컬 처리)
	Action     string `json:"action,omitempty"`     // "delete" (삭제 충돌 해소용)
}

// OutgoingMessage 공용 출력 구조.
type OutgoingMessage struct {
	Type  string `json:"type"`
	Vault string `json:"vault,omitempty"`
	Path  string `json:"path,omitempty"`

	// sync_result
	ToUpload     []string          `json:"toUpload,omitempty"`
	ToUpdate     []string          `json:"toUpdate,omitempty"`
	ToDownload   []ServerFileEntry `json:"toDownload,omitempty"`
	ToDelete     []string          `json:"toDelete,omitempty"`
	ToUpdateMeta []MetaEntry       `json:"toUpdateMeta,omitempty"`
	Conflicts    []ConflictEntry   `json:"conflicts,omitempty"`

	// file_check_result
	Action               string `json:"action,omitempty"`
	Content              string `json:"content,omitempty"`
	Encoding             string `json:"encoding,omitempty"`
	CurrentServerVersion int64  `json:"currentServerVersion,omitempty"`
	CurrentServerHash    string `json:"currentServerHash,omitempty"`

	// file_*_result
	Ok       bool           `json:"ok,omitempty"`
	Noop     bool           `json:"noop,omitempty"`
	Conflict *ConflictEntry `json:"conflict,omitempty"`

	Error string `json:"error,omitempty"`
}

func MarshalMessage(msg OutgoingMessage) ([]byte, error) {
	return json.Marshal(msg)
}

func UnmarshalMessage(data []byte) (IncomingMessage, error) {
	var msg IncomingMessage
	err := json.Unmarshal(data, &msg)
	return msg, err
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ws/ -run "TestUnmarshal|TestMarshal" -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/ws/messages.go server/internal/ws/messages_test.go
git commit -m "feat(ws): redefine messages for optimistic locking protocol"
```

---

## Task 9: WebSocket handler — sync_init 재작성

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

> 이 태스크에서 `handler.go`를 부분 재작성. 기존 `vault_create`, `file_upload` 브랜치 포함 파일 전체를 날리고 새 구조로 시작. 이후 태스크에서 각 메시지 타입 추가.

- [ ] **Step 1: Write the failing test**

`server/internal/ws/handler_test.go` 전체 교체:

```go
package ws

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"obsidian-goat-sync/internal/db"
	"obsidian-goat-sync/internal/storage"
)

type fakeClient struct {
	sent []OutgoingMessage
}

func (c *fakeClient) Send(msg OutgoingMessage) { c.sent = append(c.sent, msg) }
func (c *fakeClient) SetVault(v string)        {}
func (c *fakeClient) Vault() string            { return "" }

func newTestHandler(t *testing.T) (*Handler, *db.Queries, *storage.Storage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sync.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	q := db.NewQueries(database)
	s := storage.New(t.TempDir())
	q.CreateVault("v")
	h := NewHandler(q, s)
	return h, q, s
}

func dispatch(h *Handler, c ClientSender, msg any) {
	data, _ := json.Marshal(msg)
	h.HandleMessage(c, data)
}

func findSent(c *fakeClient, typ string) *OutgoingMessage {
	for i := range c.sent {
		if c.sent[i].Type == typ {
			return &c.sent[i]
		}
	}
	return nil
}

func TestSyncInitEmptyServerUploadsAll(t *testing.T) {
	h, _, _ := newTestHandler(t)
	c := &fakeClient{}

	dispatch(h, c, map[string]any{
		"type":  "sync_init",
		"vault": "v",
		"files": []map[string]any{
			{"path": "a.md", "currentClientHash": "h1"},
			{"path": "b.md", "currentClientHash": "h2"},
		},
	})

	out := findSent(c, "sync_result")
	if out == nil {
		t.Fatal("sync_result missing")
	}
	if len(out.ToUpload) != 2 {
		t.Errorf("toUpload = %v, want 2 entries", out.ToUpload)
	}
}

func TestSyncInitServerAheadReturnsDownload(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "serverhash")
	s.WriteFile("v", "a.md", []byte("server body"))

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type":  "sync_init",
		"vault": "v",
		"files": []map[string]any{
			{"path": "a.md", "prevServerVersion": prev, "prevServerHash": "serverhash", "currentClientHash": "serverhash"},
		},
	})
	out := findSent(c, "sync_result")
	if out == nil {
		t.Fatal("sync_result missing")
	}
	// prev==server && hash==server → skip
	if len(out.ToUpload) != 0 || len(out.ToDownload) != 0 || len(out.Conflicts) != 0 {
		t.Errorf("expected skip, got %+v", out)
	}

	// 두 번째 케이스: 서버가 앞서있음 + 로컬 unchanged
	q.UpdateFile("v", "a.md", 1, "serverhash2")
	s.WriteFile("v", "a.md", []byte("server body v2"))
	c2 := &fakeClient{}
	dispatch(h, c2, map[string]any{
		"type":  "sync_init",
		"vault": "v",
		"files": []map[string]any{
			{"path": "a.md", "prevServerVersion": prev, "prevServerHash": "serverhash", "currentClientHash": "serverhash"},
		},
	})
	out2 := findSent(c2, "sync_result")
	if len(out2.ToDownload) != 1 {
		t.Fatalf("toDownload len = %d", len(out2.ToDownload))
	}
	if out2.ToDownload[0].CurrentServerVersion != 2 {
		t.Errorf("version = %d, want 2", out2.ToDownload[0].CurrentServerVersion)
	}
}

func TestSyncInitHashDivergedIsConflict(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "serverH")
	s.WriteFile("v", "a.md", []byte("server body"))
	q.UpdateFile("v", "a.md", 1, "serverH2")
	s.WriteFile("v", "a.md", []byte("server body v2"))

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type":  "sync_init",
		"vault": "v",
		"files": []map[string]any{
			{"path": "a.md", "prevServerVersion": prev, "prevServerHash": "oldH", "currentClientHash": "localH"},
		},
	})
	out := findSent(c, "sync_result")
	if len(out.Conflicts) != 1 {
		t.Fatalf("conflicts len = %d", len(out.Conflicts))
	}
	if out.Conflicts[0].CurrentServerVersion != 2 {
		t.Errorf("conflict version = %d", out.Conflicts[0].CurrentServerVersion)
	}
}

func TestSyncInitServerOnlyFileIsDownload(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "onlyserver.md", "h")
	s.WriteFile("v", "onlyserver.md", []byte("body"))

	c := &fakeClient{}
	dispatch(h, c, map[string]any{
		"type":  "sync_init",
		"vault": "v",
		"files": []map[string]any{},
	})
	out := findSent(c, "sync_result")
	if len(out.ToDownload) != 1 {
		t.Fatalf("toDownload len = %d", len(out.ToDownload))
	}
}

func TestSyncInitServerTombstoneIsToDelete(t *testing.T) {
	h, q, _ := newTestHandler(t)
	q.CreateFile("v", "gone.md", "h")
	q.DeleteFile("v", "gone.md", 1)

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type":  "sync_init",
		"vault": "v",
		"files": []map[string]any{
			{"path": "gone.md", "prevServerVersion": prev, "prevServerHash": "h", "currentClientHash": "h"},
		},
	})
	out := findSent(c, "sync_result")
	if len(out.ToDelete) != 1 || out.ToDelete[0] != "gone.md" {
		t.Errorf("toDelete = %v", out.ToDelete)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ws/ -run TestSyncInit -v
```

Expected: FAIL. `NewHandler` 시그니처 변경 필요 (hub 제거), `ClientSender` 인터페이스 없음, `sync_init` 재분류 로직 없음.

- [ ] **Step 3: Write minimal implementation**

`server/internal/ws/handler.go` 전체 교체:

```go
package ws

import (
	"database/sql"
	"encoding/base64"
	"log"

	"obsidian-goat-sync/internal/db"
	"obsidian-goat-sync/internal/storage"
	syncpkg "obsidian-goat-sync/internal/sync"
)

// ClientSender handler가 의존하는 최소 인터페이스 (테스트에서 fake 주입).
type ClientSender interface {
	Send(OutgoingMessage)
	SetVault(string)
	Vault() string
}

type Handler struct {
	queries *db.Queries
	storage *storage.Storage
}

func NewHandler(q *db.Queries, s *storage.Storage) *Handler {
	return &Handler{queries: q, storage: s}
}

func (h *Handler) HandleMessage(client ClientSender, data []byte) {
	msg, err := UnmarshalMessage(data)
	if err != nil {
		log.Printf("ws: parse failed: %v", err)
		return
	}
	switch msg.Type {
	case "sync_init":
		h.handleSyncInit(client, msg)
	// 이후 태스크에서 추가:
	// "file_check", "file_create", "file_update", "file_delete", "conflict_resolve"
	default:
		log.Printf("ws: unknown type %s", msg.Type)
	}
}

func (h *Handler) handleSyncInit(client ClientSender, msg IncomingMessage) {
	client.SetVault(msg.Vault)

	serverFiles, err := h.queries.ListAllFiles(msg.Vault)
	if err != nil {
		client.Send(OutgoingMessage{Type: "sync_result", Error: err.Error()})
		return
	}
	serverMap := make(map[string]db.File, len(serverFiles))
	for _, f := range serverFiles {
		serverMap[f.Path] = f
	}

	clientMap := make(map[string]ClientFileEntry, len(msg.Files))
	for _, cf := range msg.Files {
		clientMap[cf.Path] = cf
	}

	out := OutgoingMessage{Type: "sync_result", Vault: msg.Vault}

	for _, cf := range msg.Files {
		in := syncpkg.FileClassifyInput{
			HasPrev:           cf.PrevServerVersion != nil,
			PrevServerHash:    cf.PrevServerHash,
			CurrentClientHash: cf.CurrentClientHash,
		}
		if cf.PrevServerVersion != nil {
			in.PrevServerVersion = *cf.PrevServerVersion
		}
		if sf, ok := serverMap[cf.Path]; ok {
			in.ServerExists = true
			in.ServerIsDeleted = sf.IsDeleted
			in.ServerVersion = sf.Version
			in.ServerHash = sf.Hash
		}

		res := syncpkg.Classify(in)
		switch res.Action {
		case syncpkg.ActionSkip:
			continue
		case syncpkg.ActionUpload:
			out.ToUpload = append(out.ToUpload, cf.Path)
		case syncpkg.ActionUpdate:
			out.ToUpdate = append(out.ToUpdate, cf.Path)
		case syncpkg.ActionDownload:
			entry, ok := h.buildServerEntry(msg.Vault, serverMap[cf.Path])
			if ok {
				out.ToDownload = append(out.ToDownload, entry)
			}
		case syncpkg.ActionDelete:
			out.ToDelete = append(out.ToDelete, cf.Path)
		case syncpkg.ActionUpdateMeta:
			sf := serverMap[cf.Path]
			out.ToUpdateMeta = append(out.ToUpdateMeta, MetaEntry{
				Path:                 cf.Path,
				CurrentServerVersion: sf.Version,
				CurrentServerHash:    sf.Hash,
			})
		case syncpkg.ActionConflict:
			entry, ok := h.buildConflictEntry(msg.Vault, serverMap[cf.Path], cf)
			if ok {
				out.Conflicts = append(out.Conflicts, entry)
			}
		}
	}

	// 서버만 가지고 있는 활성 파일 → 다운로드
	for _, sf := range serverFiles {
		if sf.IsDeleted {
			continue
		}
		if _, sent := clientMap[sf.Path]; sent {
			continue
		}
		entry, ok := h.buildServerEntry(msg.Vault, sf)
		if ok {
			out.ToDownload = append(out.ToDownload, entry)
		}
	}

	client.Send(out)
}

func (h *Handler) buildServerEntry(vault string, sf db.File) (ServerFileEntry, bool) {
	content, err := h.storage.ReadFile(vault, sf.Path)
	if err != nil {
		log.Printf("ws: read %s/%s failed: %v", vault, sf.Path, err)
		return ServerFileEntry{}, false
	}
	entry := ServerFileEntry{
		Path:                 sf.Path,
		CurrentServerVersion: sf.Version,
		CurrentServerHash:    sf.Hash,
	}
	if isBinary(content) {
		entry.Content = base64.StdEncoding.EncodeToString(content)
		entry.Encoding = "base64"
	} else {
		entry.Content = string(content)
	}
	return entry, true
}

func (h *Handler) buildConflictEntry(vault string, sf db.File, cf ClientFileEntry) (ConflictEntry, bool) {
	content, err := h.storage.ReadFile(vault, sf.Path)
	entry := ConflictEntry{
		Path:                 sf.Path,
		PrevServerVersion:    cf.PrevServerVersion,
		CurrentClientHash:    cf.CurrentClientHash,
		CurrentServerVersion: sf.Version,
		CurrentServerHash:    sf.Hash,
		Kind:                 "modify",
	}
	if err != nil {
		log.Printf("ws: read %s/%s failed: %v", vault, sf.Path, err)
		return entry, true // content 없이도 충돌 통보
	}
	if isBinary(content) {
		entry.CurrentServerContent = base64.StdEncoding.EncodeToString(content)
		entry.Encoding = "base64"
	} else {
		entry.CurrentServerContent = string(content)
	}
	return entry, true
}

func decodeContent(content, encoding string) []byte {
	if encoding == "base64" {
		data, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return []byte(content)
		}
		return data
	}
	return []byte(content)
}

func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// 향후 file_check 등에서 사용할 보조 함수들은 동일 패키지에 확장.
var _ = sql.ErrNoRows
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ws/ -run TestSyncInit -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/ws/handler.go server/internal/ws/handler_test.go
git commit -m "feat(ws): rewrite sync_init using classify"
```

---

## Task 10: WebSocket handler — file_check 추가

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/ws/handler_test.go` 파일 끝에 추가:

```go
func TestFileCheckUpToDate(t *testing.T) {
	h, q, _ := newTestHandler(t)
	q.CreateFile("v", "a.md", "h")

	c := &fakeClient{}
	c.SetVault("v")
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_check", "vault": "v", "path": "a.md",
		"prevServerVersion": prev, "prevServerHash": "h", "currentClientHash": "h",
	})
	out := findSent(c, "file_check_result")
	if out == nil || out.Action != "up-to-date" {
		t.Errorf("action = %v", out)
	}
}

func TestFileCheckDownload(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	q.UpdateFile("v", "a.md", 1, "h2")
	s.WriteFile("v", "a.md", []byte("server v2"))

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_check", "vault": "v", "path": "a.md",
		"prevServerVersion": prev, "prevServerHash": "h1", "currentClientHash": "h1",
	})
	out := findSent(c, "file_check_result")
	if out.Action != "download" {
		t.Errorf("action = %q", out.Action)
	}
	if out.CurrentServerVersion != 2 {
		t.Errorf("version = %d", out.CurrentServerVersion)
	}
	if out.Content != "server v2" {
		t.Errorf("content = %q", out.Content)
	}
}

func TestFileCheckConflict(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	q.UpdateFile("v", "a.md", 1, "h2")
	s.WriteFile("v", "a.md", []byte("server v2"))

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_check", "vault": "v", "path": "a.md",
		"prevServerVersion": prev, "prevServerHash": "h1", "currentClientHash": "localChanged",
	})
	out := findSent(c, "file_check_result")
	if out.Action != "conflict" {
		t.Errorf("action = %q", out.Action)
	}
	if out.Content != "server v2" {
		t.Errorf("content = %q", out.Content)
	}
}

func TestFileCheckDeleted(t *testing.T) {
	h, q, _ := newTestHandler(t)
	q.CreateFile("v", "a.md", "h")
	q.DeleteFile("v", "a.md", 1)

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_check", "vault": "v", "path": "a.md",
		"prevServerVersion": prev, "prevServerHash": "h", "currentClientHash": "h",
	})
	out := findSent(c, "file_check_result")
	if out.Action != "deleted" {
		t.Errorf("action = %q", out.Action)
	}
}

func TestFileCheckUpdateMeta(t *testing.T) {
	h, q, _ := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	q.UpdateFile("v", "a.md", 1, "h2")

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_check", "vault": "v", "path": "a.md",
		"prevServerVersion": prev, "prevServerHash": "h1", "currentClientHash": "h2",
	})
	out := findSent(c, "file_check_result")
	if out.Action != "update-meta" {
		t.Errorf("action = %q", out.Action)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ws/ -run TestFileCheck -v
```

Expected: FAIL. `file_check` 분기 없음.

- [ ] **Step 3: Write minimal implementation**

`server/internal/ws/handler.go`의 `HandleMessage`에 `case "file_check"` 추가 + 새 메서드:

```go
func (h *Handler) HandleMessage(client ClientSender, data []byte) {
	msg, err := UnmarshalMessage(data)
	if err != nil {
		log.Printf("ws: parse failed: %v", err)
		return
	}
	switch msg.Type {
	case "sync_init":
		h.handleSyncInit(client, msg)
	case "file_check":
		h.handleFileCheck(client, msg)
	default:
		log.Printf("ws: unknown type %s", msg.Type)
	}
}

func (h *Handler) handleFileCheck(client ClientSender, msg IncomingMessage) {
	sf, err := h.queries.GetFile(msg.Vault, msg.Path)
	in := syncpkg.FileClassifyInput{
		HasPrev:           msg.PrevServerVersion != nil,
		PrevServerHash:    msg.PrevServerHash,
		CurrentClientHash: msg.CurrentClientHash,
	}
	if msg.PrevServerVersion != nil {
		in.PrevServerVersion = *msg.PrevServerVersion
	}
	if err == nil {
		in.ServerExists = true
		in.ServerIsDeleted = sf.IsDeleted
		in.ServerVersion = sf.Version
		in.ServerHash = sf.Hash
	}

	res := syncpkg.Classify(in)
	out := OutgoingMessage{Type: "file_check_result", Vault: msg.Vault, Path: msg.Path}

	switch res.Action {
	case syncpkg.ActionSkip:
		out.Action = "up-to-date"
	case syncpkg.ActionUpdateMeta:
		out.Action = "update-meta"
		out.CurrentServerVersion = sf.Version
		out.CurrentServerHash = sf.Hash
	case syncpkg.ActionDownload:
		out.Action = "download"
		if entry, ok := h.buildServerEntry(msg.Vault, sf); ok {
			out.Content = entry.Content
			out.Encoding = entry.Encoding
			out.CurrentServerVersion = entry.CurrentServerVersion
			out.CurrentServerHash = entry.CurrentServerHash
		}
	case syncpkg.ActionConflict:
		out.Action = "conflict"
		if entry, ok := h.buildServerEntry(msg.Vault, sf); ok {
			out.Content = entry.Content
			out.Encoding = entry.Encoding
			out.CurrentServerVersion = entry.CurrentServerVersion
			out.CurrentServerHash = entry.CurrentServerHash
		}
	case syncpkg.ActionDelete:
		out.Action = "deleted"
		out.CurrentServerVersion = sf.Version
	case syncpkg.ActionUpload:
		// file_check에서 upload가 나올 경우: 서버에 없음. 클라가 file_create로 처리하면 됨.
		out.Action = "up-to-date"
	case syncpkg.ActionUpdate:
		// 로컬만 변경됨. 클라가 file_update로 올릴 거라 알림만.
		out.Action = "up-to-date"
	}
	client.Send(out)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ws/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/ws/handler.go server/internal/ws/handler_test.go
git commit -m "feat(ws): handle file_check message"
```

---

## Task 11: WebSocket handler — file_create 추가

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/ws/handler_test.go` 파일 끝에 추가:

```go
func TestFileCreateNew(t *testing.T) {
	h, q, s := newTestHandler(t)

	c := &fakeClient{}
	dispatch(h, c, map[string]any{
		"type": "file_create", "vault": "v", "path": "a.md",
		"content": "hello", "currentClientHash": "h1",
	})
	out := findSent(c, "file_create_result")
	if out == nil || !out.Ok {
		t.Fatalf("file_create_result = %+v", out)
	}
	if out.CurrentServerVersion != 1 {
		t.Errorf("version = %d", out.CurrentServerVersion)
	}
	if out.CurrentServerHash != "h1" {
		t.Errorf("hash = %q", out.CurrentServerHash)
	}
	body, _ := s.ReadFile("v", "a.md")
	if string(body) != "hello" {
		t.Errorf("disk = %q", string(body))
	}
	f, _ := q.GetFile("v", "a.md")
	if f.Hash != "h1" {
		t.Errorf("db hash = %q", f.Hash)
	}
}

func TestFileCreateConflictOnActive(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "serverH")
	s.WriteFile("v", "a.md", []byte("server body"))

	c := &fakeClient{}
	dispatch(h, c, map[string]any{
		"type": "file_create", "vault": "v", "path": "a.md",
		"content": "client body", "currentClientHash": "clientH",
	})
	out := findSent(c, "file_create_result")
	if out.Ok {
		t.Error("expected ok=false")
	}
	if out.Conflict == nil {
		t.Fatal("conflict missing")
	}
	if out.Conflict.CurrentServerVersion != 1 {
		t.Errorf("conflict version = %d", out.Conflict.CurrentServerVersion)
	}
	if out.Conflict.CurrentServerContent != "server body" {
		t.Errorf("conflict content = %q", out.Conflict.CurrentServerContent)
	}
}

func TestFileCreateOnTombstoneReuses(t *testing.T) {
	h, q, _ := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	q.DeleteFile("v", "a.md", 1)

	c := &fakeClient{}
	dispatch(h, c, map[string]any{
		"type": "file_create", "vault": "v", "path": "a.md",
		"content": "new", "currentClientHash": "h2",
	})
	out := findSent(c, "file_create_result")
	if !out.Ok {
		t.Fatalf("expected ok=true, got %+v", out)
	}
	if out.CurrentServerVersion != 3 {
		t.Errorf("version = %d, want 3", out.CurrentServerVersion)
	}
}

func TestFileCreateBinary(t *testing.T) {
	h, _, s := newTestHandler(t)
	c := &fakeClient{}
	raw := []byte{0x00, 0x01, 0x02}
	b64 := base64.StdEncoding.EncodeToString(raw)
	dispatch(h, c, map[string]any{
		"type": "file_create", "vault": "v", "path": "bin",
		"content": b64, "encoding": "base64", "currentClientHash": "h",
	})
	out := findSent(c, "file_create_result")
	if !out.Ok {
		t.Fatalf("ok=false: %+v", out)
	}
	body, _ := s.ReadFile("v", "bin")
	if string(body) != string(raw) {
		t.Errorf("disk mismatch")
	}
}
```

`handler_test.go` 상단 import에 `"encoding/base64"` 추가.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ws/ -run TestFileCreate -v
```

Expected: FAIL. `file_create` 분기 없음.

- [ ] **Step 3: Write minimal implementation**

`HandleMessage`에 `case "file_create"` 추가 + 새 메서드:

```go
func (h *Handler) HandleMessage(client ClientSender, data []byte) {
	msg, err := UnmarshalMessage(data)
	if err != nil {
		log.Printf("ws: parse failed: %v", err)
		return
	}
	switch msg.Type {
	case "sync_init":
		h.handleSyncInit(client, msg)
	case "file_check":
		h.handleFileCheck(client, msg)
	case "file_create":
		h.handleFileCreate(client, msg)
	default:
		log.Printf("ws: unknown type %s", msg.Type)
	}
}

func (h *Handler) handleFileCreate(client ClientSender, msg IncomingMessage) {
	out := OutgoingMessage{Type: "file_create_result", Vault: msg.Vault, Path: msg.Path}

	content := decodeContent(msg.Content, msg.Encoding)
	f, err := h.queries.CreateFile(msg.Vault, msg.Path, msg.CurrentClientHash)
	if err == db.ErrFileConflict {
		existing, _ := h.queries.GetFile(msg.Vault, msg.Path)
		entry, _ := h.buildServerEntry(msg.Vault, existing)
		out.Ok = false
		out.Conflict = &ConflictEntry{
			Path:                 msg.Path,
			CurrentClientHash:    msg.CurrentClientHash,
			CurrentServerVersion: existing.Version,
			CurrentServerHash:    existing.Hash,
			CurrentServerContent: entry.Content,
			Encoding:             entry.Encoding,
			Kind:                 "modify",
		}
		client.Send(out)
		return
	}
	if err != nil {
		out.Error = err.Error()
		client.Send(out)
		return
	}

	if err := h.storage.WriteFile(msg.Vault, msg.Path, content); err != nil {
		out.Error = err.Error()
		client.Send(out)
		return
	}

	out.Ok = true
	out.CurrentServerVersion = f.Version
	out.CurrentServerHash = f.Hash
	client.Send(out)
}
```

`handler.go` 상단 import에 `"obsidian-goat-sync/internal/db"`가 이미 있음. `db.ErrFileConflict` 참조.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ws/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/ws/handler.go server/internal/ws/handler_test.go
git commit -m "feat(ws): handle file_create with optimistic locking"
```

---

## Task 12: WebSocket handler — file_update 추가

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/ws/handler_test.go` 파일 끝에 추가:

```go
func TestFileUpdateSuccess(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	s.WriteFile("v", "a.md", []byte("v1"))

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_update", "vault": "v", "path": "a.md",
		"content": "v2", "prevServerVersion": prev, "currentClientHash": "h2",
	})
	out := findSent(c, "file_update_result")
	if !out.Ok {
		t.Fatalf("ok=false: %+v", out)
	}
	if out.CurrentServerVersion != 2 {
		t.Errorf("version = %d", out.CurrentServerVersion)
	}
	body, _ := s.ReadFile("v", "a.md")
	if string(body) != "v2" {
		t.Errorf("disk = %q", string(body))
	}
}

func TestFileUpdateNoopSameHash(t *testing.T) {
	h, q, _ := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_update", "vault": "v", "path": "a.md",
		"content": "body", "prevServerVersion": prev, "currentClientHash": "h1",
	})
	out := findSent(c, "file_update_result")
	if !out.Ok || !out.Noop {
		t.Errorf("want ok=true, noop=true, got %+v", out)
	}
	if out.CurrentServerVersion != 1 {
		t.Errorf("version = %d, want 1", out.CurrentServerVersion)
	}
}

func TestFileUpdateConflictStaleVersion(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	q.UpdateFile("v", "a.md", 1, "h2")
	s.WriteFile("v", "a.md", []byte("server v2"))

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_update", "vault": "v", "path": "a.md",
		"content": "local body", "prevServerVersion": prev, "currentClientHash": "localH",
	})
	out := findSent(c, "file_update_result")
	if out.Ok {
		t.Error("expected ok=false")
	}
	if out.Conflict == nil || out.Conflict.CurrentServerVersion != 2 {
		t.Errorf("conflict wrong: %+v", out.Conflict)
	}
	if out.Conflict.CurrentServerContent != "server v2" {
		t.Errorf("conflict content = %q", out.Conflict.CurrentServerContent)
	}
}

func TestFileUpdateOnMissingFile(t *testing.T) {
	h, _, _ := newTestHandler(t)
	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_update", "vault": "v", "path": "missing.md",
		"content": "x", "prevServerVersion": prev, "currentClientHash": "h",
	})
	out := findSent(c, "file_update_result")
	if out.Ok {
		t.Error("expected ok=false on missing")
	}
	if out.Error == "" {
		t.Error("expected error message")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ws/ -run TestFileUpdate -v
```

Expected: FAIL. `file_update` 분기 없음.

- [ ] **Step 3: Write minimal implementation**

`HandleMessage`에 `case "file_update"` 추가 + 새 메서드:

```go
func (h *Handler) HandleMessage(client ClientSender, data []byte) {
	msg, err := UnmarshalMessage(data)
	if err != nil {
		log.Printf("ws: parse failed: %v", err)
		return
	}
	switch msg.Type {
	case "sync_init":
		h.handleSyncInit(client, msg)
	case "file_check":
		h.handleFileCheck(client, msg)
	case "file_create":
		h.handleFileCreate(client, msg)
	case "file_update":
		h.handleFileUpdate(client, msg)
	default:
		log.Printf("ws: unknown type %s", msg.Type)
	}
}

func (h *Handler) handleFileUpdate(client ClientSender, msg IncomingMessage) {
	out := OutgoingMessage{Type: "file_update_result", Vault: msg.Vault, Path: msg.Path}

	if msg.PrevServerVersion == nil {
		out.Error = "prevServerVersion required"
		client.Send(out)
		return
	}

	existing, err := h.queries.GetFile(msg.Vault, msg.Path)
	if err != nil {
		out.Error = "file not found"
		client.Send(out)
		return
	}
	if existing.IsDeleted {
		out.Error = "file is deleted"
		client.Send(out)
		return
	}

	// 해시 동일 → noop
	if existing.Hash == msg.CurrentClientHash && existing.Version == *msg.PrevServerVersion {
		out.Ok = true
		out.Noop = true
		out.CurrentServerVersion = existing.Version
		out.CurrentServerHash = existing.Hash
		client.Send(out)
		return
	}

	content := decodeContent(msg.Content, msg.Encoding)

	f, err := h.queries.UpdateFile(msg.Vault, msg.Path, *msg.PrevServerVersion, msg.CurrentClientHash)
	if err == db.ErrFileConflict {
		entry, _ := h.buildServerEntry(msg.Vault, existing)
		out.Ok = false
		out.Conflict = &ConflictEntry{
			Path:                 msg.Path,
			PrevServerVersion:    msg.PrevServerVersion,
			CurrentClientHash:    msg.CurrentClientHash,
			CurrentServerVersion: existing.Version,
			CurrentServerHash:    existing.Hash,
			CurrentServerContent: entry.Content,
			Encoding:             entry.Encoding,
			Kind:                 "modify",
		}
		client.Send(out)
		return
	}
	if err != nil {
		out.Error = err.Error()
		client.Send(out)
		return
	}

	if err := h.storage.WriteFile(msg.Vault, msg.Path, content); err != nil {
		out.Error = err.Error()
		client.Send(out)
		return
	}

	out.Ok = true
	out.CurrentServerVersion = f.Version
	out.CurrentServerHash = f.Hash
	client.Send(out)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ws/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/ws/handler.go server/internal/ws/handler_test.go
git commit -m "feat(ws): handle file_update with noop/conflict branches"
```

---

## Task 13: WebSocket handler — file_delete 추가

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/ws/handler_test.go` 파일 끝에 추가:

```go
func TestFileDeleteSuccess(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	s.WriteFile("v", "a.md", []byte("body"))

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_delete", "vault": "v", "path": "a.md",
		"prevServerVersion": prev,
	})
	out := findSent(c, "file_delete_result")
	if !out.Ok {
		t.Fatalf("ok=false: %+v", out)
	}
	if out.CurrentServerVersion != 2 {
		t.Errorf("version = %d", out.CurrentServerVersion)
	}
	f, _ := q.GetFile("v", "a.md")
	if !f.IsDeleted {
		t.Error("tombstone missing")
	}
	if _, err := s.ReadFile("v", "a.md"); err == nil {
		t.Error("disk file should be removed")
	}
}

func TestFileDeleteConflict(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	q.UpdateFile("v", "a.md", 1, "h2")
	s.WriteFile("v", "a.md", []byte("v2"))

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_delete", "vault": "v", "path": "a.md",
		"prevServerVersion": prev,
	})
	out := findSent(c, "file_delete_result")
	if out.Ok {
		t.Error("expected ok=false")
	}
	if out.Conflict == nil || out.Conflict.CurrentServerVersion != 2 {
		t.Errorf("conflict wrong: %+v", out.Conflict)
	}
	if out.Conflict.Kind != "modify" && out.Conflict.Kind != "delete" {
		t.Errorf("kind = %q", out.Conflict.Kind)
	}
}

func TestFileDeleteIdempotentOnMissing(t *testing.T) {
	h, _, _ := newTestHandler(t)

	c := &fakeClient{}
	prev := int64(1)
	dispatch(h, c, map[string]any{
		"type": "file_delete", "vault": "v", "path": "missing.md",
		"prevServerVersion": prev,
	})
	out := findSent(c, "file_delete_result")
	if !out.Ok {
		t.Errorf("want ok=true (idempotent), got %+v", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ws/ -run TestFileDelete -v
```

Expected: FAIL. `file_delete` 분기 없음.

- [ ] **Step 3: Write minimal implementation**

`HandleMessage`에 `case "file_delete"` 추가 + 새 메서드:

```go
func (h *Handler) HandleMessage(client ClientSender, data []byte) {
	msg, err := UnmarshalMessage(data)
	if err != nil {
		log.Printf("ws: parse failed: %v", err)
		return
	}
	switch msg.Type {
	case "sync_init":
		h.handleSyncInit(client, msg)
	case "file_check":
		h.handleFileCheck(client, msg)
	case "file_create":
		h.handleFileCreate(client, msg)
	case "file_update":
		h.handleFileUpdate(client, msg)
	case "file_delete":
		h.handleFileDelete(client, msg)
	default:
		log.Printf("ws: unknown type %s", msg.Type)
	}
}

func (h *Handler) handleFileDelete(client ClientSender, msg IncomingMessage) {
	out := OutgoingMessage{Type: "file_delete_result", Vault: msg.Vault, Path: msg.Path}

	if msg.PrevServerVersion == nil {
		out.Error = "prevServerVersion required"
		client.Send(out)
		return
	}

	newVersion, err := h.queries.DeleteFile(msg.Vault, msg.Path, *msg.PrevServerVersion)
	switch err {
	case nil:
		_ = h.storage.DeleteFile(msg.Vault, msg.Path)
		out.Ok = true
		out.CurrentServerVersion = newVersion
	case db.ErrFileNotFound:
		// 이미 없음 → idempotent 성공
		out.Ok = true
	case db.ErrFileConflict:
		existing, _ := h.queries.GetFile(msg.Vault, msg.Path)
		entry, _ := h.buildServerEntry(msg.Vault, existing)
		out.Ok = false
		out.Conflict = &ConflictEntry{
			Path:                 msg.Path,
			PrevServerVersion:    msg.PrevServerVersion,
			CurrentServerVersion: existing.Version,
			CurrentServerHash:    existing.Hash,
			CurrentServerContent: entry.Content,
			Encoding:             entry.Encoding,
			Kind:                 "modify",
		}
	default:
		out.Error = err.Error()
	}
	client.Send(out)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ws/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/ws/handler.go server/internal/ws/handler_test.go
git commit -m "feat(ws): handle file_delete with tombstone + idempotent"
```

---

## Task 14: WebSocket handler — conflict_resolve 추가

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Write the failing test**

`server/internal/ws/handler_test.go` 파일 끝에 추가:

```go
func TestConflictResolveLocalModifySuccess(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	q.UpdateFile("v", "a.md", 1, "h2")
	s.WriteFile("v", "a.md", []byte("v2"))

	c := &fakeClient{}
	prev := int64(2)
	dispatch(h, c, map[string]any{
		"type": "conflict_resolve", "vault": "v", "path": "a.md",
		"resolution": "local",
		"content": "local body", "currentClientHash": "localH",
		"prevServerVersion": prev,
	})
	out := findSent(c, "conflict_resolve_result")
	if !out.Ok {
		t.Fatalf("ok=false: %+v", out)
	}
	if out.CurrentServerVersion != 3 {
		t.Errorf("version = %d", out.CurrentServerVersion)
	}
	body, _ := s.ReadFile("v", "a.md")
	if string(body) != "local body" {
		t.Errorf("disk = %q", string(body))
	}
}

func TestConflictResolveReconflict(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	q.UpdateFile("v", "a.md", 1, "h2")
	q.UpdateFile("v", "a.md", 2, "h3")
	s.WriteFile("v", "a.md", []byte("v3"))

	c := &fakeClient{}
	prev := int64(2) // 클라가 알던 서버 버전 (stale)
	dispatch(h, c, map[string]any{
		"type": "conflict_resolve", "vault": "v", "path": "a.md",
		"resolution": "local",
		"content": "local", "currentClientHash": "localH",
		"prevServerVersion": prev,
	})
	out := findSent(c, "conflict_resolve_result")
	if out.Ok {
		t.Error("expected ok=false on reconflict")
	}
	if out.Conflict == nil || out.Conflict.CurrentServerVersion != 3 {
		t.Errorf("conflict wrong: %+v", out.Conflict)
	}
}

func TestConflictResolveLocalDeleteSuccess(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	q.UpdateFile("v", "a.md", 1, "h2")
	s.WriteFile("v", "a.md", []byte("v2"))

	c := &fakeClient{}
	prev := int64(2)
	dispatch(h, c, map[string]any{
		"type": "conflict_resolve", "vault": "v", "path": "a.md",
		"resolution": "local", "action": "delete",
		"prevServerVersion": prev,
	})
	out := findSent(c, "conflict_resolve_result")
	if !out.Ok {
		t.Fatalf("ok=false: %+v", out)
	}
	if out.CurrentServerVersion != 3 {
		t.Errorf("version = %d", out.CurrentServerVersion)
	}
	f, _ := q.GetFile("v", "a.md")
	if !f.IsDeleted {
		t.Error("expected tombstone")
	}
}

func TestConflictResolveLocalDeleteReconflict(t *testing.T) {
	h, q, s := newTestHandler(t)
	q.CreateFile("v", "a.md", "h1")
	q.UpdateFile("v", "a.md", 1, "h2")
	q.UpdateFile("v", "a.md", 2, "h3")
	s.WriteFile("v", "a.md", []byte("v3"))

	c := &fakeClient{}
	prev := int64(2)
	dispatch(h, c, map[string]any{
		"type": "conflict_resolve", "vault": "v", "path": "a.md",
		"resolution": "local", "action": "delete",
		"prevServerVersion": prev,
	})
	out := findSent(c, "conflict_resolve_result")
	if out.Ok {
		t.Error("expected ok=false")
	}
	if out.Conflict == nil {
		t.Error("conflict missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ws/ -run TestConflictResolve -v
```

Expected: FAIL. `conflict_resolve` 분기 없음.

- [ ] **Step 3: Write minimal implementation**

`HandleMessage`에 `case "conflict_resolve"` 추가 + 새 메서드:

```go
func (h *Handler) HandleMessage(client ClientSender, data []byte) {
	msg, err := UnmarshalMessage(data)
	if err != nil {
		log.Printf("ws: parse failed: %v", err)
		return
	}
	switch msg.Type {
	case "sync_init":
		h.handleSyncInit(client, msg)
	case "file_check":
		h.handleFileCheck(client, msg)
	case "file_create":
		h.handleFileCreate(client, msg)
	case "file_update":
		h.handleFileUpdate(client, msg)
	case "file_delete":
		h.handleFileDelete(client, msg)
	case "conflict_resolve":
		h.handleConflictResolve(client, msg)
	default:
		log.Printf("ws: unknown type %s", msg.Type)
	}
}

func (h *Handler) handleConflictResolve(client ClientSender, msg IncomingMessage) {
	out := OutgoingMessage{Type: "conflict_resolve_result", Vault: msg.Vault, Path: msg.Path}

	if msg.Resolution != "local" {
		out.Error = "only resolution=local is accepted"
		client.Send(out)
		return
	}
	if msg.PrevServerVersion == nil {
		out.Error = "prevServerVersion required"
		client.Send(out)
		return
	}

	if msg.Action == "delete" {
		newVersion, err := h.queries.DeleteFile(msg.Vault, msg.Path, *msg.PrevServerVersion)
		switch err {
		case nil:
			_ = h.storage.DeleteFile(msg.Vault, msg.Path)
			out.Ok = true
			out.CurrentServerVersion = newVersion
		case db.ErrFileConflict:
			existing, _ := h.queries.GetFile(msg.Vault, msg.Path)
			entry, _ := h.buildServerEntry(msg.Vault, existing)
			out.Ok = false
			out.Conflict = &ConflictEntry{
				Path:                 msg.Path,
				PrevServerVersion:    msg.PrevServerVersion,
				CurrentServerVersion: existing.Version,
				CurrentServerHash:    existing.Hash,
				CurrentServerContent: entry.Content,
				Encoding:             entry.Encoding,
				Kind:                 "delete",
			}
		case db.ErrFileNotFound:
			out.Ok = true
		default:
			out.Error = err.Error()
		}
		client.Send(out)
		return
	}

	// resolution=local + 수정 (기본)
	content := decodeContent(msg.Content, msg.Encoding)
	f, err := h.queries.UpdateFile(msg.Vault, msg.Path, *msg.PrevServerVersion, msg.CurrentClientHash)
	if err == db.ErrFileConflict {
		existing, _ := h.queries.GetFile(msg.Vault, msg.Path)
		entry, _ := h.buildServerEntry(msg.Vault, existing)
		out.Ok = false
		out.Conflict = &ConflictEntry{
			Path:                 msg.Path,
			PrevServerVersion:    msg.PrevServerVersion,
			CurrentClientHash:    msg.CurrentClientHash,
			CurrentServerVersion: existing.Version,
			CurrentServerHash:    existing.Hash,
			CurrentServerContent: entry.Content,
			Encoding:             entry.Encoding,
			Kind:                 "modify",
		}
		client.Send(out)
		return
	}
	if err != nil {
		out.Error = err.Error()
		client.Send(out)
		return
	}

	if err := h.storage.WriteFile(msg.Vault, msg.Path, content); err != nil {
		out.Error = err.Error()
		client.Send(out)
		return
	}
	out.Ok = true
	out.CurrentServerVersion = f.Version
	out.CurrentServerHash = f.Hash
	client.Send(out)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ws/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/ws/handler.go server/internal/ws/handler_test.go
git commit -m "feat(ws): handle conflict_resolve with prev version revalidation"
```

---

## Task 15: Client 구조 재정비 (`SendMessage` → `Send`, vault 접근자)

**Files:**
- Modify: `server/internal/ws/client.go`

> 기존 `Client.SendMessage`는 handler에서 쓰지 않음. handler 인터페이스 `ClientSender`를 실제 `Client`에 맞추기.

- [ ] **Step 1: Write the failing test**

`server/internal/ws/client_test.go` 신규:

```go
package ws

import "testing"

// Client이 ClientSender 인터페이스를 만족해야 handler에서 사용 가능.
func TestClientImplementsSender(t *testing.T) {
	var _ ClientSender = (*Client)(nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ws/ -run TestClientImplementsSender -v
```

Expected: FAIL. `Send(OutgoingMessage)`, `Vault()`, `SetVault(string)` 메서드가 현재 Client에 없음 (기존 `SendMessage` 존재).

- [ ] **Step 3: Write minimal implementation**

`server/internal/ws/client.go` 전체 교체:

```go
package ws

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

type Client struct {
	conn    *websocket.Conn
	send    chan []byte
	vault   string
	handler MessageHandler
}

type MessageHandler interface {
	HandleMessage(client ClientSender, data []byte)
}

func NewClient(conn *websocket.Conn, handler MessageHandler) *Client {
	return &Client{
		conn:    conn,
		send:    make(chan []byte, 256),
		handler: handler,
	}
}

func (c *Client) ReadPump() {
	defer c.conn.Close()
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		c.handler.HandleMessage(c, message)
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) Send(msg OutgoingMessage) {
	data, err := MarshalMessage(msg)
	if err != nil {
		log.Printf("ws: marshal failed: %v", err)
		return
	}
	select {
	case c.send <- data:
	default:
	}
}

func (c *Client) SetVault(v string) { c.vault = v }
func (c *Client) Vault() string     { return c.vault }
```

`hub.go`/`hub_test.go`는 더 이상 필요 없음 (remote_change 폐기). 삭제.

```bash
rm server/internal/ws/hub.go server/internal/ws/hub_test.go
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ws/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/ws/client.go server/internal/ws/client_test.go
git rm server/internal/ws/hub.go server/internal/ws/hub_test.go
git commit -m "refactor(ws): simplify client, drop hub (no remote_change)"
```

---

## Task 16: main.go에서 hub 참조 제거

**Files:**
- Modify: `server/cmd/server/main.go`

- [ ] **Step 1: Write the failing test**

별도 테스트 없이 컴파일 성공 확인.

- [ ] **Step 2: Run to verify failure**

```bash
go build ./...
```

Expected: FAIL. `ws.NewHub`, `hub.Run`, `NewHandler(q, store, hub)` 등 hub 참조로 컴파일 에러.

- [ ] **Step 3: Write minimal implementation**

`server/cmd/server/main.go` 전체 교체:

```go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"obsidian-goat-sync/internal/config"
	"obsidian-goat-sync/internal/dashboard"
	"obsidian-goat-sync/internal/db"
	"obsidian-goat-sync/internal/github"
	"obsidian-goat-sync/internal/storage"
	"obsidian-goat-sync/internal/ws"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	cfg := config.Load()

	database, err := db.Open(cfg.DataDir + "/sync.db")
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	queries := db.NewQueries(database)
	store := storage.New(cfg.DataDir)

	handler := ws.NewHandler(queries, store)

	backup := github.NewBackupService(queries, store)
	go backup.Start()

	mux := http.NewServeMux()

	dash := dashboard.New(cfg, queries, store)
	dash.RegisterRoutes(mux)

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		valid, err := queries.ValidateToken(token)
		if err != nil || !valid {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("upgrade error: %v", err)
			return
		}

		client := ws.NewClient(conn, handler)
		go client.WritePump()
		go client.ReadPump()
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("Obsidian Goat Sync running on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
```

- [ ] **Step 4: Run build to verify success**

```bash
go build ./...
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/cmd/server/main.go
git commit -m "refactor(cmd): drop hub wiring from main"
```

---

## Task 17: Dashboard가 새 File/Vault 필드 사용하도록 수정 (빌드 에러 해소)

**Files:**
- Modify: `server/internal/dashboard/handler.go`
- Modify: `server/internal/dashboard/templates.go`

> 이 태스크는 Plan 5(대시보드 GitHub 백업 확장)와 겹치지 않는다. 여기서는 새 필드로의 **컴파일 적응**만. GitHub 관련 UI 확장은 Plan 5.

- [ ] **Step 1: Write the failing test**

```bash
go build ./...
```

Expected: FAIL. `Vault.CreatedAt` 필드 없음 (→ `InsertedAt`). `File.ModifiedAt` 없음 (→ `UpdatedAt`). `ListFiles` 없음 (→ `ListActiveFiles`).

- [ ] **Step 2: Run build to reproduce**

동일 명령어, 오류 그대로 확인.

- [ ] **Step 3: Write minimal implementation**

`server/internal/dashboard/handler.go` 수정:

```go
func (d *Dashboard) handleIndex(w http.ResponseWriter, r *http.Request) {
	vaults, _ := d.queries.ListVaults()

	type vaultInfo struct {
		Name       string
		InsertedAt string
		FileCount  int
		TotalSize  int64
	}
	var infos []vaultInfo
	for _, v := range vaults {
		count, size, _ := d.storage.VaultStats(v.Name)
		infos = append(infos, vaultInfo{
			Name:       v.Name,
			InsertedAt: v.InsertedAt,
			FileCount:  count,
			TotalSize:  size,
		})
	}

	w.Header().Set("Content-Type", "text/html")
	indexTemplate.Execute(w, infos)
}

func (d *Dashboard) handleVaultFiles(w http.ResponseWriter, r *http.Request, vaultName string) {
	files, err := d.queries.ListActiveFiles(vaultName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}
```

`server/internal/dashboard/templates.go` 수정 (vault 테이블 열 이름과 파일 목록):

```go
// indexTemplate 내부 <tr data-vault="{{.Name}}"> 블록에서
// {{.CreatedAt}} → {{.InsertedAt}}
// 파일 테이블 JS render: f.ModifiedAt → f.UpdatedAt
```

구체적으로 `templates.go`에서 치환:

```
{{.CreatedAt}}  →  {{.InsertedAt}}
```

그리고 JS render 부분:

```
'<tr><td class="mono">' + f.Path + '</td><td>' + f.ModifiedAt + '</td></tr>'
```

다음으로 교체:

```
'<tr><td class="mono">' + f.Path + '</td><td>' + f.UpdatedAt + '</td></tr>'
```

- [ ] **Step 4: Run build to verify success**

```bash
go build ./...
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/dashboard/handler.go server/internal/dashboard/templates.go
git commit -m "refactor(dashboard): adapt to new File/Vault field names"
```

---

## Task 18: GitHub 백업 서비스 — access_token/author 적용

**Files:**
- Modify: `server/internal/github/backup.go`
- Modify: `server/internal/github/backup_test.go`

> 이 태스크는 Plan 5에서 다시 손대지만, DB 필드 교체에 따른 **컴파일 적응 + 기본 사용**만 여기서. 대시보드 UI는 Plan 5에서.

- [ ] **Step 1: Write the failing test**

`server/internal/github/backup_test.go` 신규/교체:

```go
package github

import (
	"strings"
	"testing"

	"obsidian-goat-sync/internal/db"
)

func TestBuildRemoteURLInjectsToken(t *testing.T) {
	cfg := db.GitHubConfig{
		RemoteURL:   "https://github.com/acme/repo.git",
		AccessToken: "ghp_secret",
	}
	got := buildRemoteURL(cfg)
	if !strings.Contains(got, "ghp_secret@github.com") {
		t.Errorf("token not injected: %s", got)
	}
}

func TestBuildRemoteURLHandlesExisting(t *testing.T) {
	cfg := db.GitHubConfig{
		RemoteURL:   "https://user@github.com/acme/repo.git",
		AccessToken: "newtok",
	}
	got := buildRemoteURL(cfg)
	if !strings.Contains(got, "newtok@github.com") {
		t.Errorf("token not replaced: %s", got)
	}
}

func TestBuildAuthorStringFormat(t *testing.T) {
	got := buildAuthor("Alice", "alice@example.com")
	want := "Alice <alice@example.com>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseIntervalFallback(t *testing.T) {
	if d := parseInterval("2h").String(); d != "2h0m0s" {
		t.Errorf("2h parsed = %s", d)
	}
	if d := parseInterval("garbage").String(); d != "1h0m0s" {
		t.Errorf("fallback != 1h: %s", d)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/github/ -v
```

Expected: FAIL. `buildRemoteURL`, `buildAuthor` 없음. 기존 `backup.go`가 새 `db.GitHubConfig` 필드 참조하지 않음.

- [ ] **Step 3: Write minimal implementation**

`server/internal/github/backup.go` 전체 교체:

```go
package github

import (
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"obsidian-goat-sync/internal/db"
	"obsidian-goat-sync/internal/storage"
)

type BackupService struct {
	queries *db.Queries
	storage *storage.Storage
	stop    chan struct{}
}

func NewBackupService(q *db.Queries, s *storage.Storage) *BackupService {
	return &BackupService{queries: q, storage: s, stop: make(chan struct{})}
}

func (b *BackupService) Start() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.runBackups()
		case <-b.stop:
			return
		}
	}
}

func (b *BackupService) Stop() {
	close(b.stop)
}

func (b *BackupService) runBackups() {
	vaults, err := b.queries.ListVaults()
	if err != nil {
		log.Printf("backup: list vaults: %v", err)
		return
	}
	for _, v := range vaults {
		cfg, err := b.queries.GetGitHubConfig(v.Name)
		if err != nil || !cfg.Enabled {
			continue
		}
		b.backupVault(v.Name, cfg)
	}
}

func (b *BackupService) backupVault(vaultName string, cfg db.GitHubConfig) {
	dir := b.storage.VaultDir(vaultName)
	remote := buildRemoteURL(cfg)

	if !isGitRepo(dir) {
		run(dir, "git", "init")
		run(dir, "git", "remote", "add", "origin", remote)
	} else {
		run(dir, "git", "remote", "set-url", "origin", remote)
	}

	run(dir, "git", "add", "-A")

	if err := run(dir, "git", "diff", "--cached", "--quiet"); err != nil {
		author := buildAuthor(cfg.AuthorName, cfg.AuthorEmail)
		args := []string{"commit", "-m", "auto backup: " + time.Now().UTC().Format(time.RFC3339)}
		if author != "" {
			args = append(args, "--author", author)
		}
		run(dir, "git", args...)
	}

	run(dir, "git", "push", "-u", "origin", cfg.Branch)
}

// buildRemoteURL access_token을 URL userinfo로 주입.
func buildRemoteURL(cfg db.GitHubConfig) string {
	if cfg.AccessToken == "" {
		return cfg.RemoteURL
	}
	u, err := url.Parse(cfg.RemoteURL)
	if err != nil {
		return cfg.RemoteURL
	}
	u.User = url.User(cfg.AccessToken)
	return u.String()
}

// buildAuthor "Name <email>" 포맷. 이름/이메일 둘 중 하나라도 없으면 빈 문자열.
func buildAuthor(name, email string) string {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	if name == "" || email == "" {
		return ""
	}
	return fmt.Sprintf("%s <%s>", name, email)
}

func parseInterval(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Hour
	}
	return d
}

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
}

func run(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("backup: %s %v failed: %s", name, args, string(output))
	}
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/github/ -v
go build ./...
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/github/backup.go server/internal/github/backup_test.go
git commit -m "feat(github): use access_token/author fields in backup commits"
```

---

## Task 19: 통합 점검 — 전체 테스트 + 빌드

**Files:** 없음 (검증만)

- [ ] **Step 1: Run full test suite**

```bash
cd server
go test ./... -v -count=1
```

Expected: 모든 패키지 PASS.

- [ ] **Step 2: Run build**

```bash
go build ./...
```

Expected: 오류 없음.

- [ ] **Step 3: Run go vet**

```bash
go vet ./...
```

Expected: 경고 없음.

- [ ] **Step 4: Commit (빈 커밋으로 마일스톤 표시 — 선택)**

건너뛰어도 무방. 스크린샷성 커밋이 필요하면:

```bash
git commit --allow-empty -m "checkpoint: server protocol rewrite complete"
```

---

## 스펙 커버리지 검증

| 스펙 항목 | 구현 태스크 |
|---|---|
| SQLite 스키마 (vaults/files/tokens/github_configs) | Task 1 |
| files.version / files.hash | Task 1, 4 |
| files.inserted_at / updated_at | Task 1, 4 |
| vaults inserted_at / updated_at | Task 1, 2 |
| tokens inserted_at / updated_at | Task 1, 3 |
| github_configs access_token/author_name/author_email | Task 1, 5 |
| 충돌 감지 판정표 (sync_init) | Task 6, 9 |
| file_check 판정 | Task 6, 10 |
| file_create 낙관적 락 + tombstone 재활용 | Task 4, 11 |
| file_update 낙관적 락 + noop | Task 4, 12 |
| file_delete 낙관적 락 + idempotent | Task 4, 13 |
| conflict_resolve (modify/delete) + 재충돌 | Task 14 |
| remote_change 폐기 | Task 15 (hub 제거), 16 |
| 바이너리 base64 처리 | Task 9, 11 |
| access_token URL 주입 + author 커밋 | Task 18 |
| 대시보드 빌드 적응 | Task 17 |
