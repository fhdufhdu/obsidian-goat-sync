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

func assertResponseMetaVersion(t *testing.T, resp OutgoingMessage, want int64) {
	t.Helper()
	if resp.Meta == nil {
		t.Fatalf("missing meta in response: %#v", resp)
	}
	if resp.Meta.ServerVersion != want {
		t.Errorf("expected version=%d, got %d", want, resp.Meta.ServerVersion)
	}
}

func conflictServerVersion(t *testing.T, conflict *ConflictInfo) int64 {
	t.Helper()
	if conflict == nil {
		t.Fatal("expected conflict info")
	}
	return conflict.ServerVersion
}

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
		Files: []FilePayload{
			{Path: "notes/new.md", LocalHash: "clienthash"},
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "sync_result" {
		t.Fatalf("expected sync_result, got %s", resp.Type)
	}
	if len(resp.ToDownload) != 0 {
		t.Errorf("expected no downloads, got %v", resp.ToDownload)
	}
	if len(resp.ToUpdateMeta) != 0 {
		t.Errorf("expected no metadata updates, got %v", resp.ToUpdateMeta)
	}
	if len(resp.Conflicts) != 0 {
		t.Errorf("expected no conflicts, got %v", resp.Conflicts)
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
		Files: []FilePayload{
			{Path: "notes/hello.md", LocalHash: "serverhash"},
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
		Files: []FilePayload{
			{Path: "notes/hello.md", LocalHash: "clienthash"},
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
		Files: []FilePayload{
			{Path: "notes/hello.md", BaseVersion: int64Ptr(1), BaseHash: "hash1", LocalHash: "hash1"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToDownload) != 0 || len(resp.Conflicts) != 0 {
		t.Errorf("expected all empty buckets, got download=%v conflicts=%v", resp.ToDownload, resp.Conflicts)
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
		Files: []FilePayload{
			{Path: "notes/hello.md", BaseVersion: int64Ptr(1), BaseHash: "hash1", LocalHash: "clienthash"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToDownload) != 0 || len(resp.Conflicts) != 0 {
		t.Errorf("expected toUpdate-like path not in download/conflict buckets, got download=%v conflicts=%v", resp.ToDownload, resp.Conflicts)
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
		Files: []FilePayload{
			{Path: "notes/hello.md", BaseVersion: int64Ptr(1), BaseHash: "hash1", LocalHash: "hash2"},
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
		Files: []FilePayload{
			{Path: "notes/hello.md", BaseVersion: int64Ptr(1), BaseHash: "hash1", LocalHash: "hash1"},
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
		Files: []FilePayload{},
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
		Files: []FilePayload{
			{Path: "notes/old.md", BaseVersion: int64Ptr(1), BaseHash: "hash1", LocalHash: "hash1"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.Conflicts) != 0 {
		t.Errorf("expected tombstone cleanup with no conflicts, got %v", resp.Conflicts)
	}
}

func TestHandleFileCreate_New(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.CreateVaultDir("personal")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "file_create",
		Vault:   "personal",
		Path:    "notes/new.md",
		Content: "# New Note",
		File: &FilePayload{
			LocalHash: "hash1",
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "file_create_result" {
		t.Fatalf("expected file_create_result, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true")
	}
	assertResponseMetaVersion(t, resp, 1)

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
		Type:    "file_create",
		Vault:   "personal",
		Path:    "notes/existing.md",
		Content: "conflicting content",
		File: &FilePayload{
			LocalHash: "newhash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Ok == nil || *resp.Ok {
		t.Fatal("expected ok=false for active file conflict")
	}
	if resp.Conflict == nil {
		t.Fatal("expected conflict info")
	}
	if got := conflictServerVersion(t, resp.Conflict); got != 1 {
		t.Errorf("expected server version=1 in conflict, got %d", got)
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
		Type:    "file_create",
		Vault:   "personal",
		Path:    "notes/old.md",
		Content: "new content",
		File: &FilePayload{
			LocalHash: "newhash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true for tombstone reuse")
	}
	assertResponseMetaVersion(t, resp, 3)
}

func TestHandleFileUpdate_Success(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("old"))
	q.CreateFile("personal", "notes/hello.md", "oldhash")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "file_update",
		Vault:   "personal",
		Path:    "notes/hello.md",
		Content: "updated",
		File: &FilePayload{
			BaseVersion: int64Ptr(1),
			LocalHash:   "newhash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "file_update_result" {
		t.Fatalf("expected file_update_result, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true")
	}
	assertResponseMetaVersion(t, resp, 2)

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
		Type:    "file_update",
		Vault:   "personal",
		Path:    "notes/hello.md",
		Content: "content",
		File: &FilePayload{
			BaseVersion: int64Ptr(1),
			LocalHash:   "samehash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true")
	}
	if resp.Action != "noop" {
		t.Errorf("expected action=noop, got %s", resp.Action)
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
		Type:    "file_update",
		Vault:   "personal",
		Path:    "notes/hello.md",
		Content: "client version",
		File: &FilePayload{
			BaseVersion: int64Ptr(1),
			LocalHash:   "clienthash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Ok == nil || *resp.Ok {
		t.Fatal("expected ok=false for conflict")
	}
	if resp.Conflict == nil {
		t.Fatal("expected conflict info")
	}
	if got := conflictServerVersion(t, resp.Conflict); got != 2 {
		t.Errorf("expected server version=2, got %d", got)
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
		Type:  "file_delete",
		Vault: "personal",
		Path:  "notes/old.md",
		File: &FilePayload{
			BaseVersion: int64Ptr(1),
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "file_delete_result" {
		t.Fatalf("expected file_delete_result, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true")
	}
	assertResponseMetaVersion(t, resp, 2)

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
		Type:  "file_delete",
		Vault: "personal",
		Path:  "notes/hello.md",
		File: &FilePayload{
			BaseVersion: int64Ptr(1),
		},
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
		Type:  "file_delete",
		Vault: "personal",
		Path:  "notes/nonexistent.md",
		File: &FilePayload{
			BaseVersion: int64Ptr(1),
		},
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
		Type:       "conflict_resolve",
		Vault:      "personal",
		Path:       "notes/hello.md",
		Resolution: "local",
		Content:    "local content",
		File: &FilePayload{
			BaseVersion: int64Ptr(1),
			LocalHash:   "localhash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "conflict_resolve_result" {
		t.Fatalf("expected conflict_resolve_result, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true")
	}
	assertResponseMetaVersion(t, resp, 2)
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
		Type:       "conflict_resolve",
		Vault:      "personal",
		Path:       "notes/hello.md",
		Resolution: "local",
		Content:    "local content",
		File: &FilePayload{
			BaseVersion: int64Ptr(1),
			LocalHash:   "localhash",
		},
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
		Type:       "conflict_resolve",
		Vault:      "personal",
		Path:       "notes/old.md",
		Resolution: "local",
		Action:     "delete",
		File: &FilePayload{
			BaseVersion: int64Ptr(1),
		},
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

func TestIncomingMessageUsesCamelCaseProtocol(t *testing.T) {
	base := int64(7)
	msg := mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/a.md",
		Content: "hello",
		File: &FilePayload{
			Path:        "notes/a.md",
			BaseVersion: &base,
			BaseHash:    "basehash",
			LocalHash:   "localhash",
		},
	})

	decoded, err := UnmarshalMessage(msg)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != "filePut" {
		t.Fatalf("type = %s", decoded.Type)
	}
	if decoded.File == nil || decoded.File.BaseVersion == nil || *decoded.File.BaseVersion != 7 {
		t.Fatalf("missing baseVersion in file payload: %#v", decoded.File)
	}
}
