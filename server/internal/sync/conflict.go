package sync

import (
	"database/sql"

	"obsidian-goat-sync/internal/db"
)

type SyncAction int

const (
	ActionSkip SyncAction = iota
	ActionToUpload
	ActionToUpdate
	ActionToDownload
	ActionToDelete
	ActionToUpdateMeta
	ActionConflict
)

type ClassifyResult struct {
	Action SyncAction
}

func ClassifyFile(clientFile db.SyncInitFile, serverFile db.File, serverExists bool, serverIsDeleted bool) ClassifyResult {
	hasPrev := clientFile.PrevServerVersion != nil

	if !hasPrev {
		if !serverExists {
			return ClassifyResult{Action: ActionToUpload}
		}
		if serverIsDeleted {
			return ClassifyResult{Action: ActionToUpload}
		}
		if clientFile.CurrentClientHash == serverFile.Hash {
			return ClassifyResult{Action: ActionToUpdateMeta}
		}
		return ClassifyResult{Action: ActionConflict}
	}

	prevVer := *clientFile.PrevServerVersion

	if !serverExists {
		return ClassifyResult{Action: ActionToUpload}
	}

	if serverIsDeleted {
		if prevVer <= serverFile.Version {
			return ClassifyResult{Action: ActionToDelete}
		}
		return ClassifyResult{Action: ActionToUpload}
	}

	if prevVer == serverFile.Version {
		if clientFile.CurrentClientHash == serverFile.Hash {
			return ClassifyResult{Action: ActionSkip}
		}
		return ClassifyResult{Action: ActionToUpdate}
	}

	if prevVer < serverFile.Version {
		if clientFile.CurrentClientHash == serverFile.Hash {
			return ClassifyResult{Action: ActionToUpdateMeta}
		}
		if clientFile.PrevServerHash == clientFile.CurrentClientHash {
			return ClassifyResult{Action: ActionToDownload}
		}
		return ClassifyResult{Action: ActionConflict}
	}

	return ClassifyResult{Action: ActionToUpload}
}

type OptimisticResult struct {
	OK        bool
	Noop      bool
	Err       error
	ErrNoRows bool
}

func CheckFileCreate(serverFile db.File, serverExists bool) OptimisticResult {
	if !serverExists || serverFile.IsDeleted {
		return OptimisticResult{OK: true}
	}
	return OptimisticResult{OK: false}
}

func CheckFileUpdate(serverFile db.File, serverExists bool, prevServerVersion int64, currentClientHash string) OptimisticResult {
	if !serverExists || serverFile.IsDeleted {
		return OptimisticResult{OK: false, ErrNoRows: true}
	}
	if prevServerVersion != serverFile.Version {
		return OptimisticResult{OK: false}
	}
	if currentClientHash == serverFile.Hash {
		return OptimisticResult{OK: true, Noop: true}
	}
	return OptimisticResult{OK: true}
}

func CheckFileDelete(serverFile db.File, serverErr error, prevServerVersion int64) OptimisticResult {
	if serverErr == sql.ErrNoRows || serverFile.IsDeleted {
		return OptimisticResult{OK: true, Noop: true}
	}
	if prevServerVersion != serverFile.Version {
		return OptimisticResult{OK: false}
	}
	return OptimisticResult{OK: true}
}
