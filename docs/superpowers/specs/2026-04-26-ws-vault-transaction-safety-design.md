# WebSocket Vault and Transaction Safety Design

작성일: 2026-04-26

## 목표

sync WebSocket 프로토콜이 별도 `vaultCreate` 선행 핸드셰이크에 의존하지 않도록 한다.
서버는 `vault`가 포함된 모든 메시지에서 vault row를 보장하고, 메시지 단위 DB 트랜잭션과 파일시스템 finalize 절차를 통해 DB와 파일 상태의 부분 적용을 줄인다.
플러그인은 서버가 보낸 모든 error payload를 사용자에게 명확히 보여준다.

## 배경

현재 플러그인은 `syncInit`과 `filePut`을 보내지만 sync 시작 경로에서 `vaultCreate`를 보내지 않는다.
서버의 `syncInit`은 vault가 없어도 빈 파일 목록처럼 동작할 수 있고, 이후 `filePut`에서 `files.vault_name -> vaults.name` 외래키 제약으로 실패한다.
또한 기존 `filePut` 경로는 파일 내용을 storage에 먼저 쓴 뒤 DB insert/update를 수행하므로, DB 실패 시 파일만 디스크에 남는 partial write가 가능하다.
서버가 `filePutResult.error` 또는 generic `{type:"error"}`를 보내도 플러그인 result handler가 이를 사용자에게 표시하지 않는 경로가 있다.

## 요구사항

1. sync protocol은 별도 `vaultCreate` 선행 핸드셰이크에 의존하지 않는다.
2. 서버는 `vault`가 포함된 모든 WebSocket 메시지 처리 전에 vault 이름을 검증한다.
3. vault 이름이 비어 있으면 서버는 명시적 error response를 보내고 메시지 처리를 중단한다.
4. vault가 없으면 서버가 자동 생성한다.
5. vault가 이미 있으면 정상으로 간주한다.
6. 각 WebSocket 메시지는 하나의 DB transaction boundary 안에서 처리된다.
7. 메시지 처리 중 DB error가 발생하면 해당 메시지의 DB 변경은 rollback된다.
8. 파일 쓰기/삭제가 필요한 메시지는 DB 상태 조회와 검증을 먼저 수행한다.
9. 파일 쓰기는 최종 경로가 아니라 임시 파일에 먼저 쓴다.
10. DB commit이 성공하면 임시 파일을 최종 경로로 이동한다.
11. DB transaction 실패 또는 rollback 시 임시 파일은 삭제한다.
12. 파일 삭제는 대상 파일을 임시/trash 위치로 이동해 준비하고, DB commit 성공 시 확정 삭제한다.
13. DB rollback 또는 실패 시 삭제 준비 파일은 원래 위치로 복원한다.
14. DB commit 이후 파일 finalize가 실패하면 서버는 강한 로그와 명시적 error response를 남긴다.
15. 플러그인은 generic `{type:"error"}` 메시지를 Notice로 보여준다.
16. 플러그인은 result 메시지의 `error`를 action 처리보다 먼저 Notice로 보여준다.
17. path가 있는 error는 Notice에 path를 포함한다.
18. raw WebSocket payload 로그는 디버깅 목적으로 유지한다.

## 비목표

- `vaultCreate` 메시지 자체를 삭제하지 않는다. 대시보드나 관리 용도로 남아 있어도 된다.
- 파일시스템과 SQLite를 완전한 단일 원자 트랜잭션으로 묶으려 하지 않는다. 대신 temp/trash finalize와 보상 로그로 위험을 줄인다.
- sync matrix 판정 규칙을 바꾸지 않는다.
- 플러그인 conflict UI를 개편하지 않는다.

## 서버 설계

### Vault 보장

서버 DB 계층에 `EnsureVault(name string) error`를 추가한다.
이 함수는 빈 이름을 거부하고, 정상 이름은 `INSERT OR IGNORE`로 생성한다.
모든 WebSocket 메시지는 dispatch 전에 `EnsureVault`를 호출한다.
`vaultCreate`도 같은 함수를 사용해 idempotent하게 동작한다.

### 트랜잭션 경계

DB 계층은 `*sql.DB`와 `*sql.Tx`를 모두 사용할 수 있는 query interface를 갖는다.
root `Queries`는 `InTx(func(*Queries) error) error`를 제공한다.
핸들러는 메시지를 처리할 때 tx-scoped `Queries`를 가진 handler clone을 사용한다.

응답은 transaction 안에서 바로 client로 보내지 않고 recorder에 모은다.
DB commit이 성공하면 recorder의 메시지를 client로 flush한다.
DB 처리 자체가 실패하면 rollback 후 error response를 보낸다.
recorder에 `Error` 필드가 있는 응답이 기록되면 그 메시지는 실패한 메시지 처리로 간주하고 DB transaction을 rollback한다.
rollback 후에는 recorder에 모인 error response를 client로 flush한다.

### 파일 finalize

파일 write/delete는 DB 트랜잭션에 자동 포함되지 않으므로 handler는 메시지 처리 중 파일 finalize 작업을 예약한다.

쓰기:

1. DB 상태를 조회하고 matrix decision을 계산한다.
2. 쓰기가 허용되면 최종 경로가 아니라 temp 경로에 content를 쓴다.
3. DB metadata insert/update를 tx 안에서 수행한다.
4. commit 성공 후 temp 파일을 최종 경로로 rename한다.
5. rollback 또는 DB 실패 시 temp 파일을 삭제한다.

삭제:

1. DB 상태를 조회하고 matrix decision을 계산한다.
2. 삭제가 허용되면 기존 파일을 trash/temp 경로로 rename해 준비한다.
3. DB tombstone update를 tx 안에서 수행한다.
4. commit 성공 후 trash/temp 파일을 삭제한다.
5. rollback 또는 DB 실패 시 trash/temp 파일을 원래 경로로 복원한다.

commit 이후 finalize 실패는 DB rollback이 불가능하다.
이 경우 서버는 `log.Printf`로 강하게 남기고, 해당 message result에 `error`를 포함해 클라이언트가 사용자에게 표시하게 한다.

## 플러그인 설계

`SyncManager.start()`는 generic `"error"` handler를 등록한다.
모든 result handler는 가장 먼저 `msg.error`를 확인한다.
에러가 있으면 `console.error`에 원본 payload를 남기고 `new Notice(...)`를 보여준 뒤, 해당 메시지의 일반 action 처리를 중단한다.

에러 Notice 형식:

- path 있음: `[obsidian-goat-sync] <type> failed for <path>: <error>`
- path 없음: `[obsidian-goat-sync] <type> failed: <error>`

`filePutResult.error`는 dirty queue entry를 retryable로 되돌릴 수 있어야 한다.
`fileDeleteResult.error`는 delete queue entry를 남겨 재시도 가능하게 한다.
`fileCheckResult.error`, `conflictResolveResult.error`, generic error는 사용자 표시와 console 기록을 우선한다.

## 테스트 전략

서버:

- `EnsureVault`가 idempotent하게 vault를 생성하는 DB 테스트.
- 빈 vault 이름이 error를 반환하는 DB 테스트.
- vault row가 없는 상태에서 `filePut`이 성공하고 files row와 vault row가 함께 생기는 handler 테스트.
- DB 실패 시 temp 파일이 삭제되는 storage/handler 테스트.
- DB commit 성공 후 temp 파일이 최종 경로로 이동되는 테스트.
- 삭제 rollback 시 trash 파일이 원래 경로로 복원되는 테스트.

플러그인:

- generic `"error"` 메시지가 Notice를 호출하는 테스트.
- `filePutResult.error`가 Notice를 호출하고 action 처리를 건너뛰는 테스트.
- path가 있는 error Notice에 path가 포함되는 테스트.

## 성공 조건

- fresh server DB에서 플러그인이 바로 sync를 시작해도 FK constraint 에러가 발생하지 않는다.
- `syncInit -> syncResult(toPut) -> filePut` 흐름에서 서버가 vault row를 자동 생성한다.
- DB 실패 시 파일만 디스크에 남는 partial write가 없다.
- 서버 error payload는 플러그인 사용자에게 Notice로 표시된다.
- raw WebSocket 로그는 계속 확인 가능하다.
