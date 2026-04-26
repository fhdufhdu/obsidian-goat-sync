package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Storage struct {
	dataDir string
}

func New(dataDir string) *Storage {
	return &Storage{dataDir: dataDir}
}

type StagedFileOp struct {
	TempPath  string
	FinalPath string
	commit    func() error
	rollback  func() error
}

func (op *StagedFileOp) Commit() error {
	if op == nil || op.commit == nil {
		return nil
	}
	return op.commit()
}

func (op *StagedFileOp) Rollback() error {
	if op == nil || op.rollback == nil {
		return nil
	}
	return op.rollback()
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

func (s *Storage) StageWrite(vault, filePath string, data []byte) (*StagedFileOp, error) {
	final := s.vaultPath(vault, filePath)
	if err := os.MkdirAll(filepath.Dir(final), 0755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(final), ".goat-sync-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	return &StagedFileOp{
		TempPath:  tmpPath,
		FinalPath: final,
		commit: func() error {
			if err := os.MkdirAll(filepath.Dir(final), 0755); err != nil {
				return err
			}
			return os.Rename(tmpPath, final)
		},
		rollback: func() error {
			err := os.Remove(tmpPath)
			if os.IsNotExist(err) {
				return nil
			}
			return err
		},
	}, nil
}

func objectRef(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Storage) objectPath(ref string) (string, error) {
	const prefix = "sha256:"
	if !strings.HasPrefix(ref, prefix) || len(ref) != len(prefix)+64 {
		return "", fmt.Errorf("invalid content ref %q", ref)
	}
	hash := strings.TrimPrefix(ref, prefix)
	return filepath.Join(s.dataDir, "objects", "sha256", hash[:2], hash), nil
}

func (s *Storage) StageObjectWrite(data []byte) (string, *StagedFileOp, error) {
	ref := objectRef(data)
	final, err := s.objectPath(ref)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(filepath.Dir(final), 0755); err != nil {
		return "", nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(final), ".goat-object-*")
	if err != nil {
		return "", nil, err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", nil, err
	}
	return ref, &StagedFileOp{
		TempPath:  tmpPath,
		FinalPath: final,
		commit: func() error {
			if _, err := os.Stat(final); err == nil {
				return os.Remove(tmpPath)
			}
			return os.Rename(tmpPath, final)
		},
		rollback: func() error {
			err := os.Remove(tmpPath)
			if os.IsNotExist(err) {
				return nil
			}
			return err
		},
	}, nil
}

func (s *Storage) ReadObject(ref string) ([]byte, error) {
	path, err := s.objectPath(ref)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *Storage) StageDelete(vault, filePath string) (*StagedFileOp, error) {
	final := s.vaultPath(vault, filePath)
	if _, err := os.Stat(final); err != nil {
		if os.IsNotExist(err) {
			return &StagedFileOp{FinalPath: final}, nil
		}
		return nil, err
	}
	trashDir := filepath.Join(filepath.Dir(final), ".goat-sync-trash")
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(trashDir, filepath.Base(filePath)+".*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	_ = os.Remove(tmpPath)
	if err := os.Rename(final, tmpPath); err != nil {
		if os.IsNotExist(err) {
			return &StagedFileOp{}, nil
		}
		return nil, err
	}
	return &StagedFileOp{
		TempPath:  tmpPath,
		FinalPath: final,
		commit: func() error {
			err := os.Remove(tmpPath)
			if os.IsNotExist(err) {
				return nil
			}
			return err
		},
		rollback: func() error {
			if err := os.MkdirAll(filepath.Dir(final), 0755); err != nil {
				return err
			}
			err := os.Rename(tmpPath, final)
			if os.IsNotExist(err) {
				return nil
			}
			return err
		},
	}, nil
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
