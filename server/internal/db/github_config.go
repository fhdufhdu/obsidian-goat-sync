package db

import (
	"database/sql"
	"time"
)

type GitHubConfig struct {
	VaultName   string
	RemoteURL   string
	Branch      string
	Interval    string
	AccessToken string
	AuthorName  string
	AuthorEmail string
	Enabled     bool
}

func (q *Queries) SetGitHubConfig(cfg GitHubConfig) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := q.db.Exec(`
		INSERT INTO github_configs (vault_name, remote_url, branch, interval, access_token, author_name, author_email, enabled, inserted_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (vault_name)
		DO UPDATE SET remote_url = excluded.remote_url, branch = excluded.branch,
		              interval = excluded.interval, access_token = excluded.access_token,
		              author_name = excluded.author_name, author_email = excluded.author_email,
		              enabled = excluded.enabled, updated_at = excluded.updated_at`,
		cfg.VaultName, cfg.RemoteURL, cfg.Branch, cfg.Interval,
		cfg.AccessToken, cfg.AuthorName, cfg.AuthorEmail, cfg.Enabled,
		now, now,
	)
	return err
}

func (q *Queries) GetGitHubConfig(vaultName string) (GitHubConfig, error) {
	var cfg GitHubConfig
	err := q.db.QueryRow(
		"SELECT vault_name, remote_url, branch, interval, access_token, author_name, author_email, enabled FROM github_configs WHERE vault_name = ?",
		vaultName,
	).Scan(&cfg.VaultName, &cfg.RemoteURL, &cfg.Branch, &cfg.Interval,
		&cfg.AccessToken, &cfg.AuthorName, &cfg.AuthorEmail, &cfg.Enabled)
	if err == sql.ErrNoRows {
		return cfg, sql.ErrNoRows
	}
	return cfg, err
}

func (q *Queries) DeleteGitHubConfig(vaultName string) error {
	_, err := q.db.Exec("DELETE FROM github_configs WHERE vault_name = ?", vaultName)
	return err
}

func maskToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "****" + token[len(token)-4:]
}

func (cfg GitHubConfig) MaskedAccessToken() string {
	if cfg.AccessToken == "" {
		return ""
	}
	return maskToken(cfg.AccessToken)
}
