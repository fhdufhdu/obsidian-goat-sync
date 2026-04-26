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
