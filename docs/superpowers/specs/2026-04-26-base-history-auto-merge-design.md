# Base History and Auto Merge Design

작성일: 2026-04-26

## 목표

서버가 클라이언트의 `baseVersion`에 해당하는 `baseHash`와 `baseContent`를 조회할 수 있도록 path 기반 버전 이력을 저장한다.
그 이력을 사용해 M013/M033/M048 계열의 잦은 conflict를 `toDownload`, `autoMerge 후 toDownload`, `conflict` 세 흐름으로 나눈다.
자동 병합은 텍스트 파일에 한해 Google `diff-match-patch` 기반 wrapper로 시도하고, 실패하면 기존 conflict 흐름으로 내려간다.

## 배경

이 설계는 서버를 canonical file store로 간주한다.
클라이언트 vault는 서버 파일의 로컬 캐시이자 편집 작업공간이다.
서버는 파일별 revision history와 content object를 소유하고, 클라이언트는 마지막으로 동기화한 base revision을 함께 보내 로컬 변경 여부를 증명한다.
현재 프로토콜 필드 이름은 `baseVersion`, `serverVersion`을 유지하지만, 의미상 이 값들은 path별 server revision이다.

현재 서버 DB의 `files` 테이블은 경로별 최신 `version`, `hash`, `is_deleted`만 저장한다.
클라이언트는 `baseVersion`, `baseHash`, `localHash`를 보낼 수 있지만 서버 판정 로직은 `baseHash`를 사용하지 않고, 과거 버전의 content도 조회할 수 없다.

대표 문제는 다음 흐름이다.

1. B 클라이언트가 빈 vault에서 동기화해 서버 v1을 내려받고 메타를 저장한다.
2. A 클라이언트가 같은 파일을 수정해 서버가 v2가 된다.
3. B 클라이언트가 아직 로컬 파일을 수정하지 않았는데 `syncInit` 또는 `fileCheck`를 보낸다.
4. 서버는 `baseVersion != serverVersion`, `localHash != serverHash`만 보고 M013/M033으로 conflict를 만든다.

base 이력이 있으면 같은 조건을 더 정확히 분류할 수 있다.

```text
localHash == baseHash
=> 로컬 미수정, 서버만 변경
=> toDownload

localHash != baseHash + baseContent/serverContent/localContent로 병합 가능
=> 양쪽 수정이지만 자동 병합 가능
=> autoMerge 후 병합 결과를 toDownload 흐름에 포함해 적용

base row 없음 또는 병합 불가
=> conflict
```

## 요구사항

1. 파일 버전 이력은 path 기반 단일 테이블 `file_versions`로 저장한다.
2. `file_versions`에는 서버 내부 row 식별용 surrogate `id` PK를 둔다.
3. `(vault_name, path, version)`은 `UNIQUE`로 보장한다.
4. 클라이언트 프로토콜의 version은 DB `id`가 아니라 path별 `version`이다.
5. 이전 DB 구조 마이그레이션은 작성하지 않는다. 새 스키마 기준으로 코드를 변경한다.
6. 경로별 최신 상태는 버전 이력 중 가장 큰 `version`으로 판단한다.
7. 새 파일 생성, 파일 수정, tombstone 생성은 모두 새 version row insert로 표현한다.
8. 서버는 `baseVersion`이 있는 요청에서 해당 base row의 hash와 content를 조회할 수 있어야 한다.
9. M013/M033/M048 계열은 base-aware 판정으로 `toDownload`, `autoMerge 후 toDownload`, `conflict` 세 흐름을 구분한다.
10. `localHash == baseHash`이면 conflict가 아니라 서버 최신 content를 적용하는 `toDownload`가 된다.
11. `localHash != baseHash`이고 base/server/local content가 병합 가능하면 autoMerge 흐름으로 들어간다.
12. autoMerge 성공 시 서버는 merged content를 새 version으로 저장하고, `toDownload` 응답 형태로 클라이언트에 내려준다.
13. autoMerge 실패, binary/base64 파일, base row 없음은 기존 conflict 흐름을 유지한다.
14. first-run mismatch 행인 M003/M023/M037은 자동 병합하지 않고 기존 conflict 정책을 유지한다.
15. tombstone row의 `hash`와 `content_ref`는 삭제 직전 active version의 값을 유지한다.
16. `docs/message-matrix.csv`에는 base-aware/autoMerge 흐름을 정식 행으로 추가하고, 전체 행 ID를 M001부터 순차 증가하도록 다시 매긴다.
17. 기존 conflict modal UX는 바꾸지 않는다.

## 비목표

- 실시간 공동 편집을 지원하지 않는다.
- CRDT/OT 모델을 도입하지 않는다.
- rename/move를 같은 파일 identity로 추적하지 않는다.
- `file_id` 기반 정규화 스키마를 도입하지 않는다.
- first-run mismatch를 자동 병합하지 않는다.
- 바이너리 파일 병합을 지원하지 않는다.
- Obsidian 설정 JSON의 key-level merge는 별도 설계로 남긴다.
- 의미적 충돌을 완벽하게 감지하려 하지 않는다.

## DB 설계

기존 `files` 테이블은 최신 상태 테이블이 아니라 버전 원장 테이블로 대체한다.
테이블 이름은 `file_versions`로 확정한다.
이전 스키마와의 마이그레이션은 작성하지 않으며, 기존 `files` 참조 코드는 `file_versions` 기준 query helper로 수정한다.

```sql
CREATE TABLE file_versions (
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

CREATE INDEX idx_file_versions_latest
ON file_versions(vault_name, path, version DESC);
```

`id`는 서버 내부 row 식별과 향후 다른 테이블의 단일 컬럼 FK를 위한 surrogate key다.
프로토콜에서 사용하는 파일 버전은 계속 `(vault_name, path, version)`의 `version` 값이며, `id`는 클라이언트에 노출하지 않는다.

`content_ref`는 서버 storage에서 해당 버전의 content를 읽기 위한 참조다.
구현은 content-addressed object storage로 고정한다.
같은 content는 한 번만 저장하고, 여러 version row가 같은 object를 가리킬 수 있다.

```text
content_ref = "sha256:<content-hash>"
object path = "objects/sha256/ab/abcdef..."
```

삭제 버전은 `is_deleted = 1`로 저장한다.
삭제 row의 `hash`와 `content_ref`는 삭제 직전 active version의 값을 유지한다.
이 정책은 기존 tombstone 행의 `localHash == serverHash` 판정과 delete-conflict 응답의 server content 표시를 유지하기 위한 것이다.
삭제 row 자체는 최신 상태가 tombstone임을 나타내고, content는 사용자가 복구/비교할 수 있는 삭제 직전 서버 내용을 가리킨다.

## DB Helper

최신 상태는 `(vault_name, path)`별 가장 큰 `version` row다.
DB helper는 기존 호출부가 크게 흔들리지 않도록 `db.File` shape을 유지한다.

- `GetFile(vault, path)` — latest row 조회.
- `GetFileVersion(vault, path, version)` — 특정 base row 조회.
- `ListActiveFiles(vault)` — latest row 중 `is_deleted = 0`만 조회.
- `CreateFile(vault, path, hash, contentRef, encoding)` — version 1 insert.
- `UpdateFile(vault, path, hash, contentRef, encoding)` — latest version + 1 insert.
- `DeleteFile(vault, path)` — latest version + 1 tombstone insert.
- `CreateFileFromTombstone(vault, path, hash, contentRef, encoding, prevVersion)` — tombstone latest의 다음 version으로 active row insert.

동일 path의 다음 version 계산과 insert는 같은 DB transaction 안에서 수행한다.

## Storage 설계

현재 storage는 path별 최신 파일을 읽고 쓴다.
새 구조에서는 최신 파일 storage와 content-addressed object storage를 함께 유지한다.

쓰기 성공 흐름:

1. WebSocket handler가 temp 파일에 새 content를 stage한다.
2. 같은 content를 object temp path에도 stage하고, 최종 `content_ref`를 미리 계산한다.
3. object temp path를 content-addressed final object path로 finalize한다.
4. object finalize가 성공하면 DB transaction 안에서 새 `file_versions` row를 insert한다.
5. DB commit이 성공하면 latest temp 파일을 최신 path로 finalize한다.
6. latest finalize가 실패하면 DB row의 `content_ref`는 유효하므로 다음 download/merge는 object storage에서 content를 읽을 수 있다. 서버는 error response와 로그를 남기고, consistency check가 latest path 불일치를 감지하게 한다.

서버가 base content를 읽어야 하는 경우 `content_ref`로 object storage를 읽는다.
object finalize는 DB row insert보다 먼저 수행한다.
DB transaction이 실패하면 finalized object가 orphan으로 남을 수 있지만, content-addressed object는 idempotent하고 안전하게 재사용 가능하다.
orphan object 정리는 별도 maintenance 대상이며 sync correctness에는 영향을 주지 않는다.
테스트 helper는 committed row의 `content_ref`가 없는 경우와 latest path가 latest row content와 다른 경우를 감지하는 consistency check를 제공한다.

## Matrix 설계

기존 매트릭스는 해시 비교 축이 `localHash == serverHash`뿐이다.
base가 있는 active 파일 행에는 다음 비교 축을 추가한다.

```text
baseVersion row
localHash == baseHash
autoMerge 가능 여부
```

`docs/message-matrix.csv`에는 다음 컬럼을 추가한다.

- `base row`: `있음` / `없음` / `해당없음`
- `base 해시 비교`: `localHash == baseHash` / `localHash != baseHash` / `해당없음`
- `autoMerge`: `가능` / `불가` / `해당없음`

확장 대상:

- 기존 M013 위치: `syncInit`, active server, `baseVersion != serverVersion`, `localHash != serverHash`
- 기존 M033 위치: `fileCheck`, active server, `baseVersion != serverVersion`, `localHash != serverHash`
- 기존 M048 위치: `filePut`, active server, `baseVersion != serverVersion`, `localHash != serverHash`

새 판정:

```text
base row 있음 + localHash == baseHash
=> 로컬 미수정, 서버만 변경
=> toDownload

base row 있음 + localHash != baseHash + autoMerge 가능
=> 로컬도 바뀌고 서버도 바뀜
=> autoMerge
=> 성공하면 병합 결과를 toDownload로 적용
=> 실패하면 conflict

base row 있음 + localHash != baseHash + autoMerge 불가
=> 로컬도 바뀌고 서버도 바뀜
=> conflict

base row 없음
=> 조상 불명
=> conflict
```

`filePut`은 이미 클라이언트가 content를 보내는 단계지만, base와 같은 content를 서버에 다시 저장하면 안 된다.
따라서 M048의 base-match 분기는 서버 최신을 클라이언트에 적용하도록 `toDownload` 계열 응답을 사용한다.
`filePutResult`는 `action: "toDownload"`와 최신 `content`, `encoding`, `meta`를 포함하도록 프로토콜을 확장한다.
`mergePut` 요청의 응답은 `mergePutResult`를 사용하며, 성공 시 같은 `toDownload` payload shape을 사용한다.

M003/M023/M037은 `baseVersion`이 없으므로 바꾸지 않는다.
서버 history에 같은 hash의 과거 버전이 있더라도, 이번 단계에서는 first-run import 정책을 확장하지 않는다.

`docs/message-matrix.csv`에는 base-aware/autoMerge 행을 정식 행으로 추가한다.
추가 행은 별칭 ID를 쓰지 않고, 전체 표를 M001부터 순차 증가하도록 다시 매긴다.
`server/internal/sync/matrix_test.go` fixture도 새 CSV 행 ID와 1:1로 다시 맞춘다.

확장 행은 기존 M013/M033/M048 위치마다 다음 값으로 삽입한다.

| 메시지 | 기존 위치 | 버전 비교 | 해시 비교 | base row | base 해시 비교 | autoMerge | 결과 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| syncInit | M013 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 있음 | `localHash == baseHash` | 해당없음 | `syncResult.toDownload` |
| syncInit | M013 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 있음 | `localHash != baseHash` | 가능 | `syncResult.toAutoMerge` |
| syncInit | M013 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 있음 | `localHash != baseHash` | 불가 | `syncResult.conflicts` |
| syncInit | M013 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 없음 | 해당없음 | 해당없음 | `syncResult.conflicts` |
| fileCheck | M033 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 있음 | `localHash == baseHash` | 해당없음 | `fileCheckResult.toDownload` |
| fileCheck | M033 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 있음 | `localHash != baseHash` | 가능 | `fileCheckResult.autoMergeRequired` |
| fileCheck | M033 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 있음 | `localHash != baseHash` | 불가 | `fileCheckResult.conflict` |
| fileCheck | M033 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 없음 | 해당없음 | 해당없음 | `fileCheckResult.conflict` |
| filePut | M048 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 있음 | `localHash == baseHash` | 해당없음 | `filePutResult.toDownload` |
| filePut | M048 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 있음 | `localHash != baseHash` | 가능 | 즉시 merge, 성공 시 `filePutResult.toDownload`, 실패 시 `filePutResult.conflict` |
| filePut | M048 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 있음 | `localHash != baseHash` | 불가 | `filePutResult.conflict` |
| filePut | M048 위치 | `baseVersion != serverVersion` | `localHash != serverHash` | 없음 | 해당없음 | 해당없음 | `filePutResult.conflict` |

## 자동 병합 프로토콜

`syncInit`과 `fileCheck`는 현재 요청에 content를 포함하지 않는다.
따라서 양쪽 변경이 감지되고 autoMerge가 가능한 텍스트 파일이면 서버는 follow-up을 요청한다.
`filePut`은 현재 요청에 content를 포함하므로 follow-up 없이 즉시 자동 병합을 시도한다.

새 action:

```text
autoMergeRequired
```

예:

```json
{
  "type": "fileCheckResult",
  "path": "notes/a.md",
  "action": "autoMergeRequired",
  "meta": {
    "path": "notes/a.md",
    "serverVersion": 2,
    "serverHash": "server-hash",
    "isDeleted": false
  }
}
```

`autoMergeRequired` 응답의 `meta.serverVersion`은 클라이언트가 관찰한 merge 대상 서버 버전이다.
클라이언트는 현재 content를 포함해 새 `mergePut` 메시지를 보낸다.
`mergePut`은 기존 `filePut` payload에 `expectedServerVersion`을 추가한다.

```json
{
  "type": "mergePut",
  "vault": "personal",
  "path": "notes/a.md",
  "content": "local content",
  "file": {
    "path": "notes/a.md",
    "exists": true,
    "baseVersion": 1,
    "baseHash": "base-hash",
    "localHash": "local-hash"
  },
  "expectedServerVersion": 2
}
```

`syncInit`은 bulk 응답인 `syncResult`를 사용하므로 단일 `action` 대신 `toAutoMerge` 배열을 추가한다.

```json
{
  "type": "syncResult",
  "vault": "personal",
  "toAutoMerge": [
    {
      "path": "notes/a.md",
      "baseVersion": 1,
      "baseHash": "base-hash",
      "localHash": "local-hash",
      "serverVersion": 2,
      "serverHash": "server-hash"
    }
  ]
}
```

플러그인은 `syncResult.toAutoMerge[]`의 각 entry에 대해 현재 로컬 content를 읽고 `mergePut`을 보낸다.
`fileCheck`는 단일 path 응답이므로 `action: "autoMergeRequired"`를 사용한다.

서버는 `filePut` 또는 `mergePut`에서 base row, local content, latest server content를 사용해 자동 병합을 시도한다.
`mergePut`에서 latest version이 `expectedServerVersion`과 다르면 다음처럼 처리한다.

```text
latest version > expectedServerVersion
=> 클라이언트가 본 서버 버전보다 더 최신 변경이 있음
=> 병합하지 않고 최신 meta로 autoMergeRequired를 다시 보낸다

latest version < expectedServerVersion
=> 서버 상태가 클라이언트 기대보다 뒤로 간 비정상 상태
=> 최신 server content를 포함한 conflict 또는 error를 반환한다
```

병합 성공 응답:

```json
{
  "type": "mergePutResult",
  "path": "notes/a.md",
  "action": "toDownload",
  "content": "merged content",
  "meta": {
    "path": "notes/a.md",
    "serverVersion": 3,
    "serverHash": "merged-hash",
    "isDeleted": false
  }
}
```

`action: "toDownload"`는 서버가 확정한 최신 content를 로컬에도 적용하라는 뜻이다.
자동 병합 성공 시 이 content는 server latest 원본이 아니라 merged content다.
플러그인은 기존 download 적용 경로를 재사용해 self-write suppression을 걸고 `content`를 로컬 파일에 쓴 뒤, meta를 갱신하고 dirty queue entry를 완료 처리한다.

`mergePut`은 `syncInit`/`fileCheck`에서 시작된 follow-up 병합에만 사용한다.
`filePut`은 이미 local content가 있으므로 자동 병합 조건을 만족하면 같은 요청 안에서 병합을 시도한다.

프로토콜 구현 요구:

- 서버 `IncomingMessage`에 `ExpectedServerVersion *int64` 필드를 추가한다.
- 서버 known message type과 dispatch에 `mergePut`을 추가한다.
- 서버 outgoing message type으로 `mergePutResult`를 사용한다.
- 플러그인 `WsClient`에 `sendMergePut` builder/sender를 추가한다.
- 플러그인 `ServerMessage`/action 타입은 `mergePutResult`, `autoMergeRequired`, `toAutoMerge`를 표현할 수 있어야 한다.
- `SyncManager.start()`는 `mergePutResult` listener를 등록한다.

## diff-match-patch 적용

Google `diff-match-patch`는 2-way diff/patch 라이브러리이므로 3-way merge helper를 별도로 감싼다.
구현은 `MergeText(base, local, server) (merged string, ok bool)` wrapper를 먼저 만들고, 아래 계약을 테스트로 고정한다.

1. diff-match-patch로 `base -> local`, `base -> server` diff를 만든다.
2. 각 diff에서 base 문자열 좌표계 기준 changed range 목록을 추출한다.
3. insert-only 변경은 삽입 위치를 zero-length range로 표현한다.
4. 두 changed range가 겹치거나 같은 위치에 서로 다른 insert를 만들면 merge failure로 처리한다.
5. 겹치지 않는 변경은 base 좌표 기준 시작 위치 오름차순으로 정렬한다.
6. base를 앞에서부터 복사하면서 각 range를 local/server 쪽 replacement text로 치환해 merged 문자열을 만든다.
7. wrapper는 merged 결과를 다시 hash 계산 대상 문자열로 반환한다.

구현 시 사용할 Go 라이브러리는 `github.com/sergi/go-diff/diffmatchpatch`를 우선 검토한다.
라이브러리 API가 changed range 추출에 맞지 않으면 diff-match-patch 호출부를 얇게 감싼 뒤, range extraction은 자체 코드로 구현한다.

자동 병합 실패 조건:

- base content를 읽을 수 없다.
- server latest content를 읽을 수 없다.
- decoded local content의 hash가 `file.localHash`와 다르다.
- `file.localHash`가 비어 있거나 누락됐다.
- encoding이 base64 또는 binary로 판단된다.
- diff-match-patch patch 적용 결과가 일부라도 실패한다.
- local 변경 범위와 server 변경 범위가 겹친다.
- 병합 결과 hash 계산 또는 저장에 실패한다.
- 병합 중 서버 latest version이 바뀌어 optimistic lock이 깨지고, 재시도 가능한 최신 변경이 아니다.

실패 시 기존 conflict response를 보낸다.

## 서버 흐름

`syncInit` / `fileCheck`:

1. latest row와 base row를 조회한다.
2. `localHash == serverHash`이면 기존 updateMeta 계열로 처리한다.
3. `localHash == baseHash`이면 `toDownload`로 서버 최신 content/meta를 보낸다.
4. `localHash != baseHash`이고 텍스트 파일이며 base/latest content를 읽을 수 있으면 `syncInit`은 `syncResult.toAutoMerge`에 추가하고, `fileCheck`는 `autoMergeRequired`를 보낸다.
5. 그 외는 기존 conflict로 처리한다.

`filePut`:

1. latest row와 base row를 조회한다.
2. `baseVersion == serverVersion`이면 기존 put/update 경로를 유지한다.
3. `baseVersion != serverVersion`이고 `localHash == baseHash`이면 새 version을 만들지 않고 `toDownload`로 서버 최신 content/meta를 보낸다.
4. `baseVersion != serverVersion`이고 `localHash != baseHash`이며 텍스트 파일이면 base/local/server content로 자동 병합을 즉시 시도한다.
5. 병합 성공 시 merged content를 새 version으로 저장하고 `toDownload`로 merged content/meta를 보낸다.
6. 병합 실패 또는 병합 대상이 아니면 일반 put으로 저장하지 않고 conflict로 처리한다.

`mergePut`:

1. latest row와 base row를 같은 DB transaction 안에서 조회한다.
2. latest version이 `expectedServerVersion`보다 크면 최신 meta로 `autoMergeRequired`를 다시 보낸다.
3. latest version이 `expectedServerVersion`보다 작으면 최신 server content를 포함한 conflict 또는 error를 반환한다.
4. latest version이 `expectedServerVersion`과 같으면 base/local/server content로 자동 병합을 시도한다.
5. 성공하면 merged content를 stage write한다.
6. 새 `file_versions` row를 insert한다.
7. DB commit 후 object storage와 latest storage를 finalize한다.
8. merged content/meta를 `mergePutResult`의 `toDownload` 응답 형태로 클라이언트에 보낸다.

## 플러그인 흐름

- `syncResult.toDownload` 기존 처리로 로컬 파일과 meta가 갱신된다.
- `fileCheckResult.action === "toDownload"` 처리로 로컬 파일과 meta가 갱신된다.
- `filePutResult.action === "toDownload"` 신규 처리로 dirty queue를 제거하고 서버 최신 또는 merged content를 적용한다.
- `syncResult.toAutoMerge` 또는 `autoMergeRequired`를 받으면 플러그인은 해당 path를 merge-in-flight로 표시한다.
- merge-in-flight path는 dirty queue flush와 delete queue flush가 건너뛴다.
- 해당 path에 dirty queue entry가 있으면 mergePut이 그 entry를 claim하고, mergePut 결과로만 complete/remove 한다.
- 플러그인은 현재 로컬 파일 content를 읽고 `mergePut`을 보낸다.
- `mergePutResult.action === "toDownload"` 응답에 `content`가 있으면 기존 download 적용 경로로 로컬 파일을 덮어쓴다.
- 이후 meta를 최신 version/hash로 갱신하고 dirty queue entry를 완료 처리한다.
- mergePut이 conflict/error로 끝나면 merge-in-flight를 해제하고 기존 conflict/blocked path 흐름으로 넘긴다.
- 병합 실패 conflict를 받으면 기존 conflict queue/modal 흐름을 사용한다.

## 테스트 전략

서버 DB:

- 새 스키마가 `file_versions`를 생성한다.
- `file_versions.id` surrogate key와 `(vault_name, path, version)` unique key가 존재한다.
- `CreateFile`은 version 1 active row를 만든다.
- `UpdateFile`은 version을 증가시키고 이전 row를 보존한다.
- `DeleteFile`은 tombstone row를 append하고 삭제 직전 `hash/content_ref`를 유지한다.
- `GetFile`은 latest row를 반환한다.
- `GetFileVersion`은 과거 version row를 반환한다.
- `ListActiveFiles`는 latest tombstone path를 제외한다.

Matrix:

- CSV와 fixture는 M001부터 순차 증가하는 행 ID를 사용한다.
- active server, `baseVersion != serverVersion`, `localHash == serverHash` -> 기존 updateMeta 계열 유지.
- active server, `baseVersion != serverVersion`, `localHash != serverHash`, base row 있음, `localHash == baseHash` -> `toDownload`.
- active server, `baseVersion != serverVersion`, `localHash != serverHash`, base row 있음, `localHash != baseHash`, autoMerge 가능 -> autoMerge 흐름.
- active server, `baseVersion != serverVersion`, `localHash != serverHash`, base row 있음, `localHash != baseHash`, autoMerge 불가 -> `conflict`.
- active server, `baseVersion != serverVersion`, `localHash != serverHash`, base row 없음 -> `conflict`.
- M003/M023/M037 first-run mismatch는 자동 병합하지 않는다.
- tombstone conflict 행은 기존 정책을 유지한다.

Merge unit:

- 서로 다른 줄을 수정하면 성공한다.
- 같은 줄을 다르게 수정하면 실패한다.
- base -> local patch 적용 실패는 실패한다.
- base -> server patch 적용 실패는 실패한다.
- base64 encoding은 merge를 시도하지 않는다.

WebSocket:

- `syncInit`에서 B가 v1 base를 갖고 있고 서버가 v2일 때, B local hash가 v1이면 `toDownload`를 받는다.
- `syncInit`에서 양쪽 변경이고 자동 병합 가능하면 `syncResult.toAutoMerge`를 받는다.
- `fileCheck`에서 같은 조건이면 `toDownload`와 content/meta를 받는다.
- `filePut`에서 같은 조건이면 서버에 새 version을 만들지 않고 최신 서버 content/meta를 돌려준다.
- `filePut` 양쪽 변경 clean merge가 성공하면 새 version이 생성되고 `toDownload`로 merged content/meta가 내려온다.
- `autoMergeRequired` 후 `mergePut` clean merge가 성공하면 새 version이 생성되고 `mergePutResult.action === "toDownload"`로 merged content/meta가 내려온다.
- merge 실패 시 기존 conflict response가 나온다.
- `mergePut.expectedServerVersion`보다 latest version이 높으면 최신 meta로 `autoMergeRequired`가 다시 나온다.
- `mergePut.expectedServerVersion`보다 latest version이 낮으면 conflict 또는 error가 나온다.

Plugin:

- `syncResult.toDownload`가 로컬 파일과 meta를 갱신한다.
- `fileCheckResult.action === "toDownload"`가 로컬 파일과 meta를 갱신한다.
- `filePutResult.action === "toDownload"`가 dirty queue를 정리하고 로컬 파일/meta를 갱신한다.
- `syncResult.toAutoMerge`가 merge-in-flight를 설정하고 `mergePut`을 보낸다.
- `autoMergeRequired` 수신 시 현재 content와 `expectedServerVersion`을 포함해 `mergePut` follow-up을 보낸다.
- merge-in-flight path는 dirty/delete flush가 건너뛴다.
- `mergePutResult.action === "toDownload"`로 merged content를 받으면 로컬 파일을 덮어쓰고 meta를 갱신한다.
- merge conflict 응답은 기존 conflict modal로 들어간다.
- 기존 conflictResolve 로컬/서버 선택은 계속 동작한다.

## 리스크

- 최신 상태를 `MAX(version)`으로 계산하므로 쿼리와 인덱스가 정확해야 한다.
- version insert와 content history 저장이 어긋나면 병합에 사용할 baseContent가 사라진다.
- `filePutResult.toDownload`와 `mergePutResult.toDownload` 처리는 플러그인 dirty queue 처리와 충돌하지 않게 설계해야 한다.
- diff-match-patch의 clean 적용이 의미적 안전을 보장하지 않는다.
- Markdown 구조상 링크, heading, block reference는 텍스트 병합이 성공해도 의미 충돌이 남을 수 있다.
- follow-up 방식은 상태 전이가 늘어나므로 queue/idempotency 테스트가 중요하다.
- 마이그레이션을 생략하므로 기존 로컬 DB는 새 스키마와 맞지 않을 수 있다. 이번 계획에서는 기존 데이터 보존을 요구하지 않는다.

## 완료 기준

1. 서버가 path별 버전 이력을 DB에 저장한다.
2. 서버가 baseVersion row를 조회해 base hash/content를 판정과 병합에 사용할 수 있다.
3. `file_versions.id` surrogate key와 `(vault_name, path, version)` unique key가 함께 존재한다.
4. M013/M033/M048의 base-aware 케이스가 `toDownload`, `autoMerge 후 toDownload`, `conflict`로 분류된다.
5. 텍스트 파일 양쪽 변경 케이스에서 자동 병합이 시도된다.
6. clean merge 성공 시 서버와 클라이언트가 `toDownload` 흐름으로 merged content에 수렴한다.
7. merge 실패 시 기존 conflict UX가 유지된다.
8. `docs/message-matrix.csv`에 base-aware/autoMerge flow가 반영되고 행 ID가 M001부터 순차 증가한다.
9. 기존 conflict 케이스, tombstone 케이스, conflictResolve 흐름은 회귀하지 않는다.
