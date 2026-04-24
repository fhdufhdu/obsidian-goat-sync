# Obsidian Sync 재설계 — 낙관적 락 기반 동기화

작성일: 2026-04-22
대체 대상: [2026-04-21-obsidian-sync-design.md](2026-04-21-obsidian-sync-design.md)

## 개요

옵시디언 볼트 동기화 시스템을 `(version, hash)` 낙관적 락 모델로 전면 재설계한다. 기존 mtime 기반 비교·`remote_change` push 모델은 폐기.

- **서버 권위 버전**: 파일별 version 증가는 서버가 담당. 클라는 `prevServerVersion`만 알고 출발.
- **해시 주체 클라이언트**: SHA-256 해시 계산은 클라가 담당, 서버는 받은 해시를 그대로 저장. 재계산 없음.
- **충돌 감지 서버**: 모든 쓰기 메시지에 `prevServerVersion`을 포함, 서버가 `currentServerVersion`과 비교해 충돌 판정.
- **충돌 해소 클라이언트**: 3선택지(서버/로컬/신규 저장) UI. 삭제 충돌은 2선택지(서버/로컬).

## 구성요소

### 유지
- 옵시디언 플러그인 (TypeScript, Obsidian API)
- Go 서버 (WebSocket + HTTP)
- SQLite (메타데이터)
- 파일시스템 스토리지 (`/app/data/vaults/<vault>/<path>`)
- Docker 패키징
- 대시보드 (Go html/template, admin 환경변수 인증)
- GitHub 백업 (주기적 자동 커밋 + push)
- 토큰 인증, WS 연결 라이프사이클 (healthcheck + reconnect)

### 재설계
- 동기화 모델: mtime → `(prevServerVersion, prevServerHash, currentClientHash)` 낙관적 락
- WebSocket 메시지 전반
- 클라이언트 로컬 메타 구조 (`Record<path, {prevServerVersion, prevServerHash}>`)
- 충돌 UI (모달 + 좌측 파일 목록 + 3 카드)

### 폐기
- `remote_change` 메시지 (다른 기기 변경 push)
- `baseModifiedAt`/`newModifiedAt` 필드
- 기존 `FileWatcher.fileMetadata: Record<path, modifiedAt>` 구조
- 서버 `files.modified_at` 컬럼

## 용어

| 용어 | 의미 |
|---|---|
| `prevServerVersion` / `prevServerHash` | 클라이언트 로컬 메타에 저장된 "마지막 동기화 시점의 서버 값" |
| `currentClientHash` | 클라이언트가 로컬 파일을 방금 읽고 계산한 현재 해시 |
| `currentServerVersion` / `currentServerHash` / `currentServerContent` | 서버 DB 현재 값 (신규 버전 부여 후에도 동일 용어 사용) |

## Docker 구성

기존 유지. 환경변수로 admin 계정, 볼륨 마운트로 데이터 영속.

```yaml
services:
  obsidian-sync:
    image: obsidian-sync
    ports:
      - "8080:8080"
    environment:
      - OBSIDIAN_SYNC_ADMIN_USER=admin
      - OBSIDIAN_SYNC_ADMIN_PASS=password
      - OBSIDIAN_SYNC_PORT=8080
    volumes:
      - ./data:/app/data
```

## 서버 디렉토리 구조

```
/app/data/
├── vaults/
│   ├── personal/
│   ├── work/
│   └── ...
└── sync.db
```

디스크에는 실 파일만. 삭제된 파일은 디스크에서 제거, 서버 DB에 tombstone 유지.

## SQLite 스키마

기존 DB 폐기 후 신규 스키마 초기화.

```sql
CREATE TABLE vaults (
    name        TEXT PRIMARY KEY,
    inserted_at TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE files (
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

CREATE TABLE tokens (
    token       TEXT PRIMARY KEY,
    is_active   INTEGER NOT NULL DEFAULT 1,
    inserted_at TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE github_configs (
    vault_name    TEXT PRIMARY KEY,
    remote_url    TEXT NOT NULL,
    branch        TEXT NOT NULL DEFAULT 'main',
    interval      TEXT NOT NULL DEFAULT '1h',
    access_token  TEXT NOT NULL,
    author_name   TEXT NOT NULL,
    author_email  TEXT NOT NULL,
    enabled       INTEGER NOT NULL DEFAULT 1,
    inserted_at   TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    FOREIGN KEY (vault_name) REFERENCES vaults(name) ON DELETE CASCADE
);
```

- `version` 파일별 카운터. 삭제 후 재생성 시 `version+1` 이어붙임.
- `hash` SHA-256 hex 64자, 클라가 계산한 값 그대로 저장.
- `is_deleted=1` tombstone. 디스크 파일은 실제 제거.
- `access_token` 평문 저장, 대시보드 표시 시 마스킹.

## 서버 엔드포인트

```
WebSocket  /ws?token=xxx            # 플러그인 동기화
HTTP       /                        # 대시보드 UI
HTTP       /api/vaults              # 볼트 CRUD
HTTP       /api/vaults/:name/github # GitHub 설정 (access_token, author 포함)
HTTP       /api/tokens              # 토큰 관리
HTTP       /api/auth/login          # 대시보드 로그인
```

## WebSocket 프로토콜

### 메시지 일람

| 방향 | 타입 | 용도 |
|---|---|---|
| C→S | `sync_init` | 볼트 전체 상태 제출 (볼트 열기, 재연결 직후) |
| S→C | `sync_result` | 업로드/업데이트/다운로드/삭제/메타갱신/충돌 분류 응답 |
| C→S | `file_check` | 단건 파일 상태 확인 (파일 열 때) |
| S→C | `file_check_result` | up-to-date / update-meta / download / conflict / deleted |
| C→S | `file_create` | 신규 생성 (런타임, `toUpload` 처리, conflict 신규 저장 포함) |
| C→S | `file_update` | 수정 (낙관적 락) |
| C→S | `file_delete` | 삭제 (낙관적 락) |
| S→C | `file_{create,update,delete}_result` | 처리 결과 또는 충돌 통보 |
| C→S | `conflict_resolve` | 충돌 해소 선택(로컬/서버) 전송 |
| S→C | `conflict_resolve_result` | 최종 버전 응답 또는 재충돌 통보 |

### 공통 규칙

- 모든 메시지 JSON. 공통 필드 `type`, `vault`.
- 쓰기 메시지(`file_update`, `file_delete`, `conflict_resolve`)에 `prevServerVersion` 포함, 서버가 `currentServerVersion`과 비교.
- 바이너리는 `encoding: "base64"` + content 필드에 base64.
- 해시는 클라가 계산(SHA-256 hex 64자). 서버는 받은 값을 DB에 그대로 저장.
- 쓰기 성공 응답은 `{ok:true, currentServerVersion, currentServerHash}` 포함. 클라는 로컬 메타를 `prevServerVersion := currentServerVersion`, `prevServerHash := currentServerHash`로 갱신.
- 충돌 응답은 `{ok:false, conflict:{currentServerVersion, currentServerHash, currentServerContent}}`.

### 메시지 상세

#### sync_init / sync_result

```json
// C→S
{
  "type": "sync_init",
  "vault": "personal",
  "files": [
    { "path": "notes/hello.md",
      "prevServerVersion": 5, "prevServerHash": "abc...",
      "currentClientHash": "xyz..." },
    { "path": "notes/fresh.md",
      "currentClientHash": "aaa..." }
  ]
}

// S→C
{
  "type": "sync_result",
  "vault": "personal",
  "toUpload":      ["notes/new.md"],
  "toUpdate":      ["notes/hello.md"],
  "toDownload":    [
    { "path": "notes/x.md", "content": "...",
      "currentServerVersion": 3, "currentServerHash": "...",
      "encoding": "base64" }
  ],
  "toDelete":      ["notes/old.md"],
  "toUpdateMeta":  [
    { "path": "notes/y.md",
      "currentServerVersion": 4, "currentServerHash": "..." }
  ],
  "conflicts":     [
    { "path": "notes/idea.md",
      "prevServerVersion": 3, "currentClientHash": "...",
      "currentServerVersion": 5, "currentServerHash": "...",
      "currentServerContent": "...", "encoding": "..." }
  ]
}
```

클라 제출 목록에 없는데 서버에 활성 record가 있는 파일은 `toDownload`로 함께 내려감.

#### file_check / file_check_result

```json
// C→S
{
  "type": "file_check", "vault": "personal",
  "path": "notes/hello.md",
  "prevServerVersion": 5, "prevServerHash": "abc...",
  "currentClientHash": "xyz..."
}

// S→C (경우별)
{ "type": "file_check_result", "path": "notes/hello.md", "action": "up-to-date" }
{ "action": "update-meta", "currentServerVersion": 7, "currentServerHash": "..." }
{ "action": "download",    "content": "...", "currentServerVersion": 7, "currentServerHash": "...", "encoding": "..." }
{ "action": "conflict",    "currentServerVersion": 7, "currentServerHash": "...", "currentServerContent": "...", "encoding": "..." }
{ "action": "deleted",     "currentServerVersion": 7 }
```

#### file_create / file_create_result

```json
// C→S
{ "type": "file_create", "vault": "personal",
  "path": "notes/x.md", "content": "...", "currentClientHash": "..." }

// S→C (정상)
{ "type": "file_create_result", "path": "notes/x.md",
  "ok": true, "currentServerVersion": 1, "currentServerHash": "..." }

// S→C (이미 활성 파일 있음 → 충돌)
{ "ok": false,
  "conflict": { "currentServerVersion": 5, "currentServerHash": "...", "currentServerContent": "..." } }
```

서버에 tombstone이 있으면 `version+1`로 재활용, 활성 record가 있으면 충돌.

#### file_update / file_update_result

```json
// C→S
{ "type": "file_update", "vault": "personal",
  "path": "notes/hello.md", "content": "...",
  "prevServerVersion": 5, "currentClientHash": "..." }

// S→C
{ "ok": true, "currentServerVersion": 6, "currentServerHash": "..." }
{ "ok": true, "noop": true, "currentServerVersion": 5, "currentServerHash": "..." }   // 해시 동일
{ "ok": false,
  "conflict": { "currentServerVersion": 7, "currentServerHash": "...", "currentServerContent": "..." } }
```

#### file_delete / file_delete_result

```json
// C→S
{ "type": "file_delete", "vault": "personal",
  "path": "notes/old.md", "prevServerVersion": 5 }

// S→C
{ "ok": true, "currentServerVersion": 6 }
{ "ok": false,
  "conflict": { "currentServerVersion": 7, "currentServerHash": "...", "currentServerContent": "..." } }
```

#### conflict_resolve / conflict_resolve_result

```json
// C→S (수정 충돌, 로컬 선택)
{ "type": "conflict_resolve", "vault": "personal",
  "path": "notes/hello.md",
  "resolution": "local",
  "content": "...", "currentClientHash": "...",
  "prevServerVersion": 7 }

// C→S (삭제 충돌, 로컬 선택 = 강제 삭제)
{ "type": "conflict_resolve",
  "path": "notes/old.md",
  "resolution": "local", "action": "delete",
  "prevServerVersion": 7 }

// S→C (성공)
{ "type": "conflict_resolve_result", "path": "notes/hello.md",
  "ok": true, "currentServerVersion": 8, "currentServerHash": "..." }

// S→C (재충돌 - UI 대기 중 서버가 다시 앞섬)
{ "ok": false,
  "conflict": { "currentServerVersion": 9, "currentServerHash": "...", "currentServerContent": "..." } }
```

"서버 선택"과 "신규 저장"은 클라이언트 로컬 처리만으로 완료:
- **서버 선택** — 로컬 파일을 `currentServerContent`로 덮어쓰고 메타 갱신. 추가 서버 통신 없음.
- **신규 저장** — conflict 경로(`path.conflict-TIMESTAMP.ext`) 생성 후 그 경로로 `file_create`. 원본 경로는 `currentServerContent`로 덮어쓰고 메타 갱신.

## 서버 충돌 감지 판정표

### sync_init 파일별 분류

| 클라 제출 상태 | 서버 상태 | 추가 조건 | 응답 분류 |
|---|---|---|---|
| prev* 없음 | record 없음 | `currentClientHash` 있음 | `toUpload` |
| prev* 없음 | tombstone | — | `toUpload` (version+1 재활용) |
| prev* 없음 | 활성 | `currentClientHash == currentServerHash` | `toUpdateMeta` |
| prev* 없음 | 활성 | 해시 다름 | `conflicts` |
| prev* 있음 | record 없음 | — | `toUpload` (서버 기록 유실 방어) |
| prev* 있음 | tombstone, prev.v ≤ server.v | — | `toDelete` |
| prev* 있음 | 활성, prev.v == server.v | 해시 동일 | 스킵 |
| prev* 있음 | 활성, prev.v == server.v | 해시 다름 | `toUpdate` |
| prev* 있음 | 활성, prev.v < server.v | `currentClientHash == currentServerHash` | `toUpdateMeta` |
| prev* 있음 | 활성, prev.v < server.v | `prevServerHash == currentClientHash` | `toDownload` |
| prev* 있음 | 활성, prev.v < server.v | 해시 둘 다 다름 | `conflicts` |

클라 제출 목록에 없는데 서버에 활성 record → `toDownload`.

### file_check 단건 분류

동일 판정표를 단건에 적용. 다만 클라 제출에 없는 파일 케이스는 없음 (클라가 조회 중인 파일이니).

### file_update/file_create/file_delete 낙관적 락

| 메시지 | 서버 상태 | 조건 | 처리 |
|---|---|---|---|
| `file_create` | record 없음 or tombstone | — | 저장. `currentServerVersion = prevVersion + 1` (신규는 1) |
| `file_create` | 활성 | — | 충돌 응답 |
| `file_update` | 활성, prevServerVersion == currentServerVersion | 해시 동일 | `noop` 응답 |
| `file_update` | 활성, prevServerVersion == currentServerVersion | 해시 다름 | 저장. `currentServerVersion += 1` |
| `file_update` | 활성, prevServerVersion != currentServerVersion | — | 충돌 응답 |
| `file_update` | tombstone/없음 | — | 에러 또는 create 전환 (초기엔 에러) |
| `file_delete` | 활성, prev == current | — | 삭제. `is_deleted=1, currentServerVersion += 1` |
| `file_delete` | 활성, prev != current | — | 충돌 응답 |
| `file_delete` | tombstone/없음 | — | 무시 (idempotent) |

### conflict_resolve 검증

- `resolution: "local"` + content 포함 → 수정 충돌 해소. `file_update` 정책 동일하게 prevServerVersion 재검증. 통과 시 저장, 실패 시 재충돌.
- `resolution: "local"` + `action: "delete"` → 삭제 충돌 해소. `file_delete` 정책 동일하게 재검증.
- `resolution: "server"`는 서버 통신 불필요하므로 해당 메시지 자체가 전송되지 않음.

## 클라이언트 설계

### 모듈 구성

```
plugin/src/
├── main.ts                 # 플러그인 진입점
├── settings.ts             # 설정 UI + 저장
├── ws-client.ts            # WebSocket 래퍼 (연결·재연결·healthcheck·send·on)
├── sync.ts                 # SyncManager — 전체 흐름 조율
├── file-meta-store.ts      # fileMeta 영속 저장·조회·디바운스
├── file-watcher.ts         # Vault 이벤트 → 변경 이벤트 emit
├── hash.ts                 # SHA-256 계산 (텍스트/base64)
├── conflict-queue.ts       # 충돌 파일 큐 + 선택 상태
└── conflict-modal.ts       # 3 카드 UI (Obsidian Modal)
```

### FileMetaStore

```ts
interface FileMeta {
  prevServerVersion: number;
  prevServerHash: string;
}

class FileMetaStore {
  private data: Record<string, FileMeta>;
  get(path: string): FileMeta | undefined;
  set(path: string, meta: FileMeta): void;
  remove(path: string): void;
  entries(): [string, FileMeta][];
  // 500ms 디바운스 저장 → data.json
}
```

`data.json` 구조:
```json
{
  "serverUrl": "...",
  "token": "...",
  "vaultName": "personal",
  "fileMeta": {
    "notes/hello.md": { "prevServerVersion": 5, "prevServerHash": "..." }
  }
}
```

기존 `metadata` 필드는 무시·삭제.

### SyncManager 흐름

- **start**: WebSocket 연결, healthcheck 시작, FileWatcher 구독, `sync_init` 실행.
- **onFileCreate**: 로컬 해시 계산 → `file_create` 전송 → 응답으로 메타 갱신.
- **onFileModify**: `fileMeta.prevServerHash !== currentClientHash`인 경우에만 `file_update` 전송 (불필요 전송 회피). 충돌 응답 시 ConflictQueue에 추가.
- **onFileDelete**: `file_delete` 전송.
- **onFileOpen**: `file_check` 전송 → 응답 액션에 따라 처리.
- **onReconnect**: `sync_init` 재실행.
- **onSyncResult**: `toUpload`→file_create, `toUpdate`→file_update, `toDownload`→로컬 덮어쓰기+메타, `toDelete`→로컬 삭제+메타 제거, `toUpdateMeta`→메타만 갱신, `conflicts`→ConflictQueue 적재 후 모달 열기.

### ConflictQueue

```ts
interface ConflictEntry {
  path: string;
  prevServerVersion?: number;
  currentClientContent: Uint8Array | string;
  currentClientHash: string;
  currentServerVersion: number;
  currentServerHash: string;
  currentServerContent: Uint8Array | string;
  encoding?: "base64";
  kind: "modify" | "delete";   // delete일 때는 content 없음, 2카드
  selection?: "server" | "local" | "new";
}

class ConflictQueue {
  add(entry: ConflictEntry): void;
  list(): ConflictEntry[];
  selectAt(path: string, choice: "server"|"local"|"new"): void;
  remove(path: string): void;
  isAllResolved(): boolean;
}
```

### ConflictModal

Obsidian `Modal` 기반. 구조:

- **좌측 사이드바**: 충돌 파일 목록. 현재 선택된 파일 하이라이트. 각 행에 선택 상태 표시(서버/로컬/신규/미결).
- **우측 3 카드 영역**:
  - **SERVER 카드**: 서버 content 미리보기 (텍스트 = monospace, 이미지 = 썸네일, 기타 바이너리 = 메타). 상단에 `currentServerVersion`.
  - **LOCAL 카드**: 로컬 content 미리보기 (동일 규칙). 스크롤은 SERVER와 동기화 (wheel·scroll 이벤트 연동).
  - **신규 저장 카드**: 생성될 경로(`path.conflict-TIMESTAMP.ext`) 표시 + 처리 요약.
- **카드 클릭 동작**: 선택 저장 + 다음 미결 파일로 포커스 이동. 이미 선택한 파일 되돌아가 변경 가능.
- **하단 [모두 적용] 버튼**: 전체 파일 선택 완료 시 활성화. 클릭 시 각 선택대로 일괄 처리.
- **삭제 충돌**: 3카드 대신 2카드 (서버 선택 = 복구 / 로컬 선택 = 강제 삭제). "신규 저장" 카드 없음.
- **바이너리**: 이미지는 썸네일 표시, 나머지(PDF/zip 등)는 크기/해시/수정일 메타만 노출.

### 해시 계산 (`hash.ts`)

```ts
async function sha256(data: ArrayBuffer | string): Promise<string>;
```

Web Crypto API (`crypto.subtle.digest`) 사용. 텍스트는 UTF-8 인코딩 후 계산.

## 충돌 해소 플로우

### A. 수정 충돌 (3선택지)

1. 사용자 `notes/hello.md` 편집.
2. FileWatcher → SyncManager → 로컬 해시 계산.
3. `file_update` 전송. 서버 응답 `conflict`.
4. ConflictQueue.add, ConflictModal 열기(또는 기존 모달에 추가).
5. 사용자 선택:
   - **서버 선택**: 로컬 파일을 `currentServerContent`로 덮어쓰기. 메타 `prevServerVersion := currentServerVersion`. 통신 없음.
   - **로컬 선택**: `conflict_resolve {resolution:"local", content, currentClientHash, prevServerVersion:currentServerVersion}`. 성공 응답으로 메타 갱신. 재충돌이면 모달 카드가 새 서버 값으로 교체.
   - **신규 저장**: 플러그인이 conflict 경로 생성 → 로컬 내용을 그 경로에 저장 → `file_create` 전송. 원본 경로는 `currentServerContent`로 덮어쓰기 + 메타 갱신.
6. 선택 완료 후 모달 닫힘.

### B. 삭제 충돌 (2선택지)

1. 사용자 `notes/old.md` 삭제.
2. `file_delete` 전송. 서버 응답 `conflict` (현재 서버 내용 포함).
3. ConflictQueue에 `kind:"delete"`로 추가, 삭제 모달 표시.
4. 사용자 선택:
   - **서버 선택(복구)**: 로컬 파일을 `currentServerContent`로 재생성. 메타 갱신.
   - **로컬 선택(강제 삭제)**: `conflict_resolve {resolution:"local", action:"delete", prevServerVersion:currentServerVersion}`. 성공 시 메타 제거.

### C. 파일 열기

`file_check` 응답:
- `up-to-date` → 동작 없음
- `update-meta` → 메타만 갱신
- `download` → 로컬 변경 없으면 조용히 덮어쓰기, 있으면 신규 저장 케이스로 처리
- `conflict` → 모달
- `deleted` → 로컬 파일 제거 + 메타 제거

### D. sync_init

볼트 열기·재연결 직후 자동 실행. `sync_result` 분류별 순차 처리. 충돌은 모달에 일괄 적재.

### E. 다른 기기 변경 전파

- 서버 push 없음 (`remote_change` 폐기).
- 파일을 열면 `file_check`로 발견. 또는 재연결 시 `sync_init`으로 발견.
- Trade-off: 실시간성 저하. 수용.

## 대시보드

기존 화면 구조 유지. GitHub 백업 설정 화면을 확장.

### GitHub 백업 설정 UI

볼트 상세 화면 내 섹션:

- Remote URL (text input)
- Branch (text input, default `main`)
- Interval (text input, default `1h`, 포맷: Go `time.Duration`)
- Access Token (password input, 저장 후 마스킹 표시 `ghp_****1234`, 재입력 시만 업데이트)
- Author Name (text input)
- Author Email (email input)
- Enabled (checkbox)
- [저장] 버튼 → `PUT /api/vaults/:name/github`

마스킹 정책: DB는 평문 저장. API 응답·템플릿 렌더링 시 앞 4자 + `****` + 뒤 4자. 빈 값 전송은 "변경 없음"으로 해석.

## GitHub 백업 동작

볼트 디렉토리별 git repo 초기화. 저장된 `author_name`/`author_email`을 `git commit --author` 로 사용. `access_token`은 `https://<token>@github.com/<owner>/<repo>.git` 형태로 remote에 주입. `interval` 마다 자동 `git add -A` + commit + push.

## 에러 처리

- **네트워크 단절**: `WsClient.startHealthCheck` 유지. 실패 시 reconnect. 재연결 성공 시 `sync_init` 자동 재실행.
- **전송 중 단절**: 응답 못 받은 `file_*` 메시지는 개별 재시도 없음. 다음 `sync_init`이 `toUpdate`/`toUpload`로 재분류하여 자동 복구.
- **서버 에러 응답**: WS 메시지 `error` 필드. 클라는 `Notice`로 표시.
- **conflict_resolve 재충돌**: 모달 현재 카드를 새 서버 값으로 교체 + 토스트 "서버에 더 최신 변경이 있습니다".
- **파일 I/O 실패**: 해당 파일만 스킵 + Notice, 전체 동기화 중단하지 않음.
- **큰 파일**: chunking 미구현. 메시지 크기 초과 시 에러. 향후 확장.

## 마이그레이션

### 서버
- 기존 `sync.db` 폐기. 초기 기동 시 신규 스키마 DDL 실행.
- 기존 `/app/data/vaults/` 디렉토리는 선택적으로 스캔 후 `files` 테이블 재구성하는 1회성 init 스크립트 제공 (또는 완전 재업로드 유도).

### 플러그인
- 기존 `data.json`의 `metadata` 필드 무시. `fileMeta` 필드가 없으면 빈 객체로 시작.
- 첫 `sync_init`에서 `prevServerVersion`/`prevServerHash` 없이 `currentClientHash`만 전송. 서버가 `toUpload`/`toUpdateMeta`/`conflicts`로 분류.

## 테스트 전략

### Go 서버

- `internal/db/file_test.go` — version/hash CRUD, tombstone 재활용, sync_init 판정표 케이스별
- `internal/db/github_config_test.go` — access_token/author 필드 저장·조회
- `internal/ws/handler_test.go` — 각 메시지 경로 (정상/noop/충돌/재충돌)
- `internal/sync/conflict_test.go` — 낙관적 락 판정 단위 테스트
- 기존 테스트 중 mtime 의존 제거

### 플러그인

- 테스트 러너는 Vitest 도입 (esbuild 호환, Obsidian API는 모킹)
- `FileMetaStore` — 읽기·쓰기·디바운스
- `SyncManager` — sync_result 분류별 처리, conflict 큐 연계
- `ConflictQueue` — 추가·선택·완료 판정
- `ConflictModal` — 카드 클릭·선택 번복·일괄 적용 (DOM 테스트)
- `hash.ts` — 텍스트·바이너리 해시 정확성

### 통합 (수동)

- docker-compose로 서버 기동
- 플러그인 2개 인스턴스 연결 (별도 볼트 폴더)
- 시나리오 체크리스트:
  - 동시 편집 후 두 번째 업로드 시 충돌 UI
  - 삭제 충돌 복구·강제 삭제
  - 신규 설치(메타 없음) 상태에서 서버 파일과 해시 비교
  - 네트워크 단절·재연결 후 sync_init 일괄 복구
  - GitHub 백업 access_token·author 설정 저장 → commit 확인

## 관측성

- 서버: 기존 로깅 유지 + 충돌 건수 카운터 (로그 기반 집계 가능)
- 클라: `console.debug` + 주요 이벤트(연결/충돌/에러) `Notice` 표시

## 기술 스택

| 영역 | 기술 |
|---|---|
| 서버 | Go |
| DB | SQLite |
| 실시간 | WebSocket (gorilla/websocket 또는 현 구현 유지) |
| 대시보드 | Go html/template |
| 플러그인 | TypeScript, Obsidian API |
| 해시 | SHA-256 (Web Crypto API / Go crypto/sha256) |
| 플러그인 빌드 | esbuild |
| 플러그인 테스트 | Vitest |
| 배포 | Docker |

## 개발 단계

1. 서버 DB 스키마 교체 + 폐기 스크립트
2. 서버 WS messages 재설계 (`messages.go` 전면 교체)
3. 서버 handler 판정표 구현 + 단위 테스트
4. 플러그인 `FileMetaStore`, `hash.ts`
5. 플러그인 `SyncManager` 재작성 + 런타임 메시지 경로
6. `ConflictQueue` + `ConflictModal` UI
7. 대시보드 GitHub config 확장 (access_token, author 필드 + 마스킹)
8. 통합 검증 시나리오 수행

## 향후 고려사항 (현재 미포함)

- 대용량 파일 WebSocket chunking
- E2E 암호화
- 버전 히스토리 / 롤백
- 실시간 push (WebSocket broadcast) 재도입
- access_token 서버측 암호화 저장
