package storage

import (
	"io/fs"
	"os"
	"path/filepath"
)

type Storage struct {
	dataDir string
}

func New(dataDir string) *Storage {
	return &Storage{dataDir: dataDir}
}

func (s *Storage) vaultPath(vault, filePath string) string {
	return filepath.Join(s.dataDir, "vaults", vault, filePath)
}

func (s *Storage) WriteFile(vault, filePath string, data []byte) error {
	full := s.vaultPath(vault, filePath)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0644)
}

func (s *Storage) ReadFile(vault, filePath string) ([]byte, error) {
	return os.ReadFile(s.vaultPath(vault, filePath))
}

func (s *Storage) DeleteFile(vault, filePath string) error {
	return os.Remove(s.vaultPath(vault, filePath))
}

func (s *Storage) CreateVaultDir(vault string) error {
	return os.MkdirAll(filepath.Join(s.dataDir, "vaults", vault), 0755)
}

func (s *Storage) DeleteVaultDir(vault string) error {
	return os.RemoveAll(filepath.Join(s.dataDir, "vaults", vault))
}

func (s *Storage) VaultStats(vault string) (fileCount int, totalSize int64, err error) {
	root := filepath.Join(s.dataDir, "vaults", vault)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		fileCount++
		info, err := d.Info()
		if err != nil {
			return err
		}
		totalSize += info.Size()
		return nil
	})
	return
}

func (s *Storage) VaultDir(vault string) string {
	return filepath.Join(s.dataDir, "vaults", vault)
}
