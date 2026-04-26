package db

import (
	"database/sql"
	"time"
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
	var file File
	err := q.InTx(func(txq *Queries) error {
		inserted, err := txq.insertFileVersion(vaultName, path, 1, hash, contentRef, encoding, false)
		if err != nil {
			return err
		}
		file = inserted
		return nil
	})
	return file, err
}

func (q *Queries) CreateFileFromTombstone(vaultName, path, hash, contentRef, encoding string, prevVersion int64) (File, error) {
	_ = prevVersion
	var file File
	err := q.InTx(func(txq *Queries) error {
		latest, err := txq.GetFile(vaultName, path)
		if err != nil {
			return err
		}
		inserted, err := txq.insertFileVersion(vaultName, path, latest.Version+1, hash, contentRef, encoding, false)
		if err != nil {
			return err
		}
		file = inserted
		return nil
	})
	return file, err
}

func (q *Queries) UpdateFile(vaultName, path, hash, contentRef, encoding string) (File, error) {
	var file File
	err := q.InTx(func(txq *Queries) error {
		latest, err := txq.GetFile(vaultName, path)
		if err != nil {
			return err
		}
		inserted, err := txq.insertFileVersion(vaultName, path, latest.Version+1, hash, contentRef, encoding, false)
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
	err := q.InTx(func(txq *Queries) error {
		latest, err := txq.GetFile(vaultName, path)
		if err != nil {
			return err
		}
		if latest.IsDeleted {
			file = latest
			return nil
		}
		inserted, err := txq.insertFileVersion(vaultName, path, latest.Version+1, latest.Hash, latest.ContentRef, latest.Encoding, true)
		if err != nil {
			return err
		}
		file = inserted
		return nil
	})
	return file, err
}

func (q *Queries) insertFileVersion(vaultName, path string, version int64, hash, contentRef, encoding string, isDeleted bool) (File, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := q.db.Exec(`
		INSERT INTO file_versions (vault_name, path, version, hash, content_ref, encoding, is_deleted, inserted_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		vaultName, path, version, hash, contentRef, encoding, isDeleted, now, now,
	)
	if err != nil {
		return File{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return File{}, err
	}
	return q.getFileByID(id)
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

func (q *Queries) getFileByID(id int64) (File, error) {
	return scanFile(q.db.QueryRow(`
		SELECT id, vault_name, path, version, hash, COALESCE(content_ref, ''), encoding, is_deleted, inserted_at, updated_at
		FROM file_versions
		WHERE id = ?`,
		id,
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
