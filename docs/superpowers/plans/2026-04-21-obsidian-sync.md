# Obsidian Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 옵시디언 볼트를 자체 홈 서버와 실시간 WebSocket 동기화하는 시스템 구축 (Go 서버 + TypeScript 옵시디언 플러그인)

**Architecture:** Go 서버가 WebSocket으로 플러그인과 통신, SQLite로 메타데이터 관리, 파일시스템에 볼트 구조 미러링. HTTP 대시보드로 볼트/토큰/GitHub 설정 관리. 옵시디언 플러그인이 파일 변경 감지하여 실시간 동기화.

**Tech Stack:** Go, SQLite (github.com/mattn/go-sqlite3), gorilla/websocket, html/template, TypeScript, Obsidian API, Docker

---

## File Structure

### Go Server

```
server/
├── cmd/server/main.go              # 엔트리포인트, 서버 초기화
├── internal/
│   ├── config/config.go            # 환경변수 파싱
│   ├── db/
│   │   ├── db.go                   # SQLite 연결 + 마이그레이션
│   │   ├── vault.go                # vault CRUD
│   │   ├── file.go                 # file 메타데이터 CRUD
│   │   ├── token.go                # token CRUD
│   │   └── github_config.go        # github_config CRUD
│   ├── storage/storage.go          # 파일시스템 읽기/쓰기
│   ├── ws/
│   │   ├── hub.go                  # WebSocket 연결 관리, 브로드캐스트
│   │   ├── client.go               # 개별 WebSocket 클라이언트
│   │   ├── messages.go             # 메시지 타입 정의
│   │   └── handler.go              # 메시지별 핸들러
│   ├── sync/conflict.go            # 충돌 판단 로직
│   ├── dashboard/
│   │   ├── handler.go              # HTTP 핸들러 (대시보드 + API)
│   │   ├── auth.go                 # 세션 기반 admin 인증
│   │   └── templates.go            # Go embed 템플릿 (login, index)
│   └── github/backup.go           # 주기적 git commit + push
├── go.mod
├── go.sum
├── Dockerfile
└── docker-compose.yml
```

### Go Server Tests

```
server/
├── internal/
│   ├── config/config_test.go
│   ├── db/
│   │   ├── db_test.go
│   │   ├── vault_test.go
│   │   ├── file_test.go
│   │   ├── token_test.go
│   │   └── github_config_test.go
│   ├── storage/storage_test.go
│   ├── sync/conflict_test.go
│   ├── ws/
│   │   ├── hub_test.go
│   │   └── handler_test.go
│   └── github/backup_test.go
```

### Obsidian Plugin

```
plugin/
├── manifest.json
├── package.json
├── tsconfig.json
├── esbuild.config.mjs
├── src/
│   ├── main.ts                     # 플러그인 엔트리포인트
│   ├── settings.ts                 # 설정 탭 UI
│   ├── ws-client.ts                # WebSocket 클라이언트
│   ├── file-watcher.ts             # 파일 변경 감지
│   └── sync.ts                     # 동기화 오케스트레이션
└── styles.css
```

---

## Task 1: Go 프로젝트 초기화 + Config

**Files:**
- Create: `server/go.mod`
- Create: `server/cmd/server/main.go`
- Create: `server/internal/config/config.go`
- Test: `server/internal/config/config_test.go`

- [ ] **Step 1: Go 모듈 초기화**

```bash
cd server
go mod init obsidian-sync
```

- [ ] **Step 2: config_test.go 작성**

```go
// server/internal/config/config_test.go
package config

import (
	"os"
	"testing"
)

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("OBSIDIAN_SYNC_ADMIN_USER", "testadmin")
	os.Setenv("OBSIDIAN_SYNC_ADMIN_PASS", "testpass")
	os.Setenv("OBSIDIAN_SYNC_PORT", "9090")
	defer func() {
		os.Unsetenv("OBSIDIAN_SYNC_ADMIN_USER")
		os.Unsetenv("OBSIDIAN_SYNC_ADMIN_PASS")
		os.Unsetenv("OBSIDIAN_SYNC_PORT")
	}()

	cfg := Load()

	if cfg.AdminUser != "testadmin" {
		t.Errorf("expected AdminUser=testadmin, got %s", cfg.AdminUser)
	}
	if cfg.AdminPass != "testpass" {
		t.Errorf("expected AdminPass=testpass, got %s", cfg.AdminPass)
	}
	if cfg.Port != "9090" {
		t.Errorf("expected Port=9090, got %s", cfg.Port)
	}
}

func TestLoadDefaults(t *testing.T) {
	os.Unsetenv("OBSIDIAN_SYNC_ADMIN_USER")
	os.Unsetenv("OBSIDIAN_SYNC_ADMIN_PASS")
	os.Unsetenv("OBSIDIAN_SYNC_PORT")

	cfg := Load()

	if cfg.Port != "8080" {
		t.Errorf("expected default Port=8080, got %s", cfg.Port)
	}
	if cfg.DataDir != "/app/data" {
		t.Errorf("expected default DataDir=/app/data, got %s", cfg.DataDir)
	}
}
```

- [ ] **Step 3: 테스트 실패 확인**

```bash
cd server && go test ./internal/config/ -v
```
Expected: FAIL — package not found

- [ ] **Step 4: config.go 구현**

```go
// server/internal/config/config.go
package config

import "os"

type Config struct {
	AdminUser string
	AdminPass string
	Port      string
	DataDir   string
}

func Load() Config {
	return Config{
		AdminUser: getEnv("OBSIDIAN_SYNC_ADMIN_USER", "admin"),
		AdminPass: getEnv("OBSIDIAN_SYNC_ADMIN_PASS", ""),
		Port:      getEnv("OBSIDIAN_SYNC_PORT", "8080"),
		DataDir:   "/app/data",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 5: 테스트 통과 확인**

```bash
cd server && go test ./internal/config/ -v
```
Expected: PASS

- [ ] **Step 6: main.go 스캐폴딩**

```go
// server/cmd/server/main.go
package main

import (
	"fmt"
	"obsidian-sync/internal/config"
)

func main() {
	cfg := config.Load()
	fmt.Printf("Starting obsidian-sync on port %s\n", cfg.Port)
}
```

- [ ] **Step 7: 커밋**

```bash
git add server/
git commit -m "feat: init Go project with config"
```

---

## Task 2: SQLite DB + 마이그레이션

**Files:**
- Create: `server/internal/db/db.go`
- Test: `server/internal/db/db_test.go`

- [ ] **Step 1: go-sqlite3 의존성 추가**

```bash
cd server && go get github.com/mattn/go-sqlite3
```

- [ ] **Step 2: db_test.go 작성**

```go
// server/internal/db/db_test.go
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

	var tableName string
	err = database.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='vaults'").Scan(&tableName)
	if err != nil {
		t.Fatalf("vaults table not created: %v", err)
	}

	err = database.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='files'").Scan(&tableName)
	if err != nil {
		t.Fatalf("files table not created: %v", err)
	}

	err = database.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='tokens'").Scan(&tableName)
	if err != nil {
		t.Fatalf("tokens table not created: %v", err)
	}

	err = database.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='github_configs'").Scan(&tableName)
	if err != nil {
		t.Fatalf("github_configs table not created: %v", err)
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
```

- [ ] **Step 3: 테스트 실패 확인**

```bash
cd server && go test ./internal/db/ -v
```
Expected: FAIL

- [ ] **Step 4: db.go 구현**

```go
// server/internal/db/db.go
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
		name       TEXT PRIMARY KEY,
		created_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS files (
		vault_name  TEXT NOT NULL,
		path        TEXT NOT NULL,
		modified_at TEXT NOT NULL,
		is_deleted  INTEGER DEFAULT 0,
		PRIMARY KEY (vault_name, path),
		FOREIGN KEY (vault_name) REFERENCES vaults(name)
	);

	CREATE TABLE IF NOT EXISTS tokens (
		token      TEXT PRIMARY KEY,
		created_at TEXT NOT NULL,
		is_active  INTEGER DEFAULT 1
	);

	CREATE TABLE IF NOT EXISTS github_configs (
		vault_name TEXT PRIMARY KEY,
		remote_url TEXT NOT NULL,
		branch     TEXT DEFAULT 'main',
		interval   TEXT DEFAULT '1h',
		enabled    INTEGER DEFAULT 1,
		FOREIGN KEY (vault_name) REFERENCES vaults(name)
	);`

	_, err := db.Exec(schema)
	return err
}
```

- [ ] **Step 5: 테스트 통과 확인**

```bash
cd server && go test ./internal/db/ -v
```
Expected: PASS

- [ ] **Step 6: 커밋**

```bash
git add server/
git commit -m "feat: add SQLite DB with schema migration"
```

---

## Task 3: Vault CRUD

**Files:**
- Create: `server/internal/db/vault.go`
- Test: `server/internal/db/vault_test.go`

- [ ] **Step 1: vault_test.go 작성**

```go
// server/internal/db/vault_test.go
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
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd server && go test ./internal/db/ -v -run TestCreateVault
```
Expected: FAIL

- [ ] **Step 3: vault.go 구현**

```go
// server/internal/db/vault.go
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
	Name      string
	CreatedAt string
}

func (q *Queries) CreateVault(name string) error {
	_, err := q.db.Exec(
		"INSERT INTO vaults (name, created_at) VALUES (?, ?)",
		name, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (q *Queries) ListVaults() ([]Vault, error) {
	rows, err := q.db.Query("SELECT name, created_at FROM vaults ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vaults []Vault
	for rows.Next() {
		var v Vault
		if err := rows.Scan(&v.Name, &v.CreatedAt); err != nil {
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

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd server && go test ./internal/db/ -v
```
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add server/
git commit -m "feat: add vault CRUD"
```

---

## Task 4: File 메타데이터 CRUD

**Files:**
- Create: `server/internal/db/file.go`
- Test: `server/internal/db/file_test.go`

- [ ] **Step 1: file_test.go 작성**

```go
// server/internal/db/file_test.go
package db

import (
	"testing"
)

func TestUpsertFile(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")

	err := q.UpsertFile("personal", "notes/hello.md", "2026-04-21T10:00:00Z")
	if err != nil {
		t.Fatalf("failed to upsert file: %v", err)
	}

	f, err := q.GetFile("personal", "notes/hello.md")
	if err != nil {
		t.Fatalf("failed to get file: %v", err)
	}
	if f.ModifiedAt != "2026-04-21T10:00:00Z" {
		t.Errorf("expected modifiedAt=2026-04-21T10:00:00Z, got %s", f.ModifiedAt)
	}
	if f.IsDeleted {
		t.Error("expected is_deleted=false")
	}
}

func TestUpsertFileUpdate(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")
	q.UpsertFile("personal", "notes/hello.md", "2026-04-21T10:00:00Z")

	err := q.UpsertFile("personal", "notes/hello.md", "2026-04-21T15:00:00Z")
	if err != nil {
		t.Fatalf("failed to upsert file: %v", err)
	}

	f, _ := q.GetFile("personal", "notes/hello.md")
	if f.ModifiedAt != "2026-04-21T15:00:00Z" {
		t.Errorf("expected updated modifiedAt, got %s", f.ModifiedAt)
	}
}

func TestMarkFileDeleted(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")
	q.UpsertFile("personal", "notes/hello.md", "2026-04-21T10:00:00Z")

	err := q.MarkFileDeleted("personal", "notes/hello.md")
	if err != nil {
		t.Fatalf("failed to mark deleted: %v", err)
	}

	f, _ := q.GetFile("personal", "notes/hello.md")
	if !f.IsDeleted {
		t.Error("expected is_deleted=true")
	}
}

func TestListFiles(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")
	q.UpsertFile("personal", "notes/a.md", "2026-04-21T10:00:00Z")
	q.UpsertFile("personal", "notes/b.md", "2026-04-21T11:00:00Z")
	q.UpsertFile("personal", "notes/c.md", "2026-04-21T12:00:00Z")
	q.MarkFileDeleted("personal", "notes/c.md")

	files, err := q.ListFiles("personal")
	if err != nil {
		t.Fatalf("failed to list files: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 active files, got %d", len(files))
	}
}

func TestGetFileNotFound(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")

	_, err := q.GetFile("personal", "nonexistent.md")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd server && go test ./internal/db/ -v -run TestUpsertFile
```
Expected: FAIL

- [ ] **Step 3: file.go 구현**

```go
// server/internal/db/file.go
package db

import "database/sql"

type File struct {
	VaultName  string
	Path       string
	ModifiedAt string
	IsDeleted  bool
}

func (q *Queries) UpsertFile(vaultName, path, modifiedAt string) error {
	_, err := q.db.Exec(`
		INSERT INTO files (vault_name, path, modified_at, is_deleted)
		VALUES (?, ?, ?, 0)
		ON CONFLICT (vault_name, path)
		DO UPDATE SET modified_at = excluded.modified_at, is_deleted = 0`,
		vaultName, path, modifiedAt,
	)
	return err
}

func (q *Queries) GetFile(vaultName, path string) (File, error) {
	var f File
	err := q.db.QueryRow(
		"SELECT vault_name, path, modified_at, is_deleted FROM files WHERE vault_name = ? AND path = ?",
		vaultName, path,
	).Scan(&f.VaultName, &f.Path, &f.ModifiedAt, &f.IsDeleted)
	if err == sql.ErrNoRows {
		return f, sql.ErrNoRows
	}
	return f, err
}

func (q *Queries) MarkFileDeleted(vaultName, path string) error {
	_, err := q.db.Exec(
		"UPDATE files SET is_deleted = 1 WHERE vault_name = ? AND path = ?",
		vaultName, path,
	)
	return err
}

func (q *Queries) ListFiles(vaultName string) ([]File, error) {
	rows, err := q.db.Query(
		"SELECT vault_name, path, modified_at, is_deleted FROM files WHERE vault_name = ? AND is_deleted = 0 ORDER BY path",
		vaultName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.VaultName, &f.Path, &f.ModifiedAt, &f.IsDeleted); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd server && go test ./internal/db/ -v
```
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add server/
git commit -m "feat: add file metadata CRUD"
```

---

## Task 5: Token CRUD

**Files:**
- Create: `server/internal/db/token.go`
- Test: `server/internal/db/token_test.go`

- [ ] **Step 1: token_test.go 작성**

```go
// server/internal/db/token_test.go
package db

import (
	"testing"
)

func TestGenerateToken(t *testing.T) {
	q := setupTestDB(t)

	token, err := q.GenerateToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	if len(token) < 32 {
		t.Errorf("token too short: %d", len(token))
	}
}

func TestValidateToken(t *testing.T) {
	q := setupTestDB(t)

	token, _ := q.GenerateToken()

	valid, err := q.ValidateToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}
	if !valid {
		t.Error("expected token to be valid")
	}
}

func TestValidateTokenInvalid(t *testing.T) {
	q := setupTestDB(t)

	valid, err := q.ValidateToken("nonexistent-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("expected token to be invalid")
	}
}

func TestDeactivateToken(t *testing.T) {
	q := setupTestDB(t)

	token, _ := q.GenerateToken()
	err := q.DeactivateToken(token)
	if err != nil {
		t.Fatalf("failed to deactivate: %v", err)
	}

	valid, _ := q.ValidateToken(token)
	if valid {
		t.Error("expected deactivated token to be invalid")
	}
}

func TestRegenerateToken(t *testing.T) {
	q := setupTestDB(t)

	oldToken, _ := q.GenerateToken()
	newToken, err := q.RegenerateToken(oldToken)
	if err != nil {
		t.Fatalf("failed to regenerate: %v", err)
	}
	if newToken == oldToken {
		t.Error("new token should differ from old")
	}

	oldValid, _ := q.ValidateToken(oldToken)
	if oldValid {
		t.Error("old token should be deactivated")
	}

	newValid, _ := q.ValidateToken(newToken)
	if !newValid {
		t.Error("new token should be valid")
	}
}

func TestListTokens(t *testing.T) {
	q := setupTestDB(t)

	q.GenerateToken()
	q.GenerateToken()

	tokens, err := q.ListTokens()
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd server && go test ./internal/db/ -v -run TestGenerateToken
```
Expected: FAIL

- [ ] **Step 3: token.go 구현**

```go
// server/internal/db/token.go
package db

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type Token struct {
	Token     string
	CreatedAt string
	IsActive  bool
}

func (q *Queries) GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	_, err := q.db.Exec(
		"INSERT INTO tokens (token, created_at, is_active) VALUES (?, ?, 1)",
		token, time.Now().UTC().Format(time.RFC3339),
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
	_, err := q.db.Exec("UPDATE tokens SET is_active = 0 WHERE token = ?", token)
	return err
}

func (q *Queries) RegenerateToken(oldToken string) (string, error) {
	if err := q.DeactivateToken(oldToken); err != nil {
		return "", err
	}
	return q.GenerateToken()
}

func (q *Queries) ListTokens() ([]Token, error) {
	rows, err := q.db.Query("SELECT token, created_at, is_active FROM tokens ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.Token, &t.CreatedAt, &t.IsActive); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd server && go test ./internal/db/ -v
```
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add server/
git commit -m "feat: add token CRUD with generate/validate/regenerate"
```

---

## Task 6: GitHub Config CRUD

**Files:**
- Create: `server/internal/db/github_config.go`
- Test: `server/internal/db/github_config_test.go`

- [ ] **Step 1: github_config_test.go 작성**

```go
// server/internal/db/github_config_test.go
package db

import "testing"

func TestSetGitHubConfig(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")

	cfg := GitHubConfig{
		VaultName: "personal",
		RemoteURL: "git@github.com:user/vault.git",
		Branch:    "main",
		Interval:  "1h",
		Enabled:   true,
	}
	err := q.SetGitHubConfig(cfg)
	if err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	got, err := q.GetGitHubConfig("personal")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}
	if got.RemoteURL != "git@github.com:user/vault.git" {
		t.Errorf("expected remote URL, got %s", got.RemoteURL)
	}
	if got.Branch != "main" {
		t.Errorf("expected branch=main, got %s", got.Branch)
	}
}

func TestUpdateGitHubConfig(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")

	q.SetGitHubConfig(GitHubConfig{
		VaultName: "personal",
		RemoteURL: "git@github.com:user/vault.git",
		Branch:    "main",
		Interval:  "1h",
		Enabled:   true,
	})

	q.SetGitHubConfig(GitHubConfig{
		VaultName: "personal",
		RemoteURL: "git@github.com:user/vault2.git",
		Branch:    "dev",
		Interval:  "30m",
		Enabled:   false,
	})

	got, _ := q.GetGitHubConfig("personal")
	if got.RemoteURL != "git@github.com:user/vault2.git" {
		t.Errorf("expected updated remote URL, got %s", got.RemoteURL)
	}
	if got.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestGetGitHubConfigNotFound(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")

	_, err := q.GetGitHubConfig("personal")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd server && go test ./internal/db/ -v -run TestSetGitHub
```
Expected: FAIL

- [ ] **Step 3: github_config.go 구현**

```go
// server/internal/db/github_config.go
package db

import "database/sql"

type GitHubConfig struct {
	VaultName string
	RemoteURL string
	Branch    string
	Interval  string
	Enabled   bool
}

func (q *Queries) SetGitHubConfig(cfg GitHubConfig) error {
	_, err := q.db.Exec(`
		INSERT INTO github_configs (vault_name, remote_url, branch, interval, enabled)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (vault_name)
		DO UPDATE SET remote_url = excluded.remote_url, branch = excluded.branch,
		              interval = excluded.interval, enabled = excluded.enabled`,
		cfg.VaultName, cfg.RemoteURL, cfg.Branch, cfg.Interval, cfg.Enabled,
	)
	return err
}

func (q *Queries) GetGitHubConfig(vaultName string) (GitHubConfig, error) {
	var cfg GitHubConfig
	err := q.db.QueryRow(
		"SELECT vault_name, remote_url, branch, interval, enabled FROM github_configs WHERE vault_name = ?",
		vaultName,
	).Scan(&cfg.VaultName, &cfg.RemoteURL, &cfg.Branch, &cfg.Interval, &cfg.Enabled)
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

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd server && go test ./internal/db/ -v
```
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add server/
git commit -m "feat: add GitHub config CRUD"
```

---

## Task 7: 파일시스템 Storage

**Files:**
- Create: `server/internal/storage/storage.go`
- Test: `server/internal/storage/storage_test.go`

- [ ] **Step 1: storage_test.go 작성**

```go
// server/internal/storage/storage_test.go
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
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd server && go test ./internal/storage/ -v
```
Expected: FAIL

- [ ] **Step 3: storage.go 구현**

```go
// server/internal/storage/storage.go
package storage

import (
	"io/fs"
	"os"
	"path/filepath"
)

type Storage struct {
	dataDir string
}

func New(dataDir string) *Storage {
	return &Storage{dataDir: dataDir}
}

func (s *Storage) vaultPath(vault, filePath string) string {
	return filepath.Join(s.dataDir, "vaults", vault, filePath)
}

func (s *Storage) WriteFile(vault, filePath string, data []byte) error {
	full := s.vaultPath(vault, filePath)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0644)
}

func (s *Storage) ReadFile(vault, filePath string) ([]byte, error) {
	return os.ReadFile(s.vaultPath(vault, filePath))
}

func (s *Storage) DeleteFile(vault, filePath string) error {
	return os.Remove(s.vaultPath(vault, filePath))
}

func (s *Storage) CreateVaultDir(vault string) error {
	return os.MkdirAll(filepath.Join(s.dataDir, "vaults", vault), 0755)
}

func (s *Storage) DeleteVaultDir(vault string) error {
	return os.RemoveAll(filepath.Join(s.dataDir, "vaults", vault))
}

func (s *Storage) VaultStats(vault string) (fileCount int, totalSize int64, err error) {
	root := filepath.Join(s.dataDir, "vaults", vault)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		fileCount++
		info, err := d.Info()
		if err != nil {
			return err
		}
		totalSize += info.Size()
		return nil
	})
	return
}

func (s *Storage) VaultDir(vault string) string {
	return filepath.Join(s.dataDir, "vaults", vault)
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd server && go test ./internal/storage/ -v
```
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add server/
git commit -m "feat: add filesystem storage layer"
```

---

## Task 8: 충돌 판단 로직

**Files:**
- Create: `server/internal/sync/conflict.go`
- Test: `server/internal/sync/conflict_test.go`

- [ ] **Step 1: conflict_test.go 작성**

```go
// server/internal/sync/conflict_test.go
package sync

import (
	"strings"
	"testing"
)

func TestCheckCreateConflict_NoExisting(t *testing.T) {
	result := CheckCreateConflict("notes/new.md", "", false)
	if result.HasConflict {
		t.Error("expected no conflict for new file")
	}
}

func TestCheckCreateConflict_ExistingFile(t *testing.T) {
	result := CheckCreateConflict("notes/hello.md", "2026-04-21T10:00:00Z", true)
	if !result.HasConflict {
		t.Fatal("expected conflict for existing file")
	}
	if !strings.Contains(result.ConflictPath, "notes/hello.conflict-") {
		t.Errorf("unexpected conflict path: %s", result.ConflictPath)
	}
	if !strings.HasSuffix(result.ConflictPath, ".md") {
		t.Error("conflict path should end with .md")
	}
}

func TestCheckUpdateConflict_NoConflict(t *testing.T) {
	result := CheckUpdateConflict(
		"notes/hello.md",
		"2026-04-21T10:00:00Z",
		"2026-04-21T10:00:00Z",
	)
	if result.HasConflict {
		t.Error("expected no conflict when baseModifiedAt == serverModifiedAt")
	}
}

func TestCheckUpdateConflict_BaseNewerThanServer(t *testing.T) {
	result := CheckUpdateConflict(
		"notes/hello.md",
		"2026-04-21T15:00:00Z",
		"2026-04-21T10:00:00Z",
	)
	if result.HasConflict {
		t.Error("expected no conflict when baseModifiedAt > serverModifiedAt")
	}
}

func TestCheckUpdateConflict_Conflict(t *testing.T) {
	result := CheckUpdateConflict(
		"notes/hello.md",
		"2026-04-21T10:00:00Z",
		"2026-04-21T15:00:00Z",
	)
	if !result.HasConflict {
		t.Fatal("expected conflict when baseModifiedAt < serverModifiedAt")
	}
	if !strings.Contains(result.ConflictPath, "notes/hello.conflict-") {
		t.Errorf("unexpected conflict path: %s", result.ConflictPath)
	}
}

func TestConflictPath_NoExtension(t *testing.T) {
	result := CheckCreateConflict("notes/README", "2026-04-21T10:00:00Z", true)
	if !strings.Contains(result.ConflictPath, "notes/README.conflict-") {
		t.Errorf("unexpected conflict path: %s", result.ConflictPath)
	}
}

func TestConflictPath_HiddenFile(t *testing.T) {
	result := CheckCreateConflict(".obsidian/app.json", "2026-04-21T10:00:00Z", true)
	if !strings.Contains(result.ConflictPath, ".obsidian/app.conflict-") {
		t.Errorf("unexpected conflict path: %s", result.ConflictPath)
	}
	if !strings.HasSuffix(result.ConflictPath, ".json") {
		t.Error("should preserve .json extension")
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd server && go test ./internal/sync/ -v
```
Expected: FAIL

- [ ] **Step 3: conflict.go 구현**

```go
// server/internal/sync/conflict.go
package sync

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type ConflictResult struct {
	HasConflict  bool
	ConflictPath string
}

func CheckCreateConflict(path, serverModifiedAt string, existsOnServer bool) ConflictResult {
	if !existsOnServer {
		return ConflictResult{}
	}
	return ConflictResult{
		HasConflict:  true,
		ConflictPath: makeConflictPath(path),
	}
}

func CheckUpdateConflict(path, baseModifiedAt, serverModifiedAt string) ConflictResult {
	if baseModifiedAt >= serverModifiedAt {
		return ConflictResult{}
	}
	return ConflictResult{
		HasConflict:  true,
		ConflictPath: makeConflictPath(path),
	}
}

func makeConflictPath(path string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	ts := time.Now().UTC().Format("20060102T150405Z")
	if ext == "" {
		return fmt.Sprintf("%s.conflict-%s", base, ts)
	}
	return fmt.Sprintf("%s.conflict-%s%s", base, ts, ext)
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd server && go test ./internal/sync/ -v
```
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add server/
git commit -m "feat: add conflict detection logic"
```

---

## Task 9: WebSocket 메시지 타입 + Hub

**Files:**
- Create: `server/internal/ws/messages.go`
- Create: `server/internal/ws/hub.go`
- Create: `server/internal/ws/client.go`
- Test: `server/internal/ws/hub_test.go`

- [ ] **Step 1: gorilla/websocket 의존성 추가**

```bash
cd server && go get github.com/gorilla/websocket
```

- [ ] **Step 2: messages.go 작성**

```go
// server/internal/ws/messages.go
package ws

import "encoding/json"

type FileEntry struct {
	Path       string `json:"path"`
	ModifiedAt string `json:"modifiedAt,omitempty"`
	Content    string `json:"content,omitempty"`
	Encoding   string `json:"encoding,omitempty"`
}

type IncomingMessage struct {
	Type           string      `json:"type"`
	Vault          string      `json:"vault"`
	Path           string      `json:"path,omitempty"`
	Content        string      `json:"content,omitempty"`
	Encoding       string      `json:"encoding,omitempty"`
	ModifiedAt     string      `json:"modifiedAt,omitempty"`
	BaseModifiedAt string      `json:"baseModifiedAt,omitempty"`
	NewModifiedAt  string      `json:"newModifiedAt,omitempty"`
	Files          []FileEntry `json:"files,omitempty"`
}

type OutgoingMessage struct {
	Type          string      `json:"type"`
	Vault         string      `json:"vault,omitempty"`
	ToUpload      []string    `json:"toUpload,omitempty"`
	ToDownload    []FileEntry `json:"toDownload,omitempty"`
	ToDelete      []string    `json:"toDelete,omitempty"`
	FilesToAdd    []FileEntry `json:"filesToAdd,omitempty"`
	FilesToDelete []string    `json:"filesToDelete,omitempty"`
	Error         string      `json:"error,omitempty"`
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

- [ ] **Step 3: hub_test.go 작성**

```go
// server/internal/ws/hub_test.go
package ws

import (
	"testing"
)

func TestHubRegisterUnregister(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	client := &Client{
		hub:  hub,
		send: make(chan []byte, 256),
	}

	hub.Register <- client

	// Verify client registered
	hub.mu.RLock()
	if len(hub.clients) != 1 {
		t.Errorf("expected 1 client, got %d", len(hub.clients))
	}
	hub.mu.RUnlock()

	hub.Unregister <- client

	// Give hub goroutine time to process
	// Check by trying to register another
	client2 := &Client{
		hub:  hub,
		send: make(chan []byte, 256),
	}
	hub.Register <- client2

	hub.mu.RLock()
	if len(hub.clients) != 1 {
		t.Errorf("expected 1 client after unregister+register, got %d", len(hub.clients))
	}
	hub.mu.RUnlock()
}

func TestHubBroadcastToVault(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c1 := &Client{hub: hub, send: make(chan []byte, 256), vault: "personal"}
	c2 := &Client{hub: hub, send: make(chan []byte, 256), vault: "personal"}
	c3 := &Client{hub: hub, send: make(chan []byte, 256), vault: "work"}

	hub.Register <- c1
	hub.Register <- c2
	hub.Register <- c3

	hub.BroadcastToVault("personal", []byte("hello"), c1)

	// c2 should receive, c1 (sender) and c3 (different vault) should not
	msg := <-c2.send
	if string(msg) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(msg))
	}

	select {
	case m := <-c3.send:
		t.Errorf("c3 should not receive, got '%s'", string(m))
	default:
	}
}
```

- [ ] **Step 4: 테스트 실패 확인**

```bash
cd server && go test ./internal/ws/ -v
```
Expected: FAIL

- [ ] **Step 5: hub.go 구현**

```go
// server/internal/ws/hub.go
package ws

import "sync"

type Hub struct {
	clients    map[*Client]bool
	Register   chan *Client
	Unregister chan *Client
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) BroadcastToVault(vault string, message []byte, sender *Client) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		if client != sender && client.vault == vault {
			select {
			case client.send <- message:
			default:
			}
		}
	}
}
```

- [ ] **Step 6: client.go 스캐폴딩**

```go
// server/internal/ws/client.go
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
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	vault   string
	handler MessageHandler
}

type MessageHandler interface {
	HandleMessage(client *Client, data []byte)
}

func NewClient(hub *Hub, conn *websocket.Conn, handler MessageHandler) *Client {
	return &Client{
		hub:     hub,
		conn:    conn,
		send:    make(chan []byte, 256),
		handler: handler,
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister <- c
		c.conn.Close()
	}()
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

func (c *Client) SendMessage(msg OutgoingMessage) {
	data, err := MarshalMessage(msg)
	if err != nil {
		log.Printf("failed to marshal message: %v", err)
		return
	}
	select {
	case c.send <- data:
	default:
	}
}
```

- [ ] **Step 7: 테스트 통과 확인**

```bash
cd server && go test ./internal/ws/ -v
```
Expected: PASS

- [ ] **Step 8: 커밋**

```bash
git add server/
git commit -m "feat: add WebSocket hub, client, message types"
```

---

## Task 10: WebSocket 핸들러 (sync_init, file ops, vault_create)

**Files:**
- Create: `server/internal/ws/handler.go`
- Test: `server/internal/ws/handler_test.go`

- [ ] **Step 1: handler_test.go 작성**

```go
// server/internal/ws/handler_test.go
package ws

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"obsidian-sync/internal/db"
	"obsidian-sync/internal/storage"
)

type testClient struct {
	sent []OutgoingMessage
}

func setupHandler(t *testing.T) (*Handler, *db.Queries, *storage.Storage, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	q := db.NewQueries(database)
	s := storage.New(dir)
	hub := NewHub()
	go hub.Run()
	h := NewHandler(q, s, hub)
	return h, q, s, dir
}

func makeClient(hub *Hub, vault string) *Client {
	return &Client{
		hub:   hub,
		send:  make(chan []byte, 256),
		vault: vault,
	}
}

func readResponse(t *testing.T, c *Client) OutgoingMessage {
	t.Helper()
	data := <-c.send
	var msg OutgoingMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	return msg
}

func TestHandleVaultCreate(t *testing.T) {
	h, q, _, _ := setupHandler(t)
	c := makeClient(h.hub, "")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "vault_create",
		Vault: "personal",
	}))

	resp := readResponse(t, c)
	if resp.Type != "vault_created" {
		t.Errorf("expected vault_created, got %s", resp.Type)
	}

	exists, _ := q.VaultExists("personal")
	if !exists {
		t.Error("vault not created in DB")
	}
}

func TestHandleSyncInit(t *testing.T) {
	h, q, s, _ := setupHandler(t)

	q.CreateVault("personal")
	s.WriteFile("personal", "notes/server-only.md", []byte("server content"))
	q.UpsertFile("personal", "notes/server-only.md", "2026-04-21T12:00:00Z")
	s.WriteFile("personal", "notes/both.md", []byte("newer on server"))
	q.UpsertFile("personal", "notes/both.md", "2026-04-21T15:00:00Z")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "sync_init",
		Vault: "personal",
		Files: []FileEntry{
			{Path: "notes/both.md", ModifiedAt: "2026-04-21T10:00:00Z"},
			{Path: "notes/client-only.md", ModifiedAt: "2026-04-21T10:00:00Z"},
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "sync_result" {
		t.Fatalf("expected sync_result, got %s", resp.Type)
	}

	if len(resp.ToUpload) != 1 || resp.ToUpload[0] != "notes/client-only.md" {
		t.Errorf("expected toUpload=[notes/client-only.md], got %v", resp.ToUpload)
	}
	if len(resp.ToDownload) != 2 {
		t.Errorf("expected 2 toDownload, got %d", len(resp.ToDownload))
	}
}

func TestHandleFileCreate(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.CreateVaultDir("personal")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:          "file_create",
		Vault:         "personal",
		Path:          "notes/new.md",
		Content:       "# New Note",
		NewModifiedAt: "2026-04-21T15:00:00Z",
	}))

	resp := readResponse(t, c)
	if resp.Type != "file_create_result" {
		t.Fatalf("expected file_create_result, got %s", resp.Type)
	}

	content, _ := s.ReadFile("personal", "notes/new.md")
	if string(content) != "# New Note" {
		t.Errorf("file not written correctly")
	}
}

func TestHandleFileCreateConflict(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/existing.md", []byte("original"))
	q.UpsertFile("personal", "notes/existing.md", "2026-04-21T10:00:00Z")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:          "file_create",
		Vault:         "personal",
		Path:          "notes/existing.md",
		Content:       "conflicting content",
		NewModifiedAt: "2026-04-21T15:00:00Z",
	}))

	resp := readResponse(t, c)
	if len(resp.FilesToAdd) != 1 {
		t.Fatalf("expected 1 conflict file, got %d", len(resp.FilesToAdd))
	}
}

func TestHandleFileUpdate(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("old"))
	q.UpsertFile("personal", "notes/hello.md", "2026-04-21T10:00:00Z")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:           "file_update",
		Vault:          "personal",
		Path:           "notes/hello.md",
		Content:        "updated",
		BaseModifiedAt: "2026-04-21T10:00:00Z",
		NewModifiedAt:  "2026-04-21T15:00:00Z",
	}))

	resp := readResponse(t, c)
	if resp.Type != "file_update_result" {
		t.Fatalf("expected file_update_result, got %s", resp.Type)
	}

	content, _ := s.ReadFile("personal", "notes/hello.md")
	if string(content) != "updated" {
		t.Errorf("expected 'updated', got '%s'", string(content))
	}
}

func TestHandleFileUpdateConflict(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("server version"))
	q.UpsertFile("personal", "notes/hello.md", "2026-04-21T15:00:00Z")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:           "file_update",
		Vault:          "personal",
		Path:           "notes/hello.md",
		Content:        "client version",
		BaseModifiedAt: "2026-04-21T10:00:00Z",
		NewModifiedAt:  "2026-04-21T16:00:00Z",
	}))

	resp := readResponse(t, c)
	if len(resp.FilesToAdd) != 1 {
		t.Fatalf("expected 1 conflict file, got %d", len(resp.FilesToAdd))
	}
}

func TestHandleFileDelete(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/old.md", []byte("delete me"))
	q.UpsertFile("personal", "notes/old.md", "2026-04-21T10:00:00Z")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "file_delete",
		Vault: "personal",
		Path:  "notes/old.md",
	}))

	resp := readResponse(t, c)
	if resp.Type != "file_delete_result" {
		t.Fatalf("expected file_delete_result, got %s", resp.Type)
	}

	f, _ := q.GetFile("personal", "notes/old.md")
	if !f.IsDeleted {
		t.Error("expected file to be marked deleted")
	}
}

func TestHandleFileDeleteNonExistent(t *testing.T) {
	h, q, _, _ := setupHandler(t)
	q.CreateVault("personal")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "file_delete",
		Vault: "personal",
		Path:  "notes/nonexistent.md",
	}))

	resp := readResponse(t, c)
	if resp.Type != "file_delete_result" {
		t.Fatalf("expected file_delete_result, got %s", resp.Type)
	}
}

func mustJSON(msg IncomingMessage) []byte {
	data, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return data
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd server && go test ./internal/ws/ -v -run TestHandle
```
Expected: FAIL

- [ ] **Step 3: handler.go 구현**

```go
// server/internal/ws/handler.go
package ws

import (
	"database/sql"
	"encoding/base64"
	"log"

	"obsidian-sync/internal/db"
	"obsidian-sync/internal/storage"
	syncpkg "obsidian-sync/internal/sync"
)

type Handler struct {
	queries *db.Queries
	storage *storage.Storage
	hub     *Hub
}

func NewHandler(q *db.Queries, s *storage.Storage, hub *Hub) *Handler {
	return &Handler{queries: q, storage: s, hub: hub}
}

func (h *Handler) HandleMessage(client *Client, data []byte) {
	msg, err := UnmarshalMessage(data)
	if err != nil {
		log.Printf("failed to parse message: %v", err)
		return
	}

	switch msg.Type {
	case "vault_create":
		h.handleVaultCreate(client, msg)
	case "sync_init":
		h.handleSyncInit(client, msg)
	case "file_upload":
		h.handleFileUpload(client, msg)
	case "file_create":
		h.handleFileCreate(client, msg)
	case "file_update":
		h.handleFileUpdate(client, msg)
	case "file_delete":
		h.handleFileDelete(client, msg)
	default:
		log.Printf("unknown message type: %s", msg.Type)
	}
}

func (h *Handler) handleVaultCreate(client *Client, msg IncomingMessage) {
	if err := h.queries.CreateVault(msg.Vault); err != nil {
		client.SendMessage(OutgoingMessage{Type: "error", Error: err.Error()})
		return
	}
	h.storage.CreateVaultDir(msg.Vault)
	client.SendMessage(OutgoingMessage{Type: "vault_created", Vault: msg.Vault})
}

func (h *Handler) handleSyncInit(client *Client, msg IncomingMessage) {
	client.vault = msg.Vault

	serverFiles, err := h.queries.ListFiles(msg.Vault)
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: "error", Error: err.Error()})
		return
	}

	serverMap := make(map[string]db.File)
	for _, f := range serverFiles {
		serverMap[f.Path] = f
	}

	clientMap := make(map[string]FileEntry)
	for _, f := range msg.Files {
		clientMap[f.Path] = f
	}

	var toUpload []string
	var toDownload []FileEntry
	var toDelete []string

	for _, cf := range msg.Files {
		sf, exists := serverMap[cf.Path]
		if !exists {
			toUpload = append(toUpload, cf.Path)
		} else if sf.ModifiedAt > cf.ModifiedAt {
			content, err := h.storage.ReadFile(msg.Vault, cf.Path)
			if err != nil {
				continue
			}
			toDownload = append(toDownload, makeFileEntry(cf.Path, content, sf.ModifiedAt))
		} else if sf.ModifiedAt < cf.ModifiedAt {
			toUpload = append(toUpload, cf.Path)
		}
	}

	for _, sf := range serverFiles {
		if _, exists := clientMap[sf.Path]; !exists {
			content, err := h.storage.ReadFile(msg.Vault, sf.Path)
			if err != nil {
				continue
			}
			toDownload = append(toDownload, makeFileEntry(sf.Path, content, sf.ModifiedAt))
		}
	}

	client.SendMessage(OutgoingMessage{
		Type:       "sync_result",
		Vault:      msg.Vault,
		ToUpload:   toUpload,
		ToDownload: toDownload,
		ToDelete:   toDelete,
	})
}

func (h *Handler) handleFileUpload(client *Client, msg IncomingMessage) {
	content := decodeContent(msg.Content, msg.Encoding)
	h.storage.WriteFile(msg.Vault, msg.Path, content)
	h.queries.UpsertFile(msg.Vault, msg.Path, msg.ModifiedAt)

	h.broadcastChange(client, msg.Vault, []FileEntry{
		makeFileEntry(msg.Path, content, msg.ModifiedAt),
	}, nil)
}

func (h *Handler) handleFileCreate(client *Client, msg IncomingMessage) {
	serverFile, err := h.queries.GetFile(msg.Vault, msg.Path)
	existsOnServer := err == nil && !serverFile.IsDeleted

	result := syncpkg.CheckCreateConflict(msg.Path, serverFile.ModifiedAt, existsOnServer)
	content := decodeContent(msg.Content, msg.Encoding)

	var filesToAdd []FileEntry

	if result.HasConflict {
		h.storage.WriteFile(msg.Vault, result.ConflictPath, content)
		h.queries.UpsertFile(msg.Vault, result.ConflictPath, msg.NewModifiedAt)
		filesToAdd = append(filesToAdd, makeFileEntry(result.ConflictPath, content, msg.NewModifiedAt))
	} else {
		h.storage.WriteFile(msg.Vault, msg.Path, content)
		h.queries.UpsertFile(msg.Vault, msg.Path, msg.NewModifiedAt)
	}

	client.SendMessage(OutgoingMessage{
		Type:       "file_create_result",
		FilesToAdd: filesToAdd,
	})

	if result.HasConflict {
		h.broadcastChange(client, msg.Vault, filesToAdd, nil)
	} else {
		h.broadcastChange(client, msg.Vault, []FileEntry{
			makeFileEntry(msg.Path, content, msg.NewModifiedAt),
		}, nil)
	}
}

func (h *Handler) handleFileUpdate(client *Client, msg IncomingMessage) {
	serverFile, err := h.queries.GetFile(msg.Vault, msg.Path)
	if err != nil && err != sql.ErrNoRows {
		client.SendMessage(OutgoingMessage{Type: "error", Error: err.Error()})
		return
	}

	content := decodeContent(msg.Content, msg.Encoding)
	var filesToAdd []FileEntry

	if err == sql.ErrNoRows || serverFile.IsDeleted {
		h.storage.WriteFile(msg.Vault, msg.Path, content)
		h.queries.UpsertFile(msg.Vault, msg.Path, msg.NewModifiedAt)
	} else {
		result := syncpkg.CheckUpdateConflict(msg.Path, msg.BaseModifiedAt, serverFile.ModifiedAt)
		if result.HasConflict {
			h.storage.WriteFile(msg.Vault, result.ConflictPath, content)
			h.queries.UpsertFile(msg.Vault, result.ConflictPath, msg.NewModifiedAt)
			filesToAdd = append(filesToAdd, makeFileEntry(result.ConflictPath, content, msg.NewModifiedAt))
		} else {
			h.storage.WriteFile(msg.Vault, msg.Path, content)
			h.queries.UpsertFile(msg.Vault, msg.Path, msg.NewModifiedAt)
		}
	}

	client.SendMessage(OutgoingMessage{
		Type:       "file_update_result",
		FilesToAdd: filesToAdd,
	})

	if len(filesToAdd) > 0 {
		h.broadcastChange(client, msg.Vault, filesToAdd, nil)
	} else {
		h.broadcastChange(client, msg.Vault, []FileEntry{
			makeFileEntry(msg.Path, content, msg.NewModifiedAt),
		}, nil)
	}
}

func (h *Handler) handleFileDelete(client *Client, msg IncomingMessage) {
	h.queries.MarkFileDeleted(msg.Vault, msg.Path)
	h.storage.DeleteFile(msg.Vault, msg.Path)

	client.SendMessage(OutgoingMessage{
		Type: "file_delete_result",
	})

	h.broadcastChange(client, msg.Vault, nil, []string{msg.Path})
}

func (h *Handler) broadcastChange(sender *Client, vault string, filesToAdd []FileEntry, filesToDelete []string) {
	msg := OutgoingMessage{
		Type:          "remote_change",
		Vault:         vault,
		FilesToAdd:    filesToAdd,
		FilesToDelete: filesToDelete,
	}
	data, err := MarshalMessage(msg)
	if err != nil {
		return
	}
	h.hub.BroadcastToVault(vault, data, sender)
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

func makeFileEntry(path string, content []byte, modifiedAt string) FileEntry {
	entry := FileEntry{Path: path, ModifiedAt: modifiedAt}
	if isBinary(content) {
		entry.Content = base64.StdEncoding.EncodeToString(content)
		entry.Encoding = "base64"
	} else {
		entry.Content = string(content)
	}
	return entry
}

func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd server && go test ./internal/ws/ -v
```
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add server/
git commit -m "feat: add WebSocket message handlers with sync and conflict logic"
```

---

## Task 11: 대시보드 인증 + API

**Files:**
- Create: `server/internal/dashboard/auth.go`
- Create: `server/internal/dashboard/handler.go`

- [ ] **Step 1: auth.go 작성**

```go
// server/internal/dashboard/auth.go
package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

type SessionStore struct {
	sessions map[string]time.Time
	mu       sync.RWMutex
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]time.Time)}
}

func (s *SessionStore) Create() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(24 * time.Hour)
	s.mu.Unlock()
	return token, nil
}

func (s *SessionStore) Valid(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.sessions[token]
	return ok && time.Now().Before(exp)
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func (s *SessionStore) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil || !s.Valid(cookie.Value) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 2: handler.go 작성**

```go
// server/internal/dashboard/handler.go
package dashboard

import (
	"encoding/json"
	"net/http"

	"obsidian-sync/internal/config"
	"obsidian-sync/internal/db"
	"obsidian-sync/internal/storage"
)

type Dashboard struct {
	cfg      config.Config
	queries  *db.Queries
	storage  *storage.Storage
	sessions *SessionStore
}

func New(cfg config.Config, q *db.Queries, s *storage.Storage) *Dashboard {
	return &Dashboard{
		cfg:      cfg,
		queries:  q,
		storage:  s,
		sessions: NewSessionStore(),
	}
}

func (d *Dashboard) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/login", d.handleLogin)
	mux.HandleFunc("/logout", d.handleLogout)

	authed := d.sessions.AuthMiddleware
	mux.Handle("/", authed(http.HandlerFunc(d.handleIndex)))
	mux.Handle("/api/vaults", authed(http.HandlerFunc(d.handleVaults)))
	mux.Handle("/api/vaults/", authed(http.HandlerFunc(d.handleVaultGitHub)))
	mux.Handle("/api/tokens", authed(http.HandlerFunc(d.handleTokens)))
}

func (d *Dashboard) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html")
		loginTemplate.Execute(w, nil)
		return
	}

	user := r.FormValue("username")
	pass := r.FormValue("password")
	if user != d.cfg.AdminUser || pass != d.cfg.AdminPass {
		w.Header().Set("Content-Type", "text/html")
		loginTemplate.Execute(w, map[string]string{"Error": "Invalid credentials"})
		return
	}

	token, _ := d.sessions.Create()
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (d *Dashboard) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		d.sessions.Delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (d *Dashboard) handleIndex(w http.ResponseWriter, r *http.Request) {
	vaults, _ := d.queries.ListVaults()

	type vaultInfo struct {
		Name      string
		CreatedAt string
		FileCount int
		TotalSize int64
	}
	var infos []vaultInfo
	for _, v := range vaults {
		count, size, _ := d.storage.VaultStats(v.Name)
		infos = append(infos, vaultInfo{
			Name:      v.Name,
			CreatedAt: v.CreatedAt,
			FileCount: count,
			TotalSize: size,
		})
	}

	w.Header().Set("Content-Type", "text/html")
	indexTemplate.Execute(w, infos)
}

func (d *Dashboard) handleVaults(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		vaults, _ := d.queries.ListVaults()
		json.NewEncoder(w).Encode(vaults)
	case http.MethodPost:
		var req struct{ Name string }
		json.NewDecoder(r.Body).Decode(&req)
		d.queries.CreateVault(req.Name)
		d.storage.CreateVaultDir(req.Name)
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		d.queries.DeleteVault(name)
		d.storage.DeleteVaultDir(name)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (d *Dashboard) handleVaultGitHub(w http.ResponseWriter, r *http.Request) {
	// Extract vault name: /api/vaults/{name}/github
	// Simple path parsing
	parts := splitPath(r.URL.Path)
	if len(parts) < 4 {
		http.NotFound(w, r)
		return
	}
	vaultName := parts[2]

	switch r.Method {
	case http.MethodGet:
		cfg, err := d.queries.GetGitHubConfig(vaultName)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(cfg)
	case http.MethodPut:
		var cfg db.GitHubConfig
		json.NewDecoder(r.Body).Decode(&cfg)
		cfg.VaultName = vaultName
		d.queries.SetGitHubConfig(cfg)
		w.WriteHeader(http.StatusOK)
	}
}

func (d *Dashboard) handleTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tokens, _ := d.queries.ListTokens()
		json.NewEncoder(w).Encode(tokens)
	case http.MethodPost:
		action := r.URL.Query().Get("action")
		if action == "regenerate" {
			old := r.URL.Query().Get("token")
			newToken, _ := d.queries.RegenerateToken(old)
			json.NewEncoder(w).Encode(map[string]string{"token": newToken})
		} else {
			token, _ := d.queries.GenerateToken()
			json.NewEncoder(w).Encode(map[string]string{"token": token})
		}
	case http.MethodDelete:
		token := r.URL.Query().Get("token")
		d.queries.DeactivateToken(token)
		w.WriteHeader(http.StatusNoContent)
	}
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range split(path, '/') {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func split(s string, sep byte) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}
```

- [ ] **Step 3: 커밋**

```bash
git add server/
git commit -m "feat: add dashboard auth and HTTP API handlers"
```

---

## Task 12: 대시보드 HTML 템플릿

**Files:**
- Create: `server/internal/dashboard/templates.go`

- [ ] **Step 1: templates.go 작성 (Go embed)**

```go
// server/internal/dashboard/templates.go
package dashboard

import "html/template"

var loginTemplate = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html><head><title>Obsidian Sync - Login</title>
<style>
body { font-family: system-ui; max-width: 400px; margin: 100px auto; padding: 0 20px; }
form { display: flex; flex-direction: column; gap: 12px; }
input { padding: 8px; border: 1px solid #ccc; border-radius: 4px; }
button { padding: 10px; background: #7c3aed; color: white; border: none; border-radius: 4px; cursor: pointer; }
.error { color: red; }
</style></head>
<body>
<h1>Obsidian Sync</h1>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="POST" action="/login">
<input name="username" placeholder="Username" required>
<input name="password" type="password" placeholder="Password" required>
<button type="submit">Login</button>
</form>
</body></html>`))

var indexTemplate = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html><head><title>Obsidian Sync</title>
<style>
body { font-family: system-ui; max-width: 900px; margin: 0 auto; padding: 20px; }
nav { display: flex; gap: 20px; border-bottom: 1px solid #eee; padding-bottom: 10px; margin-bottom: 20px; }
nav a { text-decoration: none; color: #7c3aed; }
table { width: 100%; border-collapse: collapse; }
th, td { padding: 8px 12px; border-bottom: 1px solid #eee; text-align: left; }
button { padding: 6px 12px; background: #7c3aed; color: white; border: none; border-radius: 4px; cursor: pointer; }
button.danger { background: #dc2626; }
.token { font-family: monospace; font-size: 12px; }
</style></head>
<body>
<h1>Obsidian Sync</h1>
<nav>
<a href="/">Vaults</a>
<a href="/logout">Logout</a>
</nav>
<h2>Vaults</h2>
<table>
<tr><th>Name</th><th>Files</th><th>Size</th><th>Created</th></tr>
{{range .}}
<tr><td>{{.Name}}</td><td>{{.FileCount}}</td><td>{{.TotalSize}} bytes</td><td>{{.CreatedAt}}</td></tr>
{{else}}
<tr><td colspan="4">No vaults yet</td></tr>
{{end}}
</table>
</body></html>`))
```

- [ ] **Step 2: 커밋**

```bash
git add server/
git commit -m "feat: add dashboard HTML templates"
```

---

## Task 13: 서버 엔트리포인트 조립

**Files:**
- Modify: `server/cmd/server/main.go`

- [ ] **Step 1: main.go 완성**

```go
// server/cmd/server/main.go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"obsidian-sync/internal/config"
	"obsidian-sync/internal/dashboard"
	"obsidian-sync/internal/db"
	"obsidian-sync/internal/github"
	"obsidian-sync/internal/storage"
	"obsidian-sync/internal/ws"
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
	hub := ws.NewHub()
	go hub.Run()

	handler := ws.NewHandler(queries, store, hub)

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

		client := ws.NewClient(hub, conn, handler)
		hub.Register <- client
		go client.WritePump()
		go client.ReadPump()
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("Obsidian Sync running on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
```

- [ ] **Step 2: 빌드 확인**

```bash
cd server && go build ./cmd/server/
```
Expected: 빌드 성공 (github 패키지 아직 없으므로 Task 14 이후)

- [ ] **Step 3: 커밋**

```bash
git add server/
git commit -m "feat: wire up server entrypoint"
```

---

## Task 14: GitHub 백업 서비스

**Files:**
- Create: `server/internal/github/backup.go`
- Test: `server/internal/github/backup_test.go`

- [ ] **Step 1: backup_test.go 작성**

```go
// server/internal/github/backup_test.go
package github

import (
	"testing"
	"time"
)

func TestParseInterval(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"1h", time.Hour},
		{"30m", 30 * time.Minute},
		{"2h", 2 * time.Hour},
		{"invalid", time.Hour}, // default
	}

	for _, tt := range tests {
		got := parseInterval(tt.input)
		if got != tt.expected {
			t.Errorf("parseInterval(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd server && go test ./internal/github/ -v
```
Expected: FAIL

- [ ] **Step 3: backup.go 구현**

```go
// server/internal/github/backup.go
package github

import (
	"log"
	"os/exec"
	"time"

	"obsidian-sync/internal/db"
	"obsidian-sync/internal/storage"
)

type BackupService struct {
	queries *db.Queries
	storage *storage.Storage
	stop    chan struct{}
}

func NewBackupService(q *db.Queries, s *storage.Storage) *BackupService {
	return &BackupService{
		queries: q,
		storage: s,
		stop:    make(chan struct{}),
	}
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
		log.Printf("backup: failed to list vaults: %v", err)
		return
	}

	for _, vault := range vaults {
		cfg, err := b.queries.GetGitHubConfig(vault.Name)
		if err != nil || !cfg.Enabled {
			continue
		}
		b.backupVault(vault.Name, cfg)
	}
}

func (b *BackupService) backupVault(vaultName string, cfg db.GitHubConfig) {
	dir := b.storage.VaultDir(vaultName)

	if !isGitRepo(dir) {
		run(dir, "git", "init")
		run(dir, "git", "remote", "add", "origin", cfg.RemoteURL)
	}

	run(dir, "git", "add", "-A")

	if err := run(dir, "git", "diff", "--cached", "--quiet"); err != nil {
		run(dir, "git", "commit", "-m", "auto backup: "+time.Now().UTC().Format(time.RFC3339))
	}

	run(dir, "git", "push", "-u", "origin", cfg.Branch)
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

func parseInterval(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Hour
	}
	return d
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd server && go test ./internal/github/ -v
```
Expected: PASS

- [ ] **Step 5: 빌드 전체 확인**

```bash
cd server && go build ./cmd/server/
```
Expected: 빌드 성공

- [ ] **Step 6: 커밋**

```bash
git add server/
git commit -m "feat: add GitHub periodic backup service"
```

---

## Task 15: Dockerfile + docker-compose

**Files:**
- Create: `server/Dockerfile`
- Create: `server/docker-compose.yml`

- [ ] **Step 1: Dockerfile 작성**

```dockerfile
# server/Dockerfile
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache gcc musl-dev git

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o obsidian-sync ./cmd/server/

FROM alpine:3.19
RUN apk add --no-cache git ca-certificates
WORKDIR /app
COPY --from=builder /build/obsidian-sync .
RUN mkdir -p /app/data

EXPOSE 8080
CMD ["./obsidian-sync"]
```

- [ ] **Step 2: docker-compose.yml 작성**

```yaml
# server/docker-compose.yml
services:
  obsidian-sync:
    build: .
    ports:
      - "8080:8080"
    environment:
      - OBSIDIAN_SYNC_ADMIN_USER=admin
      - OBSIDIAN_SYNC_ADMIN_PASS=changeme
      - OBSIDIAN_SYNC_PORT=8080
    volumes:
      - ./data:/app/data
    restart: unless-stopped
```

- [ ] **Step 3: 커밋**

```bash
git add server/
git commit -m "feat: add Dockerfile and docker-compose"
```

---

## Task 16: 옵시디언 플러그인 스캐폴딩

**Files:**
- Create: `plugin/manifest.json`
- Create: `plugin/package.json`
- Create: `plugin/tsconfig.json`
- Create: `plugin/esbuild.config.mjs`
- Create: `plugin/src/main.ts`
- Create: `plugin/styles.css`

- [ ] **Step 1: manifest.json 작성**

```json
{
  "id": "obsidian-sync",
  "name": "Obsidian Sync",
  "version": "0.1.0",
  "minAppVersion": "1.0.0",
  "description": "Sync vault to self-hosted server",
  "author": "fhdufhdu",
  "isDesktopOnly": false
}
```

- [ ] **Step 2: package.json 작성**

```json
{
  "name": "obsidian-sync-plugin",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "node esbuild.config.mjs",
    "build": "node esbuild.config.mjs production"
  },
  "devDependencies": {
    "@types/node": "^20.0.0",
    "esbuild": "^0.20.0",
    "obsidian": "latest",
    "typescript": "^5.4.0"
  }
}
```

- [ ] **Step 3: tsconfig.json 작성**

```json
{
  "compilerOptions": {
    "baseUrl": ".",
    "inlineSourceMap": true,
    "inlineSources": true,
    "module": "ESNext",
    "target": "ES6",
    "allowJs": true,
    "noImplicitAny": true,
    "moduleResolution": "node",
    "importHelpers": true,
    "isolatedModules": true,
    "strictNullChecks": true,
    "lib": ["DOM", "ES5", "ES6", "ES7"]
  },
  "include": ["src/**/*.ts"]
}
```

- [ ] **Step 4: esbuild.config.mjs 작성**

```js
// plugin/esbuild.config.mjs
import esbuild from "esbuild";
import process from "process";

const prod = process.argv[2] === "production";

esbuild.build({
  entryPoints: ["src/main.ts"],
  bundle: true,
  external: ["obsidian"],
  format: "cjs",
  target: "es2018",
  logLevel: "info",
  sourcemap: prod ? false : "inline",
  treeShaking: true,
  outfile: "main.js",
  minify: prod,
  watch: !prod ? {} : false,
}).catch(() => process.exit(1));
```

- [ ] **Step 5: main.ts 스캐폴딩**

```typescript
// plugin/src/main.ts
import { Plugin } from "obsidian";
import { SyncSettingTab, SyncSettings, DEFAULT_SETTINGS } from "./settings";

export default class ObsidianSyncPlugin extends Plugin {
  settings: SyncSettings;

  async onload() {
    await this.loadSettings();
    this.addSettingTab(new SyncSettingTab(this.app, this));
  }

  onunload() {}

  async loadSettings() {
    this.settings = Object.assign({}, DEFAULT_SETTINGS, await this.loadData());
  }

  async saveSettings() {
    await this.saveData(this.settings);
  }
}
```

- [ ] **Step 6: styles.css 빈 파일**

```css
/* plugin/styles.css */
```

- [ ] **Step 7: npm install + 빌드 확인**

```bash
cd plugin && npm install && npm run build
```
Expected: 빌드 성공 (settings.ts 아직 없으므로 다음 태스크 이후)

- [ ] **Step 8: 커밋**

```bash
git add plugin/
git commit -m "feat: scaffold Obsidian plugin"
```

---

## Task 17: 플러그인 설정 UI

**Files:**
- Create: `plugin/src/settings.ts`

- [ ] **Step 1: settings.ts 작성**

```typescript
// plugin/src/settings.ts
import { App, PluginSettingTab, Setting } from "obsidian";
import type ObsidianSyncPlugin from "./main";

export interface SyncSettings {
  serverUrl: string;
  token: string;
  vaultName: string;
}

export const DEFAULT_SETTINGS: SyncSettings = {
  serverUrl: "",
  token: "",
  vaultName: "",
};

export class SyncSettingTab extends PluginSettingTab {
  plugin: ObsidianSyncPlugin;

  constructor(app: App, plugin: ObsidianSyncPlugin) {
    super(app, plugin);
    this.plugin = plugin;
  }

  display(): void {
    const { containerEl } = this;
    containerEl.empty();
    containerEl.createEl("h2", { text: "Obsidian Sync Settings" });

    new Setting(containerEl)
      .setName("Server URL")
      .setDesc("WebSocket server URL (e.g. ws://192.168.1.100:8080)")
      .addText((text) =>
        text
          .setPlaceholder("ws://your-server:8080")
          .setValue(this.plugin.settings.serverUrl)
          .onChange(async (value) => {
            this.plugin.settings.serverUrl = value;
            await this.plugin.saveSettings();
          })
      );

    new Setting(containerEl)
      .setName("API Token")
      .setDesc("Server access token")
      .addText((text) =>
        text
          .setPlaceholder("your-token")
          .setValue(this.plugin.settings.token)
          .onChange(async (value) => {
            this.plugin.settings.token = value;
            await this.plugin.saveSettings();
          })
      );

    new Setting(containerEl)
      .setName("Vault Name")
      .setDesc("Server-side vault name")
      .addText((text) =>
        text
          .setPlaceholder("personal")
          .setValue(this.plugin.settings.vaultName)
          .onChange(async (value) => {
            this.plugin.settings.vaultName = value;
            await this.plugin.saveSettings();
          })
      );
  }
}
```

- [ ] **Step 2: 빌드 확인**

```bash
cd plugin && npm run build
```
Expected: 빌드 성공

- [ ] **Step 3: 커밋**

```bash
git add plugin/
git commit -m "feat: add plugin settings UI"
```

---

## Task 18: 플러그인 WebSocket 클라이언트

**Files:**
- Create: `plugin/src/ws-client.ts`

- [ ] **Step 1: ws-client.ts 작성**

```typescript
// plugin/src/ws-client.ts

export interface FileEntry {
  path: string;
  modifiedAt?: string;
  content?: string;
  encoding?: string;
}

export interface SyncResult {
  type: string;
  vault?: string;
  toUpload?: string[];
  toDownload?: FileEntry[];
  toDelete?: string[];
  filesToAdd?: FileEntry[];
  filesToDelete?: string[];
  error?: string;
}

type MessageCallback = (msg: SyncResult) => void;

export class WsClient {
  private ws: WebSocket | null = null;
  private serverUrl: string;
  private token: string;
  private callbacks: Map<string, MessageCallback[]> = new Map();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(serverUrl: string, token: string) {
    this.serverUrl = serverUrl;
    this.token = token;
  }

  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      const url = `${this.serverUrl}/ws?token=${this.token}`;
      this.ws = new WebSocket(url);

      this.ws.onopen = () => resolve();

      this.ws.onmessage = (event) => {
        const msg: SyncResult = JSON.parse(event.data);
        const handlers = this.callbacks.get(msg.type) || [];
        handlers.forEach((cb) => cb(msg));
      };

      this.ws.onclose = () => {
        this.scheduleReconnect();
      };

      this.ws.onerror = (err) => {
        reject(err);
      };
    });
  }

  disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  on(type: string, callback: MessageCallback) {
    if (!this.callbacks.has(type)) {
      this.callbacks.set(type, []);
    }
    this.callbacks.get(type)!.push(callback);
  }

  send(msg: Record<string, unknown>) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  sendSyncInit(vault: string, files: { path: string; modifiedAt: string }[]) {
    this.send({ type: "sync_init", vault, files });
  }

  sendFileCreate(vault: string, path: string, content: string, newModifiedAt: string, encoding?: string) {
    const msg: Record<string, unknown> = { type: "file_create", vault, path, content, newModifiedAt };
    if (encoding) msg.encoding = encoding;
    this.send(msg);
  }

  sendFileUpdate(
    vault: string,
    path: string,
    content: string,
    baseModifiedAt: string,
    newModifiedAt: string,
    encoding?: string
  ) {
    const msg: Record<string, unknown> = {
      type: "file_update", vault, path, content, baseModifiedAt, newModifiedAt,
    };
    if (encoding) msg.encoding = encoding;
    this.send(msg);
  }

  sendFileDelete(vault: string, path: string) {
    this.send({ type: "file_delete", vault, path });
  }

  sendFileUpload(vault: string, path: string, content: string, modifiedAt: string, encoding?: string) {
    const msg: Record<string, unknown> = { type: "file_upload", vault, path, content, modifiedAt };
    if (encoding) msg.encoding = encoding;
    this.send(msg);
  }

  sendVaultCreate(vault: string) {
    this.send({ type: "vault_create", vault });
  }

  private scheduleReconnect() {
    if (this.reconnectTimer) return;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect().catch(() => {});
    }, 5000);
  }
}
```

- [ ] **Step 2: 빌드 확인**

```bash
cd plugin && npm run build
```
Expected: 빌드 성공

- [ ] **Step 3: 커밋**

```bash
git add plugin/
git commit -m "feat: add WebSocket client for plugin"
```

---

## Task 19: 플러그인 파일 감지 + 동기화 오케스트레이션

**Files:**
- Create: `plugin/src/file-watcher.ts`
- Create: `plugin/src/sync.ts`
- Modify: `plugin/src/main.ts`

- [ ] **Step 1: file-watcher.ts 작성**

```typescript
// plugin/src/file-watcher.ts
import { Vault, TFile, TAbstractFile } from "obsidian";

export interface FileChange {
  type: "create" | "modify" | "delete";
  path: string;
}

export class FileWatcher {
  private vault: Vault;
  private onChange: (change: FileChange) => void;
  private openedTimes: Map<string, string> = new Map();

  constructor(vault: Vault, onChange: (change: FileChange) => void) {
    this.vault = vault;
    this.onChange = onChange;
  }

  start() {
    this.vault.on("create", (file: TAbstractFile) => {
      if (file instanceof TFile) {
        this.onChange({ type: "create", path: file.path });
      }
    });

    this.vault.on("modify", (file: TAbstractFile) => {
      if (file instanceof TFile) {
        this.onChange({ type: "modify", path: file.path });
      }
    });

    this.vault.on("delete", (file: TAbstractFile) => {
      if (file instanceof TFile) {
        this.openedTimes.delete(file.path);
        this.onChange({ type: "delete", path: file.path });
      }
    });
  }

  trackOpened(path: string, modifiedAt: string) {
    this.openedTimes.set(path, modifiedAt);
  }

  getBaseModifiedAt(path: string): string | undefined {
    return this.openedTimes.get(path);
  }

  getAllFiles(): { path: string; modifiedAt: string }[] {
    const files: { path: string; modifiedAt: string }[] = [];
    this.vault.getFiles().forEach((file) => {
      files.push({
        path: file.path,
        modifiedAt: new Date(file.stat.mtime).toISOString(),
      });
    });
    return files;
  }
}
```

- [ ] **Step 2: sync.ts 작성**

```typescript
// plugin/src/sync.ts
import { Vault, TFile, normalizePath } from "obsidian";
import { WsClient, FileEntry, SyncResult } from "./ws-client";
import { FileWatcher, FileChange } from "./file-watcher";

const BINARY_EXTENSIONS = new Set([
  "png", "jpg", "jpeg", "gif", "bmp", "svg", "webp",
  "pdf", "mp3", "mp4", "webm", "wav", "ogg",
  "zip", "tar", "gz",
]);

function isBinaryPath(path: string): boolean {
  const ext = path.split(".").pop()?.toLowerCase() || "";
  return BINARY_EXTENSIONS.has(ext);
}

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary);
}

function base64ToArrayBuffer(base64: string): ArrayBuffer {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
}

export class SyncManager {
  private vault: Vault;
  private wsClient: WsClient;
  private fileWatcher: FileWatcher;
  private vaultName: string;
  private syncing = false;

  constructor(vault: Vault, serverUrl: string, token: string, vaultName: string) {
    this.vault = vault;
    this.vaultName = vaultName;
    this.wsClient = new WsClient(serverUrl, token);
    this.fileWatcher = new FileWatcher(vault, (change) => this.handleLocalChange(change));
  }

  async start() {
    await this.wsClient.connect();

    this.wsClient.on("sync_result", (msg) => this.handleSyncResult(msg));
    this.wsClient.on("file_create_result", (msg) => this.handleOperationResult(msg));
    this.wsClient.on("file_update_result", (msg) => this.handleOperationResult(msg));
    this.wsClient.on("file_delete_result", (msg) => this.handleOperationResult(msg));
    this.wsClient.on("remote_change", (msg) => this.handleRemoteChange(msg));

    this.fileWatcher.start();

    const files = this.fileWatcher.getAllFiles();
    files.forEach((f) => this.fileWatcher.trackOpened(f.path, f.modifiedAt));
    this.wsClient.sendSyncInit(this.vaultName, files);
  }

  stop() {
    this.wsClient.disconnect();
  }

  private async handleLocalChange(change: FileChange) {
    if (this.syncing) return;

    if (change.type === "delete") {
      this.wsClient.sendFileDelete(this.vaultName, change.path);
      return;
    }

    const file = this.vault.getAbstractFileByPath(change.path);
    if (!(file instanceof TFile)) return;

    const content = await this.readFileContent(file);
    const encoding = isBinaryPath(file.path) ? "base64" : undefined;
    const newModifiedAt = new Date(file.stat.mtime).toISOString();

    if (change.type === "create") {
      this.wsClient.sendFileCreate(this.vaultName, file.path, content, newModifiedAt, encoding);
    } else {
      const baseModifiedAt = this.fileWatcher.getBaseModifiedAt(file.path) || newModifiedAt;
      this.wsClient.sendFileUpdate(this.vaultName, file.path, content, baseModifiedAt, newModifiedAt, encoding);
    }

    this.fileWatcher.trackOpened(file.path, newModifiedAt);
  }

  private async handleSyncResult(msg: SyncResult) {
    this.syncing = true;

    if (msg.toDownload) {
      for (const entry of msg.toDownload) {
        await this.applyFileEntry(entry);
      }
    }

    if (msg.toDelete) {
      for (const path of msg.toDelete) {
        const file = this.vault.getAbstractFileByPath(path);
        if (file instanceof TFile) {
          await this.vault.delete(file);
        }
      }
    }

    if (msg.toUpload) {
      for (const path of msg.toUpload) {
        const file = this.vault.getAbstractFileByPath(path);
        if (file instanceof TFile) {
          const content = await this.readFileContent(file);
          const encoding = isBinaryPath(path) ? "base64" : undefined;
          const modifiedAt = new Date(file.stat.mtime).toISOString();
          this.wsClient.sendFileUpload(this.vaultName, path, content, modifiedAt, encoding);
        }
      }
    }

    this.syncing = false;
  }

  private async handleOperationResult(msg: SyncResult) {
    this.syncing = true;
    if (msg.filesToAdd) {
      for (const entry of msg.filesToAdd) {
        await this.applyFileEntry(entry);
      }
    }
    if (msg.filesToDelete) {
      for (const path of msg.filesToDelete) {
        const file = this.vault.getAbstractFileByPath(path);
        if (file instanceof TFile) {
          await this.vault.delete(file);
        }
      }
    }
    this.syncing = false;
  }

  private async handleRemoteChange(msg: SyncResult) {
    this.syncing = true;
    if (msg.filesToAdd) {
      for (const entry of msg.filesToAdd) {
        await this.applyFileEntry(entry);
        if (entry.modifiedAt) {
          this.fileWatcher.trackOpened(entry.path, entry.modifiedAt);
        }
      }
    }
    if (msg.filesToDelete) {
      for (const path of msg.filesToDelete) {
        const file = this.vault.getAbstractFileByPath(path);
        if (file instanceof TFile) {
          await this.vault.delete(file);
        }
      }
    }
    this.syncing = false;
  }

  private async applyFileEntry(entry: FileEntry) {
    if (!entry.content) return;

    const normalized = normalizePath(entry.path);
    const existing = this.vault.getAbstractFileByPath(normalized);

    if (entry.encoding === "base64") {
      const data = base64ToArrayBuffer(entry.content);
      if (existing instanceof TFile) {
        await this.vault.modifyBinary(existing, data);
      } else {
        await this.vault.createBinary(normalized, data);
      }
    } else {
      if (existing instanceof TFile) {
        await this.vault.modify(existing, entry.content);
      } else {
        await this.vault.create(normalized, entry.content);
      }
    }

    if (entry.modifiedAt) {
      this.fileWatcher.trackOpened(normalized, entry.modifiedAt);
    }
  }

  private async readFileContent(file: TFile): Promise<string> {
    if (isBinaryPath(file.path)) {
      const buffer = await this.vault.readBinary(file);
      return arrayBufferToBase64(buffer);
    }
    return await this.vault.read(file);
  }
}
```

- [ ] **Step 3: main.ts 수정 — SyncManager 연결**

```typescript
// plugin/src/main.ts
import { Plugin, Notice } from "obsidian";
import { SyncSettingTab, SyncSettings, DEFAULT_SETTINGS } from "./settings";
import { SyncManager } from "./sync";

export default class ObsidianSyncPlugin extends Plugin {
  settings: SyncSettings;
  syncManager: SyncManager | null = null;

  async onload() {
    await this.loadSettings();
    this.addSettingTab(new SyncSettingTab(this.app, this));

    this.addCommand({
      id: "connect-sync",
      name: "Connect to sync server",
      callback: () => this.connectSync(),
    });

    this.addCommand({
      id: "disconnect-sync",
      name: "Disconnect from sync server",
      callback: () => this.disconnectSync(),
    });

    if (this.settings.serverUrl && this.settings.token && this.settings.vaultName) {
      this.connectSync();
    }
  }

  onunload() {
    this.disconnectSync();
  }

  async connectSync() {
    if (this.syncManager) {
      this.disconnectSync();
    }

    const { serverUrl, token, vaultName } = this.settings;
    if (!serverUrl || !token || !vaultName) {
      new Notice("Obsidian Sync: Please configure server URL, token, and vault name");
      return;
    }

    this.syncManager = new SyncManager(this.app.vault, serverUrl, token, vaultName);
    try {
      await this.syncManager.start();
      new Notice("Obsidian Sync: Connected");
    } catch {
      new Notice("Obsidian Sync: Connection failed");
      this.syncManager = null;
    }
  }

  disconnectSync() {
    if (this.syncManager) {
      this.syncManager.stop();
      this.syncManager = null;
      new Notice("Obsidian Sync: Disconnected");
    }
  }

  async loadSettings() {
    this.settings = Object.assign({}, DEFAULT_SETTINGS, await this.loadData());
  }

  async saveSettings() {
    await this.saveData(this.settings);
  }
}
```

- [ ] **Step 4: 빌드 확인**

```bash
cd plugin && npm run build
```
Expected: 빌드 성공

- [ ] **Step 5: 커밋**

```bash
git add plugin/
git commit -m "feat: add file watcher, sync manager, wire up plugin"
```

---

## Task 20: 통합 테스트 + 최종 확인

**Files:**
- No new files — verify full build and basic flow

- [ ] **Step 1: Go 서버 전체 테스트**

```bash
cd server && go test ./... -v
```
Expected: ALL PASS

- [ ] **Step 2: Go 서버 빌드**

```bash
cd server && go build ./cmd/server/
```
Expected: 빌드 성공

- [ ] **Step 3: 플러그인 빌드**

```bash
cd plugin && npm run build
```
Expected: 빌드 성공

- [ ] **Step 4: Docker 빌드**

```bash
cd server && docker build -t obsidian-sync .
```
Expected: 이미지 빌드 성공

- [ ] **Step 5: 커밋**

```bash
git add -A
git commit -m "chore: verify full build pipeline"
```
