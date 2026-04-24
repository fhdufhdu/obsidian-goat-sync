package ws

import (
	"database/sql"
	"encoding/base64"
	"log"

	"obsidian-sync/internal/db"
	"obsidian-sync/internal/storage"
	syncpkg "obsidian-sync/internal/sync"
)

type Handler struct {
	queries *db.Queries
	storage *storage.Storage
	hub     *Hub
}

func NewHandler(q *db.Queries, s *storage.Storage, hub *Hub) *Handler {
	return &Handler{queries: q, storage: s, hub: hub}
}

func (h *Handler) HandleMessage(client *Client, data []byte) {
	msg, err := UnmarshalMessage(data)
	if err != nil {
		log.Printf("failed to parse message: %v", err)
		return
	}

	switch msg.Type {
	case "vault_create":
		h.handleVaultCreate(client, msg)
	case "sync_init":
		h.handleSyncInit(client, msg)
	case "file_check":
		h.handleFileCheck(client, msg)
	case "file_create":
		h.handleFileCreate(client, msg)
	case "file_update":
		h.handleFileUpdate(client, msg)
	case "file_delete":
		h.handleFileDelete(client, msg)
	case "conflict_resolve":
		h.handleConflictResolve(client, msg)
	default:
		log.Printf("unknown message type: %s", msg.Type)
	}
}

func (h *Handler) handleVaultCreate(client *Client, msg IncomingMessage) {
	if err := h.queries.CreateVault(msg.Vault); err != nil {
		client.SendMessage(OutgoingMessage{Type: "error", Error: err.Error()})
		return
	}
	h.storage.CreateVaultDir(msg.Vault)
	client.SendMessage(OutgoingMessage{Type: "vault_created", Vault: msg.Vault})
}

func (h *Handler) handleSyncInit(client *Client, msg IncomingMessage) {
	client.vault = msg.Vault

	serverFiles, err := h.queries.ListActiveFiles(msg.Vault)
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: "error", Error: err.Error()})
		return
	}

	serverMap := make(map[string]db.File)
	for _, f := range serverFiles {
		serverMap[f.Path] = f
	}

	clientPaths := make(map[string]bool)
	for _, f := range msg.Files {
		clientPaths[f.Path] = true
	}

	var toUpload []string
	var toUpdate []string
	var toDownload []DownloadEntry
	var toDelete []string
	var toUpdateMeta []UpdateMetaEntry
	var conflicts []SyncConflictEntry

	for _, cf := range msg.Files {
		initFile := db.SyncInitFile{
			Path:              cf.Path,
			PrevServerVersion: cf.PrevServerVersion,
			PrevServerHash:    cf.PrevServerHash,
			CurrentClientHash: cf.CurrentClientHash,
		}

		sf, sfExists := serverMap[cf.Path]

		var tombstoneFile db.File
		var tombstoneExists bool
		if !sfExists {
			tf, terr := h.queries.GetFile(msg.Vault, cf.Path)
			if terr == nil && tf.IsDeleted {
				tombstoneFile = tf
				tombstoneExists = true
			}
		}

		var serverFileForClassify db.File
		var serverExists bool
		var serverIsDeleted bool

		if sfExists {
			serverFileForClassify = sf
			serverExists = true
			serverIsDeleted = false
		} else if tombstoneExists {
			serverFileForClassify = tombstoneFile
			serverExists = true
			serverIsDeleted = true
		}

		result := syncpkg.ClassifyFile(initFile, serverFileForClassify, serverExists, serverIsDeleted)

		switch result.Action {
		case syncpkg.ActionToUpload:
			toUpload = append(toUpload, cf.Path)
		case syncpkg.ActionToUpdate:
			toUpdate = append(toUpdate, cf.Path)
		case syncpkg.ActionToDownload:
			entry, ok := h.makeDownloadEntry(msg.Vault, sf)
			if ok {
				toDownload = append(toDownload, entry)
			}
		case syncpkg.ActionToDelete:
			toDelete = append(toDelete, cf.Path)
		case syncpkg.ActionToUpdateMeta:
			if sfExists {
				toUpdateMeta = append(toUpdateMeta, UpdateMetaEntry{
					Path:                 cf.Path,
					CurrentServerVersion: sf.Version,
					CurrentServerHash:    sf.Hash,
				})
			} else if tombstoneExists {
				toUpdateMeta = append(toUpdateMeta, UpdateMetaEntry{
					Path:                 cf.Path,
					CurrentServerVersion: tombstoneFile.Version,
					CurrentServerHash:    tombstoneFile.Hash,
				})
			}
		case syncpkg.ActionConflict:
			if sfExists {
				content, err := h.storage.ReadFile(msg.Vault, cf.Path)
				if err != nil {
					continue
				}
				enc, encoded := encodeContent(content)
				conflicts = append(conflicts, SyncConflictEntry{
					Path:                 cf.Path,
					PrevServerVersion:    cf.PrevServerVersion,
					CurrentClientHash:    cf.CurrentClientHash,
					CurrentServerVersion: sf.Version,
					CurrentServerHash:    sf.Hash,
					CurrentServerContent: encoded,
					Encoding:             enc,
				})
			}
		case syncpkg.ActionSkip:
		}
	}

	for _, sf := range serverFiles {
		if !clientPaths[sf.Path] {
			entry, ok := h.makeDownloadEntry(msg.Vault, sf)
			if ok {
				toDownload = append(toDownload, entry)
			}
		}
	}

	client.SendMessage(OutgoingMessage{
		Type:         "sync_result",
		Vault:        msg.Vault,
		ToUpload:     toUpload,
		ToUpdate:     toUpdate,
		ToDownload:   toDownload,
		ToDelete:     toDelete,
		ToUpdateMeta: toUpdateMeta,
		Conflicts:    conflicts,
	})
}

func (h *Handler) handleFileCheck(client *Client, msg IncomingMessage) {
	sf, err := h.queries.GetFile(msg.Vault, msg.Path)
	serverExists := err == nil
	serverIsDeleted := serverExists && sf.IsDeleted

	initFile := db.SyncInitFile{
		Path:              msg.Path,
		PrevServerVersion: msg.PrevServerVersion,
		PrevServerHash:    msg.PrevServerHash,
		CurrentClientHash: msg.CurrentClientHash,
	}

	var sfForClassify db.File
	if serverExists {
		sfForClassify = sf
	}

	result := syncpkg.ClassifyFile(initFile, sfForClassify, serverExists, serverIsDeleted)

	resp := OutgoingMessage{
		Type: "file_check_result",
		Path: msg.Path,
	}

	switch result.Action {
	case syncpkg.ActionSkip:
		resp.Action = "up-to-date"
	case syncpkg.ActionToUpdateMeta:
		resp.Action = "update-meta"
		resp.CurrentServerVersion = sf.Version
		resp.CurrentServerHash = sf.Hash
	case syncpkg.ActionToDownload:
		content, rerr := h.storage.ReadFile(msg.Vault, msg.Path)
		if rerr != nil {
			client.SendMessage(OutgoingMessage{Type: "error", Error: rerr.Error()})
			return
		}
		enc, encoded := encodeContent(content)
		resp.Action = "download"
		resp.Content = encoded
		resp.Encoding = enc
		resp.CurrentServerVersion = sf.Version
		resp.CurrentServerHash = sf.Hash
	case syncpkg.ActionConflict:
		content, rerr := h.storage.ReadFile(msg.Vault, msg.Path)
		if rerr != nil {
			client.SendMessage(OutgoingMessage{Type: "error", Error: rerr.Error()})
			return
		}
		enc, encoded := encodeContent(content)
		resp.Action = "conflict"
		resp.Conflict = &ConflictInfo{
			CurrentServerVersion: sf.Version,
			CurrentServerHash:    sf.Hash,
			CurrentServerContent: encoded,
			Encoding:             enc,
		}
	case syncpkg.ActionToDelete:
		resp.Action = "deleted"
		resp.CurrentServerVersion = sf.Version
	default:
		resp.Action = "up-to-date"
	}

	client.SendMessage(resp)
}

func (h *Handler) handleFileCreate(client *Client, msg IncomingMessage) {
	sf, err := h.queries.GetFile(msg.Vault, msg.Path)
	serverExists := err == nil

	optResult := syncpkg.CheckFileCreate(sf, serverExists)

	if !optResult.OK {
		content, rerr := h.storage.ReadFile(msg.Vault, msg.Path)
		if rerr != nil {
			client.SendMessage(OutgoingMessage{Type: "file_create_result", Path: msg.Path, Ok: boolPtr(false), Error: rerr.Error()})
			return
		}
		enc, encoded := encodeContent(content)
		client.SendMessage(OutgoingMessage{
			Type: "file_create_result",
			Path: msg.Path,
			Ok:   boolPtr(false),
			Conflict: &ConflictInfo{
				CurrentServerVersion: sf.Version,
				CurrentServerHash:    sf.Hash,
				CurrentServerContent: encoded,
				Encoding:             enc,
			},
		})
		return
	}

	fileContent := decodeContent(msg.Content, msg.Encoding)
	if err := h.storage.WriteFile(msg.Vault, msg.Path, fileContent); err != nil {
		client.SendMessage(OutgoingMessage{Type: "file_create_result", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	var newFile db.File
	if serverExists && sf.IsDeleted {
		newFile, err = h.queries.CreateFileFromTombstone(msg.Vault, msg.Path, msg.CurrentClientHash, sf.Version)
	} else {
		newFile, err = h.queries.CreateFile(msg.Vault, msg.Path, msg.CurrentClientHash)
	}
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: "file_create_result", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	client.SendMessage(OutgoingMessage{
		Type:                 "file_create_result",
		Path:                 msg.Path,
		Ok:                   boolPtr(true),
		CurrentServerVersion: newFile.Version,
		CurrentServerHash:    newFile.Hash,
	})
}

func (h *Handler) handleFileUpdate(client *Client, msg IncomingMessage) {
	sf, err := h.queries.GetFile(msg.Vault, msg.Path)
	serverExists := err == nil && err != sql.ErrNoRows

	if err != nil && err != sql.ErrNoRows {
		client.SendMessage(OutgoingMessage{Type: "file_update_result", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	var prevVersion int64
	if msg.PrevServerVersion != nil {
		prevVersion = *msg.PrevServerVersion
	}

	optResult := syncpkg.CheckFileUpdate(sf, serverExists, prevVersion, msg.CurrentClientHash)

	if !optResult.OK {
		if optResult.ErrNoRows {
			client.SendMessage(OutgoingMessage{Type: "file_update_result", Path: msg.Path, Ok: boolPtr(false), Error: "file not found on server"})
			return
		}
		content, rerr := h.storage.ReadFile(msg.Vault, msg.Path)
		if rerr != nil {
			client.SendMessage(OutgoingMessage{Type: "file_update_result", Path: msg.Path, Ok: boolPtr(false), Error: rerr.Error()})
			return
		}
		enc, encoded := encodeContent(content)
		client.SendMessage(OutgoingMessage{
			Type: "file_update_result",
			Path: msg.Path,
			Ok:   boolPtr(false),
			Conflict: &ConflictInfo{
				CurrentServerVersion: sf.Version,
				CurrentServerHash:    sf.Hash,
				CurrentServerContent: encoded,
				Encoding:             enc,
			},
		})
		return
	}

	if optResult.Noop {
		client.SendMessage(OutgoingMessage{
			Type:                 "file_update_result",
			Path:                 msg.Path,
			Ok:                   boolPtr(true),
			Noop:                 true,
			CurrentServerVersion: sf.Version,
			CurrentServerHash:    sf.Hash,
		})
		return
	}

	fileContent := decodeContent(msg.Content, msg.Encoding)
	if werr := h.storage.WriteFile(msg.Vault, msg.Path, fileContent); werr != nil {
		client.SendMessage(OutgoingMessage{Type: "file_update_result", Path: msg.Path, Ok: boolPtr(false), Error: werr.Error()})
		return
	}

	newFile, uerr := h.queries.UpdateFile(msg.Vault, msg.Path, msg.CurrentClientHash)
	if uerr != nil {
		client.SendMessage(OutgoingMessage{Type: "file_update_result", Path: msg.Path, Ok: boolPtr(false), Error: uerr.Error()})
		return
	}

	client.SendMessage(OutgoingMessage{
		Type:                 "file_update_result",
		Path:                 msg.Path,
		Ok:                   boolPtr(true),
		CurrentServerVersion: newFile.Version,
		CurrentServerHash:    newFile.Hash,
	})
}

func (h *Handler) handleFileDelete(client *Client, msg IncomingMessage) {
	sf, err := h.queries.GetFile(msg.Vault, msg.Path)

	var prevVersion int64
	if msg.PrevServerVersion != nil {
		prevVersion = *msg.PrevServerVersion
	}

	optResult := syncpkg.CheckFileDelete(sf, err, prevVersion)

	if optResult.Noop {
		client.SendMessage(OutgoingMessage{
			Type: "file_delete_result",
			Path: msg.Path,
			Ok:   boolPtr(true),
		})
		return
	}

	if !optResult.OK {
		content, rerr := h.storage.ReadFile(msg.Vault, msg.Path)
		if rerr != nil {
			client.SendMessage(OutgoingMessage{Type: "file_delete_result", Path: msg.Path, Ok: boolPtr(false), Error: rerr.Error()})
			return
		}
		enc, encoded := encodeContent(content)
		client.SendMessage(OutgoingMessage{
			Type: "file_delete_result",
			Path: msg.Path,
			Ok:   boolPtr(false),
			Conflict: &ConflictInfo{
				CurrentServerVersion: sf.Version,
				CurrentServerHash:    sf.Hash,
				CurrentServerContent: encoded,
				Encoding:             enc,
			},
		})
		return
	}

	if derr := h.storage.DeleteFile(msg.Vault, msg.Path); derr != nil {
		log.Printf("storage delete error: %v", derr)
	}

	newFile, derr := h.queries.DeleteFile(msg.Vault, msg.Path)
	if derr != nil {
		client.SendMessage(OutgoingMessage{Type: "file_delete_result", Path: msg.Path, Ok: boolPtr(false), Error: derr.Error()})
		return
	}

	client.SendMessage(OutgoingMessage{
		Type:                 "file_delete_result",
		Path:                 msg.Path,
		Ok:                   boolPtr(true),
		CurrentServerVersion: newFile.Version,
	})
}

func (h *Handler) handleConflictResolve(client *Client, msg IncomingMessage) {
	if msg.Resolution == "local" && msg.Action == "delete" {
		h.handleConflictResolveDelete(client, msg)
		return
	}
	h.handleConflictResolveUpdate(client, msg)
}

func (h *Handler) handleConflictResolveUpdate(client *Client, msg IncomingMessage) {
	sf, err := h.queries.GetFile(msg.Vault, msg.Path)
	serverExists := err == nil && err != sql.ErrNoRows

	if err != nil && err != sql.ErrNoRows {
		client.SendMessage(OutgoingMessage{Type: "conflict_resolve_result", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	var prevVersion int64
	if msg.PrevServerVersion != nil {
		prevVersion = *msg.PrevServerVersion
	}

	optResult := syncpkg.CheckFileUpdate(sf, serverExists, prevVersion, msg.CurrentClientHash)

	if !optResult.OK {
		if optResult.ErrNoRows {
			client.SendMessage(OutgoingMessage{Type: "conflict_resolve_result", Path: msg.Path, Ok: boolPtr(false), Error: "file not found"})
			return
		}
		content, rerr := h.storage.ReadFile(msg.Vault, msg.Path)
		if rerr != nil {
			client.SendMessage(OutgoingMessage{Type: "conflict_resolve_result", Path: msg.Path, Ok: boolPtr(false), Error: rerr.Error()})
			return
		}
		enc, encoded := encodeContent(content)
		client.SendMessage(OutgoingMessage{
			Type: "conflict_resolve_result",
			Path: msg.Path,
			Ok:   boolPtr(false),
			Conflict: &ConflictInfo{
				CurrentServerVersion: sf.Version,
				CurrentServerHash:    sf.Hash,
				CurrentServerContent: encoded,
				Encoding:             enc,
			},
		})
		return
	}

	if optResult.Noop {
		client.SendMessage(OutgoingMessage{
			Type:                 "conflict_resolve_result",
			Path:                 msg.Path,
			Ok:                   boolPtr(true),
			CurrentServerVersion: sf.Version,
			CurrentServerHash:    sf.Hash,
		})
		return
	}

	fileContent := decodeContent(msg.Content, msg.Encoding)
	if werr := h.storage.WriteFile(msg.Vault, msg.Path, fileContent); werr != nil {
		client.SendMessage(OutgoingMessage{Type: "conflict_resolve_result", Path: msg.Path, Ok: boolPtr(false), Error: werr.Error()})
		return
	}

	newFile, uerr := h.queries.UpdateFile(msg.Vault, msg.Path, msg.CurrentClientHash)
	if uerr != nil {
		client.SendMessage(OutgoingMessage{Type: "conflict_resolve_result", Path: msg.Path, Ok: boolPtr(false), Error: uerr.Error()})
		return
	}

	client.SendMessage(OutgoingMessage{
		Type:                 "conflict_resolve_result",
		Path:                 msg.Path,
		Ok:                   boolPtr(true),
		CurrentServerVersion: newFile.Version,
		CurrentServerHash:    newFile.Hash,
	})
}

func (h *Handler) handleConflictResolveDelete(client *Client, msg IncomingMessage) {
	sf, err := h.queries.GetFile(msg.Vault, msg.Path)

	var prevVersion int64
	if msg.PrevServerVersion != nil {
		prevVersion = *msg.PrevServerVersion
	}

	optResult := syncpkg.CheckFileDelete(sf, err, prevVersion)

	if optResult.Noop {
		client.SendMessage(OutgoingMessage{
			Type: "conflict_resolve_result",
			Path: msg.Path,
			Ok:   boolPtr(true),
		})
		return
	}

	if !optResult.OK {
		content, rerr := h.storage.ReadFile(msg.Vault, msg.Path)
		if rerr != nil {
			client.SendMessage(OutgoingMessage{Type: "conflict_resolve_result", Path: msg.Path, Ok: boolPtr(false), Error: rerr.Error()})
			return
		}
		enc, encoded := encodeContent(content)
		client.SendMessage(OutgoingMessage{
			Type: "conflict_resolve_result",
			Path: msg.Path,
			Ok:   boolPtr(false),
			Conflict: &ConflictInfo{
				CurrentServerVersion: sf.Version,
				CurrentServerHash:    sf.Hash,
				CurrentServerContent: encoded,
				Encoding:             enc,
			},
		})
		return
	}

	h.storage.DeleteFile(msg.Vault, msg.Path)
	newFile, derr := h.queries.DeleteFile(msg.Vault, msg.Path)
	if derr != nil {
		client.SendMessage(OutgoingMessage{Type: "conflict_resolve_result", Path: msg.Path, Ok: boolPtr(false), Error: derr.Error()})
		return
	}

	client.SendMessage(OutgoingMessage{
		Type:                 "conflict_resolve_result",
		Path:                 msg.Path,
		Ok:                   boolPtr(true),
		CurrentServerVersion: newFile.Version,
	})
}

func (h *Handler) makeDownloadEntry(vault string, sf db.File) (DownloadEntry, bool) {
	content, err := h.storage.ReadFile(vault, sf.Path)
	if err != nil {
		return DownloadEntry{}, false
	}
	enc, encoded := encodeContent(content)
	return DownloadEntry{
		Path:                 sf.Path,
		Content:              encoded,
		CurrentServerVersion: sf.Version,
		CurrentServerHash:    sf.Hash,
		Encoding:             enc,
	}, true
}

func decodeContent(content, encoding string) []byte {
	if encoding == "base64" {
		data, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return []byte(content)
		}
		return data
	}
	return []byte(content)
}

func encodeContent(data []byte) (encoding string, content string) {
	if isBinary(data) {
		return "base64", base64.StdEncoding.EncodeToString(data)
	}
	return "", string(data)
}

func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
