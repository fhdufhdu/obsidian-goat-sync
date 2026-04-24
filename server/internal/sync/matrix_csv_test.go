package sync

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatrixFixturesCoverEveryCSVRow(t *testing.T) {
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
