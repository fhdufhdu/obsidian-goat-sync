package sync

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMessageMatrixCSV(t *testing.T) {
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
	wantHeader := []string{
		"id", "구분", "클라이언트 메시지", "클라이언트 파일", "클라이언트 baseVersion",
		"서버 파일 상태", "버전 비교", "해시 비교", "base row", "base 해시 비교",
		"autoMerge", "서버 행동", "서버 메시지", "서버 메시지 내용", "비고",
	}
	if !reflect.DeepEqual(rows[0], wantHeader) {
		t.Fatalf("csv header = %#v, want %#v", rows[0], wantHeader)
	}
	for i, row := range rows[1:] {
		wantID := fmt.Sprintf("M%03d", i+1)
		if got := row[0]; got != wantID {
			t.Fatalf("row %d id = %q, want %q", i+2, got, wantID)
		}
	}

	fixtures := map[string]matrixFixture{}
	for _, f := range matrixFixtures() {
		fixtures[f.ID] = f
	}

	for _, row := range rows[1:] {
		id := row[0]
		fixture, ok := fixtures[id]
		if !ok {
			t.Fatalf("missing matrix fixture for csv row %s", id)
		}
		assertCSVRowMatchesFixture(t, rows[0], row, fixture)
	}
}

func assertCSVRowMatchesFixture(t *testing.T, header, row []string, fixture matrixFixture) {
	t.Helper()
	cells := csvCells(header, row)
	assertEqual(t, fixture.ID, cells["id"], fixture.ID)
	assertEqual(t, fixture.ID, fixture.Message, parseMatrixMessage(t, fixture.ID, cells["클라이언트 메시지"]))
	assertEqual(t, fixture.ID, fixture.ClientExists, parseBoolKR(t, fixture.ID, "클라이언트 파일", cells["클라이언트 파일"]))
	assertEqual(t, fixture.ID, fixture.BaseVersion != nil, parseBoolKR(t, fixture.ID, "클라이언트 baseVersion", cells["클라이언트 baseVersion"]))
	assertEqual(t, fixture.ID, fixture.ServerState, parseServerState(t, fixture.ID, cells["서버 파일 상태"]))
	assertEqual(t, fixture.ID, fixture.VersionMatch, parseVersionMatch(t, fixture.ID, cells["버전 비교"]))
	assertEqual(t, fixture.ID, fixture.HashMatch, parseHashMatch(t, fixture.ID, "해시 비교", cells["해시 비교"]))
	assertEqual(t, fixture.ID, fixture.BaseRowExists, parseBaseRowExists(t, fixture.ID, cells["base row"]))
	assertEqual(t, fixture.ID, fixture.BaseHashMatch, parseBaseHashMatch(t, fixture.ID, cells["base 해시 비교"]))
	assertEqual(t, fixture.ID, fixture.AutoMerge, parseAutoMerge(t, fixture.ID, cells["autoMerge"]))
	assertEqual(t, fixture.ID, fixture.Expected, parseExpectedAction(t, fixture.ID, cells["서버 행동"], cells["서버 메시지 내용"]))
}

func csvCells(header, row []string) map[string]string {
	cells := make(map[string]string, len(header))
	for i, name := range header {
		cells[name] = row[i]
	}
	return cells
}

func assertEqual[T comparable](t *testing.T, id string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", id, got, want)
	}
}

func parseBoolKR(t *testing.T, id, column, value string) bool {
	t.Helper()
	switch value {
	case "있음":
		return true
	case "없음":
		return false
	default:
		t.Fatalf("%s: unsupported %s value %q", id, column, value)
		return false
	}
}

func parseBaseRowExists(t *testing.T, id, value string) bool {
	t.Helper()
	switch value {
	case "있음":
		return true
	case "없음", "해당없음":
		return false
	default:
		t.Fatalf("%s: unsupported base row value %q", id, value)
		return false
	}
}

func parseMatrixMessage(t *testing.T, id, value string) MatrixMessage {
	t.Helper()
	values := map[string]MatrixMessage{
		"syncInit":   MessageSyncInit,
		"fileCheck":  MessageFileCheck,
		"filePut":    MessageFilePut,
		"fileDelete": MessageFileDelete,
	}
	return parseMapped(t, id, "클라이언트 메시지", value, values)
}

func parseServerState(t *testing.T, id, value string) ServerStateKind {
	t.Helper()
	values := map[string]ServerStateKind{
		"없음":  ServerMissing,
		"있음":  ServerActive,
		"삭제됨": ServerTombstone,
	}
	return parseMapped(t, id, "서버 파일 상태", value, values)
}

func parseVersionMatch(t *testing.T, id, value string) VersionMatch {
	t.Helper()
	values := map[string]VersionMatch{
		"해당없음":                              VersionNotApplicable,
		"baseVersion == serverVersion":      VersionEqualServer,
		"baseVersion != serverVersion":      VersionNotEqualServer,
		"baseVersion == deletedFromVersion": VersionEqualDeletedFrom,
		"baseVersion != deletedFromVersion": VersionNotEqualDeletedFrom,
		"상관없음":                              VersionAny,
	}
	return parseMapped(t, id, "버전 비교", value, values)
}

func parseHashMatch(t *testing.T, id, column, value string) HashMatch {
	t.Helper()
	values := map[string]HashMatch{
		"해당없음":                    HashNotApplicable,
		"localHash == serverHash": HashEqual,
		"localHash != serverHash": HashDifferent,
	}
	return parseMapped(t, id, column, value, values)
}

func parseBaseHashMatch(t *testing.T, id, value string) HashMatch {
	t.Helper()
	values := map[string]HashMatch{
		"해당없음":                  HashNotApplicable,
		"localHash == baseHash": HashEqual,
		"localHash != baseHash": HashDifferent,
	}
	return parseMapped(t, id, "base 해시 비교", value, values)
}

func parseAutoMerge(t *testing.T, id, value string) AutoMergeState {
	t.Helper()
	values := map[string]AutoMergeState{
		"해당없음": AutoMergeNotApplicable,
		"가능":   AutoMergePossible,
		"불가":   AutoMergeImpossible,
	}
	return parseMapped(t, id, "autoMerge", value, values)
}

func parseExpectedAction(t *testing.T, id, serverAction, serverMessageContent string) MatrixAction {
	t.Helper()
	if serverAction == "자동 병합" {
		return MatrixActionAutoMerge
	}
	values := map[string]MatrixAction{
		"none":              MatrixActionNone,
		"toPut":             MatrixActionToPut,
		"put":               MatrixActionPut,
		"toUpdateMeta":      MatrixActionToUpdateMeta,
		"updateMeta":        MatrixActionUpdateMeta,
		"okUpdateMeta":      MatrixActionOkUpdateMeta,
		"toDownload":        MatrixActionToDownload,
		"toDeleteLocal":     MatrixActionToDeleteLocal,
		"toRemoveMeta":      MatrixActionToRemoveMeta,
		"okRemoveMeta":      MatrixActionOkRemoveMeta,
		"upToDate":          MatrixActionUpToDate,
		"toAutoMerge":       MatrixActionAutoMerge,
		"autoMergeRequired": MatrixActionAutoMerge,
		"conflict":          MatrixActionConflict,
		"deleteConflict":    MatrixActionDeleteConflict,
	}
	return parseMapped(t, id, "서버 메시지 내용", serverMessageContent, values)
}

func parseMapped[T any](t *testing.T, id, column, value string, values map[string]T) T {
	t.Helper()
	parsed, ok := values[value]
	if !ok {
		t.Fatalf("%s: unsupported %s value %q", id, column, value)
	}
	return parsed
}
