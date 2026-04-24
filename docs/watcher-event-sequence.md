# Watcher Event Sequence

동기화 큐는 서버 기능이 아니라 클라이언트 내부 선처리 기능이다.
서버는 큐를 알지 못하고, 클라이언트가 필요한 큐를 비운 뒤 `syncInit`을 보낸다.

클라이언트 큐는 두 종류로 나눈다.

- `deleteQueue`: 삭제 의도를 보존하는 영속 큐
- `dirtyQueue`: create/modify 이벤트를 순차 처리하는 메모리 큐

`selfWriteSuppress`는 큐가 아니라 watcher 이벤트 중복 방지용 메모리 상태다.
`blockedPaths`는 자동 처리할 수 없는 conflict path를 `syncInit`에서 제외하기 위한 메모리 상태다.

모든 worker와 `syncInit`은 하나의 전역 `syncWorkerMutex`를 공유한다.
네트워크 전송 단위에서 `deleteQueue`, `dirtyQueue`, `syncInit`이 동시에 실행되면 같은 path에 대해 `filePut`과 `fileDelete`가 뒤섞일 수 있기 때문이다.
실행 우선순위는 항상 `deleteQueue` 다음 `dirtyQueue` 다음 `syncInit`이다.

## 목적

파일 삭제는 watcher 이벤트가 발생한 시점에 서버로 바로 전송되지 못할 수 있다.
오프라인 상태이거나 연결이 끊긴 상태에서 삭제가 발생하면 삭제 의도가 사라지고,
다음 `syncInit`에서 서버 파일을 다시 다운로드할 수 있다.

이를 막기 위해 클라이언트는 삭제 이벤트를 durable queue에 먼저 저장한다.

## 저장 범위

영속 저장이 필요한 상태와 메모리에만 두는 상태를 분리한다.

- `deleteQueue`: 영속 저장한다.
- `dirtyQueue`: 메모리에만 저장한다.
- `selfWriteSuppress`: 메모리에만 저장한다.
- `blockedPaths`: 메모리에만 저장한다.
- worker running flag 또는 mutex: 메모리에만 저장한다.

`dirtyQueue`는 재시작 중 유실되어도 로컬 파일이 남아 있으므로 다음 `syncInit`이 현재 파일 hash를 다시 계산해 처리할 수 있다.
`selfWriteSuppress`는 watcher 중복 방지용 임시 상태라서 영속화하면 이후 사용자 수정을 잘못 무시할 수 있다.
`blockedPaths`는 같은 세션에서 같은 conflict를 반복 표시하지 않기 위한 임시 상태다.
재시작 후에는 사라져도 되며, `syncInit`이 현재 서버/로컬 상태를 다시 판단한다.

## deleteQueue 저장 시점

1. Obsidian 파일 삭제 watcher가 발생한다.
2. 클라이언트 메타에 해당 경로의 `baseVersion`이 있으면 삭제 큐에 저장한다.
3. 메타가 없으면 서버에 삭제할 근거가 없으므로 큐에 저장하지 않는다.

큐 항목은 최소한 다음 값을 가진다.

- `path`
- `baseVersion`
- `serverHash`
- `queuedAt`
- `status`

`serverHash`는 queued 당시 클라이언트 메타에 저장돼 있던 마지막 known server hash다.
표시, 디버깅, 충돌 화면 context 용도다.
삭제 성공 판단에는 사용하지 않고, 서버 active의 `serverVersion` 또는 tombstone의 `deletedFromVersion`과 큐의 `baseVersion`을 비교한다.

`deleteQueue`는 path-keyed queue다.
같은 path의 delete 항목이 이미 있으면 새 항목을 추가하지 않는다.
기존 항목의 `baseVersion`과 `serverHash`는 최초 삭제 의도를 보존하기 위해 덮어쓰지 않고,
필요하면 표시용 `queuedAt`만 갱신한다.

## deleteQueue 저장 방식

`deleteQueue` 파일은 직접 덮어쓰지 않는다.
큐 파일이 쓰는 도중 앱이 종료되면 JSON이 깨지고 삭제 의도가 유실될 수 있기 때문이다.

저장 방식은 다음 순서를 따른다.

1. 메모리 큐를 수정한다.
2. 전체 큐 내용을 같은 디렉터리의 임시 파일에 쓴다.
3. 임시 파일 쓰기가 성공하면 실제 큐 파일로 rename한다.

같은 디렉터리 안의 rename은 대부분의 파일시스템에서 atomic하게 동작하므로,
다음 실행 때 기존 큐 파일 또는 새 큐 파일 중 하나를 보게 된다.
앱 시작 시 남아 있는 임시 파일은 이전 저장 중 중단된 흔적으로 보고 정리한다.
런타임과 파일시스템이 지원하면 임시 파일 flush와 parent directory fsync를 best-effort로 수행한다.

`setInterval` worker, watcher callback, 앱 시작 flush가 동시에 큐 파일을 건드리지 않도록
`DeleteQueue` 모듈 하나가 읽기/쓰기 책임을 갖고 내부 mutex로 직렬화한다.

## deleteQueue 처리 시점

삭제 큐는 `setInterval` 기반 worker가 별도로 처리한다.
worker는 전역 `syncWorkerMutex`를 획득한 경우에만 실행한다.

처리 순서는 다음과 같다.

1. 서버 연결이 없으면 이번 interval은 건너뛴다.
2. `syncWorkerMutex`가 이미 잡혀 있으면 이번 interval은 건너뛴다.
3. `pending` 항목을 순차적으로 읽는다.
4. 처리 시점에 로컬 파일이 다시 생겼으면 삭제 전파를 취소하고 큐에서 제거한 뒤 같은 path를 `dirtyQueue`에 등록한다.
   이미 dirty 항목이 있으면 유지하고, 기존 `baseVersion` 또는 tombstone `baseVersion`은 덮어쓰지 않는다.
5. 로컬 파일이 없으면 `fileDelete` 메시지를 서버에 보낸다.
6. 서버 응답이 `okUpdateMeta`이면 응답의 `serverVersion`, `serverHash`, `isDeleted`로 로컬 메타를 갱신한 뒤 큐에서 제거한다.
7. 서버 응답이 `okRemoveMeta`이면 로컬 메타를 제거한 뒤 큐에서 제거한다.
8. 서버 응답이 `deleteConflict`이면 큐 항목을 제거하고 `blockedPaths`에 `deleteConflict` 상태로 등록한다.
9. 네트워크 오류, timeout, 서버 5xx 같은 transient failure가 발생하면 큐 항목은 `pending` 또는 `retryableFailed` 상태로 남긴다.

transient failure가 하나라도 있으면 `syncInit`은 시작하지 않는다.
클라이언트는 Obsidian 메시지로 "서버 연결이 불안전해서 동기화가 중지됩니다"를 표시한다.
다음 interval 또는 재연결 시 삭제 큐 처리를 다시 시도한다.

## dirtyQueue

create/modify watcher도 즉시 서버로 보내지 않고 `dirtyQueue`에 넣는다.
서버 동기화 중 watcher 이벤트가 발생하거나 연결이 끊기면 이벤트가 누락될 수 있기 때문이다.

`dirtyQueue`는 삭제 큐와 달리 영속 저장하지 않는다.
같은 path 이벤트는 모두 보존하지 않고 마지막 상태만 남긴다.

예시 구조는 다음과 같다.

- `path`
- `baseVersion`
- `queuedAt`
- `lastSeenHash`
- `sentHash`
- `status`

처음 enqueue할 때 현재 로컬 메타의 `baseVersion`을 캡처한다.
같은 path에서 create/modify 이벤트가 여러 번 발생하면 `queuedAt`과 `lastSeenHash`만 갱신하고 `baseVersion`은 덮어쓰지 않는다.
중간 이벤트보다 최종 파일 내용이 중요하지만, 충돌 판정 기준은 최초 변경이 시작된 기준 버전이어야 하기 때문이다.

`dirtyQueue`는 path-keyed coalescing queue다.
worker가 항목을 처리할 때는 queue lock 안에서 `pending` 또는 `retryableFailed` 항목을 `inFlight`로 claim하고,
`path`, `baseVersion`, claim 당시의 `lastSeenHash`만 snapshot으로 가져간다.
파일 읽기와 hash 계산은 queue lock 밖에서 수행한다.
전송할 실제 content hash를 계산한 뒤 다시 queue lock을 잡고 그 hash를 `sentHash`로 기록한다.
이때 watcher가 아직 같은 항목의 `lastSeenHash`를 갱신하지 않았고 claim 당시 hash와 같다면,
worker가 읽은 실제 hash를 `lastSeenHash`에도 반영할 수 있다.
watcher가 이미 더 최신 hash를 기록했다면 `lastSeenHash`는 덮어쓰지 않는다.

같은 path가 `inFlight`인 동안 watcher 이벤트가 들어오면 `status`, `baseVersion`, `sentHash`는 유지하고
`queuedAt`과 `lastSeenHash`만 최신 파일 상태로 갱신한다.

처리 순서는 다음과 같다.

1. 서버 연결이 없으면 이번 interval은 건너뛴다.
2. `syncWorkerMutex`가 이미 잡혀 있으면 이번 interval은 건너뛴다.
3. dirty 항목을 순차적으로 claim한다.
4. 로컬 파일이 없고 `baseVersion`이 있으면 먼저 `deleteQueue`에 atomic 저장한 뒤 dirty 항목을 제거한다.
5. 로컬 파일이 없고 `baseVersion`이 없으면 dirty 항목만 제거한다.
6. 로컬 파일을 다시 읽고 현재 hash를 계산한다.
7. 실제 전송할 content hash를 `sentHash`로 기록한다.
8. `fileCheck` 메시지를 서버에 보낸다.
9. `fileCheckResult upToDate`이면 응답의 서버 메타로 로컬 메타를 보정하고 dirty 완료 처리를 실행한다.
10. `fileCheckResult updateMeta`이면 응답의 서버 메타를 저장하고 dirty 완료 처리를 실행한다.
11. `fileCheckResult put`이면 dirty 항목이 캡처한 `baseVersion`과 실제 전송 snapshot으로 `filePut`을 보낸다.
12. `filePutResult okUpdateMeta`이면 응답의 서버 메타를 저장하고 dirty 완료 처리를 실행한다.
13. `fileCheckResult toDeleteLocal` 또는 `filePutResult toDeleteLocal`이면 `selfWriteSuppress`에 delete suppress를 등록하고 로컬 파일을 삭제한 뒤 tombstone 메타를 저장하고 dirty 항목을 제거한다.
14. `conflict`, `deleteConflict`이면 dirty 항목을 제거하고 `blockedPaths`에 등록한다. 충돌 UI에 보여줄 로컬 내용은 완료 시점의 파일을 다시 읽어 최신 상태를 사용한다.
15. transient failure가 발생하면 서버 반영이 없으므로 `baseVersion`은 유지하고 dirty 항목을 `pending` 또는 `retryableFailed`로 되돌려 다음 interval 또는 재연결 때 최신 `lastSeenHash`로 재시도한다.

dirty 완료 처리는 queue lock 안에서 현재 항목과 `sentHash`를 비교한다.
현재 항목이 없으면 이미 다른 흐름이 처리한 것이므로 종료한다.
`lastSeenHash == sentHash`이면 처리 중 새 변경이 없으므로 dirty 항목을 제거한다.
`lastSeenHash != sentHash`이면 처리 중 새 변경이 있었으므로 dirty 항목을 유지하고,
`baseVersion`만 방금 성공한 응답의 `serverVersion`으로 rebase한 뒤 `sentHash`를 비우고 `pending`으로 되돌린다.

같은 path에 `deleteQueue` 항목이 생기면 `dirtyQueue` 항목은 제거한다.
삭제 의도가 수정 업로드보다 우선한다.

`dirtyQueue`는 자동 재시도 대상만 보관한다.
`conflict`, `deleteConflict`는 자동 재시도 대상이 아니므로 큐에 계속 남기지 않고 `blockedPaths`로 옮긴다.
`blockedPaths`에 있는 path는 사용자가 충돌을 해결하거나 파일을 다시 수정하기 전까지 자동 업로드하지 않는다.
같은 path에 새 create/modify watcher 이벤트가 들어오면 기존 `blockedPaths` 항목을 해제하고 새 dirty 항목으로 다시 등록한다.

## blockedPaths

`blockedPaths`는 자동 처리할 수 없는 path를 `syncInit` 대상에서 제외하기 위한 메모리 상태다.

예시 구조는 다음과 같다.

- `path`
- `reason`: `conflict`, `deleteConflict`
- `serverVersion`
- `serverHash`
- `isDeleted`
- `createdAt`

처리 규칙은 다음과 같다.

1. `deleteQueue` 또는 `dirtyQueue` 처리 중 `conflict`, `deleteConflict`가 발생하면 해당 path를 `blockedPaths`에 등록한다.
2. 등록된 path는 `syncInit` 파일 스캔과 서버 결과 적용 대상에서 제외한다.
3. 제외하는 이유는 같은 conflict를 같은 세션에서 반복 표시하지 않기 위해서다.
4. 사용자가 conflict UI에서 해결하면 `blockedPaths`에서 제거하고, 선택한 해결 방식에 따라 `dirtyQueue` 또는 `deleteQueue`에 다시 넣는다.
5. 같은 path에 새 create/modify watcher 이벤트가 들어오면 `blockedPaths`에서 제거하고 `dirtyQueue`에 넣는다.
6. 같은 path에 delete watcher 이벤트가 들어오면 `blockedPaths`에서 제거한다. 로컬 메타에 `baseVersion`이 있으면 `deleteQueue`에 atomic 저장하고, 없으면 제거만 한다.
7. 앱 재시작 시 `blockedPaths`는 사라진다. 재시작 후 `syncInit`이 현재 상태를 다시 판단한다.

## selfWriteSuppress

`selfWriteSuppress`는 플러그인이 서버 내용을 로컬에 쓰거나 로컬 파일을 삭제하면서 발생하는 watcher 이벤트를 무시하기 위한 메모리 상태다.

플러그인이 직접 파일을 쓰거나 삭제하기 직전에 path를 suppress 목록에 등록한다.
watcher 이벤트가 들어오면 suppress 목록과 비교해 플러그인 자신이 만든 이벤트인지 판단한다.

권장 구조는 다음과 같다.

- `path`
- `operation`: `write` 또는 `delete`
- `expectedHash`: `write`에서만 사용
- `until`

처리 규칙은 다음과 같다.

1. 서버 다운로드처럼 플러그인이 실제 파일 내용을 쓰기 직전에는 `operation=write`, `expectedHash`, 만료 시각을 저장한다.
2. `toDeleteLocal`처럼 플러그인이 파일을 삭제하기 직전에는 `operation=delete`와 만료 시각을 저장한다.
3. watcher 이벤트가 오면 suppress 항목의 operation을 확인한다.
4. `write` 항목은 현재 파일 hash가 `expectedHash`와 같으면 self-write 이벤트로 보고 무시한다.
5. `delete` 항목은 파일이 없으면 self-delete 이벤트로 보고 무시한다.
6. 예상과 다르면 사용자가 다시 수정한 것으로 보고 `dirtyQueue` 또는 `deleteQueue`에 넣는다.
7. 만료된 suppress 항목은 제거한다.

`selfWriteSuppress`는 재시작 후 유지하지 않는다.
재시작 후에는 `syncInit`이 실제 로컬/서버 상태를 다시 판단한다.

## syncInit 관계

`syncInit`은 필요한 클라이언트 큐 처리가 먼저 끝난 뒤 실행한다.

앱 시작 또는 재연결 시 순서는 다음과 같다.

1. 전역 `syncWorkerMutex` 획득
2. 영속 `deleteQueue` 처리 로직 실행
3. 자동 처리 가능한 pending delete를 모두 처리
4. 성공한 항목은 큐에서 제거
5. 충돌 항목은 `blockedPaths`로 옮기고 자동 처리 대상에서 제외
6. 앱이 살아 있는 재연결이면 메모리 `dirtyQueue` 처리 로직 실행
7. transient failure가 없을 때만 `syncInit` 실행
8. transient failure가 있으면 Obsidian 메시지를 표시하고 동기화를 중지
9. 작업이 끝나면 `syncWorkerMutex` 해제

앱 재시작에서는 `dirtyQueue`가 메모리에만 있었기 때문에 사라진다.
이 경우 `deleteQueue`만 먼저 비우고 `syncInit`을 실행한다.
로컬 파일 수정 내용은 파일 자체에 남아 있으므로 `syncInit`에서 다시 감지한다.

앱이 살아 있는 단순 재연결에서는 메모리 `dirtyQueue`가 남아 있을 수 있다.
이 경우 `deleteQueue`를 먼저 비운 뒤 `dirtyQueue`를 비우고 `syncInit`을 실행한다.
이 오케스트레이션 중에는 이미 전역 mutex를 잡고 있으므로 내부 queue 처리 로직은 mutex를 다시 획득하지 않는다.

`syncInit`은 `blockedPaths`에 있는 path를 제외하고 처리한다.

`syncInit`에서 `클라이언트 파일 없음 + baseVersion 있음 + 서버 active`가 발견되면,
삭제 큐 처리가 실패했거나 서버가 이후 변경된 상태로 보고 `deleteConflict`로 처리한다.

## Create/Modify Watcher

create/modify watcher는 `dirtyQueue`에 path를 넣고 즉시 서버로 전송하지 않는다.
다만 서버에서 내려받거나 플러그인이 직접 쓴 파일 때문에 다시 watcher가 도는 self-write 이벤트는 무시한다.

처리 순서는 다음과 같다.

1. Obsidian 파일 create 또는 modify watcher가 발생한다.
2. debounce를 적용해 짧은 시간 안의 중복 이벤트를 하나로 묶는다.
3. self-write 이벤트면 무시한다.
4. 파일이 실제로 존재하지 않으면 `baseVersion` 유무에 따라 `deleteQueue` 저장 또는 dirty 제거만 수행한다.
5. 파일이 존재하면 `dirtyQueue[path]`를 저장하거나 갱신한다. 최초 enqueue 때 캡처한 `baseVersion`은 같은 path의 후속 이벤트로 덮어쓰지 않는다.
6. dirty worker가 현재 파일 내용을 다시 읽어 `fileCheck`와 `filePut`을 순차 처리한다.

삭제 후 같은 이름으로 파일을 다시 생성한 경우에도 기존 tombstone 메타의 `baseVersion`을 그대로 사용한다.
이 `baseVersion`은 서버가 tombstone 이후 다른 변경이 있었는지 판단하는 기준이다.
서버 tombstone의 `serverVersion`과 클라이언트 `baseVersion`이 같고 로컬 hash가 tombstone hash와 다르면,
클라이언트는 같은 경로를 새 내용으로 다시 생성하려는 것으로 보고 `filePut` 흐름을 탄다.

## Response Payload Contract

응답 이름은 모두 camelCase를 사용한다.
응답별 필수 payload와 후속 액션은 다음과 같다.

- `none`: payload 없음. 후속 액션 없음.
- `toDownload`: 서버 파일 내용, `serverVersion`, `serverHash`, `isDeleted=false`. 클라이언트는 파일을 내려받고 메타를 저장한다.
- `toPut`: 후속 `filePut` 필요. 클라이언트는 `syncInit` 판단에 사용한 자기 메타의 `baseVersion`을 그대로 `filePut`에 전달한다. 서버가 없는 신규 파일이면 `baseVersion=null`을 전달한다.
- `put`: 후속 `filePut` 필요. 클라이언트는 `fileCheck`에 사용한 자기 메타의 `baseVersion`을 그대로 `filePut`에 전달한다. 서버가 없는 신규 파일이면 `baseVersion=null`을 전달한다.
- `toUpdateMeta`, `updateMeta`, `okUpdateMeta`: `serverVersion`, `serverHash`, `isDeleted`. 클라이언트는 로컬 메타를 이 값으로 갱신한다.
- `toRemoveMeta`, `okRemoveMeta`: path. 클라이언트는 해당 path의 로컬 메타를 제거한다.
- `upToDate`: `serverVersion`, `serverHash`, `isDeleted=false`. 클라이언트는 필요하면 메타만 보정하고 파일은 건드리지 않는다.
- `toDeleteLocal`: tombstone `serverVersion`, `serverHash`, `isDeleted=true`. 클라이언트는 `selfWriteSuppress`에 delete suppress를 등록하고 로컬 파일을 삭제한 뒤 tombstone 메타를 저장한다.
- `conflict`: 로컬 상태와 서버 active 상태를 비교할 수 있는 서버 파일 내용, `serverVersion`, `serverHash`, `isDeleted=false`.
- `deleteConflict`: 서버 active 또는 tombstone 메타. 서버가 tombstone이면 `isDeleted=true`를 포함한다.

서버는 후속 `filePut`에 쓸 새 `baseVersion`을 응답으로 보정해주지 않는다.
클라이언트가 서버가 알려준 최신 버전을 무지성으로 `filePut`에 넣으면 충돌이 사라지기 때문이다.
따라서 `toPut`과 `put`은 "업로드하라"는 액션만 의미하고, 업로드 기준 버전은 클라이언트가 이미 가진 메타를 사용한다.

## Server Version Rules

서버의 `serverVersion`은 path 단위로 단조 증가한다.
파일 생성, 파일 수정, 파일 삭제, tombstone에서 같은 이름으로 재생성할 때 모두 새 버전을 발급한다.

삭제는 서버 레코드를 없애는 동작이 아니라 tombstone으로 상태를 바꾸는 동작이다.
예를 들어 active 파일의 `serverVersion`이 5라면,
`fileDelete` 성공 후 tombstone은 `serverVersion` 6, 삭제 직전 내용의 `serverHash`, `deletedFromVersion` 5를 가진다.
현재 DB 스키마에서 `deletedFromVersion`은 저장 컬럼이 아니라 `tombstone.serverVersion - 1`로 계산하는 도메인 값이다.

클라이언트가 같은 이름으로 파일을 다시 만들면 tombstone 메타의 `baseVersion` 6을 그대로 사용한다.
서버 tombstone의 현재 `serverVersion`도 6이면 `filePut`은 새 active 파일을 만들고 `serverVersion` 7을 반환한다.
서버 tombstone의 현재 `serverVersion`이 6이 아니면 마지막 동기화 이후 서버 상태가 바뀐 것이므로 `deleteConflict` 또는 `conflict`로 처리한다.

`fileDelete`가 tombstone 상태에 다시 도착하면 `baseVersion == deletedFromVersion`일 때만 idempotent retry로 처리한다.
이는 이전 `fileDelete`는 성공했지만 응답이 유실된 경우를 안전하게 마무리하기 위한 예외다.
이 경우 서버는 현재 tombstone 메타를 `okUpdateMeta`로 반환하고, 클라이언트는 메타를 갱신한 뒤 `deleteQueue` 항목을 제거한다.
`baseVersion != deletedFromVersion`이면 클라이언트 삭제 기준과 서버 tombstone의 삭제 기준이 다르므로 `deleteConflict`로 처리한다.
삭제 재시도는 삭제 의도를 확인하는 동작이므로 tombstone의 `serverHash`와 deleteQueue의 표시용 `serverHash`를 비교하지 않는다.

## 서버 책임

서버는 `deleteQueue`, `dirtyQueue`, `selfWriteSuppress`를 저장하지 않는다.
서버는 클라이언트가 보낸 `fileCheck`, `filePut`, `fileDelete`, `syncInit` 메시지만 처리한다.

`fileDelete` 처리 기준은 `message-matrix.csv`를 따른다.
