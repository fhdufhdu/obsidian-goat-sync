# WebSocket Vault and Transaction Safety Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make WebSocket sync messages self-sufficient by ensuring vault rows server-side, processing each message inside a DB transaction, staging file writes/deletes safely, and surfacing server errors in the Obsidian plugin.

**Architecture:** The server DB layer gains transaction-scoped `Queries` and idempotent `EnsureVault`. WebSocket handlers record responses during transaction processing, finalize staged file operations only after commit, and return explicit errors when DB or finalize work fails. The plugin registers generic and result-specific error handling before action handling so server failures become Obsidian Notices.

**Tech Stack:** Go 1.25, SQLite, Gorilla WebSocket, TypeScript, Obsidian API, Vitest.

---

## Source Documents

- `docs/superpowers/specs/2026-04-26-ws-vault-transaction-safety-design.md`
- `server/internal/db/db.go`
- `server/internal/db/vault.go`
- `server/internal/storage/storage.go`
- `server/internal/ws/handler.go`
- `plugin/src/sync.ts`
- `plugin/src/ws-client.ts`

## File Structure

### Server

- Modify `server/internal/db/vault.go`: add `ErrInvalidVaultName`, `EnsureVault`, and keep `CreateVault` strict for duplicate tests.
- Modify `server/internal/db/db.go`: make `Queries` usable with `*sql.DB` and `*sql.Tx`; add `InTx`.
- Modify `server/internal/db/vault_test.go`: test idempotent ensure and invalid vault names.
- Modify `server/internal/storage/storage.go`: add temp write and trash delete preparation helpers.
- Modify `server/internal/storage/storage_test.go`: test temp commit/cleanup and trash restore/finalize.
- Modify `server/internal/ws/handler.go`: add transaction wrapper, response recorder, vault ensure, and staged file finalization.
- Review `server/internal/ws/client.go`: confirm raw outgoing payload logging remains in place.
- Modify `server/internal/ws/handler_test.go`: add fresh-vault filePut success and error-path coverage.

### Plugin

- Modify `plugin/src/sync.ts`: register generic error handler and handle `msg.error` first in result handlers.
- Modify `plugin/src/__tests__/ws-client-protocol.test.ts`: keep raw logging test.
- Create `plugin/src/__tests__/sync-errors.test.ts`: test Notice behavior for generic and result errors.

---

### Task 1: Add DB Transaction Support and Idempotent Vault Ensure

**Files:**
- Modify: `server/internal/db/db.go`
- Modify: `server/internal/db/vault.go`
- Modify: `server/internal/db/vault_test.go`

- [ ] **Step 1: Write failing DB tests**

Append these tests to `server/internal/db/vault_test.go`:

```go
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
```

- [ ] **Step 2: Run DB tests and verify they fail**

Run:

```bash
cd server
rtk go test ./internal/db -run 'TestEnsureVault|TestInTx' -count=1
```

Expected: FAIL because `EnsureVault` and `InTx` are undefined.

- [ ] **Step 3: Make `Queries` transaction-capable**

In `server/internal/db/db.go`, replace the current `Queries` definition with this:

```go
type dbtx interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

type Queries struct {
	db    dbtx
	begin func() (*sql.Tx, error)
}

func NewQueries(db *sql.DB) *Queries {
	return &Queries{
		db: db,
		begin: func() (*sql.Tx, error) {
			return db.Begin()
		},
	}
}

func newTxQueries(tx *sql.Tx) *Queries {
	return &Queries{db: tx}
}

func (q *Queries) InTx(fn func(*Queries) error) error {
	if q.begin == nil {
		return fn(q)
	}
	tx, err := q.begin()
	if err != nil {
		return err
	}
	txq := newTxQueries(tx)
	if err := fn(txq); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
```

Keep the existing `Open` and `migrate` functions unchanged.

- [ ] **Step 4: Add idempotent `EnsureVault`**

In `server/internal/db/vault.go`, add imports and function:

```go
import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

var ErrInvalidVaultName = errors.New("invalid vault name")

func (q *Queries) EnsureVault(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrInvalidVaultName
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := q.db.Exec(
		"INSERT OR IGNORE INTO vaults (name, inserted_at, updated_at) VALUES (?, ?, ?)",
		name, now, now,
	)
	return err
}
```

Keep `CreateVault` strict:

```go
func (q *Queries) CreateVault(name string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := q.db.Exec(
		"INSERT INTO vaults (name, inserted_at, updated_at) VALUES (?, ?, ?)",
		name, now, now,
	)
	return err
}
```

- [ ] **Step 5: Run DB tests**

Run:

```bash
cd server
rtk go test ./internal/db -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/db/db.go server/internal/db/vault.go server/internal/db/vault_test.go
git commit -m "feat: add transactional vault ensure"
```

---

### Task 2: Add Safe Storage Staging Helpers

**Files:**
- Modify: `server/internal/storage/storage.go`
- Modify: `server/internal/storage/storage_test.go`

- [ ] **Step 1: Write failing storage tests**

Append these tests to `server/internal/storage/storage_test.go`:

```go
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
```

- [ ] **Step 2: Run storage tests and verify they fail**

```bash
cd server
rtk go test ./internal/storage -run 'TestStage' -count=1
```

Expected: FAIL because `StageWrite` and `StageDelete` are undefined.

- [ ] **Step 3: Add staging types and helpers**

Append this code to `server/internal/storage/storage.go`:

```go
type StagedFileOp struct {
	TempPath  string
	FinalPath string
	commit    func() error
	rollback  func() error
}

func (op *StagedFileOp) Commit() error {
	if op == nil || op.commit == nil {
		return nil
	}
	return op.commit()
}

func (op *StagedFileOp) Rollback() error {
	if op == nil || op.rollback == nil {
		return nil
	}
	return op.rollback()
}

func (s *Storage) StageWrite(vault, filePath string, data []byte) (*StagedFileOp, error) {
	final := s.vaultPath(vault, filePath)
	if err := os.MkdirAll(filepath.Dir(final), 0755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(final), ".goat-sync-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	return &StagedFileOp{
		TempPath:  tmpPath,
		FinalPath: final,
		commit: func() error {
			if err := os.MkdirAll(filepath.Dir(final), 0755); err != nil {
				return err
			}
			return os.Rename(tmpPath, final)
		},
		rollback: func() error {
			err := os.Remove(tmpPath)
			if os.IsNotExist(err) {
				return nil
			}
			return err
		},
	}, nil
}

func (s *Storage) StageDelete(vault, filePath string) (*StagedFileOp, error) {
	final := s.vaultPath(vault, filePath)
	trashDir := filepath.Join(filepath.Dir(final), ".goat-sync-trash")
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(trashDir, filepath.Base(filePath)+".*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	_ = os.Remove(tmpPath)
	if err := os.Rename(final, tmpPath); err != nil {
		if os.IsNotExist(err) {
			return &StagedFileOp{}, nil
		}
		return nil, err
	}
	return &StagedFileOp{
		TempPath:  tmpPath,
		FinalPath: final,
		commit: func() error {
			err := os.Remove(tmpPath)
			if os.IsNotExist(err) {
				return nil
			}
			return err
		},
		rollback: func() error {
			if err := os.MkdirAll(filepath.Dir(final), 0755); err != nil {
				return err
			}
			err := os.Rename(tmpPath, final)
			if os.IsNotExist(err) {
				return nil
			}
			return err
		},
	}, nil
}
```

- [ ] **Step 4: Run storage tests**

```bash
cd server
rtk go test ./internal/storage -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/storage/storage.go server/internal/storage/storage_test.go
git commit -m "feat: stage sync storage operations"
```

---

### Task 3: Wrap WebSocket Message Handling in Transactions and Ensure Vault

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Write failing fresh-vault `filePut` test**

Append this test to `server/internal/ws/handler_test.go`:

```go
func TestHandleFilePutCreatesMissingVault(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/new.md",
		Content: "hello",
		File: &FilePayload{
			Path:      "notes/new.md",
			Exists:    true,
			LocalHash: "hash-new",
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "filePutResult" || resp.Error != "" {
		t.Fatalf("expected successful filePutResult, got %#v", resp)
	}
	if resp.Action != "okUpdateMeta" {
		t.Fatalf("expected okUpdateMeta, got %s", resp.Action)
	}
	exists, err := q.VaultExists("personal")
	if err != nil {
		t.Fatalf("vault exists: %v", err)
	}
	if !exists {
		t.Fatal("expected vault to be auto-created")
	}
	content, err := s.ReadFile("personal", "notes/new.md")
	if err != nil {
		t.Fatalf("expected file content: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("content = %q", content)
	}
}
```

- [ ] **Step 2: Run handler test and verify it fails**

```bash
cd server
rtk go test ./internal/ws -run TestHandleFilePutCreatesMissingVault -count=1
```

Expected before implementation: FAIL with a `FOREIGN KEY constraint failed` response or missing success action.

- [ ] **Step 3: Add response recorder and handler clone**

In `server/internal/ws/handler.go`, add these helper types near `Handler`:

```go
type messageSender interface {
	SendMessage(OutgoingMessage)
}

type responseRecorder struct {
	messages []OutgoingMessage
	failed   bool
}

func (r *responseRecorder) SendMessage(msg OutgoingMessage) {
	if msg.Error != "" {
		r.failed = true
	}
	r.messages = append(r.messages, msg)
}

type rollbackResponseError struct{}

func (rollbackResponseError) Error() string {
	return "rollback after websocket error response"
}

func (h *Handler) withQueries(q *db.Queries) *Handler {
	clone := *h
	clone.queries = q
	return &clone
}
```

Change handler method signatures from `client *Client` to `sender messageSender, client *Client` only where they need both sender and `client.vault`.
For methods that do not need `client.vault`, use `sender messageSender`.
For the first pass, preserve call structure by using `recorder` as sender and `client` for vault state.

- [ ] **Step 4: Dispatch inside `InTx` and ensure vault**

Replace `HandleMessage` dispatch with this structure:

```go
func (h *Handler) HandleMessage(client *Client, data []byte) {
	msg, err := UnmarshalMessage(data)
	if err != nil {
		log.Printf("failed to parse message: %v", err)
		return
	}
	log.Printf("ws incoming raw: %s", string(data))

	var recorder responseRecorder
	var finalizers []func() error
	var rollbacks []func() error

	err = h.queries.InTx(func(txq *db.Queries) error {
		if msg.Type != "" {
			if msg.Vault == "" {
				recorder.SendMessage(OutgoingMessage{Type: "error", Error: db.ErrInvalidVaultName.Error()})
				return rollbackResponseError{}
			}
			if err := txq.EnsureVault(msg.Vault); err != nil {
				recorder.SendMessage(OutgoingMessage{Type: "error", Vault: msg.Vault, Error: err.Error()})
				return rollbackResponseError{}
			}
		}
		txh := h.withQueries(txq)
		txh.dispatchMessage(&recorder, client, msg, &finalizers, &rollbacks)
		if recorder.failed {
			return rollbackResponseError{}
		}
		return nil
	})
	if err != nil {
		for _, rollback := range rollbacks {
			if rerr := rollback(); rerr != nil {
				log.Printf("ws rollback failed: %v", rerr)
			}
		}
		if _, ok := err.(rollbackResponseError); ok {
			for _, out := range recorder.messages {
				client.SendMessage(out)
			}
			return
		}
		client.SendMessage(OutgoingMessage{Type: "error", Vault: msg.Vault, Error: err.Error()})
		return
	}

	for _, finalize := range finalizers {
		if ferr := finalize(); ferr != nil {
			log.Printf("ws finalize failed: %v", ferr)
			client.SendMessage(OutgoingMessage{Type: "error", Vault: msg.Vault, Path: msg.Path, Error: ferr.Error()})
			return
		}
	}
	for _, out := range recorder.messages {
		client.SendMessage(out)
	}
}
```

Add `dispatchMessage`:

```go
func (h *Handler) dispatchMessage(sender messageSender, client *Client, msg IncomingMessage, finalizers *[]func() error, rollbacks *[]func() error) {
	switch msg.Type {
	case "vaultCreate":
		h.handleVaultCreate(sender, msg)
	case "syncInit":
		h.handleSyncInit(sender, client, msg)
	case "fileCheck":
		h.handleFileCheck(sender, msg)
	case "filePut":
		h.handleFilePut(sender, client, msg, finalizers, rollbacks)
	case "fileDelete":
		h.handleFileDelete(sender, client, msg, finalizers, rollbacks)
	case "conflictResolve":
		h.handleConflictResolve(sender, client, msg, finalizers, rollbacks)
	default:
		log.Printf("unknown message type: %s", msg.Type)
	}
}
```

- [ ] **Step 5: Update `vaultCreate` to use ensure semantics**

Change `handleVaultCreate`:

```go
func (h *Handler) handleVaultCreate(sender messageSender, msg IncomingMessage) {
	if err := h.queries.EnsureVault(msg.Vault); err != nil {
		sender.SendMessage(OutgoingMessage{Type: "error", Vault: msg.Vault, Error: err.Error()})
		return
	}
	if err := h.storage.CreateVaultDir(msg.Vault); err != nil {
		sender.SendMessage(OutgoingMessage{Type: "error", Vault: msg.Vault, Error: err.Error()})
		return
	}
	sender.SendMessage(OutgoingMessage{Type: "vault_created", Vault: msg.Vault})
}
```

- [ ] **Step 6: Run fresh-vault handler test**

```bash
cd server
rtk go test ./internal/ws -run TestHandleFilePutCreatesMissingVault -count=1
```

Expected: PASS.

- [ ] **Step 7: Run server ws tests**

```bash
cd server
rtk go test ./internal/ws -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add server/internal/ws/handler.go server/internal/ws/handler_test.go
git commit -m "fix: ensure vaults during websocket messages"
```

---

### Task 4: Stage `filePut` and `fileDelete` File Operations

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Write failing filePut staging rollback test**

Append this test to `server/internal/ws/handler_test.go`:

```go
func TestHandleFilePutDoesNotLeaveFileWhenDBFails(t *testing.T) {
	h, _, s, _ := setupHandler(t)
	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "",
		Path:    "notes/bad.md",
		Content: "should not stay",
		File: &FilePayload{
			Path:      "notes/bad.md",
			Exists:    true,
			LocalHash: "hash-bad",
		},
	}))

	resp := readResponse(t, c)
	if resp.Error == "" {
		t.Fatalf("expected error response, got %#v", resp)
	}
	if _, err := s.ReadFile("", "notes/bad.md"); err == nil {
		t.Fatal("expected no final file after failed message")
	}
}
```

- [ ] **Step 2: Run rollback test**

```bash
cd server
rtk go test ./internal/ws -run TestHandleFilePutDoesNotLeaveFileWhenDBFails -count=1
```

Expected before staging is fully wired: FAIL if final file is left behind or no error is returned.

- [ ] **Step 3: Replace direct `WriteFile` in `handleFilePut`**

Inside `handleFilePut`, replace direct `h.storage.WriteFile` with staged write:

```go
stage, err := h.storage.StageWrite(msg.Vault, path, fileContent)
if err != nil {
	sender.SendMessage(OutgoingMessage{Type: "filePutResult", Path: path, Error: err.Error()})
	return
}
*finalizers = append(*finalizers, stage.Commit)
*rollbacks = append(*rollbacks, stage.Rollback)
```

Then perform DB create/update exactly as before using tx-scoped `h.queries`.

- [ ] **Step 4: Replace direct delete in `handleFileDelete` and conflict delete**

Where the handler currently calls `h.storage.DeleteFile(...)`, stage it instead:

```go
stage, err := h.storage.StageDelete(msg.Vault, path)
if err != nil {
	sender.SendMessage(OutgoingMessage{Type: "fileDeleteResult", Path: path, Error: err.Error()})
	return
}
*finalizers = append(*finalizers, stage.Commit)
*rollbacks = append(*rollbacks, stage.Rollback)
```

Use `conflictResolveResult` as the response type in conflict resolve delete paths.

- [ ] **Step 5: Ensure rollback finalizers run on early error responses**

In `HandleMessage`, after `InTx` returns successfully but before finalize, inspect recorded messages:

```go
for _, out := range recorder.messages {
	if out.Error != "" {
		for _, rollback := range rollbacks {
			if rerr := rollback(); rerr != nil {
				log.Printf("ws rollback failed: %v", rerr)
			}
		}
		for _, out := range recorder.messages {
			client.SendMessage(out)
		}
		return
	}
}
```

This prevents staged files from being finalized when the handler already produced an error response.
The `responseRecorder.failed` flag and `rollbackResponseError` from Task 3 are the primary rollback mechanism; this scan is a defensive check.

- [ ] **Step 6: Run staging-related tests**

```bash
cd server
rtk go test ./internal/ws -run 'TestHandleFilePutCreatesMissingVault|TestHandleFilePutDoesNotLeaveFileWhenDBFails|TestHandleFileDelete|TestHandleConflictResolve' -count=1
```

Expected: PASS.

- [ ] **Step 7: Run server tests**

```bash
cd server
rtk go test ./... 
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add server/internal/ws/handler.go server/internal/ws/handler_test.go
git commit -m "fix: stage websocket file side effects"
```

---

### Task 5: Surface Server Errors in the Plugin

**Files:**
- Modify: `plugin/src/sync.ts`
- Create: `plugin/src/__tests__/sync-errors.test.ts`

- [ ] **Step 1: Write failing plugin error tests**

Create `plugin/src/__tests__/sync-errors.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { SyncManager } from "../sync";

const notices: string[] = [];

vi.mock("obsidian", () => ({
  Notice: class {
    constructor(message: string) {
      notices.push(message);
    }
  },
  normalizePath: (path: string) => path,
}));

function makeManager() {
  return Object.create(SyncManager.prototype) as SyncManager & {
    handleServerError: (msg: { type: string; path?: string; error?: string }) => void;
    handleFilePutResult: (msg: { type: string; path?: string; error?: string }) => Promise<void>;
  };
}

describe("sync error notices", () => {
  it("shows generic server errors", () => {
    notices.length = 0;
    const manager = makeManager();

    manager.handleServerError({ type: "error", error: "boom" });

    expect(notices).toEqual(["[obsidian-goat-sync] error failed: boom"]);
  });

  it("shows result errors with path before action handling", async () => {
    notices.length = 0;
    const manager = makeManager();

    await manager.handleFilePutResult({ type: "filePutResult", path: "notes/a.md", error: "FOREIGN KEY constraint failed" });

    expect(notices).toEqual([
      "[obsidian-goat-sync] filePutResult failed for notes/a.md: FOREIGN KEY constraint failed",
    ]);
  });
});
```

- [ ] **Step 2: Run plugin error tests and verify they fail**

```bash
cd plugin
rtk npm test -- --run src/__tests__/sync-errors.test.ts
```

Expected: FAIL because `handleServerError` is missing or private inaccessible. Make it public enough for tests or test through registered callbacks in Step 3 if preferred.

- [ ] **Step 3: Add reusable error handler**

In `plugin/src/sync.ts`, add this method inside `SyncManager`:

```ts
handleServerError(msg: ServerMessage): boolean {
  if (!msg.error) return false;
  console.error("[obsidian-goat-sync] server error", msg);
  const message = msg.path
    ? `[obsidian-goat-sync] ${msg.type} failed for ${msg.path}: ${msg.error}`
    : `[obsidian-goat-sync] ${msg.type} failed: ${msg.error}`;
  new Notice(message);
  return true;
}
```

- [ ] **Step 4: Register generic error and guard result handlers**

In `start()`, add:

```ts
this.wsClient.on("error", (msg) => this.handleServerError(msg));
```

At the top of each result handler, before path/action checks, add:

```ts
if (this.handleServerError(msg)) return;
```

Apply this to:

- `handleFilePutResult`
- `handleFileDeleteResult`
- `handleFileCheckResult`
- `handleConflictResolveResult`

- [ ] **Step 5: Run plugin error tests**

```bash
cd plugin
rtk npm test -- --run src/__tests__/sync-errors.test.ts
```

Expected: PASS.

- [ ] **Step 6: Run plugin tests and build**

```bash
cd plugin
rtk npm test
rtk npm run build
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add plugin/src/sync.ts plugin/src/__tests__/sync-errors.test.ts
git commit -m "fix: show sync server errors"
```

---

### Task 6: Full Verification and Cleanup

**Files:**
- Review: `server/internal/ws/handler.go`
- Review: `server/internal/storage/storage.go`
- Review: `plugin/src/sync.ts`

- [ ] **Step 1: Search for direct unsafe file writes in WebSocket handlers**

Run:

```bash
rtk rg -n "storage\\.WriteFile|storage\\.DeleteFile|CreateFile\\(|UpdateFile\\(|DeleteFile\\(" server/internal/ws/handler.go
```

Expected:

- No `storage.WriteFile` in write paths.
- No direct `storage.DeleteFile` in delete paths.
- DB create/update/delete calls remain but occur after decision and staging setup.

- [ ] **Step 2: Confirm raw outgoing logging still exists**

Run:

```bash
rtk rg -n "ws outgoing raw" server/internal/ws/client.go
```

Expected: at least one match in `server/internal/ws/client.go` containing:

```go
log.Printf("ws outgoing raw: %s", string(data))
```

- [ ] **Step 3: Run all server tests**

```bash
cd server
rtk go test ./...
```

Expected: PASS.

- [ ] **Step 4: Run all plugin tests**

```bash
cd plugin
rtk npm test
```

Expected: PASS.

- [ ] **Step 5: Build plugin**

```bash
cd plugin
rtk npm run build
```

Expected: PASS.

- [ ] **Step 6: Manual raw-log scenario**

With a fresh server DB, connect the plugin and create a new note.
Expected raw logs include:

```text
ws incoming raw: {"type":"syncInit",...}
ws outgoing raw: {"type":"syncResult",...,"toPut":[...]}
ws incoming raw: {"type":"filePut",...}
ws outgoing raw: {"type":"filePutResult",...,"action":"okUpdateMeta",...}
```

Expected DB state:

```sql
SELECT name FROM vaults;
SELECT vault_name, path, version, hash, is_deleted FROM files;
```

The vault row exists before or with the file row, and no FK error appears.

- [ ] **Step 7: Commit verification notes if docs changed**

If any implementation note is added to docs, commit it:

```bash
git add docs/superpowers/plans/2026-04-26-ws-vault-transaction-safety.md docs/superpowers/specs/2026-04-26-ws-vault-transaction-safety-design.md
git commit -m "docs: plan websocket vault transaction safety"
```

If docs were already committed before implementation, skip this step.

---

## Final Test Matrix

Run all:

```bash
cd server
rtk go test ./...
cd ../plugin
rtk npm test
rtk npm run build
```

Expected:

- Server tests pass.
- Plugin tests pass.
- Plugin production build succeeds.
- `server/docker-compose.yml` and `server/go.mod` pre-existing unrelated changes are not reverted or included unless the user explicitly asks.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-26-ws-vault-transaction-safety.md`.

Two execution options:

1. **Subagent-Driven (recommended)** - Dispatch a fresh subagent per task, with spec and quality review between tasks.
2. **Inline Execution** - Execute tasks in this session using executing-plans, with batch checkpoints.

Use `superpowers:subagent-driven-development` for option 1 or `superpowers:executing-plans` for option 2.
