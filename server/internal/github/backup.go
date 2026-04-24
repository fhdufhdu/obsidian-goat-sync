package github

import (
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"

	"obsidian-sync/internal/db"
	"obsidian-sync/internal/storage"
)

type BackupService struct {
	queries *db.Queries
	storage *storage.Storage
	stop    chan struct{}
	mu      sync.Mutex
	lastRun map[string]time.Time
}

func NewBackupService(q *db.Queries, s *storage.Storage) *BackupService {
	return &BackupService{
		queries: q,
		storage: s,
		stop:    make(chan struct{}),
		lastRun: make(map[string]time.Time),
	}
}

func (b *BackupService) Start() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.runBackups()
		case <-b.stop:
			return
		}
	}
}

func (b *BackupService) Stop() {
	close(b.stop)
}

func (b *BackupService) runBackups() {
	vaults, err := b.queries.ListVaults()
	if err != nil {
		log.Printf("backup: failed to list vaults: %v", err)
		return
	}

	present := make(map[string]struct{}, len(vaults))
	now := time.Now()
	for _, vault := range vaults {
		present[vault.Name] = struct{}{}
		cfg, err := b.queries.GetGitHubConfig(vault.Name)
		if err != nil || !cfg.Enabled {
			continue
		}
		interval := parseInterval(cfg.Interval)
		b.mu.Lock()
		last, seen := b.lastRun[vault.Name]
		b.mu.Unlock()
		if seen && now.Sub(last) < interval {
			continue
		}
		b.backupVault(vault.Name, cfg)
		b.mu.Lock()
		b.lastRun[vault.Name] = now
		b.mu.Unlock()
	}

	b.mu.Lock()
	for name := range b.lastRun {
		if _, ok := present[name]; !ok {
			delete(b.lastRun, name)
		}
	}
	b.mu.Unlock()
}

func (b *BackupService) backupVault(vaultName string, cfg db.GitHubConfig) {
	dir := b.storage.VaultDir(vaultName)

	remoteURL := cfg.RemoteURL
	if cfg.AccessToken != "" {
		remoteURL = injectToken(cfg.RemoteURL, cfg.AccessToken)
	}

	if !isGitRepo(dir) {
		run(dir, "git", "init")
		run(dir, "git", "remote", "add", "origin", remoteURL)
	} else {
		run(dir, "git", "remote", "set-url", "origin", remoteURL)
	}

	run(dir, "git", "add", "-A")

	if err := run(dir, "git", "diff", "--cached", "--quiet"); err != nil {
		authorFlag := fmt.Sprintf("%s <%s>", cfg.AuthorName, cfg.AuthorEmail)
		run(dir, "git", "commit", "--author", authorFlag, "-m", "auto backup: "+time.Now().UTC().Format(time.RFC3339))
	}

	run(dir, "git", "push", "-u", "origin", cfg.Branch)
}

func injectToken(remoteURL, token string) string {
	const prefix = "https://"
	if len(remoteURL) > len(prefix) && remoteURL[:len(prefix)] == prefix {
		return prefix + token + "@" + remoteURL[len(prefix):]
	}
	return remoteURL
}

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
}

func run(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("backup: %s %v failed: %s", name, args, string(output))
	}
	return err
}

func parseInterval(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Hour
	}
	return d
}
