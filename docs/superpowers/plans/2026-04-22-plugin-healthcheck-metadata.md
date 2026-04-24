# Plugin Healthcheck + Metadata Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 플러그인 재시작/슬립 복귀 시 자동 재연결 + sync_init 재발송, baseModifiedAt 영속화로 불필요한 다운로드 방지

**Architecture:** WsClient에 30초 헬스체크 추가하여 끊김 감지 → 재연결 → "reconnected" 콜백 발동. FileWatcher가 openedTimes를 data.json에 디바운스 5초로 영속화. `.obsidian/plugins/` 경로 동기화 제외.

**Tech Stack:** TypeScript, Obsidian API (Plugin.loadData/saveData, Vault)

---

## File Structure

```
plugin/src/
├── settings.ts        # Modify: SyncSettings에 fileMetadata 필드 추가
├── ws-client.ts       # Modify: 헬스체크 + reconnected 콜백 + scheduleReconnect 제거
├── file-watcher.ts    # Modify: 제외 필터 + 메타데이터 복원/디바운스 저장
├── sync.ts            # Modify: reconnected 핸들러 등록 + 메타데이터 주입
└── main.ts            # Modify: loadData/saveData에 fileMetadata 연결
```

---

### Task 1: settings.ts — fileMetadata 타입 추가

**Files:**
- Modify: `plugin/src/settings.ts`

- [ ] **Step 1: SyncSettings 인터페이스에 fileMetadata 추가**

```typescript
export interface SyncSettings {
  serverUrl: string;
  token: string;
  vaultName: string;
  fileMetadata: Record<string, string>;
}

export const DEFAULT_SETTINGS: SyncSettings = {
  serverUrl: "",
  token: "",
  vaultName: "",
  fileMetadata: {},
};
```

- [ ] **Step 2: 빌드 확인**

Run: `cd plugin && npm run build`
Expected: 빌드 성공 (fileMetadata 사용처 아직 없으므로)

- [ ] **Step 3: 커밋**

```bash
git add plugin/src/settings.ts
git commit -m "feat: add fileMetadata field to SyncSettings"
```

---

### Task 2: file-watcher.ts — 동기화 제외 필터

**Files:**
- Modify: `plugin/src/file-watcher.ts`

- [ ] **Step 1: isExcluded 함수 추가 + start()에 필터 적용**

`file-watcher.ts` 상단에 제외 함수 추가:

```typescript
function isExcluded(path: string): boolean {
  return path.startsWith(".obsidian/plugins/");
}
```

`start()` 메서드 내 각 이벤트 핸들러에 필터 적용:

```typescript
start() {
  this.vault.on("create", (file: TAbstractFile) => {
    if (file instanceof TFile && !isExcluded(file.path)) {
      this.onChange({ type: "create", path: file.path });
    }
  });

  this.vault.on("modify", (file: TAbstractFile) => {
    if (file instanceof TFile && !isExcluded(file.path)) {
      this.onChange({ type: "modify", path: file.path });
    }
  });

  this.vault.on("delete", (file: TAbstractFile) => {
    if (file instanceof TFile && !isExcluded(file.path)) {
      this.openedTimes.delete(file.path);
      this.onChange({ type: "delete", path: file.path });
    }
  });
}
```

- [ ] **Step 2: getAllFiles()에 필터 적용**

`listRecursive` 내부에서 제외 경로 스킵:

```typescript
private async listRecursive(dir: string, result: { path: string; modifiedAt: string }[]) {
  const listing = await this.vault.adapter.list(dir);
  for (const filePath of listing.files) {
    if (isExcluded(filePath)) continue;
    const stat = await this.vault.adapter.stat(filePath);
    if (stat && stat.type === "file") {
      result.push({
        path: filePath,
        modifiedAt: new Date(stat.mtime).toISOString(),
      });
    }
  }
  for (const folder of listing.folders) {
    if (isExcluded(folder)) continue;
    await this.listRecursive(folder, result);
  }
}
```

- [ ] **Step 3: 빌드 확인**

Run: `cd plugin && npm run build`
Expected: 빌드 성공

- [ ] **Step 4: 커밋**

```bash
git add plugin/src/file-watcher.ts
git commit -m "feat: exclude .obsidian/plugins/ from sync"
```

---

### Task 3: file-watcher.ts — 메타데이터 복원 + 디바운스 저장

**Files:**
- Modify: `plugin/src/file-watcher.ts`

- [ ] **Step 1: 생성자에 초기 메타데이터 + onSave 콜백 추가**

```typescript
export class FileWatcher {
  private vault: Vault;
  private onChange: (change: FileChange) => void;
  private onSave: (() => void) | null;
  private openedTimes: Map<string, string> = new Map();
  private saveTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(
    vault: Vault,
    onChange: (change: FileChange) => void,
    initialMetadata?: Record<string, string>,
    onSave?: () => void,
  ) {
    this.vault = vault;
    this.onChange = onChange;
    this.onSave = onSave || null;
    if (initialMetadata) {
      for (const [path, modifiedAt] of Object.entries(initialMetadata)) {
        this.openedTimes.set(path, modifiedAt);
      }
    }
  }
```

- [ ] **Step 2: trackOpened에 디바운스 저장 트리거 추가**

```typescript
trackOpened(path: string, modifiedAt: string) {
  this.openedTimes.set(path, modifiedAt);
  this.scheduleSave();
}

getFileMetadata(): Record<string, string> {
  return Object.fromEntries(this.openedTimes);
}

private scheduleSave() {
  if (this.saveTimer) clearTimeout(this.saveTimer);
  this.saveTimer = setTimeout(() => {
    this.saveTimer = null;
    if (this.onSave) this.onSave();
  }, 5000);
}
```

- [ ] **Step 3: delete 이벤트에서도 디바운스 저장 트리거**

```typescript
this.vault.on("delete", (file: TAbstractFile) => {
  if (file instanceof TFile && !isExcluded(file.path)) {
    this.openedTimes.delete(file.path);
    this.scheduleSave();
    this.onChange({ type: "delete", path: file.path });
  }
});
```

- [ ] **Step 4: 빌드 확인**

Run: `cd plugin && npm run build`
Expected: 빌드 성공

- [ ] **Step 5: 커밋**

```bash
git add plugin/src/file-watcher.ts
git commit -m "feat: add metadata persistence with debounced save"
```

---

### Task 4: ws-client.ts — 헬스체크 + 재연결 + reconnected 콜백

**Files:**
- Modify: `plugin/src/ws-client.ts`

- [ ] **Step 1: healthCheckTimer 필드 추가 + scheduleReconnect 제거**

`reconnectTimer` 필드와 `scheduleReconnect()` 메서드를 제거하고 `healthCheckTimer` 추가:

```typescript
export class WsClient {
  private ws: WebSocket | null = null;
  private serverUrl: string;
  private token: string;
  private callbacks: Map<string, MessageCallback[]> = new Map();
  private healthCheckTimer: ReturnType<typeof setInterval> | null = null;
```

- [ ] **Step 2: connect()에서 기존 소켓 정리 + onclose에서 scheduleReconnect 호출 제거**

```typescript
connect(): Promise<void> {
  return new Promise((resolve, reject) => {
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.onerror = null;
      this.ws.close();
      this.ws = null;
    }

    const url = `${this.serverUrl}/ws?token=${this.token}`;
    this.ws = new WebSocket(url);

    this.ws.onopen = () => resolve();

    this.ws.onmessage = (event) => {
      const msg: SyncResult = JSON.parse(event.data);
      const handlers = this.callbacks.get(msg.type) || [];
      handlers.forEach((cb) => cb(msg));
    };

    this.ws.onclose = () => {
      this.ws = null;
    };

    this.ws.onerror = (err) => {
      reject(err);
    };
  });
}
```

- [ ] **Step 3: startHealthCheck() + stopHealthCheck() 추가**

```typescript
startHealthCheck() {
  this.stopHealthCheck();
  this.healthCheckTimer = setInterval(() => {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      this.connect()
        .then(() => {
          const handlers = this.callbacks.get("reconnected") || [];
          handlers.forEach((cb) => cb({} as SyncResult));
        })
        .catch(() => {});
    }
  }, 30000);
}

private stopHealthCheck() {
  if (this.healthCheckTimer) {
    clearInterval(this.healthCheckTimer);
    this.healthCheckTimer = null;
  }
}
```

- [ ] **Step 4: disconnect()에서 healthCheck 정리**

```typescript
disconnect() {
  this.stopHealthCheck();
  if (this.ws) {
    this.ws.onclose = null;
    this.ws.close();
    this.ws = null;
  }
}
```

- [ ] **Step 5: 빌드 확인**

Run: `cd plugin && npm run build`
Expected: 빌드 성공

- [ ] **Step 6: 커밋**

```bash
git add plugin/src/ws-client.ts
git commit -m "feat: add healthcheck with 30s interval and reconnected callback"
```

---

### Task 5: sync.ts — reconnected 핸들러 + 메타데이터 주입

**Files:**
- Modify: `plugin/src/sync.ts`

- [ ] **Step 1: 생성자에 onSaveMetadata 콜백 + initialMetadata 추가**

```typescript
export class SyncManager {
  private vault: Vault;
  private wsClient: WsClient;
  private fileWatcher: FileWatcher;
  private vaultName: string;
  private syncing = false;

  constructor(
    vault: Vault,
    serverUrl: string,
    token: string,
    vaultName: string,
    initialMetadata: Record<string, string>,
    onSaveMetadata: (metadata: Record<string, string>) => void,
  ) {
    this.vault = vault;
    this.vaultName = vaultName;
    this.wsClient = new WsClient(serverUrl, token);
    this.fileWatcher = new FileWatcher(
      vault,
      (change) => this.handleLocalChange(change),
      initialMetadata,
      () => onSaveMetadata(this.fileWatcher.getFileMetadata()),
    );
  }
```

- [ ] **Step 2: start()에 reconnected 핸들러 등록 + 초기 동기화 로직 추출**

```typescript
async start() {
  await this.wsClient.connect();

  this.wsClient.on("sync_result", (msg) => this.handleSyncResult(msg));
  this.wsClient.on("file_create_result", (msg) => this.handleOperationResult(msg));
  this.wsClient.on("file_update_result", (msg) => this.handleOperationResult(msg));
  this.wsClient.on("file_delete_result", (msg) => this.handleOperationResult(msg));
  this.wsClient.on("remote_change", (msg) => this.handleRemoteChange(msg));
  this.wsClient.on("reconnected", () => this.performSyncInit());

  this.wsClient.startHealthCheck();
  this.fileWatcher.start();

  await this.performSyncInit();
}

private async performSyncInit() {
  const files = await this.fileWatcher.getAllFiles();
  files.forEach((f) => this.fileWatcher.trackOpened(f.path, f.modifiedAt));
  this.wsClient.sendSyncInit(this.vaultName, files);
}
```

- [ ] **Step 3: 빌드 확인**

Run: `cd plugin && npm run build`
Expected: 빌드 성공

- [ ] **Step 4: 커밋**

```bash
git add plugin/src/sync.ts
git commit -m "feat: wire reconnected handler and metadata injection into SyncManager"
```

---

### Task 6: main.ts — loadData/saveData 연결

**Files:**
- Modify: `plugin/src/main.ts`

- [ ] **Step 1: connectSync()에서 메타데이터 전달**

```typescript
async connectSync() {
  if (this.syncManager) {
    this.disconnectSync();
  }

  const { serverUrl, token, vaultName, fileMetadata } = this.settings;
  if (!serverUrl || !token || !vaultName) {
    new Notice("Obsidian Sync: Please configure server URL, token, and vault name");
    return;
  }

  this.syncManager = new SyncManager(
    this.app.vault,
    serverUrl,
    token,
    vaultName,
    fileMetadata || {},
    (metadata) => this.saveMetadata(metadata),
  );
  try {
    await this.syncManager.start();
    new Notice("Obsidian Sync: Connected");
  } catch {
    new Notice("Obsidian Sync: Connection failed");
    this.syncManager = null;
  }
}
```

- [ ] **Step 2: saveMetadata 헬퍼 추가**

```typescript
async saveMetadata(metadata: Record<string, string>) {
  this.settings.fileMetadata = metadata;
  await this.saveData(this.settings);
}
```

- [ ] **Step 3: 빌드 확인**

Run: `cd plugin && npm run build`
Expected: 빌드 성공, 에러 없음

- [ ] **Step 4: 커밋**

```bash
git add plugin/src/main.ts
git commit -m "feat: connect fileMetadata persistence through loadData/saveData"
```

---

### Task 7: 통합 빌드 + 수동 검증

**Files:**
- 전체 빌드

- [ ] **Step 1: 전체 빌드 확인**

Run: `cd plugin && npm run build`
Expected: 에러 없이 빌드 성공

- [ ] **Step 2: 수동 검증 체크리스트**

Obsidian에서 플러그인 로드 후:
1. 서버 연결 → sync_init 정상 발송 확인
2. 파일 생성/수정/삭제 → 서버 전송 확인
3. `.obsidian/plugins/` 내 파일 변경 → 서버 전송 안 됨 확인
4. WebSocket 끊기 (서버 중지) → 30초 내 재연결 시도 확인
5. 서버 재시작 → 자동 재연결 + sync_init 재발송 확인
6. 플러그인 재시작 → data.json에서 fileMetadata 복원 확인
7. 복원된 상태에서 sync_init → 변경 없는 파일 다운로드 안 됨 확인
