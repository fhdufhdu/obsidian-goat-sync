package db

import (
	"database/sql"
	"time"
)

type File struct {
	VaultName  string
	Path       string
	Version    int64
	Hash       string
	IsDeleted  bool
	InsertedAt string
	UpdatedAt  string
}

func (q *Queries) CreateFile(vaultName, path, hash string) (File, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := q.db.Exec(`
		INSERT INTO files (vault_name, path, version, hash, is_deleted, inserted_at, updated_at)
		VALUES (?, ?, 1, ?, 0, ?, ?)`,
		vaultName, path, hash, now, now,
	)
	if err != nil {
		return File{}, err
	}
	return q.GetFile(vaultName, path)
}

func (q *Queries) CreateFileFromTombstone(vaultName, path, hash string, prevVersion int64) (File, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	newVersion := prevVersion + 1
	_, err := q.db.Exec(`
		UPDATE files SET version = ?, hash = ?, is_deleted = 0, updated_at = ?
		WHERE vault_name = ? AND path = ?`,
		newVersion, hash, now, vaultName, path,
	)
	if err != nil {
		return File{}, err
	}
	return q.GetFile(vaultName, path)
}

func (q *Queries) UpdateFile(vaultName, path, hash string) (File, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := q.db.Exec(`
		UPDATE files SET version = version + 1, hash = ?, is_deleted = 0, updated_at = ?
		WHERE vault_name = ? AND path = ?`,
		hash, now, vaultName, path,
	)
	if err != nil {
		return File{}, err
	}
	return q.GetFile(vaultName, path)
}

func (q *Queries) DeleteFile(vaultName, path string) (File, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := q.db.Exec(`
		UPDATE files SET is_deleted = 1, version = version + 1, updated_at = ?
		WHERE vault_name = ? AND path = ?`,
		now, vaultName, path,
	)
	if err != nil {
		return File{}, err
	}
	return q.GetFile(vaultName, path)
}

func (q *Queries) GetFile(vaultName, path string) (File, error) {
	var f File
	err := q.db.QueryRow(
		"SELECT vault_name, path, version, hash, is_deleted, inserted_at, updated_at FROM files WHERE vault_name = ? AND path = ?",
		vaultName, path,
	).Scan(&f.VaultName, &f.Path, &f.Version, &f.Hash, &f.IsDeleted, &f.InsertedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return f, sql.ErrNoRows
	}
	return f, err
}

func (q *Queries) ListActiveFiles(vaultName string) ([]File, error) {
	rows, err := q.db.Query(
		"SELECT vault_name, path, version, hash, is_deleted, inserted_at, updated_at FROM files WHERE vault_name = ? AND is_deleted = 0 ORDER BY path",
		vaultName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.VaultName, &f.Path, &f.Version, &f.Hash, &f.IsDeleted, &f.InsertedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (q *Queries) ListFiles(vaultName string) ([]File, error) {
	return q.ListActiveFiles(vaultName)
}
