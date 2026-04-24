package config

import (
	"os"
	"testing"
)

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("OBSIDIAN_SYNC_ADMIN_USER", "testadmin")
	os.Setenv("OBSIDIAN_SYNC_ADMIN_PASS", "testpass")
	os.Setenv("OBSIDIAN_SYNC_PORT", "9090")
	defer func() {
		os.Unsetenv("OBSIDIAN_SYNC_ADMIN_USER")
		os.Unsetenv("OBSIDIAN_SYNC_ADMIN_PASS")
		os.Unsetenv("OBSIDIAN_SYNC_PORT")
	}()

	cfg := Load()

	if cfg.AdminUser != "testadmin" {
		t.Errorf("expected AdminUser=testadmin, got %s", cfg.AdminUser)
	}
	if cfg.AdminPass != "testpass" {
		t.Errorf("expected AdminPass=testpass, got %s", cfg.AdminPass)
	}
	if cfg.Port != "9090" {
		t.Errorf("expected Port=9090, got %s", cfg.Port)
	}
}

func TestLoadDefaults(t *testing.T) {
	os.Unsetenv("OBSIDIAN_SYNC_ADMIN_USER")
	os.Unsetenv("OBSIDIAN_SYNC_ADMIN_PASS")
	os.Unsetenv("OBSIDIAN_SYNC_PORT")

	cfg := Load()

	if cfg.Port != "8080" {
		t.Errorf("expected default Port=8080, got %s", cfg.Port)
	}
	if cfg.DataDir != "/app/data" {
		t.Errorf("expected default DataDir=/app/data, got %s", cfg.DataDir)
	}
}
