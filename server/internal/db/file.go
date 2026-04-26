package db

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrFileVersionMismatch = errors.New("file version mismatch")
	ErrFileNotTombstone    = errors.New("file is not a tombstone")
)

type File struct {
	ID         int64
	VaultName  string
	Path       string
	Version    int64
	Hash       string
	ContentRef string
	Encoding   string
	IsDeleted  bool
	InsertedAt string
	UpdatedAt  string
}

func (f File) DeletedFromVersion() int64 {
	if !f.IsDeleted {
		return 0
	}
	return f.Version - 1
}

func scanFile(scanner interface {
	Scan(dest ...any) error
}) (File, error) {
	var f File
	err := scanner.Scan(&f.ID, &f.VaultName, &f.Path, &f.Version, &f.Hash, &f.ContentRef, &f.Encoding, &f.IsDeleted, &f.InsertedAt, &f.UpdatedAt)
	return f, err
}

func (q *Queries) CreateFile(vaultName, path, hash, contentRef, encoding string) (File, error) {
	return q.insertFileVersion(vaultName, path, 1, hash, contentRef, encoding, false)
}

func (q *Queries) CreateFileFromTombstone(vaultName, path, hash, contentRef, encoding string, prevVersion int64) (File, error) {
	var file File
	err := q.withFileVersionRetry(func() error {
		now := time.Now().UTC().Format(time.RFC3339)
		inserted, err := scanFile(q.db.QueryRow(`
			INSERT INTO file_versions (vault_name, path, version, hash, content_ref, encoding, is_deleted, inserted_at, updated_at)
			SELECT vault_name, path, version + 1, ?, ?, ?, 0, ?, ?
			FROM file_versions
			WHERE id = (
				SELECT id
				FROM file_versions
				WHERE vault_name = ? AND path = ?
				ORDER BY version DESC
				LIMIT 1
			)
			  AND version = ?
			  AND is_deleted = 1
			RETURNING id, vault_name, path, version, hash, COALESCE(content_ref, ''), encoding, is_deleted, inserted_at, updated_at`,
			hash, contentRef, encoding, now, now, vaultName, path, prevVersion,
		))
		if err == nil {
			file = inserted
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}

		latest, latestErr := q.GetFile(vaultName, path)
		if latestErr != nil {
			return latestErr
		}
		if latest.Version != prevVersion {
			return ErrFileVersionMismatch
		}
		if !latest.IsDeleted {
			return ErrFileNotTombstone
		}
		return err
	})
	return file, err
}

func (q *Queries) UpdateFile(vaultName, path, hash, contentRef, encoding string) (File, error) {
	var file File
	err := q.withFileVersionRetry(func() error {
		now := time.Now().UTC().Format(time.RFC3339)
		inserted, err := scanFile(q.db.QueryRow(`
			INSERT INTO file_versions (vault_name, path, version, hash, content_ref, encoding, is_deleted, inserted_at, updated_at)
			SELECT vault_name, path, version + 1, ?, ?, ?, 0, ?, ?
			FROM file_versions
			WHERE id = (
				SELECT id
				FROM file_versions
				WHERE vault_name = ? AND path = ?
				ORDER BY version DESC
				LIMIT 1
			)
			RETURNING id, vault_name, path, version, hash, COALESCE(content_ref, ''), encoding, is_deleted, inserted_at, updated_at`,
			hash, contentRef, encoding, now, now, vaultName, path,
		))
		if err != nil {
			return err
		}
		file = inserted
		return nil
	})
	return file, err
}

func (q *Queries) DeleteFile(vaultName, path string) (File, error) {
	var file File
	err := q.withFileVersionRetry(func() error {
		now := time.Now().UTC().Format(time.RFC3339)
		inserted, err := scanFile(q.db.QueryRow(`
			INSERT INTO file_versions (vault_name, path, version, hash, content_ref, encoding, is_deleted, inserted_at, updated_at)
			SELECT vault_name, path, version + 1, hash, content_ref, encoding, 1, ?, ?
			FROM file_versions
			WHERE id = (
				SELECT id
				FROM file_versions
				WHERE vault_name = ? AND path = ?
				ORDER BY version DESC
				LIMIT 1
			)
			  AND is_deleted = 0
			RETURNING id, vault_name, path, version, hash, COALESCE(content_ref, ''), encoding, is_deleted, inserted_at, updated_at`,
			now, now, vaultName, path,
		))
		if err == nil {
			file = inserted
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}

		latest, latestErr := q.GetFile(vaultName, path)
		if latestErr != nil {
			return latestErr
		}
		if latest.IsDeleted {
			file = latest
			return nil
		}
		return err
	})
	return file, err
}

func (q *Queries) insertFileVersion(vaultName, path string, version int64, hash, contentRef, encoding string, isDeleted bool) (File, error) {
	var file File
	err := q.withFileVersionRetry(func() error {
		now := time.Now().UTC().Format(time.RFC3339)
		inserted, err := scanFile(q.db.QueryRow(`
			INSERT INTO file_versions (vault_name, path, version, hash, content_ref, encoding, is_deleted, inserted_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			RETURNING id, vault_name, path, version, hash, COALESCE(content_ref, ''), encoding, is_deleted, inserted_at, updated_at`,
			vaultName, path, version, hash, contentRef, encoding, isDeleted, now, now,
		))
		if err != nil {
			return err
		}
		file = inserted
		return nil
	})
	return file, err
}

func (q *Queries) withFileVersionRetry(fn func() error) error {
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		err = fn()
		if !isRetryableFileVersionWriteError(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	return err
}

func isRetryableFileVersionWriteError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "UNIQUE constraint failed: file_versions.vault_name, file_versions.path, file_versions.version")
}

func (q *Queries) GetFile(vaultName, path string) (File, error) {
	f, err := scanFile(q.db.QueryRow(`
		SELECT id, vault_name, path, version, hash, COALESCE(content_ref, ''), encoding, is_deleted, inserted_at, updated_at
		FROM file_versions
		WHERE vault_name = ? AND path = ?
		ORDER BY version DESC
		LIMIT 1`,
		vaultName, path,
	))
	if err == sql.ErrNoRows {
		return f, sql.ErrNoRows
	}
	return f, err
}

func (q *Queries) GetFileVersion(vaultName, path string, version int64) (File, error) {
	return scanFile(q.db.QueryRow(`
		SELECT id, vault_name, path, version, hash, COALESCE(content_ref, ''), encoding, is_deleted, inserted_at, updated_at
		FROM file_versions
		WHERE vault_name = ? AND path = ? AND version = ?`,
		vaultName, path, version,
	))
}

func (q *Queries) ListActiveFiles(vaultName string) ([]File, error) {
	rows, err := q.db.Query(
		`SELECT fv.id, fv.vault_name, fv.path, fv.version, fv.hash, COALESCE(fv.content_ref, ''), fv.encoding, fv.is_deleted, fv.inserted_at, fv.updated_at
		FROM file_versions fv
		WHERE fv.vault_name = ?
		  AND fv.is_deleted = 0
		  AND fv.version = (
			SELECT MAX(latest.version)
			FROM file_versions latest
			WHERE latest.vault_name = fv.vault_name AND latest.path = fv.path
		  )
		ORDER BY fv.path`,
		vaultName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (q *Queries) ListFiles(vaultName string) ([]File, error) {
	return q.ListActiveFiles(vaultName)
}
