package db

import "testing"

func TestSetGitHubConfig(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")

	cfg := GitHubConfig{
		VaultName:   "personal",
		RemoteURL:   "https://github.com/user/vault.git",
		Branch:      "main",
		Interval:    "1h",
		AccessToken: "ghp_testtoken1234",
		AuthorName:  "Test User",
		AuthorEmail: "test@example.com",
		Enabled:     true,
	}
	err := q.SetGitHubConfig(cfg)
	if err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	got, err := q.GetGitHubConfig("personal")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}
	if got.RemoteURL != cfg.RemoteURL {
		t.Errorf("expected remote URL %s, got %s", cfg.RemoteURL, got.RemoteURL)
	}
	if got.Branch != "main" {
		t.Errorf("expected branch=main, got %s", got.Branch)
	}
	if got.AccessToken != "ghp_testtoken1234" {
		t.Errorf("expected access token, got %s", got.AccessToken)
	}
	if got.AuthorName != "Test User" {
		t.Errorf("expected author name, got %s", got.AuthorName)
	}
	if got.AuthorEmail != "test@example.com" {
		t.Errorf("expected author email, got %s", got.AuthorEmail)
	}
}

func TestUpdateGitHubConfig(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")

	q.SetGitHubConfig(GitHubConfig{
		VaultName:   "personal",
		RemoteURL:   "https://github.com/user/vault.git",
		Branch:      "main",
		Interval:    "1h",
		AccessToken: "token1",
		AuthorName:  "User",
		AuthorEmail: "user@example.com",
		Enabled:     true,
	})

	q.SetGitHubConfig(GitHubConfig{
		VaultName:   "personal",
		RemoteURL:   "https://github.com/user/vault2.git",
		Branch:      "dev",
		Interval:    "30m",
		AccessToken: "token2",
		AuthorName:  "User2",
		AuthorEmail: "user2@example.com",
		Enabled:     false,
	})

	got, _ := q.GetGitHubConfig("personal")
	if got.RemoteURL != "https://github.com/user/vault2.git" {
		t.Errorf("expected updated remote URL, got %s", got.RemoteURL)
	}
	if got.Enabled {
		t.Error("expected enabled=false")
	}
	if got.AccessToken != "token2" {
		t.Errorf("expected token2, got %s", got.AccessToken)
	}
}

func TestGetGitHubConfigNotFound(t *testing.T) {
	q := setupTestDB(t)
	q.CreateVault("personal")

	_, err := q.GetGitHubConfig("personal")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestMaskedAccessToken(t *testing.T) {
	cfg := GitHubConfig{AccessToken: "ghp_abcdefgh1234"}
	masked := cfg.MaskedAccessToken()
	if masked == cfg.AccessToken {
		t.Error("masked token should not equal original")
	}
	if len(masked) == 0 {
		t.Error("masked token should not be empty")
	}
}

func TestMaskedAccessTokenEmpty(t *testing.T) {
	cfg := GitHubConfig{AccessToken: ""}
	if cfg.MaskedAccessToken() != "" {
		t.Error("empty token should mask to empty")
	}
}
