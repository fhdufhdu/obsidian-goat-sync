package db

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type Token struct {
	Token      string
	IsActive   bool
	InsertedAt string
	UpdatedAt  string
}

func (q *Queries) GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := q.db.Exec(
		"INSERT INTO tokens (token, is_active, inserted_at, updated_at) VALUES (?, 1, ?, ?)",
		token, now, now,
	)
	return token, err
}

func (q *Queries) ValidateToken(token string) (bool, error) {
	var count int
	err := q.db.QueryRow(
		"SELECT COUNT(*) FROM tokens WHERE token = ? AND is_active = 1",
		token,
	).Scan(&count)
	return count > 0, err
}

func (q *Queries) DeactivateToken(token string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := q.db.Exec("UPDATE tokens SET is_active = 0, updated_at = ? WHERE token = ?", now, token)
	return err
}

func (q *Queries) RegenerateToken(oldToken string) (string, error) {
	if err := q.DeactivateToken(oldToken); err != nil {
		return "", err
	}
	return q.GenerateToken()
}

func (q *Queries) ListTokens() ([]Token, error) {
	rows, err := q.db.Query("SELECT token, is_active, inserted_at, updated_at FROM tokens ORDER BY inserted_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.Token, &t.IsActive, &t.InsertedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}
