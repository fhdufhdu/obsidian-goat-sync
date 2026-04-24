# Sync Matrix and Watcher Queue Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current sync protocol and watcher behavior with the documented `message-matrix.csv` server decisions and `watcher-event-sequence.md` client queue orchestration.

**Architecture:** Server sync decisions move into a shared matrix engine used by `syncInit`, `fileCheck`, `filePut`, and `fileDelete`. Client watcher events become queue updates; `DeleteQueue`, `DirtyQueue`, `BlockedPaths`, `SelfWriteSuppress`, and `SyncOrchestrator` handle durable delete intent, same-path coalescing, conflict blocking, self-write filtering, and ordered network work.

**Tech Stack:** Go 1.25, SQLite, Gorilla WebSocket, TypeScript, Obsidian API, Vitest.

---

## Source Documents

- `docs/message-matrix.csv`
- `docs/watcher-event-sequence.md`
- `docs/superpowers/specs/2026-04-24-sync-matrix-queue-refactor-design.md`

If a task reveals a contradiction in these files, stop and ask before changing implementation behavior.

## File Structure

### Server

- Create `server/internal/sync/matrix.go`: shared decision input/output model and `DecideSyncInit`, `DecideFileCheck`, `DecideFilePut`, `DecideFileDelete`.
- Create `server/internal/sync/matrix_test.go`: table tests mapped to `docs/message-matrix.csv` row IDs.
- Create `server/internal/sync/matrix_csv_test.go`: coverage test that ensures every CSV row ID has a fixture.
- Modify `server/internal/sync/conflict.go`: remove or reduce old `ClassifyFile`, `CheckFileCreate`, `CheckFileUpdate`, `CheckFileDelete` after matrix engine lands.
- Modify `server/internal/sync/conflict_test.go`: delete old tests or convert them to matrix tests.
- Modify `server/internal/ws/messages.go`: replace snake_case protocol with camelCase message types and `baseVersion` fields.
- Modify `server/internal/ws/handler.go`: route `syncInit`, `fileCheck`, `filePut`, `fileDelete`; remove `file_create` and `file_update` handling.
- Modify `server/internal/ws/handler_test.go`: update protocol tests to new message names and actions.
- Modify `server/internal/db/file.go`: keep schema unchanged and add `File.DeletedFromVersion()`.

### Client

- Create `plugin/src/async-mutex.ts`: tiny promise-based mutex for queue state.
- Create `plugin/src/dirty-queue.ts`: path-keyed coalescing queue with claim, mark-sent, success, retry, and conflict completion.
- Create `plugin/src/delete-queue.ts`: durable path-keyed delete queue stored in `delete-queue.json` with temp-file rename.
- Create `plugin/src/blocked-paths.ts`: in-memory conflict blocking registry.
- Create `plugin/src/self-write-suppress.ts`: in-memory self-write/delete suppress registry.
- Create `plugin/src/sync-orchestrator.ts`: ordered `deleteQueue -> dirtyQueue -> syncInit` execution under a global mutex.
- Modify `plugin/src/ws-client.ts`: new camelCase protocol methods and response action types.
- Modify `plugin/src/sync.ts`: wire watcher events into queues and delegate worker execution to `SyncOrchestrator`.
- Modify `plugin/src/main.ts`: pass plugin data directory path to `DeleteQueue`.
- Modify `plugin/src/file-meta-store.ts`: keep existing persisted meta shape; convert to `baseVersion`/`baseHash` only at protocol boundaries in `sync.ts`.
- Modify `plugin/src/file-watcher.ts`: add same-path debounce before queue enqueue.
- Add tests under `plugin/src/__tests__/` for every new queue module and orchestrator ordering.

---

### Task 1: Add Server Matrix Test Fixtures and CSV Coverage

**Files:**
- Create: `server/internal/sync/matrix_test.go`
- Create: `server/internal/sync/matrix_csv_test.go`
- Read: `docs/message-matrix.csv`

- [ ] **Step 1: Add matrix fixture test skeleton**

Create `server/internal/sync/matrix_test.go` with this initial content:

```go
package sync

import "testing"

type matrixFixture struct {
	ID            string
	Message       MatrixMessage
	ClientExists  bool
	BaseVersion   *int64
	ServerState   ServerStateKind
	VersionMatch  VersionMatch
	HashMatch     HashMatch
	Expected      MatrixAction
}

func ptr64(v int64) *int64 { return &v }

func matrixFixtures() []matrixFixture {
	return []matrixFixture{
		{ID: "M001", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionToPut},
		{ID: "M002", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionToUpdateMeta},
		{ID: "M003", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M004", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M005", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M006", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M007", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionToPut},
		{ID: "M008", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M009", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M010", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionNone},
		{ID: "M011", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionToPut},
		{ID: "M012", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionToUpdateMeta},
		{ID: "M013", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M014", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionConflict},
		{ID: "M015", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionToDownload},
		{ID: "M016", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionNone},
		{ID: "M017", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionNone},
		{ID: "M018", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionAny, HashMatch: HashNotApplicable, Expected: ActionDeleteConflict},
		{ID: "M019", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionToRemoveMeta},
		{ID: "M020", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionToUpdateMeta},
		{ID: "M021", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionPut},
		{ID: "M022", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionUpdateMeta},
		{ID: "M023", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M024", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M025", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M026", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M027", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionPut},
		{ID: "M028", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M029", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M030", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionUpToDate},
		{ID: "M031", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionPut},
		{ID: "M032", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionUpdateMeta},
		{ID: "M033", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M034", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionConflict},
		{ID: "M035", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionOkUpdateMeta},
		{ID: "M036", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionOkUpdateMeta},
		{ID: "M037", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M038", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M039", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M040", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M041", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionOkUpdateMeta},
		{ID: "M042", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M043", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M044", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionConflict},
		{ID: "M045", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionOkUpdateMeta},
		{ID: "M046", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionOkUpdateMeta},
		{ID: "M047", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionOkUpdateMeta},
		{ID: "M048", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M049", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionOkRemoveMeta},
		{ID: "M050", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionDeleteConflict},
		{ID: "M051", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionOkRemoveMeta},
		{ID: "M052", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashNotApplicable, Expected: ActionOkUpdateMeta},
		{ID: "M053", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashNotApplicable, Expected: ActionDeleteConflict},
		{ID: "M054", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionOkRemoveMeta},
		{ID: "M055", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualDeletedFrom, HashMatch: HashNotApplicable, Expected: ActionOkUpdateMeta},
		{ID: "M056", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(9), ServerState: ServerTombstone, VersionMatch: VersionNotEqualDeletedFrom, HashMatch: HashNotApplicable, Expected: ActionDeleteConflict},
	}
}

func TestMatrixFixtures(t *testing.T) {
	for _, tc := range matrixFixtures() {
		t.Run(tc.ID, func(t *testing.T) {
			input := DecisionInputFromFixture(tc)
			got := Decide(input)
			if got.Action != tc.Expected {
				t.Fatalf("expected %s, got %s", tc.Expected, got.Action)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails to compile**

Run:

```bash
cd server && rtk go test ./internal/sync
```

Expected: FAIL with undefined names such as `MatrixMessage`, `MatrixAction`, and `Decide`.

- [ ] **Step 3: Add CSV row coverage test**

Create `server/internal/sync/matrix_csv_test.go`:

```go
package sync

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatrixFixturesCoverEveryCSVRow(t *testing.T) {
	path := filepath.Join("..", "..", "..", "docs", "message-matrix.csv")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read matrix csv: %v", err)
	}

	text := strings.TrimPrefix(string(data), "\ufeff")
	rows, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		t.Fatalf("parse matrix csv: %v", err)
	}
	if len(rows) < 2 || rows[0][0] != "id" {
		t.Fatalf("csv must have id header, got %#v", rows[0])
	}

	fixtures := map[string]bool{}
	for _, f := range matrixFixtures() {
		fixtures[f.ID] = true
	}

	for _, row := range rows[1:] {
		id := row[0]
		if !fixtures[id] {
			t.Fatalf("missing matrix fixture for csv row %s", id)
		}
	}
}
```

- [ ] **Step 4: Run coverage test and confirm it fails**

Run:

```bash
cd server && rtk go test ./internal/sync -run TestMatrixFixturesCoverEveryCSVRow
```

Expected: FAIL with `missing matrix fixture for csv row M004`.

- [ ] **Step 5: Commit failing tests**

```bash
rtk git add server/internal/sync/matrix_test.go server/internal/sync/matrix_csv_test.go
rtk git commit -m "test: add sync matrix row coverage"
```

---

### Task 2: Implement Server Matrix Engine

**Files:**
- Create: `server/internal/sync/matrix.go`
- Modify: `server/internal/sync/matrix_test.go`
- Test: `server/internal/sync/matrix_test.go`

- [ ] **Step 1: Add the matrix engine types**

Create `server/internal/sync/matrix.go`:

```go
package sync

type MatrixMessage string

const (
	MessageSyncInit   MatrixMessage = "syncInit"
	MessageFileCheck  MatrixMessage = "fileCheck"
	MessageFilePut    MatrixMessage = "filePut"
	MessageFileDelete MatrixMessage = "fileDelete"
)

type ServerStateKind string

const (
	ServerMissing   ServerStateKind = "missing"
	ServerActive    ServerStateKind = "active"
	ServerTombstone ServerStateKind = "tombstone"
)

type VersionMatch string

const (
	VersionNotApplicable      VersionMatch = "n/a"
	VersionEqualServer        VersionMatch = "base==server"
	VersionNotEqualServer     VersionMatch = "base!=server"
	VersionEqualDeletedFrom   VersionMatch = "base==deletedFrom"
	VersionNotEqualDeletedFrom VersionMatch = "base!=deletedFrom"
	VersionAny                VersionMatch = "any"
)

type HashMatch string

const (
	HashNotApplicable HashMatch = "n/a"
	HashEqual         HashMatch = "equal"
	HashDifferent     HashMatch = "different"
)

type MatrixAction string

const (
	ActionNone           MatrixAction = "none"
	ActionToPut          MatrixAction = "toPut"
	ActionPut            MatrixAction = "put"
	ActionToUpdateMeta   MatrixAction = "toUpdateMeta"
	ActionUpdateMeta     MatrixAction = "updateMeta"
	ActionOkUpdateMeta   MatrixAction = "okUpdateMeta"
	ActionToDownload     MatrixAction = "toDownload"
	ActionToDeleteLocal  MatrixAction = "toDeleteLocal"
	ActionToRemoveMeta   MatrixAction = "toRemoveMeta"
	ActionOkRemoveMeta   MatrixAction = "okRemoveMeta"
	ActionUpToDate       MatrixAction = "upToDate"
	ActionConflict       MatrixAction = "conflict"
	ActionDeleteConflict MatrixAction = "deleteConflict"
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
}

type DecisionResult struct {
	Action MatrixAction
}

func DeletedFromVersion(serverVersion int64) int64 {
	return serverVersion - 1
}
```

- [ ] **Step 2: Add fixture conversion helper in tests**

Append to `server/internal/sync/matrix_test.go`:

```go
func DecisionInputFromFixture(f matrixFixture) DecisionInput {
	serverVersion := int64(10)
	deletedFrom := DeletedFromVersion(serverVersion)
	localHash := "local"
	serverHash := "server"
	if f.HashMatch == HashEqual {
		localHash = serverHash
	}

	baseVersion := f.BaseVersion
	switch f.VersionMatch {
	case VersionEqualServer:
		baseVersion = ptr64(serverVersion)
	case VersionNotEqualServer:
		baseVersion = ptr64(serverVersion - 1)
	case VersionEqualDeletedFrom:
		baseVersion = ptr64(deletedFrom)
	case VersionNotEqualDeletedFrom:
		baseVersion = ptr64(deletedFrom - 1)
	}

	return DecisionInput{
		Message:            f.Message,
		ClientExists:       f.ClientExists,
		BaseVersion:        baseVersion,
		LocalHash:          localHash,
		ServerState:        f.ServerState,
		ServerVersion:      serverVersion,
		ServerHash:         serverHash,
		DeletedFromVersion: deletedFrom,
	}
}
```

- [ ] **Step 3: Implement `Decide` dispatcher and message-specific functions**

Append to `server/internal/sync/matrix.go`:

```go
func Decide(input DecisionInput) DecisionResult {
	switch input.Message {
	case MessageSyncInit:
		return DecideSyncInit(input)
	case MessageFileCheck:
		return DecideFileCheck(input)
	case MessageFilePut:
		return DecideFilePut(input)
	case MessageFileDelete:
		return DecideFileDelete(input)
	default:
		return DecisionResult{Action: ActionConflict}
	}
}

func DecideSyncInit(input DecisionInput) DecisionResult {
	return decideReadOrCheck(input, ActionToPut, ActionToUpdateMeta, ActionNone)
}

func DecideFileCheck(input DecisionInput) DecisionResult {
	return decideReadOrCheck(input, ActionPut, ActionUpdateMeta, ActionUpToDate)
}

func decideReadOrCheck(input DecisionInput, putAction, updateMetaAction, cleanAction MatrixAction) DecisionResult {
	hasBase := input.BaseVersion != nil

	if input.ClientExists {
		if !hasBase {
			switch input.ServerState {
			case ServerMissing:
				return DecisionResult{Action: putAction}
			case ServerActive:
				if input.LocalHash == input.ServerHash {
					return DecisionResult{Action: updateMetaAction}
				}
				return DecisionResult{Action: ActionConflict}
			case ServerTombstone:
				if input.LocalHash == input.ServerHash {
					return DecisionResult{Action: ActionToDeleteLocal}
				}
				return DecisionResult{Action: ActionDeleteConflict}
			}
		}

		switch input.ServerState {
		case ServerMissing:
			return DecisionResult{Action: ActionConflict}
		case ServerTombstone:
			if *input.BaseVersion == input.ServerVersion {
				if input.LocalHash == input.ServerHash {
					return DecisionResult{Action: ActionToDeleteLocal}
				}
				return DecisionResult{Action: putAction}
			}
			if input.LocalHash == input.ServerHash {
				return DecisionResult{Action: ActionToDeleteLocal}
			}
			return DecisionResult{Action: ActionDeleteConflict}
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
			return DecisionResult{Action: ActionConflict}
		}
	}

	if !hasBase {
		switch input.ServerState {
		case ServerActive:
			return DecisionResult{Action: ActionToDownload}
		default:
			return DecisionResult{Action: ActionNone}
		}
	}

	switch input.ServerState {
	case ServerActive:
		return DecisionResult{Action: ActionDeleteConflict}
	case ServerMissing:
		return DecisionResult{Action: ActionToRemoveMeta}
	case ServerTombstone:
		return DecisionResult{Action: ActionToUpdateMeta}
	default:
		return DecisionResult{Action: ActionNone}
	}
}
```

- [ ] **Step 4: Implement `filePut` and `fileDelete` decisions**

Append to `server/internal/sync/matrix.go`:

```go
func DecideFilePut(input DecisionInput) DecisionResult {
	hasBase := input.BaseVersion != nil
	if !input.ClientExists {
		return DecisionResult{Action: ActionConflict}
	}

	if !hasBase {
		switch input.ServerState {
		case ServerMissing:
			return DecisionResult{Action: ActionOkUpdateMeta}
		case ServerActive:
			if input.LocalHash == input.ServerHash {
				return DecisionResult{Action: ActionOkUpdateMeta}
			}
			return DecisionResult{Action: ActionConflict}
		case ServerTombstone:
			if input.LocalHash == input.ServerHash {
				return DecisionResult{Action: ActionToDeleteLocal}
			}
			return DecisionResult{Action: ActionDeleteConflict}
		}
	}

	switch input.ServerState {
	case ServerMissing:
		return DecisionResult{Action: ActionConflict}
	case ServerTombstone:
		if *input.BaseVersion == input.ServerVersion {
			if input.LocalHash == input.ServerHash {
				return DecisionResult{Action: ActionToDeleteLocal}
			}
			return DecisionResult{Action: ActionOkUpdateMeta}
		}
		if input.LocalHash == input.ServerHash {
			return DecisionResult{Action: ActionToDeleteLocal}
		}
		return DecisionResult{Action: ActionDeleteConflict}
	case ServerActive:
		if *input.BaseVersion == input.ServerVersion {
			return DecisionResult{Action: ActionOkUpdateMeta}
		}
		if input.LocalHash == input.ServerHash {
			return DecisionResult{Action: ActionOkUpdateMeta}
		}
		return DecisionResult{Action: ActionConflict}
	default:
		return DecisionResult{Action: ActionConflict}
	}
}

func DecideFileDelete(input DecisionInput) DecisionResult {
	hasBase := input.BaseVersion != nil
	if !hasBase {
		switch input.ServerState {
		case ServerActive:
			return DecisionResult{Action: ActionDeleteConflict}
		default:
			return DecisionResult{Action: ActionOkRemoveMeta}
		}
	}

	switch input.ServerState {
	case ServerActive:
		if *input.BaseVersion == input.ServerVersion {
			return DecisionResult{Action: ActionOkUpdateMeta}
		}
		return DecisionResult{Action: ActionDeleteConflict}
	case ServerMissing:
		return DecisionResult{Action: ActionOkRemoveMeta}
	case ServerTombstone:
		if *input.BaseVersion == input.DeletedFromVersion {
			return DecisionResult{Action: ActionOkUpdateMeta}
		}
		return DecisionResult{Action: ActionDeleteConflict}
	default:
		return DecisionResult{Action: ActionOkRemoveMeta}
	}
}
```

- [ ] **Step 5: Run fixture coverage test**

Run:

```bash
cd server && rtk go test ./internal/sync -run TestMatrixFixturesCoverEveryCSVRow
```

Expected: PASS because `matrixFixtures()` includes every CSV row ID from `M001` through `M056`.

- [ ] **Step 6: Run sync package tests**

Run:

```bash
cd server && rtk go test ./internal/sync
```

Expected: PASS.

- [ ] **Step 7: Commit matrix engine**

```bash
rtk git add server/internal/sync/matrix.go server/internal/sync/matrix_test.go server/internal/sync/matrix_csv_test.go
rtk git commit -m "feat: add sync matrix decision engine"
```

---

### Task 3: Replace Server WebSocket Protocol Types

**Files:**
- Modify: `server/internal/ws/messages.go`
- Modify: `server/internal/ws/handler_test.go`
- Test: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Write a protocol unmarshalling test**

Append to `server/internal/ws/handler_test.go`:

```go
func TestIncomingMessageUsesCamelCaseProtocol(t *testing.T) {
	base := int64(7)
	msg := mustJSON(IncomingMessage{
		Type:   "filePut",
		Vault:  "personal",
		Path:   "notes/a.md",
		Content: "hello",
		File: &FilePayload{
			Path:        "notes/a.md",
			BaseVersion: &base,
			BaseHash:    "basehash",
			LocalHash:   "localhash",
		},
	})

	var decoded IncomingMessage
	if err := UnmarshalMessage(msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != "filePut" {
		t.Fatalf("type = %s", decoded.Type)
	}
	if decoded.File == nil || decoded.File.BaseVersion == nil || *decoded.File.BaseVersion != 7 {
		t.Fatalf("missing baseVersion in file payload: %#v", decoded.File)
	}
}
```

- [ ] **Step 2: Run test and confirm it fails**

Run:

```bash
cd server && rtk go test ./internal/ws -run TestIncomingMessageUsesCamelCaseProtocol
```

Expected: FAIL with undefined `FilePayload` or missing `File` field.

- [ ] **Step 3: Replace message structs**

In `server/internal/ws/messages.go`, replace the old sync file structs with:

```go
type FilePayload struct {
	Path        string `json:"path"`
	Exists      bool   `json:"exists"`
	BaseVersion *int64 `json:"baseVersion,omitempty"`
	BaseHash    string `json:"baseHash,omitempty"`
	LocalHash   string `json:"localHash,omitempty"`
}

type IncomingMessage struct {
	Type       string        `json:"type"`
	Vault      string        `json:"vault"`
	Path       string        `json:"path,omitempty"`
	Content    string        `json:"content,omitempty"`
	Encoding   string        `json:"encoding,omitempty"`
	File       *FilePayload  `json:"file,omitempty"`
	Files      []FilePayload `json:"files,omitempty"`
	Resolution string        `json:"resolution,omitempty"`
	Action     string        `json:"action,omitempty"`
}

type ServerMetaPayload struct {
	Path          string `json:"path,omitempty"`
	ServerVersion int64  `json:"serverVersion"`
	ServerHash    string `json:"serverHash,omitempty"`
	IsDeleted     bool   `json:"isDeleted"`
}

type DownloadEntry struct {
	Path          string `json:"path"`
	Content       string `json:"content"`
	ServerVersion int64  `json:"serverVersion"`
	ServerHash    string `json:"serverHash"`
	Encoding      string `json:"encoding,omitempty"`
}

type ConflictInfo struct {
	ServerVersion int64  `json:"serverVersion"`
	ServerHash    string `json:"serverHash"`
	ServerContent string `json:"serverContent"`
	IsDeleted     bool   `json:"isDeleted"`
	Encoding      string `json:"encoding,omitempty"`
}

type SyncConflictEntry struct {
	Path          string `json:"path"`
	BaseVersion  *int64 `json:"baseVersion,omitempty"`
	LocalHash    string `json:"localHash"`
	ServerVersion int64 `json:"serverVersion"`
	ServerHash    string `json:"serverHash"`
	ServerContent string `json:"serverContent"`
	IsDeleted     bool   `json:"isDeleted"`
	Encoding      string `json:"encoding,omitempty"`
}

type OutgoingMessage struct {
	Type      string              `json:"type"`
	Vault     string              `json:"vault,omitempty"`
	Path      string              `json:"path,omitempty"`
	Action    string              `json:"action,omitempty"`
	Ok        *bool               `json:"ok,omitempty"`
	Content   string              `json:"content,omitempty"`
	Encoding  string              `json:"encoding,omitempty"`
	Meta      *ServerMetaPayload  `json:"meta,omitempty"`
	Conflict  *ConflictInfo       `json:"conflict,omitempty"`
	ToDownload []DownloadEntry    `json:"toDownload,omitempty"`
	ToUpdateMeta []ServerMetaPayload `json:"toUpdateMeta,omitempty"`
	Conflicts []SyncConflictEntry `json:"conflicts,omitempty"`
	Error     string              `json:"error,omitempty"`
}
```

- [ ] **Step 4: Keep backward-compatible Go helpers only inside tests**

Remove production fields `PrevServerVersion`, `PrevServerHash`, `CurrentClientHash`, `ToUpload`, `ToUpdate`, and `ToDelete`. Update tests to use `FilePayload` and response `Action` where needed.

- [ ] **Step 5: Run message test**

Run:

```bash
cd server && rtk go test ./internal/ws -run TestIncomingMessageUsesCamelCaseProtocol
```

Expected: PASS.

- [ ] **Step 6: Commit protocol structs**

```bash
rtk git add server/internal/ws/messages.go server/internal/ws/handler_test.go
rtk git commit -m "feat: define sync matrix websocket protocol"
```

---

### Task 4: Refactor Server Handlers to Matrix Decisions

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `server/internal/ws/handler_test.go`
- Modify: `server/internal/db/file.go`
- Test: `server/internal/ws/handler_test.go`

- [ ] **Step 1: Add `filePut` handler tests**

In `server/internal/ws/handler_test.go`, add:

```go
func TestHandleFilePut_NewFile_OKUpdateMeta(t *testing.T) {
	h, q, _, _ := setupHandler(t)
	q.CreateVault("personal")
	c := makeClient(h.hub, "personal")

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/new.md",
		Content: "content",
		File: &FilePayload{
			Path:      "notes/new.md",
			Exists:    true,
			LocalHash: "hash-new",
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "filePutResult" || resp.Action != "okUpdateMeta" {
		t.Fatalf("expected filePutResult okUpdateMeta, got %#v", resp)
	}
	if resp.Meta == nil || resp.Meta.ServerVersion != 1 || resp.Meta.ServerHash != "hash-new" {
		t.Fatalf("bad meta: %#v", resp.Meta)
	}
}
```

- [ ] **Step 2: Add `fileDelete` idempotency test**

Add:

```go
func TestHandleFileDelete_TombstoneRetry_OKUpdateMeta(t *testing.T) {
	h, q, _, _ := setupHandler(t)
	q.CreateVault("personal")
	q.CreateFile("personal", "notes/a.md", "hash-a")
	deleted, err := q.DeleteFile("personal", "notes/a.md")
	if err != nil {
		t.Fatalf("delete setup: %v", err)
	}
	base := deleted.Version - 1
	c := makeClient(h.hub, "personal")

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "fileDelete",
		Vault: "personal",
		Path:  "notes/a.md",
		File: &FilePayload{
			Path:        "notes/a.md",
			Exists:      false,
			BaseVersion: &base,
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "fileDeleteResult" || resp.Action != "okUpdateMeta" {
		t.Fatalf("expected idempotent okUpdateMeta, got %#v", resp)
	}
	if resp.Meta == nil || !resp.Meta.IsDeleted || resp.Meta.ServerVersion != deleted.Version {
		t.Fatalf("bad tombstone meta: %#v", resp.Meta)
	}
}
```

- [ ] **Step 3: Run new handler tests and confirm failure**

Run:

```bash
cd server && rtk go test ./internal/ws -run 'TestHandleFile(Put|Delete)'
```

Expected: FAIL because `filePut` route and new response shape are not implemented.

- [ ] **Step 4: Add DB helper for server state**

In `server/internal/db/file.go`, add:

```go
func (f File) DeletedFromVersion() int64 {
	if !f.IsDeleted {
		return 0
	}
	return f.Version - 1
}
```

- [ ] **Step 5: Update message routing**

In `server/internal/ws/handler.go`, change `HandleMessage` cases:

```go
switch msg.Type {
case "vaultCreate":
	h.handleVaultCreate(client, msg)
case "syncInit":
	h.handleSyncInit(client, msg)
case "fileCheck":
	h.handleFileCheck(client, msg)
case "filePut":
	h.handleFilePut(client, msg)
case "fileDelete":
	h.handleFileDelete(client, msg)
case "conflictResolve":
	h.handleConflictResolve(client, msg)
default:
	log.Printf("unknown message type: %s", msg.Type)
}
```

- [ ] **Step 6: Implement `decisionInputForPath` helper**

Add to `server/internal/ws/handler.go`:

```go
func (h *Handler) decisionInputForPath(msg IncomingMessage, payload FilePayload, message syncpkg.MatrixMessage) (syncpkg.DecisionInput, db.File, bool, error) {
	sf, err := h.queries.GetFile(msg.Vault, payload.Path)
	if err != nil && err != sql.ErrNoRows {
		return syncpkg.DecisionInput{}, db.File{}, false, err
	}

	state := syncpkg.ServerMissing
	serverExists := false
	if err == nil {
		serverExists = true
		if sf.IsDeleted {
			state = syncpkg.ServerTombstone
		} else {
			state = syncpkg.ServerActive
		}
	}

	deletedFrom := int64(0)
	if serverExists && sf.IsDeleted {
		deletedFrom = sf.DeletedFromVersion()
	}

	return syncpkg.DecisionInput{
		Message:            message,
		ClientExists:       payload.Exists,
		BaseVersion:        payload.BaseVersion,
		LocalHash:          payload.LocalHash,
		ServerState:        state,
		ServerVersion:      sf.Version,
		ServerHash:         sf.Hash,
		DeletedFromVersion: deletedFrom,
	}, sf, serverExists, nil
}
```

- [ ] **Step 7: Implement `handleFilePut`**

Replace old `handleFileCreate` and `handleFileUpdate` with:

```go
func (h *Handler) handleFilePut(client *Client, msg IncomingMessage) {
	if msg.File == nil {
		client.SendMessage(OutgoingMessage{Type: "filePutResult", Path: msg.Path, Action: string(syncpkg.ActionConflict), Error: "missing file payload"})
		return
	}
	input, sf, serverExists, err := h.decisionInputForPath(msg, *msg.File, syncpkg.MessageFilePut)
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: "filePutResult", Path: msg.Path, Error: err.Error()})
		return
	}

	result := syncpkg.DecideFilePut(input)
	switch result.Action {
	case syncpkg.ActionOkUpdateMeta:
		fileContent := decodeContent(msg.Content, msg.Encoding)
		if err := h.storage.WriteFile(msg.Vault, msg.File.Path, fileContent); err != nil {
			client.SendMessage(OutgoingMessage{Type: "filePutResult", Path: msg.File.Path, Error: err.Error()})
			return
		}

		var newFile db.File
		if !serverExists {
			newFile, err = h.queries.CreateFile(msg.Vault, msg.File.Path, msg.File.LocalHash)
		} else if sf.IsDeleted {
			newFile, err = h.queries.CreateFileFromTombstone(msg.Vault, msg.File.Path, msg.File.LocalHash, sf.Version)
		} else if msg.File.BaseVersion != nil && *msg.File.BaseVersion == sf.Version && msg.File.LocalHash != sf.Hash {
			newFile, err = h.queries.UpdateFile(msg.Vault, msg.File.Path, msg.File.LocalHash)
		} else {
			newFile = sf
		}
		if err != nil {
			client.SendMessage(OutgoingMessage{Type: "filePutResult", Path: msg.File.Path, Error: err.Error()})
			return
		}
		client.SendMessage(OutgoingMessage{Type: "filePutResult", Path: msg.File.Path, Action: "okUpdateMeta", Meta: serverMeta(newFile)})
	case syncpkg.ActionToDeleteLocal:
		client.SendMessage(OutgoingMessage{Type: "filePutResult", Path: msg.File.Path, Action: "toDeleteLocal", Meta: serverMeta(sf)})
	case syncpkg.ActionConflict, syncpkg.ActionDeleteConflict:
		h.sendConflictResult(client, "filePutResult", msg.File.Path, result.Action, sf)
	default:
		client.SendMessage(OutgoingMessage{Type: "filePutResult", Path: msg.File.Path, Action: string(result.Action)})
	}
}
```

- [ ] **Step 8: Implement `serverMeta` and conflict helper**

Add:

```go
func serverMeta(f db.File) *ServerMetaPayload {
	return &ServerMetaPayload{
		Path:          f.Path,
		ServerVersion: f.Version,
		ServerHash:    f.Hash,
		IsDeleted:     f.IsDeleted,
	}
}

func (h *Handler) sendConflictResult(client *Client, typ, path string, action syncpkg.MatrixAction, sf db.File) {
	content, err := h.storage.ReadFile(client.vault, path)
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: typ, Path: path, Action: string(action), Error: err.Error()})
		return
	}
	enc, encoded := encodeContent(content)
	client.SendMessage(OutgoingMessage{
		Type:   typ,
		Path:   path,
		Action: string(action),
		Conflict: &ConflictInfo{
			ServerVersion: sf.Version,
			ServerHash:    sf.Hash,
			ServerContent: encoded,
			IsDeleted:     sf.IsDeleted,
			Encoding:      enc,
		},
	})
}
```

- [ ] **Step 9: Implement `handleFileDelete` with matrix action**

Update `handleFileDelete` to use `DecisionInput` and return `fileDeleteResult` actions `okUpdateMeta`, `okRemoveMeta`, or `deleteConflict`.

- [ ] **Step 10: Run server ws tests**

Run:

```bash
cd server && rtk go test ./internal/ws
```

Expected: PASS after updating old assertions to new protocol names.

- [ ] **Step 11: Commit server handler refactor**

```bash
rtk git add server/internal/db/file.go server/internal/ws/handler.go server/internal/ws/handler_test.go
rtk git commit -m "feat: apply sync matrix websocket handlers"
```

---

### Task 5: Add Client Queue Primitives

**Files:**
- Create: `plugin/src/async-mutex.ts`
- Create: `plugin/src/dirty-queue.ts`
- Create: `plugin/src/__tests__/dirty-queue.test.ts`
- Test: `plugin/src/__tests__/dirty-queue.test.ts`

- [ ] **Step 1: Add dirty queue tests**

Create `plugin/src/__tests__/dirty-queue.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { DirtyQueue } from "../dirty-queue";

describe("DirtyQueue", () => {
  it("coalesces same-path pending changes", async () => {
    const q = new DirtyQueue();
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H1" });
    await q.enqueue({ path: "a.md", baseVersion: 11, lastSeenHash: "H2" });
    expect(q.list()).toEqual([
      { path: "a.md", baseVersion: 10, lastSeenHash: "H2", status: "pending", queuedAt: expect.any(Number) },
    ]);
  });

  it("keeps inFlight status and updates only latest hash", async () => {
    const q = new DirtyQueue();
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H1" });
    const claim = await q.claimNext();
    expect(claim?.lastSeenHash).toBe("H1");
    await q.markSentHash("a.md", "H1", "H1");
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H2" });
    expect(q.get("a.md")).toMatchObject({ status: "inFlight", sentHash: "H1", baseVersion: 10, lastSeenHash: "H2" });
  });

  it("removes entry when sent hash is still latest", async () => {
    const q = new DirtyQueue();
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H1" });
    await q.claimNext();
    await q.markSentHash("a.md", "H1", "H1");
    await q.completeSuccess("a.md", "H1", { serverVersion: 11, serverHash: "H1" });
    expect(q.get("a.md")).toBeUndefined();
  });

  it("rebases entry when in-flight update changed latest hash", async () => {
    const q = new DirtyQueue();
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H1" });
    await q.claimNext();
    await q.markSentHash("a.md", "H1", "H1");
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H2" });
    await q.completeSuccess("a.md", "H1", { serverVersion: 11, serverHash: "H1" });
    expect(q.get("a.md")).toMatchObject({ baseVersion: 11, lastSeenHash: "H2", status: "pending" });
  });
});
```

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
cd plugin && rtk npm test -- src/__tests__/dirty-queue.test.ts
```

Expected: FAIL because `dirty-queue.ts` does not exist.

- [ ] **Step 3: Add async mutex**

Create `plugin/src/async-mutex.ts`:

```ts
export class AsyncMutex {
  private current: Promise<void> = Promise.resolve();

  async runExclusive<T>(fn: () => T | Promise<T>): Promise<T> {
    let release!: () => void;
    const next = new Promise<void>((resolve) => {
      release = resolve;
    });
    const previous = this.current;
    this.current = previous.then(() => next);
    await previous;
    try {
      return await fn();
    } finally {
      release();
    }
  }
}
```

- [ ] **Step 4: Add dirty queue implementation**

Create `plugin/src/dirty-queue.ts`:

```ts
import { AsyncMutex } from "./async-mutex";

export type DirtyStatus = "pending" | "inFlight" | "retryableFailed";

export interface DirtyEntry {
  path: string;
  baseVersion?: number;
  queuedAt: number;
  lastSeenHash: string;
  status: DirtyStatus;
  sentHash?: string;
}

export interface DirtySnapshot {
  path: string;
  baseVersion?: number;
  lastSeenHash: string;
}

export interface ServerMeta {
  serverVersion: number;
  serverHash: string;
}

export class DirtyQueue {
  private entries = new Map<string, DirtyEntry>();
  private mutex = new AsyncMutex();

  async enqueue(input: { path: string; baseVersion?: number; lastSeenHash: string }): Promise<void> {
    await this.mutex.runExclusive(() => {
      const existing = this.entries.get(input.path);
      if (existing) {
        existing.lastSeenHash = input.lastSeenHash;
        existing.queuedAt = Date.now();
        return;
      }
      this.entries.set(input.path, {
        path: input.path,
        baseVersion: input.baseVersion,
        queuedAt: Date.now(),
        lastSeenHash: input.lastSeenHash,
        status: "pending",
      });
    });
  }

  async claimNext(): Promise<DirtySnapshot | null> {
    return await this.mutex.runExclusive(() => {
      for (const entry of this.entries.values()) {
        if (entry.status === "pending" || entry.status === "retryableFailed") {
          entry.status = "inFlight";
          return { path: entry.path, baseVersion: entry.baseVersion, lastSeenHash: entry.lastSeenHash };
        }
      }
      return null;
    });
  }

  async markSentHash(path: string, claimHash: string, sentHash: string): Promise<void> {
    await this.mutex.runExclusive(() => {
      const entry = this.entries.get(path);
      if (!entry) return;
      entry.sentHash = sentHash;
      if (entry.lastSeenHash === claimHash) {
        entry.lastSeenHash = sentHash;
      }
    });
  }

  async completeSuccess(path: string, sentHash: string, meta: ServerMeta): Promise<void> {
    await this.mutex.runExclusive(() => {
      const entry = this.entries.get(path);
      if (!entry) return;
      if (entry.lastSeenHash === sentHash) {
        this.entries.delete(path);
        return;
      }
      entry.baseVersion = meta.serverVersion;
      entry.status = "pending";
      entry.sentHash = undefined;
    });
  }

  async completeRetryableFailure(path: string): Promise<void> {
    await this.mutex.runExclusive(() => {
      const entry = this.entries.get(path);
      if (!entry) return;
      entry.status = "retryableFailed";
      entry.sentHash = undefined;
    });
  }

  async remove(path: string): Promise<void> {
    await this.mutex.runExclusive(() => {
      this.entries.delete(path);
    });
  }

  get(path: string): DirtyEntry | undefined {
    const entry = this.entries.get(path);
    return entry ? { ...entry } : undefined;
  }

  list(): DirtyEntry[] {
    return Array.from(this.entries.values()).map((entry) => ({ ...entry }));
  }
}
```

- [ ] **Step 5: Run dirty queue tests**

Run:

```bash
cd plugin && rtk npm test -- src/__tests__/dirty-queue.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit dirty queue**

```bash
rtk git add plugin/src/async-mutex.ts plugin/src/dirty-queue.ts plugin/src/__tests__/dirty-queue.test.ts
rtk git commit -m "feat: add dirty queue coalescing"
```

---

### Task 6: Add DeleteQueue, BlockedPaths, and SelfWriteSuppress

**Files:**
- Create: `plugin/src/delete-queue.ts`
- Create: `plugin/src/blocked-paths.ts`
- Create: `plugin/src/self-write-suppress.ts`
- Create: `plugin/src/__tests__/delete-queue.test.ts`
- Create: `plugin/src/__tests__/blocked-paths.test.ts`
- Create: `plugin/src/__tests__/self-write-suppress.test.ts`

- [ ] **Step 1: Write DeleteQueue tests**

Create `plugin/src/__tests__/delete-queue.test.ts` with an in-memory adapter:

```ts
import { describe, expect, it } from "vitest";
import { DeleteQueue, QueueFileAdapter } from "../delete-queue";

class MemoryAdapter implements QueueFileAdapter {
  files = new Map<string, string>();
  async read(path: string): Promise<string> {
    const value = this.files.get(path);
    if (value === undefined) throw new Error("missing");
    return value;
  }
  async write(path: string, data: string): Promise<void> {
    this.files.set(path, data);
  }
  async exists(path: string): Promise<boolean> {
    return this.files.has(path);
  }
  async remove(path: string): Promise<void> {
    this.files.delete(path);
  }
  async rename(from: string, to: string): Promise<void> {
    const value = this.files.get(from);
    if (value === undefined) throw new Error("missing temp");
    this.files.set(to, value);
    this.files.delete(from);
  }
}

describe("DeleteQueue", () => {
  it("dedupes by path and preserves original baseVersion", async () => {
    const adapter = new MemoryAdapter();
    const q = new DeleteQueue(adapter, "delete-queue.json");
    await q.load();
    await q.enqueue({ path: "a.md", baseVersion: 10, serverHash: "H10" });
    await q.enqueue({ path: "a.md", baseVersion: 11, serverHash: "H11" });
    expect(q.list()).toMatchObject([{ path: "a.md", baseVersion: 10, serverHash: "H10", status: "pending" }]);
  });

  it("persists with temp rename", async () => {
    const adapter = new MemoryAdapter();
    const q = new DeleteQueue(adapter, "delete-queue.json");
    await q.load();
    await q.enqueue({ path: "a.md", baseVersion: 10, serverHash: "H10" });
    expect(adapter.files.has("delete-queue.tmp")).toBe(false);
    expect(JSON.parse(adapter.files.get("delete-queue.json")!)).toHaveLength(1);
  });
});
```

- [ ] **Step 2: Implement `DeleteQueue`**

Create `plugin/src/delete-queue.ts`:

```ts
import { AsyncMutex } from "./async-mutex";

export interface QueueFileAdapter {
  read(path: string): Promise<string>;
  write(path: string, data: string): Promise<void>;
  exists(path: string): Promise<boolean>;
  remove(path: string): Promise<void>;
  rename(from: string, to: string): Promise<void>;
}

export interface DeleteEntry {
  path: string;
  baseVersion: number;
  serverHash: string;
  queuedAt: number;
  status: "pending" | "retryableFailed";
}

export class DeleteQueue {
  private entries = new Map<string, DeleteEntry>();
  private mutex = new AsyncMutex();
  private tempPath: string;

  constructor(private adapter: QueueFileAdapter, private queuePath: string) {
    this.tempPath = queuePath.replace(/\.json$/, ".tmp");
  }

  async load(): Promise<void> {
    await this.mutex.runExclusive(async () => {
      if (await this.adapter.exists(this.tempPath)) {
        await this.adapter.remove(this.tempPath);
      }
      if (!(await this.adapter.exists(this.queuePath))) return;
      const raw = await this.adapter.read(this.queuePath);
      const parsed = JSON.parse(raw) as DeleteEntry[];
      this.entries = new Map(parsed.map((entry) => [entry.path, entry]));
    });
  }

  async enqueue(input: { path: string; baseVersion: number; serverHash: string }): Promise<void> {
    await this.mutex.runExclusive(async () => {
      const existing = this.entries.get(input.path);
      if (existing) {
        existing.queuedAt = Date.now();
      } else {
        this.entries.set(input.path, { ...input, queuedAt: Date.now(), status: "pending" });
      }
      await this.saveLocked();
    });
  }

  async remove(path: string): Promise<void> {
    await this.mutex.runExclusive(async () => {
      this.entries.delete(path);
      await this.saveLocked();
    });
  }

  list(): DeleteEntry[] {
    return Array.from(this.entries.values()).map((entry) => ({ ...entry }));
  }

  private async saveLocked(): Promise<void> {
    const data = JSON.stringify(this.list(), null, 2);
    await this.adapter.write(this.tempPath, data);
    await this.adapter.rename(this.tempPath, this.queuePath);
  }
}
```

- [ ] **Step 3: Add BlockedPaths tests and implementation**

Create `plugin/src/__tests__/blocked-paths.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { BlockedPaths } from "../blocked-paths";

describe("BlockedPaths", () => {
  it("registers, checks, and clears blocked paths", () => {
    const blocked = new BlockedPaths();
    blocked.block({ path: "a.md", reason: "conflict", serverVersion: 5, serverHash: "H5", isDeleted: false });
    expect(blocked.has("a.md")).toBe(true);
    blocked.clear("a.md");
    expect(blocked.has("a.md")).toBe(false);
  });
});
```

Create `plugin/src/blocked-paths.ts`:

```ts
export type BlockReason = "conflict" | "deleteConflict";

export interface BlockedPath {
  path: string;
  reason: BlockReason;
  serverVersion: number;
  serverHash: string;
  isDeleted: boolean;
  createdAt: number;
}

export class BlockedPaths {
  private entries = new Map<string, BlockedPath>();

  block(input: Omit<BlockedPath, "createdAt">): void {
    this.entries.set(input.path, { ...input, createdAt: Date.now() });
  }

  clear(path: string): void {
    this.entries.delete(path);
  }

  has(path: string): boolean {
    return this.entries.has(path);
  }

  list(): BlockedPath[] {
    return Array.from(this.entries.values()).map((entry) => ({ ...entry }));
  }
}
```

- [ ] **Step 4: Add SelfWriteSuppress tests and implementation**

Create `plugin/src/__tests__/self-write-suppress.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { SelfWriteSuppress } from "../self-write-suppress";

describe("SelfWriteSuppress", () => {
  it("consumes matching write suppress", () => {
    const suppress = new SelfWriteSuppress(() => 1000);
    suppress.addWrite("a.md", "H1", 2000);
    expect(suppress.consumeWrite("a.md", "H1")).toBe(true);
    expect(suppress.consumeWrite("a.md", "H1")).toBe(false);
  });

  it("does not consume write suppress with different hash", () => {
    const suppress = new SelfWriteSuppress(() => 1000);
    suppress.addWrite("a.md", "H1", 2000);
    expect(suppress.consumeWrite("a.md", "H2")).toBe(false);
  });

  it("consumes matching delete suppress", () => {
    const suppress = new SelfWriteSuppress(() => 1000);
    suppress.addDelete("a.md", 2000);
    expect(suppress.consumeDelete("a.md", false)).toBe(true);
  });
});
```

Create `plugin/src/self-write-suppress.ts`:

```ts
type SuppressEntry =
  | { path: string; operation: "write"; expectedHash: string; until: number }
  | { path: string; operation: "delete"; until: number };

export class SelfWriteSuppress {
  private entries = new Map<string, SuppressEntry>();

  constructor(private now: () => number = () => Date.now()) {}

  addWrite(path: string, expectedHash: string, until: number): void {
    this.entries.set(path, { path, operation: "write", expectedHash, until });
  }

  addDelete(path: string, until: number): void {
    this.entries.set(path, { path, operation: "delete", until });
  }

  consumeWrite(path: string, actualHash: string): boolean {
    const entry = this.entries.get(path);
    if (!entry || entry.until < this.now() || entry.operation !== "write") return false;
    if (entry.expectedHash !== actualHash) return false;
    this.entries.delete(path);
    return true;
  }

  consumeDelete(path: string, exists: boolean): boolean {
    const entry = this.entries.get(path);
    if (!entry || entry.until < this.now() || entry.operation !== "delete") return false;
    if (exists) return false;
    this.entries.delete(path);
    return true;
  }
}
```

- [ ] **Step 5: Run queue support tests**

Run:

```bash
cd plugin && rtk npm test -- src/__tests__/delete-queue.test.ts src/__tests__/blocked-paths.test.ts src/__tests__/self-write-suppress.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit queue support modules**

```bash
rtk git add plugin/src/delete-queue.ts plugin/src/blocked-paths.ts plugin/src/self-write-suppress.ts plugin/src/__tests__/delete-queue.test.ts plugin/src/__tests__/blocked-paths.test.ts plugin/src/__tests__/self-write-suppress.test.ts
rtk git commit -m "feat: add sync queue support state"
```

---

### Task 7: Update Client WebSocket Protocol

**Files:**
- Modify: `plugin/src/ws-client.ts`
- Create: `plugin/src/__tests__/ws-client-protocol.test.ts`

- [ ] **Step 1: Add protocol serialization tests**

Create `plugin/src/__tests__/ws-client-protocol.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { buildFilePutMessage, buildSyncInitMessage } from "../ws-client";

describe("ws protocol builders", () => {
  it("builds syncInit with baseVersion and localHash", () => {
    expect(buildSyncInitMessage("personal", [{ path: "a.md", exists: true, baseVersion: 3, baseHash: "H3", localHash: "H4" }])).toEqual({
      type: "syncInit",
      vault: "personal",
      files: [{ path: "a.md", exists: true, baseVersion: 3, baseHash: "H3", localHash: "H4" }],
    });
  });

  it("builds filePut", () => {
    expect(buildFilePutMessage("personal", "a.md", "body", { path: "a.md", exists: true, baseVersion: 3, localHash: "H4" })).toMatchObject({
      type: "filePut",
      vault: "personal",
      path: "a.md",
      content: "body",
      file: { path: "a.md", exists: true, baseVersion: 3, localHash: "H4" },
    });
  });
});
```

- [ ] **Step 2: Run test and confirm failure**

Run:

```bash
cd plugin && rtk npm test -- src/__tests__/ws-client-protocol.test.ts
```

Expected: FAIL because builders do not exist.

- [ ] **Step 3: Add protocol types and builders**

In `plugin/src/ws-client.ts`, add exported types and builders:

```ts
export interface FilePayload {
  path: string;
  exists: boolean;
  baseVersion?: number;
  baseHash?: string;
  localHash?: string;
}

export interface ServerMetaPayload {
  path?: string;
  serverVersion: number;
  serverHash?: string;
  isDeleted: boolean;
}

export type ServerAction =
  | "toPut" | "toUpdateMeta" | "toDownload" | "toDeleteLocal" | "toRemoveMeta"
  | "none" | "conflict" | "deleteConflict"
  | "put" | "updateMeta" | "upToDate"
  | "okUpdateMeta" | "okRemoveMeta";

export function buildSyncInitMessage(vault: string, files: FilePayload[]) {
  return { type: "syncInit", vault, files };
}

export function buildFilePutMessage(vault: string, path: string, content: string, file: FilePayload, encoding?: string) {
  const msg: Record<string, unknown> = { type: "filePut", vault, path, content, file };
  if (encoding) msg.encoding = encoding;
  return msg;
}
```

- [ ] **Step 4: Replace send methods**

Update `WsClient` methods:

```ts
sendSyncInit(vault: string, files: FilePayload[]) {
  this.send(buildSyncInitMessage(vault, files));
}

sendFileCheck(vault: string, file: FilePayload) {
  this.send({ type: "fileCheck", vault, path: file.path, file });
}

sendFilePut(vault: string, path: string, content: string, file: FilePayload, encoding?: string) {
  this.send(buildFilePutMessage(vault, path, content, file, encoding));
}

sendFileDelete(vault: string, file: FilePayload) {
  this.send({ type: "fileDelete", vault, path: file.path, file });
}
```

Remove `sendFileCreate` and `sendFileUpdate`.

- [ ] **Step 5: Run protocol tests**

Run:

```bash
cd plugin && rtk npm test -- src/__tests__/ws-client-protocol.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit ws protocol update**

```bash
rtk git add plugin/src/ws-client.ts plugin/src/__tests__/ws-client-protocol.test.ts
rtk git commit -m "feat: update plugin websocket protocol"
```

---

### Task 8: Wire SyncManager to Queues and Orchestrator

**Files:**
- Create: `plugin/src/sync-orchestrator.ts`
- Create: `plugin/src/__tests__/sync-orchestrator.test.ts`
- Modify: `plugin/src/sync.ts`
- Modify: `plugin/src/main.ts`
- Modify: `plugin/src/file-watcher.ts`

- [ ] **Step 1: Add orchestrator ordering test**

Create `plugin/src/__tests__/sync-orchestrator.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { SyncOrchestrator } from "../sync-orchestrator";

describe("SyncOrchestrator", () => {
  it("runs delete, dirty, then syncInit under one mutex", async () => {
    const calls: string[] = [];
    const orchestrator = new SyncOrchestrator({
      flushDeleteQueue: async () => { calls.push("delete"); return "ok"; },
      flushDirtyQueue: async () => { calls.push("dirty"); return "ok"; },
      runSyncInit: async () => { calls.push("syncInit"); },
      notifyTransientFailure: () => calls.push("notice"),
    });

    await orchestrator.runStartupSync();
    expect(calls).toEqual(["delete", "dirty", "syncInit"]);
  });

  it("skips syncInit after transient failure", async () => {
    const calls: string[] = [];
    const orchestrator = new SyncOrchestrator({
      flushDeleteQueue: async () => { calls.push("delete"); return "transientFailure"; },
      flushDirtyQueue: async () => { calls.push("dirty"); return "ok"; },
      runSyncInit: async () => { calls.push("syncInit"); },
      notifyTransientFailure: () => calls.push("notice"),
    });

    await orchestrator.runStartupSync();
    expect(calls).toEqual(["delete", "notice"]);
  });
});
```

- [ ] **Step 2: Implement orchestrator**

Create `plugin/src/sync-orchestrator.ts`:

```ts
import { AsyncMutex } from "./async-mutex";

export type FlushResult = "ok" | "transientFailure";

export interface SyncOrchestratorHandlers {
  flushDeleteQueue(): Promise<FlushResult>;
  flushDirtyQueue(): Promise<FlushResult>;
  runSyncInit(): Promise<void>;
  notifyTransientFailure(): void;
}

export class SyncOrchestrator {
  private mutex = new AsyncMutex();
  private running = false;

  constructor(private handlers: SyncOrchestratorHandlers) {}

  async runStartupSync(): Promise<void> {
    await this.mutex.runExclusive(async () => {
      const deleteResult = await this.handlers.flushDeleteQueue();
      if (deleteResult === "transientFailure") {
        this.handlers.notifyTransientFailure();
        return;
      }
      const dirtyResult = await this.handlers.flushDirtyQueue();
      if (dirtyResult === "transientFailure") {
        this.handlers.notifyTransientFailure();
        return;
      }
      await this.handlers.runSyncInit();
    });
  }

  async runIntervalWorker(): Promise<void> {
    if (this.running) return;
    this.running = true;
    try {
      await this.runStartupSync();
    } finally {
      this.running = false;
    }
  }
}
```

- [ ] **Step 3: Update `main.ts` to pass queue path**

In `plugin/src/main.ts`, build a queue path and pass it to `SyncManager`:

```ts
const pluginDir = this.manifest.dir || ".obsidian/plugins/obsidian-goat-sync";
const deleteQueuePath = `${pluginDir}/delete-queue.json`;

this.syncManager = new SyncManager(
  this.app,
  this.app.vault,
  serverUrl,
  token,
  vaultName,
  this.fileMetaStore,
  deleteQueuePath,
);
```

- [ ] **Step 4: Update `SyncManager` constructor**

In `plugin/src/sync.ts`, add constructor parameter:

```ts
deleteQueuePath: string,
```

Initialize:

```ts
this.deleteQueue = new DeleteQueue(this.vault.adapter, deleteQueuePath);
this.dirtyQueue = new DirtyQueue();
this.blockedPaths = new BlockedPaths();
this.selfWriteSuppress = new SelfWriteSuppress();
this.orchestrator = new SyncOrchestrator({
  flushDeleteQueue: () => this.flushDeleteQueue(),
  flushDirtyQueue: () => this.flushDirtyQueue(),
  runSyncInit: () => this.performSyncInit(),
  notifyTransientFailure: () => new Notice("[obsidian-goat-sync] 서버 연결이 불안정해서 동기화가 중지됩니다"),
});
```

- [ ] **Step 5: Change watcher handler to enqueue**

Replace direct network sends in `handleLocalChange` with:

```ts
private async handleLocalChange(change: { type: "create" | "modify" | "delete"; path: string }) {
  this.blockedPaths.clear(change.path);

  if (change.type === "delete") {
    const exists = await this.vault.adapter.exists(change.path);
    if (this.selfWriteSuppress.consumeDelete(change.path, exists)) return;
    const meta = this.fileMeta.get(change.path);
    if (meta) {
      await this.deleteQueue.enqueue({ path: change.path, baseVersion: meta.prevServerVersion, serverHash: meta.prevServerHash });
      await this.dirtyQueue.remove(change.path);
    }
    return;
  }

  const hash = await this.computeFileHash(change.path);
  if (hash === null) return;
  if (this.selfWriteSuppress.consumeWrite(change.path, hash)) return;
  const meta = this.fileMeta.get(change.path);
  await this.dirtyQueue.enqueue({ path: change.path, baseVersion: meta?.prevServerVersion, lastSeenHash: hash });
}
```

- [ ] **Step 6: Modify syncInit file list to include missing meta entries**

In `performSyncInit`, include both actual files and metadata-only missing files:

```ts
const localFiles = await this.fileWatcher.getAllFiles();
const localPaths = new Set(localFiles.map((file) => file.path));
const files: FilePayload[] = [];

for (const { path } of localFiles) {
  if (this.blockedPaths.has(path)) continue;
  const localHash = await this.computeFileHash(path);
  if (localHash === null) continue;
  const meta = this.fileMeta.get(path);
  files.push({ path, exists: true, baseVersion: meta?.prevServerVersion, baseHash: meta?.prevServerHash, localHash });
}

for (const [path, meta] of this.fileMeta.entries()) {
  if (localPaths.has(path) || this.blockedPaths.has(path)) continue;
  files.push({ path, exists: false, baseVersion: meta.prevServerVersion, baseHash: meta.prevServerHash });
}

this.wsClient.sendSyncInit(this.vaultName, files);
```

- [ ] **Step 7: Run orchestrator tests**

Run:

```bash
cd plugin && rtk npm test -- src/__tests__/sync-orchestrator.test.ts
```

Expected: PASS.

- [ ] **Step 8: Commit orchestrator wiring**

```bash
rtk git add plugin/src/sync-orchestrator.ts plugin/src/__tests__/sync-orchestrator.test.ts plugin/src/sync.ts plugin/src/main.ts plugin/src/file-watcher.ts
rtk git commit -m "feat: route watcher changes through sync queues"
```

---

### Task 9: Complete Client Result Handling

**Files:**
- Modify: `plugin/src/sync.ts`
- Modify: `plugin/src/conflict-queue.ts`
- Modify: `plugin/src/conflict-modal.ts` only if compile errors require property renames.

- [ ] **Step 1: Update response handlers to new action names**

In `plugin/src/sync.ts`, replace old result handlers with action-based handling:

```ts
private async handleFileCheckResult(msg: ServerMessage) {
  if (!msg.path || !msg.action) return;
  switch (msg.action) {
    case "upToDate":
    case "updateMeta":
      if (msg.meta?.serverVersion !== undefined && msg.meta.serverHash) {
        this.fileMeta.set(msg.path, { prevServerVersion: msg.meta.serverVersion, prevServerHash: msg.meta.serverHash });
      }
      break;
    case "put":
      await this.putDirtyFile(msg.path);
      break;
    case "toDeleteLocal":
      if (msg.meta) {
        await this.applyServerDelete(msg.path, msg.meta.serverVersion, msg.meta.serverHash || "");
      }
      break;
    case "conflict":
    case "deleteConflict":
      await this.enqueueLatestConflict(msg);
      break;
  }
}
```

- [ ] **Step 2: Add dirty success completion in `filePutResult`**

Add:

```ts
private async handleFilePutResult(msg: ServerMessage) {
  if (!msg.path || !msg.action) return;
  if (msg.action === "okUpdateMeta" && msg.meta?.serverVersion !== undefined && msg.meta.serverHash) {
    this.fileMeta.set(msg.path, { prevServerVersion: msg.meta.serverVersion, prevServerHash: msg.meta.serverHash });
    const entry = this.dirtyQueue.get(msg.path);
    if (entry?.sentHash) {
      await this.dirtyQueue.completeSuccess(msg.path, entry.sentHash, { serverVersion: msg.meta.serverVersion, serverHash: msg.meta.serverHash });
    }
    return;
  }
  if (msg.action === "toDeleteLocal" && msg.meta) {
    await this.applyServerDelete(msg.path, msg.meta.serverVersion, msg.meta.serverHash || "");
    await this.dirtyQueue.remove(msg.path);
    return;
  }
  if (msg.action === "conflict" || msg.action === "deleteConflict") {
    await this.dirtyQueue.remove(msg.path);
    await this.enqueueLatestConflict(msg);
  }
}
```

- [ ] **Step 3: Add delete result handling**

Add:

```ts
private async handleFileDeleteResult(msg: ServerMessage) {
  if (!msg.path || !msg.action) return;
  if (msg.action === "okUpdateMeta" && msg.meta?.serverVersion !== undefined) {
    this.fileMeta.set(msg.path, { prevServerVersion: msg.meta.serverVersion, prevServerHash: msg.meta.serverHash || "" });
    await this.deleteQueue.remove(msg.path);
    return;
  }
  if (msg.action === "okRemoveMeta") {
    this.fileMeta.remove(msg.path);
    await this.deleteQueue.remove(msg.path);
    return;
  }
  if (msg.action === "deleteConflict") {
    await this.deleteQueue.remove(msg.path);
    this.blockedPaths.block({ path: msg.path, reason: "deleteConflict", serverVersion: msg.meta?.serverVersion || 0, serverHash: msg.meta?.serverHash || "", isDeleted: !!msg.meta?.isDeleted });
    await this.enqueueLatestConflict(msg);
  }
}
```

- [ ] **Step 4: Register new event names**

In `start()`, register:

```ts
this.wsClient.on("syncResult", (msg) => this.handleSyncResult(msg));
this.wsClient.on("fileCheckResult", (msg) => this.handleFileCheckResult(msg));
this.wsClient.on("filePutResult", (msg) => this.handleFilePutResult(msg));
this.wsClient.on("fileDeleteResult", (msg) => this.handleFileDeleteResult(msg));
this.wsClient.on("conflictResolveResult", (msg) => this.handleConflictResolveResult(msg));
```

Remove old `file_create_result` and `file_update_result` registrations.

- [ ] **Step 5: Run plugin tests**

Run:

```bash
cd plugin && rtk npm test
```

Expected: PASS.

- [ ] **Step 6: Commit result handling**

```bash
rtk git add plugin/src/sync.ts plugin/src/conflict-queue.ts plugin/src/conflict-modal.ts
rtk git commit -m "feat: handle sync matrix client responses"
```

---

### Task 10: Remove Legacy Protocol and Run Full Verification

**Files:**
- Modify: `server/internal/ws/handler.go`
- Modify: `plugin/src/sync.ts`
- Modify: `plugin/src/ws-client.ts`
- Modify: `server/internal/ws/handler_test.go`
- Modify: `plugin/src/__tests__/ws-client-protocol.test.ts`

- [ ] **Step 1: Search for legacy protocol names**

Run:

```bash
rtk rg "file_create|file_update|file_create_result|file_update_result|sync_init|sync_result|file_check|file_check_result|file_delete|file_delete_result|prevServerVersion|currentClientHash" server plugin
```

Expected: no hits in `server/internal` or `plugin/src`. Hits in `docs/` are allowed because historical design notes mention the removed protocol.

- [ ] **Step 2: Remove legacy handler methods**

Delete these production functions from `server/internal/ws/handler.go` and `plugin/src/sync.ts`:

- `handleFileCreate`
- `handleFileUpdate`
- `handleFileCreateResult`
- `handleFileUpdateResult`

The only write path after this step is `handleFilePut` on the server and `handleFilePutResult` on the client.

- [ ] **Step 3: Run server tests**

Run:

```bash
cd server && rtk go test ./...
```

Expected: PASS.

- [ ] **Step 4: Run plugin tests and build**

Run:

```bash
cd plugin && rtk npm test && rtk npm run build
```

Expected: PASS and build succeeds.

- [ ] **Step 5: Build graph after changes**

Run the code-review graph incremental build:

```text
Use build_or_update_graph_tool with repo_root=/Users/estsoft/project/other/obsidian-goat-sync
```

Expected: graph update succeeds.

- [ ] **Step 6: Commit cleanup**

```bash
rtk git add server plugin
rtk git commit -m "chore: remove legacy sync protocol"
```

---

## Final Review Checklist

- [ ] `docs/message-matrix.csv` has 56 row IDs and every ID is represented in server fixtures.
- [ ] `file_create` and `file_update` are absent from production code.
- [ ] `syncInit` sends metadata-only entries for local-missing files with `baseVersion`.
- [ ] `fileDelete` tombstone retry uses `DeletedFromVersion() == serverVersion - 1`.
- [ ] `DirtyQueue` never removes an entry after success when `lastSeenHash != sentHash`.
- [ ] `DeleteQueue` writes through temp file rename and cleans stale temp file on load.
- [ ] `BlockedPaths` excludes paths from `syncInit`.
- [ ] `SelfWriteSuppress` prevents plugin-generated watcher loops.
- [ ] `cd server && rtk go test ./...` passes.
- [ ] `cd plugin && rtk npm test && rtk npm run build` passes.
