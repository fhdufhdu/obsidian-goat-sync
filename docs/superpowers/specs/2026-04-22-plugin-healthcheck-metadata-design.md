# 플러그인 헬스체크 + 메타데이터 영속화 설계

## 개요

플러그인 재시작/슬립 복귀 시 동기화가 안 되는 문제 해결. 두 가지 기능 추가:

1. 주기적 헬스체크로 연결 끊김 감지 → 자동 재연결 → sync_init 재발송
2. 파일 메타데이터(baseModifiedAt) 영속화로 불필요한 파일 다운로드 방지

## 기능 1: WsClient 헬스체크 + 재연결

### 변경 대상

`plugin/src/ws-client.ts`

### 변경 사항

- `healthCheckTimer`: `setInterval` 30초 주기
- 헬스체크 로직: `ws === null || ws.readyState !== OPEN` → `connect()` 호출
- 재연결 성공 → `callbacks.get("reconnected")` 콜백 호출
- 기존 `scheduleReconnect()` 제거 — 헬스체크가 역할 대체
- `connect()` 시작 시 기존 소켓 정리
- `disconnect()` 시 `healthCheckTimer`도 정리

### 흐름

```
setInterval 30초
  → readyState 체크
  → 끊김 감지
  → connect()
  → 성공 → "reconnected" 콜백 발동
  → SyncManager가 sync_init 재발송
```

## 기능 2: 메타데이터 영속화

### 변경 대상

`plugin/src/file-watcher.ts`, `plugin/src/main.ts`

### data.json 구조

```json
{
  "serverUrl": "ws://...",
  "token": "xxx",
  "vaultName": "personal",
  "fileMetadata": {
    "notes/hello.md": "2026-04-21T10:00:00Z",
    "attachments/img.png": "2026-04-21T12:00:00Z"
  }
}
```

기존 settings + `fileMetadata`(path → baseModifiedAt 맵) 합쳐서 저장.

### FileWatcher 변경

- 생성자에서 초기 `fileMetadata` 맵 받아서 `openedTimes` 복원
- `trackOpened` 호출 시 디바운스 콜백 발동 → 외부에서 `saveData()` 호출
- 디바운스 5초

### 흐름

```
플러그인 시작
  → loadData() → fileMetadata 존재
  → FileWatcher에 전달 → openedTimes 복원
  → sync_init 시 정확한 baseModifiedAt 비교 가능

파일 동기화 완료
  → trackOpened() 호출
  → 5초 디바운스 → saveData() 호출
```

## 기능 3: 동기화 제외 패턴

### 변경 대상

`plugin/src/file-watcher.ts`

### 제외 대상

`.obsidian/plugins/` 디렉토리 전체

### 적용 위치

- `FileWatcher.start()` — 이벤트 콜백에서 `file.path` 필터링
- `FileWatcher.getAllFiles()` — sync_init용 파일 목록에서 제외

### 필터 로직

```typescript
function isExcluded(path: string): boolean {
  return path.startsWith(".obsidian/plugins/");
}
```

## SyncManager 연결

### 변경 대상

`plugin/src/sync.ts`

### 변경 사항

- `start()`에서 `wsClient.on("reconnected", ...)` 등록
- 콜백 내용: `getAllFiles()` → `sendSyncInit()` (기존 start() 초기 동기화 로직 재사용)
- 재연결 시 기존 핸들러(`sync_result`, `remote_change` 등) 재등록 불필요 — 이미 등록됨
- 디바운스 저장 콜백을 FileWatcher에 전달
- SyncManager 생성자에서 loadData()로 복원된 메타데이터를 FileWatcher에 주입

### 흐름

```
재연결 성공 ("reconnected")
  → getAllFiles()
  → sendSyncInit(vaultName, files)
  → 서버가 baseModifiedAt 비교 → 변경분만 응답
  → 기존 sync_result 핸들러가 처리
```
