package ws

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"obsidian-goat-sync/internal/db"
	"obsidian-goat-sync/internal/storage"
)

func setupHandler(t *testing.T) (*Handler, *db.Queries, *storage.Storage, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	q := db.NewQueries(database)
	s := storage.New(dir)
	hub := NewHub()
	go hub.Run()
	h := NewHandler(q, s, hub)
	return h, q, s, dir
}

func makeClient(hub *Hub, vault string) *Client {
	return &Client{
		hub:   hub,
		send:  make(chan []byte, 256),
		vault: vault,
	}
}

func readResponse(t *testing.T, c *Client) OutgoingMessage {
	t.Helper()
	data := <-c.send
	var msg OutgoingMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	return msg
}

func int64Ptr(v int64) *int64 { return &v }

func mustJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TestHandleVaultCreate(t *testing.T) {
	h, q, _, _ := setupHandler(t)
	c := makeClient(h.hub, "")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "vault_create",
		Vault: "personal",
	}))

	resp := readResponse(t, c)
	if resp.Type != "vault_created" {
		t.Errorf("expected vault_created, got %s", resp.Type)
	}

	exists, _ := q.VaultExists("personal")
	if !exists {
		t.Error("vault not created in DB")
	}
}

func TestHandleSyncInit_NoPrev_NoServer(t *testing.T) {
	h, q, _, _ := setupHandler(t)
	q.CreateVault("personal")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "sync_init",
		Vault: "personal",
		Files: []SyncInitFile{
			{Path: "notes/new.md", CurrentClientHash: "clienthash"},
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "sync_result" {
		t.Fatalf("expected sync_result, got %s", resp.Type)
	}
	if len(resp.ToUpload) != 1 || resp.ToUpload[0] != "notes/new.md" {
		t.Errorf("expected toUpload=[notes/new.md], got %v", resp.ToUpload)
	}
}

func TestHandleSyncInit_NoPrev_ActiveSameHash(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("content"))
	q.CreateFile("personal", "notes/hello.md", "serverhash")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "sync_init",
		Vault: "personal",
		Files: []SyncInitFile{
			{Path: "notes/hello.md", CurrentClientHash: "serverhash"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToUpdateMeta) != 1 {
		t.Errorf("expected toUpdateMeta=[notes/hello.md], got %v", resp.ToUpdateMeta)
	}
	if resp.ToUpdateMeta[0].Path != "notes/hello.md" {
		t.Errorf("wrong path in toUpdateMeta")
	}
}

func TestHandleSyncInit_NoPrev_ActiveDiffHash(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("server content"))
	q.CreateFile("personal", "notes/hello.md", "serverhash")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "sync_init",
		Vault: "personal",
		Files: []SyncInitFile{
			{Path: "notes/hello.md", CurrentClientHash: "clienthash"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(resp.Conflicts))
	}
}

func TestHandleSyncInit_WithPrev_SameVersion_SameHash_Skip(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("content"))
	q.CreateFile("personal", "notes/hello.md", "hash1")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "sync_init",
		Vault: "personal",
		Files: []SyncInitFile{
			{Path: "notes/hello.md", PrevServerVersion: int64Ptr(1), PrevServerHash: "hash1", CurrentClientHash: "hash1"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToUpload) != 0 || len(resp.ToUpdate) != 0 || len(resp.ToDownload) != 0 || len(resp.Conflicts) != 0 {
		t.Errorf("expected all empty (skip), got upload=%v update=%v download=%v conflicts=%v", resp.ToUpload, resp.ToUpdate, resp.ToDownload, resp.Conflicts)
	}
}

func TestHandleSyncInit_WithPrev_SameVersion_DiffHash_ToUpdate(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("content"))
	q.CreateFile("personal", "notes/hello.md", "hash1")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "sync_init",
		Vault: "personal",
		Files: []SyncInitFile{
			{Path: "notes/hello.md", PrevServerVersion: int64Ptr(1), PrevServerHash: "hash1", CurrentClientHash: "clienthash"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToUpdate) != 1 || resp.ToUpdate[0] != "notes/hello.md" {
		t.Errorf("expected toUpdate=[notes/hello.md], got %v", resp.ToUpdate)
	}
}

func TestHandleSyncInit_WithPrev_OlderVersion_SameClientHash_ToUpdateMeta(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("content"))
	q.CreateFile("personal", "notes/hello.md", "hash1")
	q.UpdateFile("personal", "notes/hello.md", "hash2")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "sync_init",
		Vault: "personal",
		Files: []SyncInitFile{
			{Path: "notes/hello.md", PrevServerVersion: int64Ptr(1), PrevServerHash: "hash1", CurrentClientHash: "hash2"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToUpdateMeta) != 1 {
		t.Errorf("expected toUpdateMeta=[notes/hello.md], got %v", resp.ToUpdateMeta)
	}
}

func TestHandleSyncInit_WithPrev_OlderVersion_PrevHashEqClient_ToDownload(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("server content"))
	q.CreateFile("personal", "notes/hello.md", "hash1")
	q.UpdateFile("personal", "notes/hello.md", "hash2")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "sync_init",
		Vault: "personal",
		Files: []SyncInitFile{
			{Path: "notes/hello.md", PrevServerVersion: int64Ptr(1), PrevServerHash: "hash1", CurrentClientHash: "hash1"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToDownload) != 1 {
		t.Errorf("expected toDownload, got %v", resp.ToDownload)
	}
}

func TestHandleSyncInit_ServerOnlyFile_ToDownload(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/server-only.md", []byte("server content"))
	q.CreateFile("personal", "notes/server-only.md", "serverhash")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "sync_init",
		Vault: "personal",
		Files: []SyncInitFile{},
	}))

	resp := readResponse(t, c)
	if len(resp.ToDownload) != 1 || resp.ToDownload[0].Path != "notes/server-only.md" {
		t.Errorf("expected server-only file in toDownload, got %v", resp.ToDownload)
	}
}

func TestHandleSyncInit_Tombstone_ToDelete(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/old.md", []byte("old content"))
	q.CreateFile("personal", "notes/old.md", "hash1")
	q.DeleteFile("personal", "notes/old.md")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "sync_init",
		Vault: "personal",
		Files: []SyncInitFile{
			{Path: "notes/old.md", PrevServerVersion: int64Ptr(1), PrevServerHash: "hash1", CurrentClientHash: "hash1"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToDelete) != 1 || resp.ToDelete[0] != "notes/old.md" {
		t.Errorf("expected toDelete=[notes/old.md], got %v", resp.ToDelete)
	}
}

func TestHandleFileCreate_New(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.CreateVaultDir("personal")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "file_create",
		Vault:             "personal",
		Path:              "notes/new.md",
		Content:           "# New Note",
		CurrentClientHash: "hash1",
	}))

	resp := readResponse(t, c)
	if resp.Type != "file_create_result" {
		t.Fatalf("expected file_create_result, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true")
	}
	if resp.CurrentServerVersion != 1 {
		t.Errorf("expected version=1, got %d", resp.CurrentServerVersion)
	}

	content, _ := s.ReadFile("personal", "notes/new.md")
	if string(content) != "# New Note" {
		t.Errorf("file not written correctly: %s", string(content))
	}
}

func TestHandleFileCreate_ActiveConflict(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/existing.md", []byte("original"))
	q.CreateFile("personal", "notes/existing.md", "originalhash")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "file_create",
		Vault:             "personal",
		Path:              "notes/existing.md",
		Content:           "conflicting content",
		CurrentClientHash: "newhash",
	}))

	resp := readResponse(t, c)
	if resp.Ok == nil || *resp.Ok {
		t.Fatal("expected ok=false for active file conflict")
	}
	if resp.Conflict == nil {
		t.Fatal("expected conflict info")
	}
	if resp.Conflict.CurrentServerVersion != 1 {
		t.Errorf("expected server version=1 in conflict, got %d", resp.Conflict.CurrentServerVersion)
	}
}

func TestHandleFileCreate_TombstoneReuse(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/old.md", []byte("old"))
	q.CreateFile("personal", "notes/old.md", "oldhash")
	q.DeleteFile("personal", "notes/old.md")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "file_create",
		Vault:             "personal",
		Path:              "notes/old.md",
		Content:           "new content",
		CurrentClientHash: "newhash",
	}))

	resp := readResponse(t, c)
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true for tombstone reuse")
	}
	if resp.CurrentServerVersion != 3 {
		t.Errorf("expected version=3 (tombstone was v2), got %d", resp.CurrentServerVersion)
	}
}

func TestHandleFileUpdate_Success(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("old"))
	q.CreateFile("personal", "notes/hello.md", "oldhash")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "file_update",
		Vault:             "personal",
		Path:              "notes/hello.md",
		Content:           "updated",
		PrevServerVersion: int64Ptr(1),
		CurrentClientHash: "newhash",
	}))

	resp := readResponse(t, c)
	if resp.Type != "file_update_result" {
		t.Fatalf("expected file_update_result, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true")
	}
	if resp.CurrentServerVersion != 2 {
		t.Errorf("expected version=2, got %d", resp.CurrentServerVersion)
	}

	content, _ := s.ReadFile("personal", "notes/hello.md")
	if string(content) != "updated" {
		t.Errorf("expected 'updated', got '%s'", string(content))
	}
}

func TestHandleFileUpdate_Noop(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("content"))
	q.CreateFile("personal", "notes/hello.md", "samehash")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "file_update",
		Vault:             "personal",
		Path:              "notes/hello.md",
		Content:           "content",
		PrevServerVersion: int64Ptr(1),
		CurrentClientHash: "samehash",
	}))

	resp := readResponse(t, c)
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true")
	}
	if !resp.Noop {
		t.Error("expected noop=true")
	}
}

func TestHandleFileUpdate_Conflict(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("server version"))
	q.CreateFile("personal", "notes/hello.md", "hash1")
	q.UpdateFile("personal", "notes/hello.md", "hash2")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "file_update",
		Vault:             "personal",
		Path:              "notes/hello.md",
		Content:           "client version",
		PrevServerVersion: int64Ptr(1),
		CurrentClientHash: "clienthash",
	}))

	resp := readResponse(t, c)
	if resp.Ok == nil || *resp.Ok {
		t.Fatal("expected ok=false for conflict")
	}
	if resp.Conflict == nil {
		t.Fatal("expected conflict info")
	}
	if resp.Conflict.CurrentServerVersion != 2 {
		t.Errorf("expected server version=2, got %d", resp.Conflict.CurrentServerVersion)
	}
}

func TestHandleFileDelete_Success(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/old.md", []byte("delete me"))
	q.CreateFile("personal", "notes/old.md", "hash1")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "file_delete",
		Vault:             "personal",
		Path:              "notes/old.md",
		PrevServerVersion: int64Ptr(1),
	}))

	resp := readResponse(t, c)
	if resp.Type != "file_delete_result" {
		t.Fatalf("expected file_delete_result, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true")
	}
	if resp.CurrentServerVersion != 2 {
		t.Errorf("expected version=2 after delete, got %d", resp.CurrentServerVersion)
	}

	f, _ := q.GetFile("personal", "notes/old.md")
	if !f.IsDeleted {
		t.Error("expected file to be marked deleted")
	}
}

func TestHandleFileDelete_Conflict(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("updated on server"))
	q.CreateFile("personal", "notes/hello.md", "hash1")
	q.UpdateFile("personal", "notes/hello.md", "hash2")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "file_delete",
		Vault:             "personal",
		Path:              "notes/hello.md",
		PrevServerVersion: int64Ptr(1),
	}))

	resp := readResponse(t, c)
	if resp.Ok == nil || *resp.Ok {
		t.Fatal("expected ok=false for conflict")
	}
	if resp.Conflict == nil {
		t.Fatal("expected conflict info")
	}
}

func TestHandleFileDelete_NonExistent_Noop(t *testing.T) {
	h, q, _, _ := setupHandler(t)
	q.CreateVault("personal")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "file_delete",
		Vault:             "personal",
		Path:              "notes/nonexistent.md",
		PrevServerVersion: int64Ptr(1),
	}))

	resp := readResponse(t, c)
	if resp.Type != "file_delete_result" {
		t.Fatalf("expected file_delete_result, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true for nonexistent (idempotent)")
	}
}

func TestHandleConflictResolve_LocalUpdate_Success(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("server content"))
	q.CreateFile("personal", "notes/hello.md", "serverhash")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "conflict_resolve",
		Vault:             "personal",
		Path:              "notes/hello.md",
		Resolution:        "local",
		Content:           "local content",
		CurrentClientHash: "localhash",
		PrevServerVersion: int64Ptr(1),
	}))

	resp := readResponse(t, c)
	if resp.Type != "conflict_resolve_result" {
		t.Fatalf("expected conflict_resolve_result, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true")
	}
	if resp.CurrentServerVersion != 2 {
		t.Errorf("expected version=2, got %d", resp.CurrentServerVersion)
	}
}

func TestHandleConflictResolve_LocalUpdate_Reconflict(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("newer server"))
	q.CreateFile("personal", "notes/hello.md", "hash1")
	q.UpdateFile("personal", "notes/hello.md", "hash2")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "conflict_resolve",
		Vault:             "personal",
		Path:              "notes/hello.md",
		Resolution:        "local",
		Content:           "local content",
		CurrentClientHash: "localhash",
		PrevServerVersion: int64Ptr(1),
	}))

	resp := readResponse(t, c)
	if resp.Ok == nil || *resp.Ok {
		t.Fatal("expected ok=false for re-conflict")
	}
	if resp.Conflict == nil {
		t.Fatal("expected conflict info in re-conflict")
	}
}

func TestHandleConflictResolve_LocalDelete_Success(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/old.md", []byte("content"))
	q.CreateFile("personal", "notes/old.md", "hash1")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:              "conflict_resolve",
		Vault:             "personal",
		Path:              "notes/old.md",
		Resolution:        "local",
		Action:            "delete",
		PrevServerVersion: int64Ptr(1),
	}))

	resp := readResponse(t, c)
	if resp.Type != "conflict_resolve_result" {
		t.Fatalf("expected conflict_resolve_result, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true for force delete")
	}

	f, _ := q.GetFile("personal", "notes/old.md")
	if !f.IsDeleted {
		t.Error("expected file to be deleted")
	}
}
