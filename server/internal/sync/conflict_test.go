package sync

import (
	"database/sql"
	"testing"

	"obsidian-sync/internal/db"
)

func int64Ptr(v int64) *int64 { return &v }

func makeInitFile(path string, prevVersion *int64, prevHash, clientHash string) db.SyncInitFile {
	return db.SyncInitFile{
		Path:              path,
		PrevServerVersion: prevVersion,
		PrevServerHash:    prevHash,
		CurrentClientHash: clientHash,
	}
}

func makeServerFile(version int64, hash string, isDeleted bool) db.File {
	return db.File{Version: version, Hash: hash, IsDeleted: isDeleted}
}

func TestClassify_NoPrev_NoServer_ToUpload(t *testing.T) {
	f := makeInitFile("a.md", nil, "", "clienthash")
	result := ClassifyFile(f, db.File{}, false, false)
	if result.Action != ActionToUpload {
		t.Errorf("expected ToUpload, got %v", result.Action)
	}
}

func TestClassify_NoPrev_Tombstone_ToUpload(t *testing.T) {
	f := makeInitFile("a.md", nil, "", "clienthash")
	sf := makeServerFile(2, "oldhash", true)
	result := ClassifyFile(f, sf, true, true)
	if result.Action != ActionToUpload {
		t.Errorf("expected ToUpload, got %v", result.Action)
	}
}

func TestClassify_NoPrev_Active_SameHash_ToUpdateMeta(t *testing.T) {
	f := makeInitFile("a.md", nil, "", "samehash")
	sf := makeServerFile(3, "samehash", false)
	result := ClassifyFile(f, sf, true, false)
	if result.Action != ActionToUpdateMeta {
		t.Errorf("expected ToUpdateMeta, got %v", result.Action)
	}
}

func TestClassify_NoPrev_Active_DiffHash_Conflict(t *testing.T) {
	f := makeInitFile("a.md", nil, "", "clienthash")
	sf := makeServerFile(3, "serverhash", false)
	result := ClassifyFile(f, sf, true, false)
	if result.Action != ActionConflict {
		t.Errorf("expected Conflict, got %v", result.Action)
	}
}

func TestClassify_WithPrev_NoServer_ToUpload(t *testing.T) {
	f := makeInitFile("a.md", int64Ptr(1), "hash1", "clienthash")
	result := ClassifyFile(f, db.File{}, false, false)
	if result.Action != ActionToUpload {
		t.Errorf("expected ToUpload, got %v", result.Action)
	}
}

func TestClassify_WithPrev_Tombstone_PrevLeServer_ToDelete(t *testing.T) {
	f := makeInitFile("a.md", int64Ptr(1), "hash1", "hash1")
	sf := makeServerFile(2, "hash1", true)
	result := ClassifyFile(f, sf, true, true)
	if result.Action != ActionToDelete {
		t.Errorf("expected ToDelete, got %v", result.Action)
	}
}

func TestClassify_WithPrev_Active_SameVersion_SameHash_Skip(t *testing.T) {
	f := makeInitFile("a.md", int64Ptr(5), "hash5", "hash5")
	sf := makeServerFile(5, "hash5", false)
	result := ClassifyFile(f, sf, true, false)
	if result.Action != ActionSkip {
		t.Errorf("expected Skip, got %v", result.Action)
	}
}

func TestClassify_WithPrev_Active_SameVersion_DiffHash_ToUpdate(t *testing.T) {
	f := makeInitFile("a.md", int64Ptr(5), "hash5", "clienthash")
	sf := makeServerFile(5, "hash5", false)
	result := ClassifyFile(f, sf, true, false)
	if result.Action != ActionToUpdate {
		t.Errorf("expected ToUpdate, got %v", result.Action)
	}
}

func TestClassify_WithPrev_Active_OlderVersion_ClientEqServer_ToUpdateMeta(t *testing.T) {
	f := makeInitFile("a.md", int64Ptr(3), "hash3", "hash5")
	sf := makeServerFile(5, "hash5", false)
	result := ClassifyFile(f, sf, true, false)
	if result.Action != ActionToUpdateMeta {
		t.Errorf("expected ToUpdateMeta, got %v", result.Action)
	}
}

func TestClassify_WithPrev_Active_OlderVersion_PrevEqClient_ToDownload(t *testing.T) {
	f := makeInitFile("a.md", int64Ptr(3), "hash3", "hash3")
	sf := makeServerFile(5, "hash5", false)
	result := ClassifyFile(f, sf, true, false)
	if result.Action != ActionToDownload {
		t.Errorf("expected ToDownload, got %v", result.Action)
	}
}

func TestClassify_WithPrev_Active_OlderVersion_BothDiff_Conflict(t *testing.T) {
	f := makeInitFile("a.md", int64Ptr(3), "hash3", "clienthash")
	sf := makeServerFile(5, "hash5", false)
	result := ClassifyFile(f, sf, true, false)
	if result.Action != ActionConflict {
		t.Errorf("expected Conflict, got %v", result.Action)
	}
}

func TestCheckFileCreate_NoRecord_OK(t *testing.T) {
	result := CheckFileCreate(db.File{}, false)
	if !result.OK {
		t.Error("expected OK=true for no record")
	}
}

func TestCheckFileCreate_Tombstone_OK(t *testing.T) {
	sf := makeServerFile(2, "hash", true)
	result := CheckFileCreate(sf, true)
	if !result.OK {
		t.Error("expected OK=true for tombstone")
	}
}

func TestCheckFileCreate_Active_Conflict(t *testing.T) {
	sf := makeServerFile(1, "hash", false)
	result := CheckFileCreate(sf, true)
	if result.OK {
		t.Error("expected OK=false for active file")
	}
}

func TestCheckFileUpdate_SameVersion_DiffHash_OK(t *testing.T) {
	sf := makeServerFile(5, "serverhash", false)
	result := CheckFileUpdate(sf, true, 5, "clienthash")
	if !result.OK || result.Noop {
		t.Errorf("expected OK=true, Noop=false, got OK=%v Noop=%v", result.OK, result.Noop)
	}
}

func TestCheckFileUpdate_SameVersion_SameHash_Noop(t *testing.T) {
	sf := makeServerFile(5, "samehash", false)
	result := CheckFileUpdate(sf, true, 5, "samehash")
	if !result.OK || !result.Noop {
		t.Errorf("expected OK=true, Noop=true, got OK=%v Noop=%v", result.OK, result.Noop)
	}
}

func TestCheckFileUpdate_DiffVersion_Conflict(t *testing.T) {
	sf := makeServerFile(7, "hash7", false)
	result := CheckFileUpdate(sf, true, 5, "clienthash")
	if result.OK {
		t.Error("expected OK=false for version mismatch")
	}
}

func TestCheckFileUpdate_NotExists_ErrNoRows(t *testing.T) {
	result := CheckFileUpdate(db.File{}, false, 1, "clienthash")
	if result.OK || !result.ErrNoRows {
		t.Error("expected OK=false, ErrNoRows=true for missing file")
	}
}

func TestCheckFileDelete_SameVersion_OK(t *testing.T) {
	sf := makeServerFile(5, "hash", false)
	result := CheckFileDelete(sf, nil, 5)
	if !result.OK || result.Noop {
		t.Errorf("expected OK=true Noop=false, got OK=%v Noop=%v", result.OK, result.Noop)
	}
}

func TestCheckFileDelete_DiffVersion_Conflict(t *testing.T) {
	sf := makeServerFile(7, "hash7", false)
	result := CheckFileDelete(sf, nil, 5)
	if result.OK {
		t.Error("expected OK=false for version mismatch")
	}
}

func TestCheckFileDelete_NotExists_Noop(t *testing.T) {
	result := CheckFileDelete(db.File{}, sql.ErrNoRows, 1)
	if !result.OK || !result.Noop {
		t.Errorf("expected OK=true Noop=true for nonexistent, got OK=%v Noop=%v", result.OK, result.Noop)
	}
}

func TestCheckFileDelete_Tombstone_Noop(t *testing.T) {
	sf := makeServerFile(2, "hash", true)
	result := CheckFileDelete(sf, nil, 1)
	if !result.OK || !result.Noop {
		t.Errorf("expected OK=true Noop=true for tombstone, got OK=%v Noop=%v", result.OK, result.Noop)
	}
}
