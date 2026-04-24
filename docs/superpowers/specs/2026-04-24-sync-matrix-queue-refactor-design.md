# Sync Matrix and Watcher Queue Refactor Design

작성일: 2026-04-24

기준 문서:

- `docs/message-matrix.csv`
- `docs/watcher-event-sequence.md`

## 목표

서버 동기화 판정과 클라이언트 watcher 처리를 문서 기준으로 재정렬한다.
`message-matrix.csv`는 서버 메시지별 판정의 소스 오브 트루스이고,
`watcher-event-sequence.md`는 클라이언트 watcher, queue, worker orchestration의 소스 오브 트루스다.

현재 코드와 문서가 다르면 문서를 우선한다. 다만 문서 자체에 모순이나 구현상 위험한 해석이 있으면 구현 전에 사용자에게 확인한다.

이번 작업은 하위 호환을 유지하지 않는 대규모 리팩토링이다. 서버와 플러그인은 같은 릴리스에서 새 프로토콜로 함께 교체한다.

## 범위

포함한다.

- 서버 sync 판정 엔진 재구성
- WebSocket 메시지 타입과 action 이름 정리
- 기존 `file_create`, `file_update` 제거 및 `filePut` 통합
- `syncInit`, `fileCheck`, `filePut`, `fileDelete`의 공통 판정 모델 도입
- 클라이언트 `deleteQueue`, `dirtyQueue`, `blockedPaths`, `selfWriteSuppress`, `syncWorkerMutex` 도입
- 서버 Go 테스트와 클라이언트 Vitest 추가

포함하지 않는다.

- 하위 호환 프로토콜 유지
- 사용자 UI 전면 개편
- 서버 DB 스키마 변경

## 전체 구조

시스템은 네 층으로 나눈다.

1. 서버 도메인 판정 계층
2. WebSocket 프로토콜 계층
3. 클라이언트 queue 계층
4. 클라이언트 worker orchestration 계층

서버 핸들러는 비즈니스 판정을 직접 갖지 않고, 표준화된 입력을 판정 엔진에 전달한 뒤 결과를 프로토콜 응답과 DB/storage side effect로 변환한다.

클라이언트 watcher는 서버 메시지를 직접 보내지 않는다. watcher는 queue 상태만 갱신하고, worker가 전역 mutex 아래에서 네트워크 전송을 수행한다.

## 서버 판정 모델

서버 판정 엔진은 메시지 종류별로 다른 코드 경로를 갖더라도 같은 입력 모델을 사용한다.

- 클라이언트 파일 존재 여부
- 클라이언트 `baseVersion`
- 클라이언트 hash
- 서버 파일 상태: 없음, active, tombstone
- 서버 `serverVersion`
- 서버 `serverHash`
- tombstone의 삭제 기준 버전

현재 DB는 삭제 시 `version = version + 1` 규칙을 사용한다. 따라서 tombstone의 삭제 기준 버전은 물리 컬럼으로 저장하지 않고 `serverVersion - 1`로 계산한다. 코드에서는 이 값을 `deletedFromVersion` 도메인 값으로 노출해 문서 용어와 맞춘다.

`deleted_from_version` 컬럼은 추가하지 않는다. 현재 규칙에서는 중복 상태가 되며, `version`과 어긋날 수 있기 때문이다.

## 서버 프로토콜

새 프로토콜은 문서의 메시지 용어에 맞춘다.

| 방향 | 타입 | 용도 |
|---|---|---|
| C→S | `syncInit` | 초기 동기화 또는 재연결 동기화 |
| S→C | `syncResult` | 초기 동기화 판정 결과 |
| C→S | `fileCheck` | 단건 파일 상태 확인 |
| S→C | `fileCheckResult` | 단건 파일 판정 결과 |
| C→S | `filePut` | 신규 생성과 수정을 통합한 쓰기 |
| S→C | `filePutResult` | 쓰기 결과 |
| C→S | `fileDelete` | 삭제 전파 |
| S→C | `fileDeleteResult` | 삭제 결과 |
| C→S | `conflictResolve` | 사용자 선택 기반 충돌 해소 |
| S→C | `conflictResolveResult` | 충돌 해소 결과 |

기존 `file_create`, `file_update`, `file_create_result`, `file_update_result`는 제거한다.

응답 action은 CSV 용어를 따르며 응답 타입별로 구분한다.

| 응답 타입 | action |
|---|---|
| `syncResult` | `toPut`, `toUpdateMeta`, `toDownload`, `toDeleteLocal`, `toRemoveMeta`, `none`, `conflict`, `deleteConflict` |
| `fileCheckResult` | `put`, `updateMeta`, `toDeleteLocal`, `upToDate`, `conflict`, `deleteConflict` |
| `filePutResult` | `okUpdateMeta`, `toDeleteLocal`, `conflict`, `deleteConflict` |
| `fileDeleteResult` | `okUpdateMeta`, `okRemoveMeta`, `deleteConflict` |

필드 이름도 문서 용어에 맞춰 정리한다. 기존 `prevServerVersion`은 프로토콜에서 `baseVersion`으로 바꾼다. 클라이언트 내부 메타 필드는 구현 중 한 번에 바꿔도 되고, 경계에서 변환해도 되지만 최종 프로토콜은 `baseVersion`을 사용한다.

## `filePut`

`filePut`은 새 파일 생성과 기존 파일 수정을 모두 처리한다.

- `baseVersion`이 없으면 서버가 해당 path를 처음 보는 클라이언트 쓰기로 판단한다.
- `baseVersion`이 있으면 서버 active 또는 tombstone 기준으로 낙관적 쓰기를 판단한다.
- tombstone 상태에서 `baseVersion == serverVersion`이고 클라이언트 hash가 tombstone hash와 다르면 삭제 후 같은 이름으로 다시 생성한 것으로 보고 active 파일로 복원한다.
- 서버 판정이 쓰기를 허용하지 않으면 storage/DB를 변경하지 않고 문서 매트릭스의 결과를 반환한다.

`filePutResult okUpdateMeta`는 서버가 파일을 저장했거나, 이미 같은 내용이라 메타만 갱신하면 되는 경우에 사용한다.

## `fileDelete`

`fileDelete`는 로컬 파일이 없는 상태에서 삭제 의도를 서버에 전파한다.

- `baseVersion`이 없고 서버 active가 있으면 `deleteConflict`다.
- `baseVersion`이 active `serverVersion`과 같으면 tombstone을 생성한다.
- 서버 tombstone이 이미 있고 `baseVersion == deletedFromVersion`이면 같은 삭제 요청의 재시도로 보고 `okUpdateMeta`를 반환한다.
- 서버 레코드가 없으면 `okRemoveMeta`를 반환한다.
- tombstone의 `deletedFromVersion`은 현재 스키마에서 `serverVersion - 1`로 계산한다.

`fileDelete`는 멱등이어야 한다. 네트워크 실패나 앱 종료 후 deleteQueue가 같은 삭제를 다시 보내도 안전해야 한다.

## 클라이언트 상태

클라이언트 상태는 저장 범위에 따라 분리한다.

| 상태 | 저장 위치 | 목적 |
|---|---|---|
| `FileMetaStore` | Obsidian plugin data | 서버에 마지막으로 성공 반영된 로컬 snapshot의 서버 메타 |
| `DeleteQueue` | 별도 JSON 파일 | 삭제 의도를 재시작 후에도 보존 |
| `DirtyQueue` | 메모리 | create/modify 최신 상태를 coalescing |
| `BlockedPaths` | 메모리 | 자동 처리 불가능한 충돌 path 제외 |
| `SelfWriteSuppress` | 메모리 | 플러그인 자신이 만든 watcher 이벤트 무시 |
| `syncWorkerMutex` | 메모리 | 네트워크 전송 단위 직렬화 |

## `DirtyQueue`

`DirtyQueue`는 `Map<path, DirtyEntry>` 기반 path-keyed coalescing queue다.

```ts
type DirtyEntry = {
  path: string;
  baseVersion?: number;
  queuedAt: number;
  lastSeenHash: string;
  status: "pending" | "inFlight" | "retryableFailed";
  sentHash?: string;
};
```

같은 path의 처리되지 않은 create/modify 이벤트는 새 항목을 만들지 않고 기존 항목의 `lastSeenHash`, `queuedAt`만 갱신한다.

`inFlight` 중 같은 path 수정이 들어와도 `status`, `sentHash`, `baseVersion`은 유지하고 `lastSeenHash`만 최신 파일 hash로 바꾼다.

worker는 queue lock 안에서 항목을 짧게 claim한다.

1. `pending` 또는 `retryableFailed` 항목 하나를 고른다.
2. 항목을 `inFlight`로 바꾼다.
3. `path`, `baseVersion`, claim 당시의 `lastSeenHash` snapshot을 반환한다.
4. 파일 읽기와 hash 계산은 queue lock 밖에서 수행한다.
5. 실제 전송할 content hash를 계산한 뒤 queue lock을 다시 잡고 그 hash를 `sentHash`로 기록한다.
6. `fileCheck`, `filePut` 네트워크 요청은 queue lock 밖에서 수행한다.

`sentHash`는 claim 시점의 관측값이 아니라 실제 전송할 content의 hash다.
파일 읽기 중 watcher가 같은 항목을 갱신하지 않았고 현재 `lastSeenHash`가 claim 당시 hash와 같다면,
worker가 읽은 실제 hash를 `lastSeenHash`에도 반영할 수 있다.
이미 watcher가 더 최신 hash를 기록했다면 `lastSeenHash`는 덮어쓰지 않는다.

성공 응답이 오면 먼저 `FileMetaStore`를 성공 반영된 snapshot 기준으로 갱신한다. 그 다음 queue lock 안에서 현재 entry를 확인한다.

- `entry.lastSeenHash == sentHash`이면 처리 중 새 변경이 없으므로 entry를 삭제한다.
- 다르면 처리 중 새 변경이 있었으므로 entry를 유지하고 `baseVersion`만 성공 응답의 최신 서버 버전으로 rebase한 뒤 `pending`으로 돌린다.

따라서 `FileMetaStore`가 H1/v11을 가리키고 실제 로컬 파일 및 `DirtyQueue.lastSeenHash`가 H2인 상태는 정상이다. 메타는 마지막 성공 snapshot이고, queue는 아직 반영되지 않은 최신 로컬 상태다.

transient failure면 서버 반영이 없으므로 `baseVersion`은 유지하고 `status`만 `retryableFailed` 또는 `pending`으로 되돌린다.

`conflict` 또는 `deleteConflict`면 queue entry를 제거하고 `BlockedPaths`에 등록한다. conflict UI에 보여줄 로컬 내용은 완료 시점에 파일을 다시 읽어 최신 상태를 담는다.

## `DeleteQueue`

`DeleteQueue`는 영속 queue다. 플러그인 데이터 디렉터리에 `delete-queue.json`을 저장한다.

저장 방식은 atomic write를 사용한다.

1. 메모리 queue를 갱신한다.
2. 같은 디렉터리의 임시 파일에 전체 queue JSON을 쓴다.
3. 임시 파일 쓰기가 성공하면 실제 queue 파일로 rename한다.

앱 시작 시 남아 있는 임시 파일은 이전 저장 중 중단된 흔적으로 보고 정리한다.
런타임과 파일시스템이 지원하면 임시 파일 flush와 parent directory fsync를 best-effort로 수행한다.

항목은 다음 값을 가진다.

- `path`
- `baseVersion`
- `serverHash`
- `queuedAt`
- `status`

`DeleteQueue`도 path-keyed queue다.
같은 path의 delete 항목이 이미 있으면 중복 항목을 추가하지 않는다.
기존 항목의 `baseVersion`과 `serverHash`는 최초 삭제 의도를 보존하기 위해 덮어쓰지 않고,
필요하면 표시용 `queuedAt`만 갱신한다.

삭제 watcher가 발생했을 때 로컬 메타가 있으면 delete 항목을 저장한다. 로컬 메타가 없으면 서버에 삭제할 근거가 없으므로 저장하지 않는다.

삭제 처리 중 로컬 파일이 다시 생기면 delete 항목을 제거하고 같은 path를 `DirtyQueue`에 넣는다.

네트워크 중 앱이 죽으면 `pending` delete를 다음 실행에서 재시도한다. 이 안전성은 서버 `fileDelete`의 멱등성에 의존한다.

## `BlockedPaths`

`BlockedPaths`는 자동 처리할 수 없는 path를 같은 세션의 `syncInit` 대상에서 제외하는 메모리 상태다.

- `conflict` 또는 `deleteConflict` 발생 시 등록한다.
- `syncInit` 파일 스캔과 서버 결과 적용에서 제외한다.
- 사용자가 conflict UI에서 해결하면 제거한다.
- 같은 path에 새 create/modify watcher 이벤트가 들어오면 제거하고 `DirtyQueue`에 넣는다.
- 같은 path에 delete watcher 이벤트가 들어오면 제거하고, 메타가 있으면 `DeleteQueue`에 넣는다.
- 앱 재시작 시 사라진다.

## `SelfWriteSuppress`

`SelfWriteSuppress`는 플러그인이 서버 내용을 로컬에 쓰거나 로컬 파일을 삭제하면서 발생하는 watcher 이벤트를 무시하기 위한 메모리 상태다.

항목은 다음 값을 가진다.

- `path`
- `operation`: `write` 또는 `delete`
- `expectedHash`: `write`에서만 사용
- `until`

서버 다운로드 적용 전에는 `write` suppress를 등록하고, expected hash가 watcher 시점의 현재 파일 hash와 같으면 무시한다.

서버 지시에 따른 로컬 삭제 전에는 `delete` suppress를 등록하고, watcher 시점에 파일이 없으면 무시한다.

예상과 다르면 사용자가 다시 수정한 것으로 보고 `DirtyQueue` 또는 `DeleteQueue`에 넣는다.

## Worker Orchestration

모든 worker와 `syncInit`은 하나의 전역 `syncWorkerMutex`를 공유한다.

실행 우선순위는 항상 다음 순서다.

1. `DeleteQueue`
2. `DirtyQueue`
3. `syncInit`

앱 시작과 재연결 시에는 하나의 critical section 안에서 필요한 queue를 먼저 비운 뒤 `syncInit`을 실행한다.

정기 worker는 mutex가 이미 잡혀 있으면 해당 interval을 건너뛴다.

transient failure가 하나라도 있으면 `syncInit`은 시작하지 않는다. 사용자에게 서버 연결 불안정으로 동기화가 중지됐다는 Notice를 표시하고 다음 interval 또는 재연결에서 재시도한다.

각 queue 내부 mutex는 queue 자료구조의 원자성을 지킨다. 전역 `syncWorkerMutex`는 queue worker와 `syncInit` 사이의 네트워크 전송 순서를 지킨다.

## 테스트 전략

구현보다 테스트를 먼저 작성한다.

### 서버

서버는 `message-matrix.csv`의 행 ID를 기준으로 Go 테이블 테스트를 만든다.

각 CSV 행은 `M001` 같은 안정적인 `id`를 가진다.
구현 테스트는 각 행을 명시적인 fixture로 옮기되, 별도 coverage test가 CSV의 모든 row id가 fixture에 존재하는지 검증한다.
한국어 설명 문구 변화에 테스트가 흔들리지 않으면서도 CSV와 테스트가 조용히 drift되지 않게 하기 위해서다.

테스트 우선순위는 다음과 같다.

1. 순수 판정 엔진 테스트
2. `syncInit`, `fileCheck`, `filePut`, `fileDelete` 핸들러의 프로토콜 변환 테스트
3. DB/storage side effect 테스트
4. delete 재시도 멱등성 테스트

### 클라이언트

클라이언트는 Vitest로 queue 단위 테스트부터 작성한다.

- `DirtyQueue`: 같은 path coalescing, `inFlight` 중 수정, 성공 시 삭제/rebase, transient failure 재시도, conflict 시 제거
- `DeleteQueue`: load/save, atomic write, 같은 path dedupe, 성공/충돌 제거, 로컬 재생성 시 dirty로 전환
- `BlockedPaths`: 등록/해제, `syncInit` 제외
- `SelfWriteSuppress`: expected hash 일치 시 무시, 불일치 시 queue 등록
- `SyncOrchestrator`: `deleteQueue -> dirtyQueue -> syncInit` 순서와 mutex skip

## 구현 순서

1. 서버 판정 엔진과 매트릭스 테스트 추가
2. WebSocket 프로토콜을 새 메시지 이름/action으로 정리
3. 서버 핸들러를 판정 엔진 위로 얇게 재구성
4. 클라이언트 queue 모듈 추가 및 Vitest 작성
5. `SyncManager`를 queue/worker orchestration 중심으로 재구성
6. 기존 `file_create`, `file_update` 프로토콜 제거
7. 빌드와 테스트 정리

## 검증 명령

```bash
cd server && rtk go test ./...
cd plugin && rtk npm test && rtk npm run build
```

## 문서 보정 사항

`message-matrix.csv`는 tombstone 비교에서 `serverVersion`과 `deletedFromVersion` 표현이 함께 등장한다.

이번 설계에서는 다음처럼 해석한다.

- tombstone 자체 최신성 판단은 tombstone의 `serverVersion` 기준이다.
- 삭제 요청의 멱등성 및 충돌 검증은 `deletedFromVersion` 기준이다.
- 현재 DB 버전 규칙에서 `deletedFromVersion = serverVersion - 1`이다.

이 해석은 DB 중복 컬럼을 만들지 않고도 문서의 삭제 재시도 의미를 구현한다.
