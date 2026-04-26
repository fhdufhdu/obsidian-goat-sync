# Base History and Auto Merge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Store path-local server version history and use it to turn false conflicts into `toDownload`, automatic text merge, or real conflict.

**Architecture:** The server remains the canonical file store. SQLite stores every path-local revision in `file_versions`, storage keeps content-addressed objects for historical reads, and WebSocket handlers use base-aware matrix decisions plus a diff-match-patch merge helper. The plugin treats successful auto-merge as a server-confirmed download and protects merge paths from dirty/delete queue races with merge-in-flight state.

**Tech Stack:** Go 1.25, SQLite, Gorilla WebSocket, `github.com/sergi/go-diff/diffmatchpatch`, TypeScript, Obsidian API, Vitest.

---

## Reference Documents

- Spec: `docs/superpowers/specs/2026-04-26-base-history-auto-merge-design.md`
- Matrix source: `docs/message-matrix.csv`
- Existing implementation plan style: `docs/superpowers/plans/2026-04-24-sync-matrix-queue-refactor.md`

## File Structure

- Modify `docs/message-matrix.csv`: add `base row`, `base 해시 비교`, `autoMerge` columns and renumber rows sequentially from `M001`.
- Modify `server/internal/sync/matrix.go`: add base-row and mergeability decision axes plus new `autoMerge` action.
- Modify `server/internal/sync/matrix_test.go`: assert base-aware M013/M033/M048 behavior.
- Modify `server/internal/sync/matrix_csv_test.go`: parse and validate the expanded CSV schema.
- Modify `server/internal/db/db.go`: replace `files` schema with `file_versions`.
- Modify `server/internal/db/file.go`: make existing file helpers operate on append-only version rows.
- Modify `server/internal/db/file_test.go` and `server/internal/db/db_test.go`: cover surrogate id, unique key, latest lookup, base lookup, tombstones.
- Modify `server/internal/storage/storage.go`: add content-addressed object helpers while preserving latest file storage.
- Modify `server/internal/storage/storage_test.go`: cover object writes/reads/idempotency.
- Create `server/internal/sync/merge.go`: implement `MergeText(base, local, server) (string, bool)`.
- Create `server/internal/sync/merge_test.go`: fix automatic merge contracts.
- Modify `server/internal/ws/messages.go`: add `mergePut`, `expectedServerVersion`, `toAutoMerge`, `autoMergeRequired`, and `mergePutResult` payload shapes.
- Modify `server/internal/ws/handler.go`: implement base-aware sync/fileCheck/filePut and new mergePut handler.
- Modify `server/internal/ws/handler_test.go`: add server protocol tests for false-conflict prevention and auto-merge.
- Modify `server/go.mod` and `server/go.sum`: add diff-match-patch dependency.
- Modify `plugin/src/ws-client.ts`: add `toAutoMerge`, `autoMergeRequired`, `mergePutResult`, and `sendMergePut`.
- Modify `plugin/src/sync.ts`: add merge-in-flight handling and `toDownload` handling for `filePutResult`/`mergePutResult`.
- Modify `plugin/src/dirty-queue.ts`: allow mergePut to claim or complete an existing dirty entry deterministically.
- Modify `plugin/src/delete-queue.ts`: no storage format change; skip merge-in-flight paths from caller.
- Modify `plugin/src/__tests__/sync-orchestrator.test.ts` and `plugin/src/__tests__/sync-errors.test.ts`: extend queue/error behavior.
- Create `plugin/src/__tests__/sync-auto-merge.test.ts`: cover plugin merge follow-up and successful apply.

## Implementation Tasks

### Task 1: Expand The Matrix Vocabulary

**Files:**
- Modify: `server/internal/sync/matrix.go`
- Test: `server/internal/sync/matrix_test.go`

- [ ] **Step 1: Write failing matrix tests for base-aware decisions**

Add these tests to `server/internal/sync/matrix_test.go`:

```go
func int64Ptr(v int64) *int64 { return &v }

func TestDecideSyncInitBaseAwareActiveDiverged(t *testing.T) {
	tests := []struct {
		name string
		in   DecisionInput
		want MatrixAction
	}{
		{
			name: "local unchanged from base downloads latest server",
			in: DecisionInput{
				Message:       MessageSyncInit,
				ClientExists:  true,
				BaseVersion:   int64Ptr(1),
				LocalHash:     "base",
				ServerState:   ServerActive,
				ServerVersion: 2,
				ServerHash:    "server",
				BaseRowExists: true,
				BaseHash:      "base",
			},
			want: MatrixActionToDownload,
		},
		{
			name: "both changed and clean merge required",
			in: DecisionInput{
				Message:       MessageSyncInit,
				ClientExists:  true,
				BaseVersion:   int64Ptr(1),
				LocalHash:     "local",
				ServerState:   ServerActive,
				ServerVersion: 2,
				ServerHash:    "server",
				BaseRowExists: true,
				BaseHash:      "base",
				AutoMerge:     AutoMergePossible,
			},
			want: MatrixActionAutoMerge,
		},
		{
			name: "missing base row remains conflict",
			in: DecisionInput{
				Message:       MessageSyncInit,
				ClientExists:  true,
				BaseVersion:   int64Ptr(1),
				LocalHash:     "local",
				ServerState:   ServerActive,
				ServerVersion: 2,
				ServerHash:    "server",
				BaseRowExists: false,
			},
			want: MatrixActionConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideSyncInit(tt.in)
			if got.Action != tt.want {
				t.Fatalf("action = %s, want %s", got.Action, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Verify the tests fail**

Run:

```bash
rtk go test -C server ./internal/sync -run TestDecideSyncInitBaseAwareActiveDiverged -count=1
```

Expected: compile failure for `BaseRowExists`, `BaseHash`, `AutoMergePossible`, or `MatrixActionAutoMerge`.

- [ ] **Step 3: Add base/merge fields and constants**

Update `server/internal/sync/matrix.go` with:

```go
type AutoMergeState string

const (
	AutoMergeNotApplicable AutoMergeState = "n/a"
	AutoMergePossible      AutoMergeState = "possible"
	AutoMergeImpossible    AutoMergeState = "impossible"
)

const (
	MatrixActionAutoMerge MatrixAction = "autoMerge"
)

type DecisionInput struct {
	Message            MatrixMessage
	ClientExists       bool
	BaseVersion        *int64
	LocalHash          string
	ServerState        ServerStateKind
	ServerVersion      int64
	ServerHash         string
	DeletedFromVersion int64
	BaseRowExists      bool
	BaseHash           string
	AutoMerge          AutoMergeState
}
```

Then adjust the active-server branch in `decideReadOrCheck`:

```go
case ServerActive:
	if *input.BaseVersion == input.ServerVersion {
		if input.LocalHash == input.ServerHash {
			return DecisionResult{Action: cleanAction}
		}
		return DecisionResult{Action: putAction}
	}
	if input.LocalHash == input.ServerHash {
		return DecisionResult{Action: updateMetaAction}
	}
	if input.BaseRowExists && input.LocalHash == input.BaseHash {
		return DecisionResult{Action: MatrixActionToDownload}
	}
	if input.BaseRowExists && input.AutoMerge == AutoMergePossible {
		return DecisionResult{Action: MatrixActionAutoMerge}
	}
	return DecisionResult{Action: MatrixActionConflict}
```

Apply the same active-server base-aware branch in `DecideFilePut`, except the normal same-server branch remains `MatrixActionOkUpdateMeta`.

- [ ] **Step 4: Verify matrix tests pass**

Run:

```bash
rtk go test -C server ./internal/sync -run 'TestDecide(SyncInit|FileCheck|FilePut)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add server/internal/sync/matrix.go server/internal/sync/matrix_test.go
rtk git commit -m "feat: add base-aware sync matrix decisions"
```

### Task 2: Expand And Renumber The Matrix CSV

**Files:**
- Modify: `docs/message-matrix.csv`
- Modify: `server/internal/sync/matrix_csv_test.go`
- Modify: `server/internal/sync/matrix_test.go`

- [ ] **Step 1: Write the CSV schema test**

Update the CSV test so the header must contain exactly these columns:

```go
wantHeader := []string{
	"id", "구분", "클라이언트 메시지", "클라이언트 파일", "클라이언트 baseVersion",
	"서버 파일 상태", "버전 비교", "해시 비교", "base row", "base 해시 비교",
	"autoMerge", "서버 행동", "서버 메시지", "서버 메시지 내용", "비고",
}
```

Add a sequential ID assertion:

```go
for i, row := range rows[1:] {
	wantID := fmt.Sprintf("M%03d", i+1)
	if got := row[0]; got != wantID {
		t.Fatalf("row %d id = %q, want %q", i+2, got, wantID)
	}
}
```

- [ ] **Step 2: Expand the fixture model**

In `server/internal/sync/matrix_test.go`, extend `matrixFixture`:

```go
type matrixFixture struct {
	ID            string
	Message       MatrixMessage
	ClientExists  bool
	BaseVersion   *int64
	ServerState   ServerStateKind
	VersionMatch  VersionMatch
	HashMatch     HashMatch
	BaseRowExists bool
	BaseHashMatch HashMatch
	AutoMerge     AutoMergeState
	Expected      MatrixAction
}
```

Update the fixture-to-input builder so:

```go
if f.BaseHashMatch == HashEqual {
	input.BaseHash = input.LocalHash
}
if f.BaseHashMatch == HashDifferent {
	input.BaseHash = "base-hash"
	input.LocalHash = "local-hash"
}
input.BaseRowExists = f.BaseRowExists
input.AutoMerge = f.AutoMerge
```

For rows whose new CSV columns are `해당없음`, use `BaseRowExists: false`, `BaseHashMatch: HashNotApplicable`, and `AutoMerge: AutoMergeNotApplicable`.

- [ ] **Step 2: Verify the CSV test fails**

Run:

```bash
rtk go test -C server ./internal/sync -run TestMessageMatrixCSV -count=1
```

Expected: FAIL because the current CSV lacks the three new columns.

- [ ] **Step 3: Rewrite `docs/message-matrix.csv`**

Insert the three columns after `해시 비교`.
For every existing non-expanded row set:

```text
base row=해당없음
base 해시 비교=해당없음
autoMerge=해당없음
```

Replace the current M013/M033/M048 rows with four rows each:

```csv
M013,초기 동기화,syncInit,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,있음,localHash == baseHash,해당없음,없음,syncResult,toDownload,로컬은 base 그대로이고 서버만 바뀌었으므로 최신 서버 내용을 내려받음
M014,초기 동기화,syncInit,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,있음,localHash != baseHash,가능,자동 병합,syncResult,toAutoMerge,양쪽 변경이지만 텍스트 자동 병합 follow-up 가능
M015,초기 동기화,syncInit,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,있음,localHash != baseHash,불가,없음,syncResult,conflict,양쪽 변경이고 자동 병합 불가
M016,초기 동기화,syncInit,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,없음,해당없음,해당없음,없음,syncResult,conflict,조상 버전을 찾을 수 없어 충돌
```

Replace the current M033 `fileCheck` row with:

```csv
M033,파일 확인,fileCheck,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,있음,localHash == baseHash,해당없음,없음,fileCheckResult,toDownload,로컬은 base 그대로이고 서버만 바뀌었으므로 최신 서버 내용을 내려받음
M034,파일 확인,fileCheck,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,있음,localHash != baseHash,가능,자동 병합,fileCheckResult,autoMergeRequired,양쪽 변경이지만 텍스트 자동 병합 follow-up 가능
M035,파일 확인,fileCheck,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,있음,localHash != baseHash,불가,없음,fileCheckResult,conflict,양쪽 변경이고 자동 병합 불가
M036,파일 확인,fileCheck,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,없음,해당없음,해당없음,없음,fileCheckResult,conflict,조상 버전을 찾을 수 없어 충돌
```

Replace the current M048 `filePut` row with:

```csv
M048,파일 저장,filePut,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,있음,localHash == baseHash,해당없음,없음,filePutResult,toDownload,로컬은 base 그대로이고 서버만 바뀌었으므로 저장하지 않고 최신 서버 내용을 내려받음
M049,파일 저장,filePut,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,있음,localHash != baseHash,가능,자동 병합,filePutResult,toDownload,요청 content로 즉시 자동 병합 후 병합 결과를 서버에 저장하고 내려받음
M050,파일 저장,filePut,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,있음,localHash != baseHash,불가,없음,filePutResult,conflict,양쪽 변경이고 자동 병합 불가
M051,파일 저장,filePut,있음,있음,있음,baseVersion != serverVersion,localHash != serverHash,없음,해당없음,해당없음,없음,filePutResult,conflict,조상 버전을 찾을 수 없어 충돌
```

After inserting rows, renumber every row from `M001` with no gaps.
Regenerate every `matrixFixtures()` ID to match the renumbered CSV. The expanded M013/M033/M048 groups must have `BaseRowExists`, `BaseHashMatch`, and `AutoMerge` values matching their CSV columns.

- [ ] **Step 4: Verify CSV and matrix tests pass**

Run:

```bash
rtk go test -C server ./internal/sync -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add docs/message-matrix.csv server/internal/sync/matrix_csv_test.go server/internal/sync/matrix_test.go
rtk git commit -m "docs: expand matrix for base-aware auto merge"
```

### Task 3: Replace Latest-Only DB With Version History

**Files:**
- Modify: `server/internal/db/db.go`
- Modify: `server/internal/db/file.go`
- Modify: `server/internal/db/db_test.go`
- Modify: `server/internal/db/file_test.go`
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Write failing DB schema tests**

Update `server/internal/db/db_test.go` so it expects `file_versions` and no longer expects `files`:

```go
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
```

- [ ] **Step 2: Write failing file-version helper tests**

Add tests to `server/internal/db/file_test.go`:

```go
func TestFileVersionsAppendHistory(t *testing.T) {
	q := setupQueries(t)
	mustEnsureVault(t, q, "personal")

	v1, err := q.CreateFile("personal", "notes/hello.md", "hash1", "sha256:hash1", "")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := q.UpdateFile("personal", "notes/hello.md", "hash2", "sha256:hash2", "")
	if err != nil {
		t.Fatal(err)
	}

	if v1.ID == 0 || v2.ID == 0 || v1.ID == v2.ID {
		t.Fatalf("expected distinct surrogate ids: v1=%d v2=%d", v1.ID, v2.ID)
	}
	if v2.Version != 2 {
		t.Fatalf("version = %d, want 2", v2.Version)
	}

	base, err := q.GetFileVersion("personal", "notes/hello.md", 1)
	if err != nil {
		t.Fatal(err)
	}
	if base.Hash != "hash1" || base.ContentRef != "sha256:hash1" {
		t.Fatalf("base row = %#v", base)
	}
}

func TestDeleteFileAppendsTombstoneWithPreviousContentRef(t *testing.T) {
	q := setupQueries(t)
	mustEnsureVault(t, q, "personal")
	_, _ = q.CreateFile("personal", "notes/a.md", "hash1", "sha256:hash1", "")

	deleted, err := q.DeleteFile("personal", "notes/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.IsDeleted || deleted.Version != 2 {
		t.Fatalf("deleted row = %#v", deleted)
	}
	if deleted.Hash != "hash1" || deleted.ContentRef != "sha256:hash1" {
		t.Fatalf("tombstone lost previous content: %#v", deleted)
	}
}
```

- [ ] **Step 3: Verify DB tests fail**

Run:

```bash
rtk go test -C server ./internal/db -count=1
```

Expected: FAIL because `file_versions`, `ID`, `ContentRef`, and the new helper signatures do not exist.

- [ ] **Step 4: Implement the schema**

Replace the `files` DDL in `server/internal/db/db.go` with:

```go
CREATE TABLE IF NOT EXISTS file_versions (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	vault_name  TEXT NOT NULL,
	path        TEXT NOT NULL,
	version     INTEGER NOT NULL,
	hash        TEXT NOT NULL,
	content_ref TEXT,
	encoding    TEXT NOT NULL DEFAULT '',
	is_deleted  INTEGER NOT NULL DEFAULT 0,
	inserted_at TEXT NOT NULL,
	updated_at  TEXT NOT NULL,
	UNIQUE (vault_name, path, version),
	FOREIGN KEY (vault_name) REFERENCES vaults(name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_file_versions_latest
ON file_versions(vault_name, path, version DESC);
```

- [ ] **Step 5: Implement append-only helpers**

Update `server/internal/db/file.go`:

```go
type File struct {
	ID         int64
	VaultName  string
	Path       string
	Version    int64
	Hash       string
	ContentRef string
	Encoding   string
	IsDeleted  bool
	InsertedAt string
	UpdatedAt  string
}
```

Use this scanner helper:

```go
func scanFile(scanner interface {
	Scan(dest ...any) error
}) (File, error) {
	var f File
	err := scanner.Scan(&f.ID, &f.VaultName, &f.Path, &f.Version, &f.Hash, &f.ContentRef, &f.Encoding, &f.IsDeleted, &f.InsertedAt, &f.UpdatedAt)
	return f, err
}
```

Implement latest lookup with:

```sql
SELECT id, vault_name, path, version, hash, COALESCE(content_ref, ''), encoding, is_deleted, inserted_at, updated_at
FROM file_versions
WHERE vault_name = ? AND path = ?
ORDER BY version DESC
LIMIT 1
```

Implement inserts by reading latest inside a transaction, computing `nextVersion := latest.Version + 1`, and inserting a new row. `CreateFile` uses version `1`; `UpdateFile` and `CreateFileFromTombstone` append active rows; `DeleteFile` appends a tombstone with the previous active row's `hash`, `content_ref`, and `encoding`.

- [ ] **Step 6: Update all compile-blocking call sites for new signatures**

Replace existing calls:

```go
q.CreateFile(vault, path, hash)
q.UpdateFile(vault, path, hash)
q.CreateFileFromTombstone(vault, path, hash, prevVersion)
```

with:

```go
q.CreateFile(vault, path, hash, contentRef, encoding)
q.UpdateFile(vault, path, hash, contentRef, encoding)
q.CreateFileFromTombstone(vault, path, hash, contentRef, encoding, prevVersion)
```

In tests that do not care about content storage, use `contentRef := "sha256:" + hash` and `encoding := ""`.
This step includes `server/internal/ws/handler.go` and `server/internal/ws/handler_test.go`; do not commit Task 3 while those packages still use the old 3-argument helper calls.

- [ ] **Step 7: Verify DB package passes**

Run:

```bash
rtk go test -C server ./internal/db ./internal/ws -run 'TestCreateFile|TestUpdateFile|TestDeleteFile|TestCreateFileFromTombstone|TestDB|TestFilePut' -count=1
```

Expected: PASS or only failures unrelated to the signature migration. There must be no compile errors from old DB helper signatures.

- [ ] **Step 8: Commit**

```bash
rtk git add server/internal/db/db.go server/internal/db/file.go server/internal/db/db_test.go server/internal/db/file_test.go server/internal/ws/handler.go server/internal/ws/handler_test.go
rtk git commit -m "feat: store append-only file version history"
```

### Task 4: Add Content-Addressed Object Storage

**Files:**
- Modify: `server/internal/storage/storage.go`
- Modify: `server/internal/storage/storage_test.go`

- [ ] **Step 1: Write object storage tests**

Add tests:

```go
func TestStageObjectWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	s := storage.New(dir)
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
	s := storage.New(dir)
	data := []byte("same")

	_, op1, _ := s.StageObjectWrite(data)
	_, op2, _ := s.StageObjectWrite(data)
	if err := op1.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := op2.Commit(); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Verify tests fail**

Run:

```bash
rtk go test -C server ./internal/storage -run TestStageObjectWrite -count=1
```

Expected: compile failure because `StageObjectWrite` and `ReadObject` do not exist.

- [ ] **Step 3: Implement object helpers**

Add to `server/internal/storage/storage.go`:

```go
func objectRef(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Storage) objectPath(ref string) (string, error) {
	const prefix = "sha256:"
	if !strings.HasPrefix(ref, prefix) || len(ref) != len(prefix)+64 {
		return "", fmt.Errorf("invalid content ref %q", ref)
	}
	hash := strings.TrimPrefix(ref, prefix)
	return filepath.Join(s.dataDir, "objects", "sha256", hash[:2], hash), nil
}

func (s *Storage) StageObjectWrite(data []byte) (string, *StagedFileOp, error) {
	ref := objectRef(data)
	final, err := s.objectPath(ref)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(filepath.Dir(final), 0755); err != nil {
		return "", nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(final), ".goat-object-*")
	if err != nil {
		return "", nil, err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", nil, err
	}
	return ref, &StagedFileOp{
		TempPath:  tmpPath,
		FinalPath: final,
		commit: func() error {
			if _, err := os.Stat(final); err == nil {
				return os.Remove(tmpPath)
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

func (s *Storage) ReadObject(ref string) ([]byte, error) {
	path, err := s.objectPath(ref)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}
```

Add imports: `crypto/sha256`, `encoding/hex`, `fmt`, `strings`.

- [ ] **Step 4: Verify storage tests pass**

Run:

```bash
rtk go test -C server ./internal/storage -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add server/internal/storage/storage.go server/internal/storage/storage_test.go
rtk git commit -m "feat: add content-addressed object storage"
```

### Task 5: Wire Version Content Refs Into Existing Writes

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`
- Modify: `server/internal/db/file_test.go`

- [ ] **Step 1: Write handler tests for historical content refs**

Add a test to `server/internal/ws/handler_test.go` that puts v1, puts v2, and reads v1 by DB row:

```go
func TestFilePutStoresHistoricalObjectRefs(t *testing.T) {
	h, _, _ := setupHandlerTest(t)

	sendJSON(t, h, IncomingMessage{
		Type: "filePut", Vault: "personal", Path: "notes/a.md", Content: "one",
		File: &FilePayload{Path: "notes/a.md", Exists: true, LocalHash: hashString("one")},
	})
	sendJSON(t, h, IncomingMessage{
		Type: "filePut", Vault: "personal", Path: "notes/a.md", Content: "two",
		File: &FilePayload{Path: "notes/a.md", Exists: true, BaseVersion: int64Ptr(1), BaseHash: hashString("one"), LocalHash: hashString("two")},
	})

	v1, err := h.queries.GetFileVersion("personal", "notes/a.md", 1)
	if err != nil {
		t.Fatal(err)
	}
	data, err := h.storage.ReadObject(v1.ContentRef)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one" {
		t.Fatalf("v1 object = %q", string(data))
	}
}
```

Create these helpers in `server/internal/ws/handler_test.go` if they are not already present. They adapt to the existing `setupHandler`, `makeClient`, `readResponse`, and `mustJSON` helpers:

```go
func setupHandlerTest(t *testing.T) (*Handler, *responseRecorder, *Client) {
	t.Helper()
	h, _, _, _ := setupHandler(t)
	return h, &responseRecorder{}, makeClient(h.hub, "personal")
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func lastMessage(t *testing.T, sender *responseRecorder) OutgoingMessage {
	t.Helper()
	if len(sender.messages) == 0 {
		t.Fatal("no messages recorded")
	}
	return sender.messages[len(sender.messages)-1]
}

func seedVersionObject(t *testing.T, h *Handler, vault, path, content, encoding string) db.File {
	t.Helper()
	data := decodeContent(content, encoding)
	ref, op, err := h.storage.StageObjectWrite(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := op.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.queries.GetFile(vault, path); err == sql.ErrNoRows {
		f, err := h.queries.CreateFile(vault, path, hashBytes(data), ref, encoding)
		if err != nil {
			t.Fatal(err)
		}
		_ = h.storage.WriteFile(vault, path, data)
		return f
	}
	f, err := h.queries.UpdateFile(vault, path, hashBytes(data), ref, encoding)
	if err != nil {
		t.Fatal(err)
	}
	_ = h.storage.WriteFile(vault, path, data)
	return f
}

func sendJSON(t *testing.T, h *Handler, msg IncomingMessage) OutgoingMessage {
	t.Helper()
	client := makeClient(h.hub, msg.Vault)
	h.HandleMessage(client, mustJSON(msg))
	return readResponse(t, client)
}
```

Prefer `sendJSON`/`HandleMessage` for new integration tests so transaction and finalizer behavior is covered. Use direct private handler calls only for narrow unit tests that do not need finalizer semantics.

- [ ] **Step 2: Verify handler tests fail**

Run:

```bash
rtk go test -C server ./internal/ws -run TestFilePutStoresHistoricalObjectRefs -count=1
```

Expected: FAIL because writes only update latest storage and metadata lacks `content_ref`.

- [ ] **Step 3: Add a write helper in handler**

Add helper:

```go
func (h *Handler) stageContent(vault, path string, data []byte, finalizers, rollbacks *[]func() error) (contentRef string, latest *storage.StagedFileOp, err error) {
	ref, objectStage, err := h.storage.StageObjectWrite(data)
	if err != nil {
		return "", nil, err
	}
	if err := objectStage.Commit(); err != nil {
		_ = objectStage.Rollback()
		return "", nil, err
	}

	latestStage, err := h.storage.StageWrite(vault, path, data)
	if err != nil {
		return "", nil, err
	}
	*finalizers = append(*finalizers, latestStage.Commit)
	*rollbacks = append(*rollbacks, latestStage.Rollback)
	return ref, latestStage, nil
}
```

Use it in `handleFilePut`, `handleConflictResolveUpdate`, and any path that creates an active new server version.
Pass `contentRef` and `msg.Encoding` into DB helpers.

- [ ] **Step 4: Use object refs for downloads and conflicts**

Change `makeDownloadEntry`, `makeSyncInitConflict`, and `sendConflictResult` to read:

```go
func (h *Handler) readFileContent(vault string, f db.File) ([]byte, error) {
	if f.ContentRef != "" {
		return h.storage.ReadObject(f.ContentRef)
	}
	return h.storage.ReadFile(vault, f.Path)
}
```

Then encode the returned content with the row's encoding where available.

- [ ] **Step 5: Verify WebSocket tests pass**

Run:

```bash
rtk go test -C server ./internal/ws -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add server/internal/ws/handler.go server/internal/ws/handler_test.go
rtk git commit -m "feat: persist version content references"
```

### Task 6: Add The Diff-Match-Patch Merge Helper

**Files:**
- Create: `server/internal/sync/merge.go`
- Create: `server/internal/sync/merge_test.go`
- Modify: `server/go.mod`
- Modify: `server/go.sum`

- [ ] **Step 1: Add dependency**

Run:

```bash
rtk go get -C server github.com/sergi/go-diff/diffmatchpatch@latest
```

Expected: `server/go.mod` contains `github.com/sergi/go-diff`.

- [ ] **Step 2: Write merge tests**

Create `server/internal/sync/merge_test.go`:

```go
package sync

import "testing"

func TestMergeTextDifferentLines(t *testing.T) {
	base := "title\none\ntwo\n"
	local := "title\nONE\ntwo\n"
	server := "title\none\nTWO\n"
	got, ok := MergeText(base, local, server)
	if !ok {
		t.Fatal("expected merge success")
	}
	want := "title\nONE\nTWO\n"
	if got != want {
		t.Fatalf("merged = %q, want %q", got, want)
	}
}

func TestMergeTextSameLineConflict(t *testing.T) {
	base := "title\none\n"
	local := "title\nlocal\n"
	server := "title\nserver\n"
	if got, ok := MergeText(base, local, server); ok {
		t.Fatalf("expected conflict, got %q", got)
	}
}

func TestMergeTextSameInsertConflict(t *testing.T) {
	base := "a\nb\n"
	local := "a\nlocal\nb\n"
	server := "a\nserver\nb\n"
	if got, ok := MergeText(base, local, server); ok {
		t.Fatalf("expected conflict, got %q", got)
	}
}

func TestMergeTextIdenticalChange(t *testing.T) {
	base := "a\nb\n"
	local := "a\nB\n"
	server := "a\nB\n"
	got, ok := MergeText(base, local, server)
	if !ok || got != local {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}
```

- [ ] **Step 3: Verify tests fail**

Run:

```bash
rtk go test -C server ./internal/sync -run TestMergeText -count=1
```

Expected: compile failure because `MergeText` does not exist.

- [ ] **Step 4: Implement `MergeText`**

Create `server/internal/sync/merge.go` with a deterministic range-based helper. Use diff-match-patch for diff generation and expose this public function:

```go
func MergeText(base, local, server string) (string, bool)
```

Represent each side's edits as:

```go
type textEdit struct {
	start int
	end   int
	text  string
}
```

Rules:

```go
if local == server {
	return local, true
}
if base == local {
	return server, true
}
if base == server {
	return local, true
}
```

For non-trivial edits, convert each diff to base-coordinate `textEdit` values. Sort all edits by `start`. Reject the merge if two edits overlap, or if both are insert-only at the same `start` with different `text`. Build output by copying unchanged base spans and replacing edited spans with edit text.

- [ ] **Step 5: Verify merge tests pass**

Run:

```bash
rtk go test -C server ./internal/sync -run TestMergeText -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Before staging, inspect `server/go.mod` because this worktree may already contain unrelated user edits:

```bash
rtk git diff -- server/go.mod server/go.sum
```

Stage only the diff-match-patch dependency hunks plus the merge helper files. If unrelated user edits are present in `server/go.mod`, use interactive hunk staging:

```bash
rtk git add server/internal/sync/merge.go server/internal/sync/merge_test.go server/go.sum
rtk git add -p server/go.mod
rtk git commit -m "feat: add text auto merge helper"
```

### Task 7: Extend Server Protocol Types

**Files:**
- Modify: `server/internal/ws/messages.go`
- Modify: `server/internal/ws/handler.go`

- [ ] **Step 1: Add compile-level protocol test**

Add a small test in `server/internal/ws/handler_test.go`:

```go
func TestMergePutMessageShape(t *testing.T) {
	raw := []byte(`{"type":"mergePut","vault":"personal","path":"notes/a.md","content":"local","expectedServerVersion":2,"file":{"path":"notes/a.md","exists":true,"baseVersion":1,"baseHash":"base","localHash":"local"}}`)
	msg, err := UnmarshalMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ExpectedServerVersion == nil || *msg.ExpectedServerVersion != 2 {
		t.Fatalf("expectedServerVersion = %#v", msg.ExpectedServerVersion)
	}
}
```

- [ ] **Step 2: Verify test fails**

Run:

```bash
rtk go test -C server ./internal/ws -run TestMergePutMessageShape -count=1
```

Expected: compile failure for `ExpectedServerVersion`.

- [ ] **Step 3: Add message fields**

Update `server/internal/ws/messages.go`:

```go
type AutoMergeEntry struct {
	Path          string `json:"path"`
	BaseVersion   int64  `json:"baseVersion"`
	BaseHash      string `json:"baseHash"`
	LocalHash     string `json:"localHash"`
	ServerVersion int64  `json:"serverVersion"`
	ServerHash    string `json:"serverHash"`
	Encoding      string `json:"encoding,omitempty"`
}

type IncomingMessage struct {
	Type                  string        `json:"type"`
	Vault                 string        `json:"vault"`
	Path                  string        `json:"path,omitempty"`
	Content               string        `json:"content,omitempty"`
	Encoding              string        `json:"encoding,omitempty"`
	File                  *FilePayload  `json:"file,omitempty"`
	Files                 []FilePayload `json:"files,omitempty"`
	Resolution            string        `json:"resolution,omitempty"`
	Action                string        `json:"action,omitempty"`
	ExpectedServerVersion *int64        `json:"expectedServerVersion,omitempty"`
}

type OutgoingMessage struct {
	Type          string              `json:"type"`
	Vault         string              `json:"vault,omitempty"`
	Path          string              `json:"path,omitempty"`
	Action        string              `json:"action,omitempty"`
	Ok            *bool               `json:"ok,omitempty"`
	Content       string              `json:"content,omitempty"`
	Encoding      string              `json:"encoding,omitempty"`
	ToPut         []string            `json:"toPut,omitempty"`
	Meta          *ServerMetaPayload  `json:"meta,omitempty"`
	Conflict      *ConflictInfo       `json:"conflict,omitempty"`
	ToDownload    []DownloadEntry     `json:"toDownload,omitempty"`
	ToAutoMerge   []AutoMergeEntry    `json:"toAutoMerge,omitempty"`
	ToUpdateMeta  []ServerMetaPayload `json:"toUpdateMeta,omitempty"`
	ToDeleteLocal []ServerMetaPayload `json:"toDeleteLocal,omitempty"`
	ToRemoveMeta  []ServerMetaPayload `json:"toRemoveMeta,omitempty"`
	Conflicts     []SyncConflictEntry `json:"conflicts,omitempty"`
	Error         string              `json:"error,omitempty"`
}
```

- [ ] **Step 4: Register `mergePut` dispatch**

In `handler.go`, update known message types:

```go
func isKnownMessageType(messageType string) bool {
	switch messageType {
	case "vaultCreate", "syncInit", "fileCheck", "filePut", "fileDelete", "conflictResolve", "mergePut":
		return true
	default:
		return false
	}
}
```

Then add dispatch:

```go
case "mergePut":
	h.handleMergePut(sender, msg, finalizers, rollbacks)
```

For this task, create a stub:

```go
func (h *Handler) handleMergePut(sender messageSender, msg IncomingMessage, finalizers *[]func() error, rollbacks *[]func() error) {
	sender.SendMessage(OutgoingMessage{Type: "mergePutResult", Path: msg.Path, Action: string(syncpkg.MatrixActionConflict), Error: "mergePut handler registered before merge behavior"})
}
```

Add a public entrypoint test that uses `HandleMessage`, not the private handler:

```go
func TestHandleMessageAcceptsMergePutType(t *testing.T) {
	h, _, _, _ := setupHandler(t)
	client := makeClient(h.hub, "personal")
	h.HandleMessage(client, []byte(`{"type":"mergePut","vault":"personal","path":"notes/a.md","expectedServerVersion":1,"file":{"path":"notes/a.md","exists":true,"baseVersion":1,"localHash":"local"},"content":"local"}`))
	msg := readResponse(t, client)
	if msg.Type != "mergePutResult" {
		t.Fatalf("message = %#v", msg)
	}
}
```

- [ ] **Step 5: Verify protocol tests pass**

Run:

```bash
rtk go test -C server ./internal/ws -run 'Test(MergePutMessageShape|HandleMessageAcceptsMergePutType)' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add server/internal/ws/messages.go server/internal/ws/handler.go server/internal/ws/handler_test.go
rtk git commit -m "feat: add auto merge protocol messages"
```

### Task 8: Implement Base-Aware `syncInit` And `fileCheck`

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Add `syncInit` false-conflict test**

Add:

```go
func TestSyncInitDownloadsWhenLocalEqualsBaseAndServerAdvanced(t *testing.T) {
	h, _, _ := setupHandlerTest(t)
	seedVersionObject(t, h, "personal", "notes/a.md", "base", "")
	seedVersionObject(t, h, "personal", "notes/a.md", "server", "")

	msg := sendJSON(t, h, IncomingMessage{
		Type: "syncInit",
		Vault: "personal",
		Files: []FilePayload{{
			Path: "notes/a.md", Exists: true,
			BaseVersion: int64Ptr(1), BaseHash: hashString("base"), LocalHash: hashString("base"),
		}},
	})

	if len(msg.ToDownload) != 1 || msg.ToDownload[0].Content != "server" {
		t.Fatalf("syncResult = %#v", msg)
	}
	if len(msg.Conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %#v", msg.Conflicts)
	}
}
```

- [ ] **Step 2: Add `fileCheck` autoMergeRequired test**

Add:

```go
func TestFileCheckReturnsAutoMergeRequiredForCleanTextCandidate(t *testing.T) {
	h, _, _ := setupHandlerTest(t)
	seedVersionObject(t, h, "personal", "notes/a.md", "a\nb\n", "")
	seedVersionObject(t, h, "personal", "notes/a.md", "a\nB-server\n", "")

	msg := sendJSON(t, h, IncomingMessage{
		Type: "fileCheck", Vault: "personal", Path: "notes/a.md",
		File: &FilePayload{Path: "notes/a.md", Exists: true, BaseVersion: int64Ptr(1), BaseHash: hashString("a\nb\n"), LocalHash: hashString("A-local\nb\n")},
	})

	if msg.Action != "autoMergeRequired" || msg.Meta == nil || msg.Meta.ServerVersion != 2 {
		t.Fatalf("fileCheckResult = %#v", msg)
	}
}
```

- [ ] **Step 3: Verify tests fail**

Run:

```bash
rtk go test -C server ./internal/ws -run 'Test(SyncInitDownloadsWhenLocalEqualsBase|FileCheckReturnsAutoMergeRequired)' -count=1
```

Expected: FAIL because base rows are not loaded into decision input and handlers do not emit auto merge.

- [ ] **Step 4: Load base rows in decision input**

Update `decisionInputForPath` to:

```go
var base db.File
baseExists := false
if payload.BaseVersion != nil {
	base, err = h.queries.GetFileVersion(msg.Vault, path, *payload.BaseVersion)
	if err != nil && err != sql.ErrNoRows {
		return syncpkg.DecisionInput{}, db.File{}, false, err
	}
	baseExists = err == nil
}
```

Populate:

```go
BaseRowExists: baseExists,
BaseHash:      base.Hash,
AutoMerge:     h.autoMergeState(msg.Vault, path, payload, sf, base, baseExists),
```

Implement `autoMergeState` to return `AutoMergePossible` only when:

```go
payload.LocalHash != ""
payload.LocalHash != base.Hash
sf.ContentRef != ""
base.ContentRef != ""
sf.Encoding != "base64"
base.Encoding != "base64"
```

- [ ] **Step 5: Emit `toAutoMerge` and `autoMergeRequired`**

In `handleSyncInit`, add:

```go
var toAutoMerge []AutoMergeEntry
```

On `MatrixActionAutoMerge` append:

```go
toAutoMerge = append(toAutoMerge, AutoMergeEntry{
	Path: cf.Path, BaseVersion: *cf.BaseVersion, BaseHash: cf.BaseHash, LocalHash: cf.LocalHash,
	ServerVersion: sf.Version, ServerHash: sf.Hash, Encoding: sf.Encoding,
})
```

Include `ToAutoMerge: toAutoMerge` in `syncResult`.

In `handleFileCheck`, on `MatrixActionAutoMerge` respond:

```go
resp.Action = "autoMergeRequired"
resp.Meta = serverMeta(sf)
```

- [ ] **Step 6: Verify WebSocket tests pass**

Run:

```bash
rtk go test -C server ./internal/ws -run 'Test(SyncInit|FileCheck)' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
rtk git add server/internal/ws/handler.go server/internal/ws/handler_test.go
rtk git commit -m "feat: make read checks base-aware"
```

### Task 9: Implement `filePut` Auto Merge And `toDownload`

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Add `filePut` base-match test**

Add:

```go
func TestFilePutReturnsDownloadWhenLocalEqualsBaseAndServerAdvanced(t *testing.T) {
	h, _, _ := setupHandlerTest(t)
	seedVersionObject(t, h, "personal", "notes/a.md", "base", "")
	seedVersionObject(t, h, "personal", "notes/a.md", "server", "")

	msg := sendJSON(t, h, IncomingMessage{
		Type: "filePut", Vault: "personal", Path: "notes/a.md", Content: "base",
		File: &FilePayload{Path: "notes/a.md", Exists: true, BaseVersion: int64Ptr(1), BaseHash: hashString("base"), LocalHash: hashString("base")},
	})

	if msg.Action != "toDownload" || msg.Content != "server" || msg.Meta.ServerVersion != 2 {
		t.Fatalf("filePutResult = %#v", msg)
	}
}
```

- [ ] **Step 2: Add `filePut` clean merge test**

Add:

```go
func TestFilePutAutoMergeSuccessCreatesVersionAndDownloadsMerged(t *testing.T) {
	h, _, _ := setupHandlerTest(t)
	seedVersionObject(t, h, "personal", "notes/a.md", "a\nb\n", "")
	seedVersionObject(t, h, "personal", "notes/a.md", "a\nserver\n", "")

	msg := sendJSON(t, h, IncomingMessage{
		Type: "filePut", Vault: "personal", Path: "notes/a.md", Content: "local\nb\n",
		File: &FilePayload{Path: "notes/a.md", Exists: true, BaseVersion: int64Ptr(1), BaseHash: hashString("a\nb\n"), LocalHash: hashString("local\nb\n")},
	})

	if msg.Action != "toDownload" || msg.Content != "local\nserver\n" {
		t.Fatalf("filePutResult = %#v", msg)
	}
	if msg.Meta.ServerVersion != 3 {
		t.Fatalf("serverVersion = %d, want 3", msg.Meta.ServerVersion)
	}
}
```

- [ ] **Step 3: Verify tests fail**

Run:

```bash
rtk go test -C server ./internal/ws -run 'TestFilePut(ReturnsDownload|AutoMergeSuccess)' -count=1
```

Expected: FAIL because `filePut` still conflicts.

- [ ] **Step 4: Add handler merge helper**

Add:

```go
func (h *Handler) tryAutoMerge(vault, path string, base, latest db.File, localContent []byte, localHash, encoding string, finalizers, rollbacks *[]func() error) (db.File, string, bool, error) {
	if localHash == "" || encoding == "base64" || base.Encoding == "base64" || latest.Encoding == "base64" {
		return db.File{}, "", false, nil
	}
	if calculated := hashBytes(localContent); calculated != localHash {
		return db.File{}, "", false, fmt.Errorf("local content hash mismatch")
	}
	baseContent, err := h.readFileContent(vault, base)
	if err != nil {
		return db.File{}, "", false, err
	}
	serverContent, err := h.readFileContent(vault, latest)
	if err != nil {
		return db.File{}, "", false, err
	}
	merged, ok := syncpkg.MergeText(string(baseContent), string(localContent), string(serverContent))
	if !ok {
		return db.File{}, "", false, nil
	}
	mergedBytes := []byte(merged)
	mergedHash := hashBytes(mergedBytes)
	contentRef, _, err := h.stageContent(vault, path, mergedBytes, finalizers, rollbacks)
	if err != nil {
		return db.File{}, "", false, err
	}
	newFile, err := h.queries.UpdateFile(vault, path, mergedHash, contentRef, "")
	if err != nil {
		return db.File{}, "", false, err
	}
	return newFile, merged, true, nil
}
```

Use the repository's existing hash helper if one already exists; otherwise add a small `hashBytes` helper that returns SHA-256 hex.

- [ ] **Step 5: Handle `MatrixActionToDownload` and `MatrixActionAutoMerge` in `handleFilePut`**

For `ToDownload`:

```go
entry, ok := h.makeDownloadEntry(msg.Vault, sf)
if !ok { /* send error */ }
sender.SendMessage(OutgoingMessage{
	Type: "filePutResult", Path: path, Action: "toDownload",
	Content: entry.Content, Encoding: entry.Encoding, Meta: serverMeta(sf),
})
```

For `AutoMerge`, load `base` via `GetFileVersion`, decode local content, call `tryAutoMerge`, and send:

```go
sender.SendMessage(OutgoingMessage{
	Type: "filePutResult", Path: path, Action: "toDownload",
	Content: merged, Encoding: mergedFile.Encoding, Meta: serverMeta(mergedFile),
})
```

If merge fails without error, call `sendConflictResult`.

- [ ] **Step 6: Verify WebSocket filePut tests pass**

Run:

```bash
rtk go test -C server ./internal/ws -run TestFilePut -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
rtk git add server/internal/ws/handler.go server/internal/ws/handler_test.go
rtk git commit -m "feat: auto merge divergent file puts"
```

### Task 10: Implement `mergePut`

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Add `mergePut` success test**

Add:

```go
func TestMergePutSuccessReturnsToDownload(t *testing.T) {
	h, _, _ := setupHandlerTest(t)
	seedVersionObject(t, h, "personal", "notes/a.md", "a\nb\n", "")
	seedVersionObject(t, h, "personal", "notes/a.md", "a\nserver\n", "")

	msg := sendJSON(t, h, IncomingMessage{
		Type: "mergePut", Vault: "personal", Path: "notes/a.md", Content: "local\nb\n",
		ExpectedServerVersion: int64Ptr(2),
		File: &FilePayload{Path: "notes/a.md", Exists: true, BaseVersion: int64Ptr(1), BaseHash: hashString("a\nb\n"), LocalHash: hashString("local\nb\n")},
	})

	if msg.Type != "mergePutResult" || msg.Action != "toDownload" || msg.Content != "local\nserver\n" {
		t.Fatalf("mergePutResult = %#v", msg)
	}
}
```

- [ ] **Step 2: Add version-race tests**

Add:

```go
func TestMergePutLatestGreaterThanExpectedReturnsAutoMergeRequired(t *testing.T) {
	h, _, _ := setupHandlerTest(t)
	seedVersionObject(t, h, "personal", "notes/a.md", "base", "")
	seedVersionObject(t, h, "personal", "notes/a.md", "server2", "")
	seedVersionObject(t, h, "personal", "notes/a.md", "server3", "")

	msg := sendJSON(t, h, IncomingMessage{
		Type: "mergePut", Vault: "personal", Path: "notes/a.md", Content: "local",
		ExpectedServerVersion: int64Ptr(2),
		File: &FilePayload{Path: "notes/a.md", Exists: true, BaseVersion: int64Ptr(1), BaseHash: hashString("base"), LocalHash: hashString("local")},
	})

	if msg.Type != "mergePutResult" || msg.Action != "autoMergeRequired" || msg.Meta.ServerVersion != 3 {
		t.Fatalf("mergePutResult = %#v", msg)
	}
}
```

- [ ] **Step 3: Verify tests fail**

Run:

```bash
rtk go test -C server ./internal/ws -run TestMergePut -count=1
```

Expected: FAIL because handler is stubbed.

- [ ] **Step 4: Implement validation**

At start of `handleMergePut`:

```go
if msg.File == nil {
	sender.SendMessage(OutgoingMessage{Type: "mergePutResult", Path: msg.Path, Error: "missing file payload"})
	return
}
if msg.File.BaseVersion == nil {
	sender.SendMessage(OutgoingMessage{Type: "mergePutResult", Path: msg.Path, Error: "missing file.baseVersion"})
	return
}
if msg.ExpectedServerVersion == nil {
	sender.SendMessage(OutgoingMessage{Type: "mergePutResult", Path: msg.Path, Error: "missing expectedServerVersion"})
	return
}
if msg.File.LocalHash == "" {
	sender.SendMessage(OutgoingMessage{Type: "mergePutResult", Path: msg.Path, Error: "missing file.localHash"})
	return
}
```

- [ ] **Step 5: Implement expected-version branching**

Load latest and base. Then:

```go
if sf.Version > *msg.ExpectedServerVersion {
	sender.SendMessage(OutgoingMessage{Type: "mergePutResult", Path: path, Action: "autoMergeRequired", Meta: serverMeta(sf)})
	return
}
if sf.Version < *msg.ExpectedServerVersion {
	h.sendConflictResult(sender, "mergePutResult", msg.Vault, path, syncpkg.MatrixActionConflict, sf)
	return
}
```

When equal, decode local content and call `tryAutoMerge`.

Before inserting the merged row, re-check latest version in the same transaction or use an update helper that validates the expected latest version:

```go
func (h *Handler) saveMergedVersion(vault, path string, expectedLatest int64, merged []byte, finalizers, rollbacks *[]func() error) (db.File, string, error) {
	latest, err := h.queries.GetFile(vault, path)
	if err != nil {
		return db.File{}, "", err
	}
	if latest.Version != expectedLatest {
		return latest, "", errServerAdvanced
	}
	mergedHash := hashBytes(merged)
	contentRef, _, err := h.stageContent(vault, path, merged, finalizers, rollbacks)
	if err != nil {
		return db.File{}, "", err
	}
	newFile, err := h.queries.UpdateFile(vault, path, mergedHash, contentRef, "")
	if err != nil {
		latestAfter, latestErr := h.queries.GetFile(vault, path)
		if latestErr == nil && latestAfter.Version > expectedLatest {
			return latestAfter, "", errServerAdvanced
		}
		return db.File{}, "", err
	}
	return newFile, string(merged), nil
}
```

If `errServerAdvanced` is returned, send `mergePutResult` with `action: "autoMergeRequired"` and `Meta: serverMeta(latest)` instead of a generic error.

- [ ] **Step 6: Send success or conflict**

On success:

```go
sender.SendMessage(OutgoingMessage{
	Type: "mergePutResult", Path: path, Action: "toDownload",
	Content: merged, Encoding: newFile.Encoding, Meta: serverMeta(newFile),
})
```

On clean merge failure:

```go
h.sendConflictResult(sender, "mergePutResult", msg.Vault, path, syncpkg.MatrixActionConflict, sf)
```

- [ ] **Step 7: Verify mergePut tests pass**

Run:

```bash
rtk go test -C server ./internal/ws -run TestMergePut -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
rtk git add server/internal/ws/handler.go server/internal/ws/handler_test.go
rtk git commit -m "feat: implement merge put follow-up"
```

### Task 11: Extend Plugin WebSocket Types

**Files:**
- Modify: `plugin/src/ws-client.ts`
- Create: `plugin/src/__tests__/ws-client-auto-merge.test.ts`

- [ ] **Step 1: Write WebSocket builder tests**

Create:

```ts
import { buildMergePutMessage } from "../ws-client";

test("buildMergePutMessage includes expectedServerVersion", () => {
  expect(buildMergePutMessage("personal", "notes/a.md", "local", {
    path: "notes/a.md",
    exists: true,
    baseVersion: 1,
    baseHash: "base",
    localHash: "local",
  }, 2)).toEqual({
    type: "mergePut",
    vault: "personal",
    path: "notes/a.md",
    content: "local",
    file: {
      path: "notes/a.md",
      exists: true,
      baseVersion: 1,
      baseHash: "base",
      localHash: "local",
    },
    expectedServerVersion: 2,
  });
});
```

- [ ] **Step 2: Verify test fails**

Run:

```bash
rtk npm --prefix plugin test -- --run src/__tests__/ws-client-auto-merge.test.ts
```

Expected: compile failure for `buildMergePutMessage`.

- [ ] **Step 3: Add types and builder**

Update `plugin/src/ws-client.ts`:

```ts
export interface AutoMergeEntry {
  path: string;
  baseVersion: number;
  baseHash: string;
  localHash: string;
  serverVersion: number;
  serverHash: string;
  encoding?: string;
}
```

Extend `ServerAction` with:

```ts
| "autoMergeRequired"
```

Extend `ServerMessage`:

```ts
toAutoMerge?: AutoMergeEntry[];
```

Add:

```ts
export function buildMergePutMessage(
  vault: string,
  path: string,
  content: string,
  file: FilePayload,
  expectedServerVersion: number,
  encoding?: string,
) {
  const msg: Record<string, unknown> = { type: "mergePut", vault, path, content, file, expectedServerVersion };
  if (encoding) msg.encoding = encoding;
  return msg;
}
```

Add sender:

```ts
sendMergePut(vault: string, path: string, content: string, file: FilePayload, expectedServerVersion: number, encoding?: string): boolean {
  return this.send(buildMergePutMessage(vault, path, content, file, expectedServerVersion, encoding));
}
```

- [ ] **Step 4: Verify plugin WebSocket tests pass**

Run:

```bash
rtk npm --prefix plugin test -- --run src/__tests__/ws-client-auto-merge.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add plugin/src/ws-client.ts plugin/src/__tests__/ws-client-auto-merge.test.ts
rtk git commit -m "feat: add plugin merge put protocol"
```

### Task 12: Add Plugin Merge-In-Flight Handling

**Files:**
- Modify: `plugin/src/sync.ts`
- Modify: `plugin/src/dirty-queue.ts`
- Create: `plugin/src/__tests__/sync-auto-merge.test.ts`

- [ ] **Step 1: Write plugin auto-merge follow-up tests**

Create `plugin/src/__tests__/sync-auto-merge.test.ts` with this scaffold and tests:

```ts
import { describe, expect, test, vi } from "vitest";
import { SyncManager } from "../sync";
import { FileMetaStore } from "../file-meta-store";
import { DirtyQueue } from "../dirty-queue";
import { sha256 } from "../hash";

function createAdapter(initial: Record<string, string>) {
  const files = new Map(Object.entries(initial));
  return {
    async exists(path: string) { return files.has(path); },
    async read(path: string) { return files.get(path) || ""; },
    async write(path: string, data: string) { files.set(path, data); },
    async readBinary(path: string) { return new TextEncoder().encode(files.get(path) || "").buffer; },
    async writeBinary(path: string, data: ArrayBuffer) { files.set(path, new TextDecoder().decode(data)); },
    async mkdir(_path: string) {},
    async remove(path: string) { files.delete(path); },
    async rename(from: string, to: string) { files.set(to, files.get(from) || ""); files.delete(from); },
  };
}

async function createSyncManagerHarness(input: {
  files: Record<string, string>;
  meta: Record<string, { prevServerVersion: number; prevServerHash: string }>;
  dirty?: Array<{ path: string; baseVersion?: number; lastSeenHash: string }>;
}) {
  const adapter = createAdapter(input.files);
  const vault = { adapter } as any;
  const fileMeta = new FileMetaStore(input.meta, async () => {});
  const manager = new SyncManager({} as any, vault, "ws://localhost", "token", "personal", fileMeta, ".goat-delete-queue.json");
  const wsClient = {
    on: vi.fn(),
    connect: vi.fn(),
    disconnect: vi.fn(),
    startHealthCheck: vi.fn(),
    sendMergePut: vi.fn(() => true),
    sendFilePut: vi.fn(() => true),
    sendFileCheck: vi.fn(() => true),
    sendFileDelete: vi.fn(() => true),
    sendSyncInit: vi.fn(() => true),
  };
  const dirtyQueue = new DirtyQueue();
  for (const entry of input.dirty || []) {
    await dirtyQueue.enqueue(entry);
  }
  (manager as any).wsClient = wsClient;
  (manager as any).dirtyQueue = dirtyQueue;
  return { manager: manager as any, wsClient, adapter, fileMeta, dirtyQueue };
}

describe("auto merge flow", () => {
test("syncResult toAutoMerge sends mergePut with current local content", async () => {
  const harness = await createSyncManagerHarness({
    files: { "notes/a.md": "local" },
    meta: { "notes/a.md": { prevServerVersion: 1, prevServerHash: "base" } },
  });
  await harness.manager["handleSyncResult"]({
    type: "syncResult",
    toAutoMerge: [{
      path: "notes/a.md",
      baseVersion: 1,
      baseHash: "base",
      localHash: "stale-local",
      serverVersion: 2,
      serverHash: "server",
    }],
  });
  expect(harness.wsClient.sendMergePut).toHaveBeenCalledWith(
    "personal",
    "notes/a.md",
    "local",
    expect.objectContaining({ path: "notes/a.md", exists: true, baseVersion: 1, baseHash: "base" }),
    2,
    undefined,
  );
});

test("mergePutResult toDownload applies merged content and clears dirty queue", async () => {
  const harness = await createSyncManagerHarness({
    files: { "notes/a.md": "local" },
    meta: { "notes/a.md": { prevServerVersion: 1, prevServerHash: "base" } },
    dirty: [{ path: "notes/a.md", baseVersion: 1, lastSeenHash: "local" }],
  });
  await harness.manager["handleMergePutResult"]({
    type: "mergePutResult",
    path: "notes/a.md",
    action: "toDownload",
    content: "merged",
    meta: { path: "notes/a.md", serverVersion: 3, serverHash: await sha256("merged"), isDeleted: false },
  });
  expect(await harness.adapter.read("notes/a.md")).toBe("merged");
  expect(harness.fileMeta.get("notes/a.md")).toEqual({ prevServerVersion: 3, prevServerHash: await sha256("merged") });
  expect(harness.dirtyQueue.get("notes/a.md")).toBeUndefined();
});

test("mergePutResult preserves dirty entry when user edited during merge", async () => {
  const localHash = await sha256("local");
  const newerHash = await sha256("newer local");
  const harness = await createSyncManagerHarness({
    files: { "notes/a.md": "newer local" },
    meta: { "notes/a.md": { prevServerVersion: 1, prevServerHash: "base" } },
    dirty: [{ path: "notes/a.md", baseVersion: 1, lastSeenHash: localHash }],
  });
  await harness.dirtyQueue.markSentHash("notes/a.md", localHash, localHash);
  await harness.dirtyQueue.enqueue({ path: "notes/a.md", baseVersion: 1, lastSeenHash: newerHash });
  await harness.manager["handleMergePutResult"]({
    type: "mergePutResult",
    path: "notes/a.md",
    action: "toDownload",
    content: "merged",
    meta: { path: "notes/a.md", serverVersion: 3, serverHash: await sha256("merged"), isDeleted: false },
  });
  expect(harness.dirtyQueue.get("notes/a.md")?.lastSeenHash).toBe(newerHash);
});
});
```

- [ ] **Step 2: Verify tests fail**

Run:

```bash
rtk npm --prefix plugin test -- --run src/__tests__/sync-auto-merge.test.ts
```

Expected: FAIL because sync manager does not listen for `mergePutResult` or handle `toAutoMerge`.

- [ ] **Step 3: Add merge-in-flight state**

In `SyncManager`:

```ts
private mergeInFlight = new Map<string, { sentHash: string }>();
```

Register listener in `start()`:

```ts
this.wsClient.on("mergePutResult", (msg) => this.handleMergePutResult(msg));
```

- [ ] **Step 4: Start follow-up merges**

Add:

```ts
private async startAutoMerge(entry: { path: string; baseVersion: number; baseHash: string; localHash?: string; serverVersion: number; serverHash: string; encoding?: string }) {
  const content = await this.readFileContent(entry.path);
  if (content === null) {
    return;
  }
  const localHash = await this.computeHashFromContent(entry.path, content);
  this.mergeInFlight.set(entry.path, { sentHash: localHash });
  const encoding = isBinaryPath(entry.path) ? "base64" : undefined;
  const sent = this.wsClient.sendMergePut(this.vaultName, entry.path, content, {
      path: entry.path,
      exists: true,
      baseVersion: entry.baseVersion,
      baseHash: entry.baseHash,
      localHash,
  }, entry.serverVersion, encoding);
  if (!sent) {
    this.mergeInFlight.delete(entry.path);
    await this.dirtyQueue.enqueue({ path: entry.path, baseVersion: entry.baseVersion, lastSeenHash: localHash });
  }
}
```

In `handleSyncResult`:

```ts
if (msg.toAutoMerge) {
  for (const entry of msg.toAutoMerge) {
    await this.startAutoMerge(entry);
  }
}
```

In `handleFileCheckResult`, add case:

```ts
case "autoMergeRequired":
  if (msg.meta) {
    const meta = this.fileMeta.get(msg.path);
    if (meta) {
      await this.startAutoMerge({
        path: msg.path,
        baseVersion: meta.prevServerVersion,
        baseHash: meta.prevServerHash,
        serverVersion: msg.meta.serverVersion,
        serverHash: msg.meta.serverHash || "",
      });
    }
  }
  break;
```

- [ ] **Step 5: Skip queue flushes while merging**

In `flushDeleteQueue`:

```ts
if (this.mergeInFlight.has(entry.path)) continue;
```

In `flushDirtyQueue`, if `claimNext()` returns a path in `mergeInFlight`, requeue it and continue. Add to `DirtyQueue`:

```ts
async release(path: string): Promise<void> {
  await this.mutex.runExclusive(() => {
    const entry = this.entries.get(path);
    if (!entry) return;
    entry.status = "pending";
    entry.sentHash = undefined;
  });
}
```

Then:

```ts
if (this.mergeInFlight.has(next.path)) {
  await this.dirtyQueue.release(next.path);
  return "ok";
}
```

- [ ] **Step 6: Handle mergePut results**

Add a merge-specific queue completion method to `DirtyQueue`:

```ts
async completeMergeSuccess(path: string, sentHash: string, meta: ServerMeta): Promise<void> {
  await this.mutex.runExclusive(() => {
    const entry = this.entries.get(path);
    if (!entry) return;
    if (entry.lastSeenHash === sentHash || entry.sentHash === sentHash) {
      this.entries.delete(path);
      return;
    }
    entry.baseVersion = meta.serverVersion;
    entry.status = "pending";
    entry.sentHash = undefined;
  });
}
```

Use it from merge result handling. Do not call `remove()` after it; that would discard a newer dirty entry created while merge was in flight.

Add:

```ts
private async handleMergePutResult(msg: ServerMessage) {
  if (this.handleServerError(msg)) return;
  if (!msg.path) return;
  try {
    if (msg.action === "toDownload" && msg.content !== undefined && msg.meta) {
      await this.applyDownloadEntry({
        path: msg.path,
        content: msg.content,
        serverVersion: msg.meta.serverVersion,
        serverHash: msg.meta.serverHash || "",
        encoding: msg.encoding,
      });
      const sentHash = this.mergeInFlight.get(msg.path)?.sentHash || "";
      await this.dirtyQueue.completeMergeSuccess(msg.path, sentHash, {
        serverVersion: msg.meta.serverVersion,
        serverHash: msg.meta.serverHash || "",
      });
      return;
    }
    if (msg.action === "autoMergeRequired" && msg.meta) {
      const meta = this.fileMeta.get(msg.path);
      if (meta) {
        await this.startAutoMerge({
          path: msg.path,
          baseVersion: meta.prevServerVersion,
          baseHash: meta.prevServerHash,
          serverVersion: msg.meta.serverVersion,
          serverHash: msg.meta.serverHash || "",
        });
      }
      return;
    }
    if ((msg.action === "conflict" || msg.action === "deleteConflict") && msg.conflict) {
      await this.enqueueLatestConflict(msg);
    }
  } finally {
    if (msg.action !== "autoMergeRequired") {
      this.mergeInFlight.delete(msg.path);
    }
  }
}
```

- [ ] **Step 7: Handle `filePutResult.toDownload`**

In `handleFilePutResult`, before conflict handling:

```ts
} else if (msg.action === "toDownload" && msg.content !== undefined && msg.meta) {
  const entry = this.dirtyQueue.get(msg.path);
  const sentHash = entry?.sentHash || entry?.lastSeenHash || "";
  await this.applyDownloadEntry({
    path: msg.path,
    content: msg.content,
    serverVersion: msg.meta.serverVersion,
    serverHash: msg.meta.serverHash || "",
    encoding: msg.encoding,
  });
  await this.dirtyQueue.completeMergeSuccess(msg.path, sentHash, {
    serverVersion: msg.meta.serverVersion,
    serverHash: msg.meta.serverHash || "",
  });
```

- [ ] **Step 8: Verify plugin auto-merge tests pass**

Run:

```bash
rtk npm --prefix plugin test -- --run src/__tests__/sync-auto-merge.test.ts
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
rtk git add plugin/src/sync.ts plugin/src/dirty-queue.ts plugin/src/__tests__/sync-auto-merge.test.ts
rtk git commit -m "feat: apply auto merge results in plugin"
```

### Task 13: Full Regression And Integration Cleanup

**Files:**
- Modify the files listed in the earlier tasks when a full regression exposes an integration mismatch.

- [ ] **Step 1: Run full server tests**

Run:

```bash
rtk go test -C server ./...
```

Expected: PASS.

- [ ] **Step 2: Run full plugin tests**

Run:

```bash
rtk npm --prefix plugin test
```

Expected: PASS.

- [ ] **Step 3: Run server package formatting**

Run:

```bash
rtk gofmt -w server/internal/db server/internal/storage server/internal/sync server/internal/ws
```

Expected: no output.

- [ ] **Step 4: Run plugin build**

Run:

```bash
rtk npm --prefix plugin run build
```

Expected: production build completes with no TypeScript errors.

- [ ] **Step 5: Check git diff for unrelated files**

Run:

```bash
rtk git status --short
rtk git diff --stat
```

Expected: the diff is limited to files named in this plan. Existing unrelated changes in `server/docker-compose.yml` or `server/go.mod` must not be reverted; if `server/go.mod` changed because of diff-match-patch, preserve both the user changes and dependency changes.

- [ ] **Step 6: Commit final fixes**

```bash
rtk git add docs/message-matrix.csv server/internal plugin/src plugin/package.json plugin/package-lock.json server/go.mod server/go.sum
rtk git commit -m "test: cover base history auto merge flow"
```

## Self-Review Notes

- Spec coverage: DB history, surrogate id, content refs, matrix expansion, `toDownload`, `toAutoMerge`, `autoMergeRequired`, `mergePut`, diff-match-patch wrapper, plugin merge-in-flight, and regression tests are covered by Tasks 1-13.
- M003/M023/M037 first-run mismatch remains conflict because Task 1 only changes base-present active-server divergent branches.
- Tombstone behavior is preserved by Task 3's append-only tombstone test and by leaving delete matrix branches unchanged.
- The plan uses `mergePutResult` for merge follow-up and `filePutResult.toDownload` for immediate `filePut` auto-merge, matching the design discussion.
- The plan intentionally does not include migration from the old `files` table because the accepted design explicitly allows replacing the current structure.
