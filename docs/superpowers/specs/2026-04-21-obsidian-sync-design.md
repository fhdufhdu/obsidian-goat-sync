# Obsidian Sync - 설계 문서

## 개요

옵시디언 볼트를 자체 홈 서버와 실시간 동기화하는 시스템. 옵시디언 플러그인(TypeScript)과 Go 서버로 구성.

## 구성요소

- **옵시디언 플러그인** (TypeScript) — 파일 변경 감지, WebSocket 통신, 충돌 파일 표시
- **Go 서버** (Docker) — WebSocket 동기화, HTTP 대시보드, SQLite 메타데이터, 파일시스템 스토리지
- **대시보드** — Go html/template 서버 렌더링, 볼트/토큰/GitHub 관리
- **GitHub 백업** — 볼트별 주기적 자동 커밋 + push

## Docker 구성

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

- 환경변수로 관리자 계정 설정
- 볼륨 마운트로 데이터 접근
- 서버 내부 경로 `/app/data` 고정

## 서버 내부 디렉토리 구조

```
/app/data/
├── vaults/
│   ├── personal/       # 볼트 1 - 옵시디언 폴더 구조 그대로
│   │   ├── daily/
│   │   └── notes/
│   ├── work/           # 볼트 2
│   │   └── projects/
│   └── ...
└── sync.db             # SQLite 메타데이터
```

## SQLite 스키마

```sql
CREATE TABLE vaults (
    name       TEXT PRIMARY KEY,
    created_at TEXT NOT NULL
);

CREATE TABLE files (
    vault_name  TEXT NOT NULL,
    path        TEXT NOT NULL,
    modified_at TEXT NOT NULL,
    is_deleted  INTEGER DEFAULT 0,
    PRIMARY KEY (vault_name, path),
    FOREIGN KEY (vault_name) REFERENCES vaults(name)
);

CREATE TABLE tokens (
    token      TEXT PRIMARY KEY,
    created_at TEXT NOT NULL,
    is_active  INTEGER DEFAULT 1
);

CREATE TABLE github_configs (
    vault_name TEXT PRIMARY KEY,
    remote_url TEXT NOT NULL,
    branch     TEXT DEFAULT 'main',
    interval   TEXT DEFAULT '1h',
    enabled    INTEGER DEFAULT 1,
    FOREIGN KEY (vault_name) REFERENCES vaults(name)
);
```

## 서버 엔드포인트

```
WebSocket  /ws?token=xxx            # 플러그인 동기화
HTTP       /                        # 대시보드 UI
HTTP       /api/vaults              # 볼트 CRUD
HTTP       /api/vaults/:name/github # GitHub 설정
HTTP       /api/tokens              # 토큰 관리
HTTP       /api/auth/login          # 대시보드 로그인 (환경변수 admin 계정)
```

## WebSocket 프로토콜

연결: `ws://server:port/ws?token=xxx`
인증 한번 → 이후 메시지에 vault 이름만 포함.

### 메시지 타입

| 메시지 타입 | 방향 | 용도 |
|------------|------|------|
| vault_create | C→S | 볼트 생성 |
| sync_init | C→S | 초기 동기화 (전체 파일 목록 비교) |
| sync_result | S→C | upload/download/delete 목록 응답 |
| file_upload | C→S | 초기 동기화 시 파일 업로드 (path, content, modifiedAt) |
| file_create | C→S | 파일 등록 (path, content, newModifiedAt) |
| file_update | C→S | 파일 수정 (path, content, baseModifiedAt, newModifiedAt) |
| file_delete | C→S | 파일 삭제 (path) |
| file_*_result | S→C | 연산 결과 (filesToAdd, filesToDelete) |
| remote_change | S→C | 다른 클라이언트 변경 push |

### 파일 전송

모든 파일(마크다운, 이미지, PDF, 첨부파일 등)에 대해 업로드/다운로드 지원.
숨김파일(`.obsidian/` 설정, `.trash/` 등 dot-prefix 파일/폴더)도 동기화 대상에 포함.
- 텍스트 파일: JSON content 필드로 전송
- 바이너리 파일(이미지, PDF 등): base64 인코딩하여 content 필드로 전송
- content 필드에 `encoding: "base64"` 플래그 포함하여 바이너리 구분

### 메시지 상세

**초기 동기화 (볼트 열기)**
```json
// Client → Server
{ "type": "sync_init", "vault": "personal", "files": [{ "path": "notes/hello.md", "modifiedAt": "2026-04-21T10:00:00Z" }] }

// Server → Client
{ "type": "sync_result", "toUpload": ["notes/new.md"], "toDownload": [{ "path": "notes/hello.md", "content": "...", "modifiedAt": "2026-04-21T12:00:00Z" }], "toDelete": ["notes/old.md"] }
```

**파일 업로드 (sync_init 응답 후 toUpload 대상)**
```json
// Client → Server
{ "type": "file_upload", "vault": "personal", "path": "notes/new.md", "content": "...", "modifiedAt": "2026-04-21T10:00:00Z" }

// 바이너리 파일
{ "type": "file_upload", "vault": "personal", "path": "attachments/image.png", "content": "<base64>", "encoding": "base64", "modifiedAt": "2026-04-21T10:00:00Z" }
```

**파일 등록**
```json
// Client → Server
{ "type": "file_create", "vault": "personal", "path": "notes/new.md", "content": "...", "newModifiedAt": "2026-04-21T15:00:00Z" }

// Server → Client
{ "type": "file_create_result", "filesToAdd": [], "filesToDelete": [] }
```

**파일 수정**
```json
// Client → Server
{ "type": "file_update", "vault": "personal", "path": "notes/hello.md", "content": "...", "baseModifiedAt": "2026-04-21T10:00:00Z", "newModifiedAt": "2026-04-21T15:30:00Z" }

// Server → Client
{ "type": "file_update_result", "filesToAdd": [], "filesToDelete": [] }
```

**파일 삭제**
```json
// Client → Server
{ "type": "file_delete", "vault": "personal", "path": "notes/old.md" }

// Server → Client
{ "type": "file_delete_result", "filesToAdd": [], "filesToDelete": [] }
```

**다른 클라이언트 변경 push**
```json
// Server → Client
{ "type": "remote_change", "vault": "personal", "filesToAdd": [{ "path": "notes/hello.md", "content": "...", "modifiedAt": "2026-04-21T15:30:00Z" }], "filesToDelete": ["notes/old.md"] }
```

## 충돌 판단 로직

### 파일 등록
- 서버에 동일 경로 없음 → 정상 등록
- 서버에 동일 경로 존재 → `path.conflict.md` 생성, filesToAdd에 conflict 파일 포함

### 파일 수정
- baseModifiedAt >= 서버 modifiedAt → 정상 덮어쓰기
- baseModifiedAt < 서버 modifiedAt → `path.conflict.md` 생성, filesToAdd에 conflict 파일 포함

### 파일 삭제
- 서버에 존재 → 삭제 (is_deleted = 1)
- 서버에 없음 → 무시

### 충돌 파일 네이밍
- 원본: `notes/hello.md`
- 충돌: `notes/hello.conflict-20260421T153000Z.md` (타임스탬프 포함, 중복 방지)

## 수정시각 관리

- 모든 수정시각은 클라이언트 파일시스템 mtime 기준
- 서버는 클라이언트 전달값 그대로 저장, 자체 시각 생성 안 함
- `baseModifiedAt`: 파일 열었을 때 읽은 mtime
- `newModifiedAt`: 파일 저장 후 mtime

## 대시보드

Go html/template 서버 렌더링. 환경변수 admin 계정으로 인증.

### 기능
- **볼트 관리**: 목록 조회, 생성, 삭제, 파일 수/용량 표시
- **GitHub 설정**: 볼트별 remote URL, branch, interval 설정/수정
- **API 키 관리**: 토큰 목록, 발급, 재발급, 비활성화

## GitHub 백업

- 볼트 디렉토리별 git repo 초기화
- 설정 간격마다 자동 커밋 + push (기본 1시간)
- 대시보드에서 볼트별 설정 관리
- 설정 항목: remote URL, branch, interval, enabled

## 기술 스택

| 구성요소 | 기술 |
|---------|------|
| 서버 | Go |
| DB | SQLite |
| 실시간 통신 | WebSocket |
| 대시보드 | Go html/template |
| 플러그인 | TypeScript (Obsidian API) |
| 배포 | Docker |

## 개발 도구

- **code-review-graph MCP**: 코드베이스 탐색, 코드 리뷰, 영향 분석 시 사용. Grep/Glob/Read 전에 그래프 도구 우선 사용.

## 향후 고려사항 (현재 미구현)

- 대용량 파일 WebSocket chunking (필요 시)
- E2E 암호화 (필요 시)
- 버전 히스토리 (필요 시)
