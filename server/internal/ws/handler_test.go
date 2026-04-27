package ws

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
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

func setupHandlerTest(t *testing.T) (*Handler, *db.Queries, *storage.Storage, string) {
	t.Helper()
	return setupHandler(t)
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

func sendJSON(t *testing.T, h *Handler, c *Client, msg IncomingMessage) {
	t.Helper()
	h.HandleMessage(c, mustJSON(msg))
}

func lastMessage(t *testing.T, c *Client) OutgoingMessage {
	t.Helper()
	return readResponse(t, c)
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
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

func assertNoGoatSyncTempFile(t *testing.T, dir, relativeDir string) {
	t.Helper()
	target := filepath.Join(dir, "vaults", relativeDir)
	entries, err := os.ReadDir(target)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("read dir: %v", err)
		}
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".goat-sync-") {
			t.Fatalf("leftover temp file: %s", entry.Name())
		}
	}
}

func assertNoGoatSyncTrash(t *testing.T, dir, relativeDir string) {
	t.Helper()
	trash := filepath.Join(dir, "vaults", relativeDir, ".goat-sync-trash")
	entries, err := os.ReadDir(trash)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("read trash dir: %v", err)
		}
		return
	}
	if len(entries) != 0 {
		t.Fatalf("expected no lingering trash files, got %d", len(entries))
	}
}

func TestHandleVaultCreate(t *testing.T) {
	h, q, _, _ := setupHandler(t)
	c := makeClient(h.hub, "")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "vaultCreate",
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
		Type:  "syncInit",
		Vault: "personal",
		Files: []FilePayload{
			{Path: "notes/new.md", Exists: true, LocalHash: "clienthash"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToPut) != 1 || resp.ToPut[0] != "notes/new.md" {
		t.Fatalf("expected toPut=[notes/new.md], got %#v", resp.ToPut)
	}
	if len(resp.ToDownload) != 0 {
		t.Errorf("expected no downloads, got %v", resp.ToDownload)
	}
	if len(resp.ToDeleteLocal) != 0 {
		t.Errorf("expected no delete-local actions, got %v", resp.ToDeleteLocal)
	}
	if len(resp.ToRemoveMeta) != 0 {
		t.Errorf("expected no remove-meta actions, got %v", resp.ToRemoveMeta)
	}
	if len(resp.ToUpdateMeta) != 0 {
		t.Errorf("expected no metadata updates, got %v", resp.ToUpdateMeta)
	}
	if len(resp.Conflicts) != 0 {
		t.Errorf("expected no conflicts, got %v", resp.Conflicts)
	}
}

func TestHandleSyncInit_WithPrev_SameVersion_DiffHash_ToUpdate(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("content"))
	q.CreateFile("personal", "notes/hello.md", "hash1", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "syncInit",
		Vault: "personal",
		Files: []FilePayload{
			{Path: "notes/hello.md", Exists: true, BaseVersion: int64Ptr(1), BaseHash: "hash1", LocalHash: "clienthash"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToPut) != 1 || resp.ToPut[0] != "notes/hello.md" {
		t.Fatalf("expected toPut=[notes/hello.md], got %#v", resp.ToPut)
	}
	if len(resp.ToDownload) != 0 {
		t.Errorf("expected no downloads, got %v", resp.ToDownload)
	}
	if len(resp.Conflicts) != 0 {
		t.Errorf("expected toUpdate-like path not in download/conflict buckets, got conflicts=%v", resp.Conflicts)
	}
}

func TestHandleSyncInit_Tombstone_ToDelete(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/old.md", []byte("old content"))
	q.CreateFile("personal", "notes/old.md", "hash1", "", "")
	q.DeleteFile("personal", "notes/old.md")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "syncInit",
		Vault: "personal",
		Files: []FilePayload{
			{Path: "notes/old.md", Exists: true, BaseVersion: int64Ptr(1), BaseHash: "hash1", LocalHash: "hash1"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToDeleteLocal) != 1 {
		t.Fatalf("expected one toDeleteLocal path, got %v", resp.ToDeleteLocal)
	}
	if !resp.ToDeleteLocal[0].IsDeleted {
		t.Fatal("expected toDeleteLocal item marked deleted")
	}
	if resp.ToDeleteLocal[0].ServerVersion != 2 {
		t.Fatalf("expected toDeleteLocal server version 2, got %#v", resp.ToDeleteLocal[0].ServerVersion)
	}
}

func TestHandleFileCreate_NoFilePayload(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.CreateVaultDir("personal")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/new.md",
		Content: "# New Note",
	}))

	resp := readResponse(t, c)
	if resp.Type != "filePutResult" {
		t.Fatalf("expected filePutResult, got %s", resp.Type)
	}
	if resp.Action != "conflict" {
		t.Fatalf("expected action=conflict, got %q", resp.Action)
	}
	if resp.Error == "" {
		t.Fatal("expected clear error when file payload is missing")
	}

	if _, err := s.ReadFile("personal", "notes/new.md"); err == nil {
		t.Fatal("expected file not written when file payload is missing")
	}
	if _, err := q.GetFile("personal", "notes/new.md"); err == nil {
		t.Fatal("expected no db row created when file payload is missing")
	}
}

func TestHandleFilePutDoesNotLeaveFileWhenDBFails(t *testing.T) {
	h, _, s, _ := setupHandler(t)
	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "",
		Path:    "notes/bad.md",
		Content: "should not stay",
		File: &FilePayload{
			Path:      "notes/bad.md",
			Exists:    true,
			LocalHash: "hash-bad",
		},
	}))

	resp := readResponse(t, c)
	if resp.Error == "" {
		t.Fatalf("expected error response, got %#v", resp)
	}
	if _, err := s.ReadFile("", "notes/bad.md"); err == nil {
		t.Fatal("expected no final file after failed message")
	}
}

func TestHandleFilePutDoesNotLeaveFileWhenDBMetadataFails(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	q := db.NewQueries(database)
	s := storage.New(dir)
	hub := NewHub()
	go hub.Run()
	h := NewHandler(q, s, hub)

	_, err = database.Exec(`CREATE TRIGGER fail_file_create BEFORE INSERT ON file_versions WHEN NEW.vault_name = 'personal' AND NEW.path = 'notes/bad.md' BEGIN SELECT RAISE(ABORT, 'forced metadata failure'); END;`)
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	c := makeClient(hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/bad.md",
		Content: "should not stay",
		File: &FilePayload{
			Path:      "notes/bad.md",
			Exists:    true,
			LocalHash: "hash-bad",
		},
	}))

	resp := readResponse(t, c)
	if resp.Error == "" {
		t.Fatalf("expected error response, got %#v", resp)
	}
	if _, err := s.ReadFile("personal", "notes/bad.md"); err == nil {
		t.Fatal("expected no final file after failed message")
	}
	if _, err := q.GetFile("personal", "notes/bad.md"); err == nil {
		t.Fatal("expected no db row after DB failure")
	}
	assertNoGoatSyncTempFile(t, dir, filepath.Join("personal", "notes"))
}

func TestHandleFilePutDoesNotLeaveTempFilesAfterSuccess(t *testing.T) {
	h, _, _, dir := setupHandler(t)

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/new.md",
		Content: "hello",
		File: &FilePayload{
			Path:      "notes/new.md",
			Exists:    true,
			LocalHash: "hash-new",
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "filePutResult" || resp.Error != "" {
		t.Fatalf("expected successful filePutResult, got %#v", resp)
	}
	assertNoGoatSyncTempFile(t, dir, filepath.Join("personal", "notes"))
}

func TestHandleFilePutCreatesMissingVault(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/new.md",
		Content: "hello",
		File: &FilePayload{
			Path:      "notes/new.md",
			Exists:    true,
			LocalHash: "hash-new",
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "filePutResult" || resp.Error != "" {
		t.Fatalf("expected successful filePutResult, got %#v", resp)
	}
	if resp.Action != "okUpdateMeta" {
		t.Fatalf("expected okUpdateMeta, got %s", resp.Action)
	}
	exists, err := q.VaultExists("personal")
	if err != nil {
		t.Fatalf("vault exists: %v", err)
	}
	if !exists {
		t.Fatal("expected vault to be auto-created")
	}
	content, err := s.ReadFile("personal", "notes/new.md")
	if err != nil {
		t.Fatalf("expected file content: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("content = %q", content)
	}
}

func TestFilePutStoresHistoricalObjectRefs(t *testing.T) {
	h, q, _, _ := setupHandlerTest(t)
	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	sendJSON(t, h, c, IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/history.md",
		Content: "one",
		File: &FilePayload{
			Path:      "notes/history.md",
			Exists:    true,
			LocalHash: hashString("one"),
		},
	})
	resp := lastMessage(t, c)
	if resp.Type != "filePutResult" || resp.Action != "okUpdateMeta" {
		t.Fatalf("expected initial okUpdateMeta, got %#v", resp)
	}

	sendJSON(t, h, c, IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/history.md",
		Content: "two",
		File: &FilePayload{
			Path:        "notes/history.md",
			Exists:      true,
			BaseVersion: int64Ptr(1),
			LocalHash:   hashString("two"),
		},
	})
	resp = lastMessage(t, c)
	if resp.Type != "filePutResult" || resp.Action != "okUpdateMeta" {
		t.Fatalf("expected update okUpdateMeta, got %#v", resp)
	}

	base, err := q.GetFileVersion("personal", "notes/history.md", 1)
	if err != nil {
		t.Fatalf("get base version: %v", err)
	}
	content, err := h.storage.ReadObject(base.ContentRef)
	if err != nil {
		t.Fatalf("read version 1 object %q: %v", base.ContentRef, err)
	}
	if string(content) != "one" {
		t.Fatalf("version 1 object content = %q, want %q", content, "one")
	}
}

func TestReadFileContentDoesNotFallbackWhenContentRefMissing(t *testing.T) {
	h, _, s, _ := setupHandlerTest(t)
	if err := s.WriteFile("personal", "notes/history.md", []byte("latest")); err != nil {
		t.Fatalf("write latest file: %v", err)
	}

	content, err := h.readFileContent("personal", db.File{
		Path:       "notes/history.md",
		ContentRef: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	if err == nil {
		t.Fatalf("expected missing object error, got content %q", content)
	}
}

func TestHandleSyncInit_NoPrev_ActiveSameHash(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("content"))
	q.CreateFile("personal", "notes/hello.md", "serverhash", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "syncInit",
		Vault: "personal",
		Files: []FilePayload{
			{Path: "notes/hello.md", Exists: true, LocalHash: "serverhash"},
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
	q.CreateFile("personal", "notes/hello.md", "serverhash", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "syncInit",
		Vault: "personal",
		Files: []FilePayload{
			{Path: "notes/hello.md", Exists: true, LocalHash: "clienthash"},
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
	q.CreateFile("personal", "notes/hello.md", "hash1", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "syncInit",
		Vault: "personal",
		Files: []FilePayload{
			{Path: "notes/hello.md", Exists: true, BaseVersion: int64Ptr(1), BaseHash: "hash1", LocalHash: "hash1"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.ToDownload) != 0 || len(resp.Conflicts) != 0 {
		t.Errorf("expected all empty buckets, got download=%v conflicts=%v", resp.ToDownload, resp.Conflicts)
	}
}

func TestHandleSyncInit_WithPrev_OlderVersion_SameClientHash_ToUpdateMeta(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("content"))
	q.CreateFile("personal", "notes/hello.md", "hash1", "", "")
	q.UpdateFile("personal", "notes/hello.md", "hash2", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "syncInit",
		Vault: "personal",
		Files: []FilePayload{
			{Path: "notes/hello.md", Exists: true, BaseVersion: int64Ptr(1), BaseHash: "hash1", LocalHash: "hash2"},
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
	q.CreateFile("personal", "notes/hello.md", "hash1", "", "")
	q.UpdateFile("personal", "notes/hello.md", "hash2", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "syncInit",
		Vault: "personal",
		Files: []FilePayload{
			{Path: "notes/hello.md", Exists: true, BaseVersion: int64Ptr(1), BaseHash: "hash1", LocalHash: "hash1"},
		},
	}))

	resp := readResponse(t, c)
	if len(resp.Conflicts) != 1 {
		t.Errorf("expected 1 conflict, got %v", resp.Conflicts)
	}
}

func TestHandleSyncInit_ServerOnlyFile_ToDownload(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/server-only.md", []byte("server content"))
	q.CreateFile("personal", "notes/server-only.md", "serverhash", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "syncInit",
		Vault: "personal",
		Files: []FilePayload{},
	}))

	resp := readResponse(t, c)
	if len(resp.ToDownload) != 1 || resp.ToDownload[0].Path != "notes/server-only.md" {
		t.Errorf("expected server-only file in toDownload, got %v", resp.ToDownload)
	}
}

func TestHandleMessageNilClientDoesNotPanic(t *testing.T) {
	h, _, _, _ := setupHandler(t)

	h.HandleMessage(nil, mustJSON(IncomingMessage{
		Type:  "filePut",
		Vault: "",
		Path:  "notes/bad.md",
		File: &FilePayload{
			Path:      "notes/bad.md",
			Exists:    true,
			LocalHash: "hash-bad",
		},
	}))
}

func TestHandleUnknownMessageDoesNotCreateVault(t *testing.T) {
	h, q, _, _ := setupHandler(t)
	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "unknownType",
		Vault: "personal",
		Path:  "notes/a.md",
	}))

	exists, err := q.VaultExists("personal")
	if err != nil {
		t.Fatalf("vault exists: %v", err)
	}
	if exists {
		t.Fatal("expected unknown message not to create vault")
	}
}

func TestHandleFileCheck_UpToDate(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("content"))
	q.CreateFile("personal", "notes/hello.md", "samehash", "", "")
	base := int64(1)

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "fileCheck",
		Vault: "personal",
		Path:  "notes/hello.md",
		File: &FilePayload{
			Path:        "notes/hello.md",
			Exists:      true,
			BaseVersion: &base,
			LocalHash:   "samehash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Action != "upToDate" {
		t.Fatalf("expected action=upToDate, got %q", resp.Action)
	}
}

func TestHandleFileCheck_UpdateMeta(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("content"))
	sf, err := q.CreateFile("personal", "notes/hello.md", "hash1", "", "")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	t.Logf("server file: %#v", sf)

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "fileCheck",
		Vault: "personal",
		Path:  "notes/hello.md",
		File: &FilePayload{
			Path:      "notes/hello.md",
			Exists:    true,
			LocalHash: "hash1",
		},
	}))

	resp := readResponse(t, c)
	t.Logf("updateMeta response: %#v", resp)
	if resp.Action != "updateMeta" {
		t.Fatalf("expected action=updateMeta, got %q", resp.Action)
	}
	if resp.Meta == nil || resp.Meta.ServerVersion != 1 || resp.Meta.ServerHash != "hash1" {
		t.Fatalf("expected updateMeta meta for server version 1, got %#v", resp.Meta)
	}
}

func TestHandleFileCheck_Put(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("content"))
	sf, err := q.CreateFile("personal", "notes/hello.md", "oldhash", "", "")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	sf, err = q.UpdateFile("personal", "notes/hello.md", "newhash", "", "")
	if err != nil {
		t.Fatalf("update file: %v", err)
	}
	t.Logf("server file: %#v", sf)
	base := int64(2)

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "fileCheck",
		Vault: "personal",
		Path:  "notes/hello.md",
		File: &FilePayload{
			Path:        "notes/hello.md",
			Exists:      true,
			BaseVersion: &base,
			LocalHash:   "hash-from-client",
		},
	}))

	resp := readResponse(t, c)
	t.Logf("put response: %#v", resp)
	if resp.Action != "put" {
		t.Fatalf("expected action=put, got %q", resp.Action)
	}
	if resp.Meta == nil || resp.Meta.ServerVersion != 2 || resp.Meta.ServerHash != "newhash" {
		t.Fatalf("expected put meta for server version 2, got %#v", resp.Meta)
	}
}

func TestHandleFileCheck_ToDeleteLocal(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/old.md", []byte("content"))
	q.CreateFile("personal", "notes/old.md", "hash1", "", "")
	q.DeleteFile("personal", "notes/old.md")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "fileCheck",
		Vault: "personal",
		Path:  "notes/old.md",
		File: &FilePayload{
			Path:      "notes/old.md",
			Exists:    true,
			LocalHash: "hash1",
		},
	}))

	resp := readResponse(t, c)
	if resp.Action != "toDeleteLocal" {
		t.Fatalf("expected action=toDeleteLocal, got %q", resp.Action)
	}
	if resp.Meta == nil || !resp.Meta.IsDeleted || resp.Meta.ServerVersion != 2 {
		t.Fatalf("expected tombstone meta for server version 2, got %#v", resp.Meta)
	}
}

func TestHandleFileCheck_Conflict(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/conflict.md", []byte("server"))
	q.CreateFile("personal", "notes/conflict.md", "hash1", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "fileCheck",
		Vault: "personal",
		Path:  "notes/conflict.md",
		File: &FilePayload{
			Path:      "notes/conflict.md",
			Exists:    true,
			LocalHash: "localhash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Action != "conflict" {
		t.Fatalf("expected action=conflict, got %q", resp.Action)
	}
	if resp.Conflict == nil || resp.Conflict.ServerVersion != 1 {
		t.Fatalf("expected conflict server version 1, got %#v", resp.Conflict)
	}
}

func TestHandleFileCheck_DeleteConflict(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/tomb.md", []byte("stale"))
	q.CreateFile("personal", "notes/tomb.md", "serverhash", "", "")
	q.DeleteFile("personal", "notes/tomb.md")
	base := int64(1)

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "fileCheck",
		Vault: "personal",
		Path:  "notes/tomb.md",
		File: &FilePayload{
			Path:        "notes/tomb.md",
			Exists:      true,
			BaseVersion: &base,
			LocalHash:   "clienthash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Action != "deleteConflict" {
		t.Fatalf("expected action=deleteConflict, got %q", resp.Action)
	}
	if resp.Conflict == nil || resp.Conflict.ServerVersion != 2 || resp.Conflict.IsDeleted != true {
		t.Fatalf("expected tombstone deleteConflict payload, got %#v", resp.Conflict)
	}
}

func TestHandleFileCreate_New(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.CreateVaultDir("personal")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/new.md",
		Content: "# New Note",
		File: &FilePayload{
			Exists:    true,
			LocalHash: "hash1",
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "filePutResult" {
		t.Fatalf("expected filePutResult, got %s", resp.Type)
	}
	if resp.Action != "okUpdateMeta" {
		t.Fatalf("expected action=okUpdateMeta, got %s", resp.Action)
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
	q.CreateFile("personal", "notes/existing.md", "originalhash", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/existing.md",
		Content: "conflicting content",
		File: &FilePayload{
			Exists:    true,
			LocalHash: "newhash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Action != "conflict" {
		t.Fatalf("expected action=conflict, got %s", resp.Action)
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
	q.CreateFile("personal", "notes/old.md", "oldhash", "", "")
	q.DeleteFile("personal", "notes/old.md")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/old.md",
		Content: "new content",
		File: &FilePayload{
			Exists:      true,
			BaseVersion: int64Ptr(2),
			LocalHash:   "newhash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Action != "okUpdateMeta" {
		t.Fatalf("expected action=okUpdateMeta, got %s", resp.Action)
	}
	assertResponseMetaVersion(t, resp, 3)
}

func TestHandleFilePut_NewFile_OKUpdateMeta(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.CreateVaultDir("personal")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/new.md",
		Content: "content",
		File: &FilePayload{
			Path:      "notes/new.md",
			Exists:    true,
			LocalHash: "hash-new",
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "filePutResult" || resp.Action != "okUpdateMeta" {
		t.Fatalf("expected filePutResult okUpdateMeta, got %#v", resp)
	}
	if resp.Meta == nil || resp.Meta.ServerVersion != 1 || resp.Meta.ServerHash != "hash-new" {
		t.Fatalf("bad meta: %#v", resp.Meta)
	}
}

func TestHandleFileDelete_TombstoneRetry_OKUpdateMeta(t *testing.T) {
	h, q, _, _ := setupHandler(t)
	q.CreateVault("personal")
	q.CreateFile("personal", "notes/a.md", "hash-a", "", "")
	deleted, err := q.DeleteFile("personal", "notes/a.md")
	if err != nil {
		t.Fatalf("delete setup: %v", err)
	}
	base := deleted.Version - 1

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "fileDelete",
		Vault: "personal",
		Path:  "notes/a.md",
		File: &FilePayload{
			Path:        "notes/a.md",
			Exists:      false,
			BaseVersion: &base,
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "fileDeleteResult" || resp.Action != "okUpdateMeta" {
		t.Fatalf("expected fileDeleteResult okUpdateMeta, got %#v", resp)
	}
	if resp.Meta == nil || !resp.Meta.IsDeleted || resp.Meta.ServerVersion != deleted.Version {
		t.Fatalf("bad tombstone meta: %#v", resp.Meta)
	}
}

func TestHandleFileUpdate_Success(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("old"))
	q.CreateFile("personal", "notes/hello.md", "oldhash", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/hello.md",
		Content: "updated",
		File: &FilePayload{
			Exists:      true,
			BaseVersion: int64Ptr(1),
			LocalHash:   "newhash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "filePutResult" {
		t.Fatalf("expected filePutResult, got %s", resp.Type)
	}
	if resp.Action != "okUpdateMeta" {
		t.Fatalf("expected action=okUpdateMeta, got %s", resp.Action)
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
	q.CreateFile("personal", "notes/hello.md", "samehash", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/hello.md",
		Content: "content",
		File: &FilePayload{
			Exists:      true,
			BaseVersion: int64Ptr(1),
			LocalHash:   "samehash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Action != "okUpdateMeta" {
		t.Fatalf("expected action=okUpdateMeta, got %s", resp.Action)
	}
	if resp.Meta == nil || resp.Meta.ServerVersion != 1 {
		t.Fatalf("bad meta: %#v", resp.Meta)
	}
}

func TestHandleFileUpdate_Conflict(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("server version"))
	q.CreateFile("personal", "notes/hello.md", "hash1", "", "")
	q.UpdateFile("personal", "notes/hello.md", "hash2", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:    "filePut",
		Vault:   "personal",
		Path:    "notes/hello.md",
		Content: "client version",
		File: &FilePayload{
			Exists:      true,
			BaseVersion: int64Ptr(1),
			LocalHash:   "clienthash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Action != "conflict" {
		t.Fatalf("expected action=conflict, got %s", resp.Action)
	}
	if resp.Conflict == nil {
		t.Fatal("expected conflict info")
	}
	if got := conflictServerVersion(t, resp.Conflict); got != 2 {
		t.Errorf("expected server version=2, got %d", got)
	}
}

func TestHandleFileDelete_Success(t *testing.T) {
	h, q, s, dir := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/old.md", []byte("delete me"))
	q.CreateFile("personal", "notes/old.md", "hash1", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "fileDelete",
		Vault: "personal",
		Path:  "notes/old.md",
		File: &FilePayload{
			Exists:      false,
			BaseVersion: int64Ptr(1),
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "fileDeleteResult" {
		t.Fatalf("expected fileDeleteResult, got %s", resp.Type)
	}
	if resp.Action != "okUpdateMeta" {
		t.Fatalf("expected action=okUpdateMeta, got %s", resp.Action)
	}
	assertResponseMetaVersion(t, resp, 2)

	f, _ := q.GetFile("personal", "notes/old.md")
	if !f.IsDeleted {
		t.Error("expected file to be marked deleted")
	}
	if _, err := s.ReadFile("personal", "notes/old.md"); err == nil {
		t.Fatal("expected local file to be removed")
	}
	assertNoGoatSyncTrash(t, dir, filepath.Join("personal", "notes"))
}

func TestHandleFileDelete_Conflict(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("updated on server"))
	q.CreateFile("personal", "notes/hello.md", "hash1", "", "")
	q.UpdateFile("personal", "notes/hello.md", "hash2", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:  "fileDelete",
		Vault: "personal",
		Path:  "notes/hello.md",
		File: &FilePayload{
			Exists:      false,
			BaseVersion: int64Ptr(1),
		},
	}))

	resp := readResponse(t, c)
	if resp.Action != "deleteConflict" {
		t.Fatalf("expected action=deleteConflict, got %s", resp.Action)
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
		Type:  "fileDelete",
		Vault: "personal",
		Path:  "notes/nonexistent.md",
		File: &FilePayload{
			BaseVersion: int64Ptr(1),
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "fileDeleteResult" {
		t.Fatalf("expected fileDeleteResult, got %s", resp.Type)
	}
	if resp.Action != "okRemoveMeta" {
		t.Fatalf("expected action=okRemoveMeta, got %s", resp.Action)
	}
}

func TestHandleConflictResolve_LocalUpdate_Success(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("server content"))
	q.CreateFile("personal", "notes/hello.md", "serverhash", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:       "conflictResolve",
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
	if resp.Type != "conflictResolveResult" {
		t.Fatalf("expected conflictResolveResult, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatal("expected ok=true")
	}
	assertResponseMetaVersion(t, resp, 2)
}

func TestHandleConflictResolve_LocalUpdate_IdempotentRetryRewritesFile(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	q.CreateFile("personal", "notes/hello.md", "serverhash", "", "")
	updated, err := q.UpdateFile("personal", "notes/hello.md", "localhash", "", "")
	if err != nil {
		t.Fatalf("update setup: %v", err)
	}
	base := updated.Version - 1

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:       "conflictResolve",
		Vault:      "personal",
		Path:       "notes/hello.md",
		Resolution: "local",
		Content:    "local content",
		File: &FilePayload{
			BaseVersion: &base,
			LocalHash:   "localhash",
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "conflictResolveResult" {
		t.Fatalf("expected conflictResolveResult, got %s", resp.Type)
	}
	if resp.Ok == nil || !*resp.Ok {
		t.Fatalf("expected ok=true, got %#v", resp)
	}
	assertResponseMetaVersion(t, resp, updated.Version)

	content, err := s.ReadFile("personal", "notes/hello.md")
	if err != nil {
		t.Fatalf("expected retry to write file content: %v", err)
	}
	if string(content) != "local content" {
		t.Fatalf("content = %q", content)
	}
}

func TestHandleConflictResolve_LocalUpdate_Reconflict(t *testing.T) {
	h, q, s, _ := setupHandler(t)
	q.CreateVault("personal")
	s.WriteFile("personal", "notes/hello.md", []byte("newer server"))
	q.CreateFile("personal", "notes/hello.md", "hash1", "", "")
	q.UpdateFile("personal", "notes/hello.md", "hash2", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:       "conflictResolve",
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
	q.CreateFile("personal", "notes/old.md", "hash1", "", "")

	c := makeClient(h.hub, "personal")
	h.hub.Register <- c

	h.HandleMessage(c, mustJSON(IncomingMessage{
		Type:       "conflictResolve",
		Vault:      "personal",
		Path:       "notes/old.md",
		Resolution: "local",
		Action:     "delete",
		File: &FilePayload{
			BaseVersion: int64Ptr(1),
		},
	}))

	resp := readResponse(t, c)
	if resp.Type != "conflictResolveResult" {
		t.Fatalf("expected conflictResolveResult, got %s", resp.Type)
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

func TestMessageBoundaryLogsRawContent(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(previous) })

	h := &Handler{}
	h.HandleMessage(nil, mustJSON(IncomingMessage{
		Type:    "unknownType",
		Vault:   "personal",
		Path:    "a.md",
		Content: "secret body",
	}))

	c := &Client{send: make(chan []byte, 1)}
	c.SendMessage(OutgoingMessage{
		Type:    "fileCheckResult",
		Path:    "a.md",
		Content: "secret response",
	})

	logs := buf.String()
	if !strings.Contains(logs, "ws incoming raw") || !strings.Contains(logs, "ws outgoing raw") {
		t.Fatalf("expected incoming and outgoing log lines, got %q", logs)
	}
	if !strings.Contains(logs, "secret body") || !strings.Contains(logs, "secret response") {
		t.Fatalf("logs did not include raw content: %q", logs)
	}
}
