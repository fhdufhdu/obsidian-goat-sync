package config

import "os"

type Config struct {
	AdminUser string
	AdminPass string
	Port      string
	DataDir   string
}

func Load() Config {
	return Config{
		AdminUser: getEnv("OBSIDIAN_GOAT_SYNC_ADMIN_USER", "admin"),
		AdminPass: getEnv("OBSIDIAN_GOAT_SYNC_ADMIN_PASS", ""),
		Port:      getEnv("OBSIDIAN_GOAT_SYNC_PORT", "8080"),
		DataDir:   "/app/data",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
