package db

import (
	"testing"
)

func TestGenerateToken(t *testing.T) {
	q := setupTestDB(t)

	token, err := q.GenerateToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	if len(token) < 32 {
		t.Errorf("token too short: %d", len(token))
	}
}

func TestValidateToken(t *testing.T) {
	q := setupTestDB(t)

	token, _ := q.GenerateToken()

	valid, err := q.ValidateToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}
	if !valid {
		t.Error("expected token to be valid")
	}
}

func TestValidateTokenInvalid(t *testing.T) {
	q := setupTestDB(t)

	valid, err := q.ValidateToken("nonexistent-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("expected token to be invalid")
	}
}

func TestDeactivateToken(t *testing.T) {
	q := setupTestDB(t)

	token, _ := q.GenerateToken()
	err := q.DeactivateToken(token)
	if err != nil {
		t.Fatalf("failed to deactivate: %v", err)
	}

	valid, _ := q.ValidateToken(token)
	if valid {
		t.Error("expected deactivated token to be invalid")
	}
}

func TestRegenerateToken(t *testing.T) {
	q := setupTestDB(t)

	oldToken, _ := q.GenerateToken()
	newToken, err := q.RegenerateToken(oldToken)
	if err != nil {
		t.Fatalf("failed to regenerate: %v", err)
	}
	if newToken == oldToken {
		t.Error("new token should differ from old")
	}

	oldValid, _ := q.ValidateToken(oldToken)
	if oldValid {
		t.Error("old token should be deactivated")
	}

	newValid, _ := q.ValidateToken(newToken)
	if !newValid {
		t.Error("new token should be valid")
	}
}

func TestListTokens(t *testing.T) {
	q := setupTestDB(t)

	q.GenerateToken()
	q.GenerateToken()

	tokens, err := q.ListTokens()
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
}
