package ws

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"log"
	"sort"
	"strings"

	"obsidian-goat-sync/internal/db"
	"obsidian-goat-sync/internal/storage"
	syncpkg "obsidian-goat-sync/internal/sync"
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

	unsupportedSyncInitActions := map[string]struct{}{}

	var toDownload []DownloadEntry
	var toUpdateMeta []UpdateMetaEntry
	var conflicts []SyncConflictEntry

	for _, cf := range msg.Files {
		initFile := db.SyncInitFile{
			Path:              cf.Path,
			PrevServerVersion: cf.BaseVersion,
			PrevServerHash:    cf.BaseHash,
			CurrentClientHash: cf.LocalHash,
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
			unsupportedSyncInitActions["toUpload"] = struct{}{}
		case syncpkg.ActionToUpdate:
			unsupportedSyncInitActions["toUpdate"] = struct{}{}
		case syncpkg.ActionToDownload:
			entry, ok := h.makeDownloadEntry(msg.Vault, sf)
			if ok {
				toDownload = append(toDownload, entry)
			}
		case syncpkg.ActionToDelete:
			unsupportedSyncInitActions["toDelete"] = struct{}{}
		case syncpkg.ActionToUpdateMeta:
			if sfExists {
				toUpdateMeta = append(toUpdateMeta, UpdateMetaEntry{
					Path:          cf.Path,
					ServerVersion: sf.Version,
					ServerHash:    sf.Hash,
				})
			} else if tombstoneExists {
				toUpdateMeta = append(toUpdateMeta, UpdateMetaEntry{
					Path:          cf.Path,
					ServerVersion: tombstoneFile.Version,
					ServerHash:    tombstoneFile.Hash,
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
					Path:          cf.Path,
					BaseVersion:   cf.BaseVersion,
					LocalHash:     cf.LocalHash,
					ServerVersion: sf.Version,
					ServerHash:    sf.Hash,
					ServerContent: encoded,
					IsDeleted:     serverIsDeleted,
					Encoding:      enc,
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

	var unsupportedActions []string
	for action := range unsupportedSyncInitActions {
		unsupportedActions = append(unsupportedActions, action)
	}
	if len(unsupportedActions) > 0 {
		sort.Strings(unsupportedActions)
		client.SendMessage(OutgoingMessage{
			Type:         "sync_result",
			Vault:        msg.Vault,
			ToDownload:   toDownload,
			ToUpdateMeta: toUpdateMeta,
			Conflicts:    conflicts,
			Error:        "legacy sync_init actions not yet supported: " + strings.Join(unsupportedActions, ", "),
		})
		return
	}

	client.SendMessage(OutgoingMessage{
		Type:         "sync_result",
		Vault:        msg.Vault,
		ToDownload:   toDownload,
		ToUpdateMeta: toUpdateMeta,
		Conflicts:    conflicts,
	})
}

func (h *Handler) handleFileCheck(client *Client, msg IncomingMessage) {
	sf, err := h.queries.GetFile(msg.Vault, msg.Path)
	serverExists := err == nil
	serverIsDeleted := serverExists && sf.IsDeleted

	baseVersion, baseHash, currentHash, err := protocolPayloadValues(msg, false, false)
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: "file_check_result", Path: msg.Path, Error: err.Error()})
		return
	}

	initFile := db.SyncInitFile{
		Path:              msg.Path,
		PrevServerVersion: baseVersion,
		PrevServerHash:    baseHash,
		CurrentClientHash: currentHash,
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
		resp.Meta = &ServerMetaPayload{
			Path:          msg.Path,
			ServerVersion: sf.Version,
			ServerHash:    sf.Hash,
		}
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
		resp.Meta = &ServerMetaPayload{
			Path:          msg.Path,
			ServerVersion: sf.Version,
			ServerHash:    sf.Hash,
		}
	case syncpkg.ActionConflict:
		content, rerr := h.storage.ReadFile(msg.Vault, msg.Path)
		if rerr != nil {
			client.SendMessage(OutgoingMessage{Type: "error", Error: rerr.Error()})
			return
		}
		enc, encoded := encodeContent(content)
		resp.Action = "conflict"
		resp.Conflict = &ConflictInfo{
			ServerVersion: sf.Version,
			ServerHash:    sf.Hash,
			ServerContent: encoded,
			IsDeleted:     serverIsDeleted,
			Encoding:      enc,
		}
	case syncpkg.ActionToDelete:
		resp.Action = "deleted"
		resp.Meta = &ServerMetaPayload{
			Path:          msg.Path,
			ServerVersion: sf.Version,
			ServerHash:    sf.Hash,
			IsDeleted:     true,
		}
	default:
		resp.Action = "up-to-date"
	}

	client.SendMessage(resp)
}

func (h *Handler) handleFileCreate(client *Client, msg IncomingMessage) {
	sf, err := h.queries.GetFile(msg.Vault, msg.Path)
	serverExists := err == nil

	_, _, currentClientHash, err := protocolPayloadValues(msg, false, true)
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: "file_create_result", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

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
				ServerVersion: sf.Version,
				ServerHash:    sf.Hash,
				ServerContent: encoded,
				IsDeleted:     sf.IsDeleted,
				Encoding:      enc,
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
		newFile, err = h.queries.CreateFileFromTombstone(msg.Vault, msg.Path, currentClientHash, sf.Version)
	} else {
		newFile, err = h.queries.CreateFile(msg.Vault, msg.Path, currentClientHash)
	}
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: "file_create_result", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	client.SendMessage(OutgoingMessage{
		Type: "file_create_result",
		Path: msg.Path,
		Ok:   boolPtr(true),
		Meta: &ServerMetaPayload{
			Path:          msg.Path,
			ServerVersion: newFile.Version,
			ServerHash:    newFile.Hash,
			IsDeleted:     false,
		},
	})
}

func (h *Handler) handleFileUpdate(client *Client, msg IncomingMessage) {
	sf, err := h.queries.GetFile(msg.Vault, msg.Path)
	serverExists := err == nil && err != sql.ErrNoRows

	if err != nil && err != sql.ErrNoRows {
		client.SendMessage(OutgoingMessage{Type: "file_update_result", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	baseVersion, _, currentClientHash, err := protocolPayloadValues(msg, true, true)
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: "file_update_result", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	var prevVersion int64
	if baseVersion != nil {
		prevVersion = *baseVersion
	}

	optResult := syncpkg.CheckFileUpdate(sf, serverExists, prevVersion, currentClientHash)

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
				ServerVersion: sf.Version,
				ServerHash:    sf.Hash,
				ServerContent: encoded,
				IsDeleted:     sf.IsDeleted,
				Encoding:      enc,
			},
		})
		return
	}

	if optResult.Noop {
		client.SendMessage(OutgoingMessage{
			Type:   "file_update_result",
			Path:   msg.Path,
			Ok:     boolPtr(true),
			Action: "noop",
			Meta: &ServerMetaPayload{
				Path:          msg.Path,
				ServerVersion: sf.Version,
				ServerHash:    sf.Hash,
				IsDeleted:     sf.IsDeleted,
			},
		})
		return
	}

	fileContent := decodeContent(msg.Content, msg.Encoding)
	if werr := h.storage.WriteFile(msg.Vault, msg.Path, fileContent); werr != nil {
		client.SendMessage(OutgoingMessage{Type: "file_update_result", Path: msg.Path, Ok: boolPtr(false), Error: werr.Error()})
		return
	}

	newFile, uerr := h.queries.UpdateFile(msg.Vault, msg.Path, currentClientHash)
	if uerr != nil {
		client.SendMessage(OutgoingMessage{Type: "file_update_result", Path: msg.Path, Ok: boolPtr(false), Error: uerr.Error()})
		return
	}

	client.SendMessage(OutgoingMessage{
		Type: "file_update_result",
		Path: msg.Path,
		Ok:   boolPtr(true),
		Meta: &ServerMetaPayload{
			Path:          msg.Path,
			ServerVersion: newFile.Version,
			ServerHash:    newFile.Hash,
		},
	})
}

func (h *Handler) handleFileDelete(client *Client, msg IncomingMessage) {
	sf, fileErr := h.queries.GetFile(msg.Vault, msg.Path)

	baseVersion, _, _, err := protocolPayloadValues(msg, true, false)
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: "file_delete_result", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	var prevVersion int64
	if baseVersion != nil {
		prevVersion = *baseVersion
	}

	optResult := syncpkg.CheckFileDelete(sf, fileErr, prevVersion)

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
				ServerVersion: sf.Version,
				ServerHash:    sf.Hash,
				ServerContent: encoded,
				IsDeleted:     sf.IsDeleted,
				Encoding:      enc,
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
		Type: "file_delete_result",
		Path: msg.Path,
		Ok:   boolPtr(true),
		Meta: &ServerMetaPayload{
			Path:          msg.Path,
			ServerVersion: newFile.Version,
			ServerHash:    newFile.Hash,
			IsDeleted:     newFile.IsDeleted,
		},
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

	baseVersion, _, currentClientHash, err := protocolPayloadValues(msg, true, true)
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: "conflict_resolve_result", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	var prevVersion int64
	if baseVersion != nil {
		prevVersion = *baseVersion
	}

	optResult := syncpkg.CheckFileUpdate(sf, serverExists, prevVersion, currentClientHash)

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
				ServerVersion: sf.Version,
				ServerHash:    sf.Hash,
				ServerContent: encoded,
				IsDeleted:     sf.IsDeleted,
				Encoding:      enc,
			},
		})
		return
	}

	if optResult.Noop {
		client.SendMessage(OutgoingMessage{
			Type: "conflict_resolve_result",
			Path: msg.Path,
			Ok:   boolPtr(true),
			Meta: &ServerMetaPayload{
				Path:          msg.Path,
				ServerVersion: sf.Version,
				ServerHash:    sf.Hash,
				IsDeleted:     sf.IsDeleted,
			},
		})
		return
	}

	fileContent := decodeContent(msg.Content, msg.Encoding)
	if werr := h.storage.WriteFile(msg.Vault, msg.Path, fileContent); werr != nil {
		client.SendMessage(OutgoingMessage{Type: "conflict_resolve_result", Path: msg.Path, Ok: boolPtr(false), Error: werr.Error()})
		return
	}

	newFile, uerr := h.queries.UpdateFile(msg.Vault, msg.Path, currentClientHash)
	if uerr != nil {
		client.SendMessage(OutgoingMessage{Type: "conflict_resolve_result", Path: msg.Path, Ok: boolPtr(false), Error: uerr.Error()})
		return
	}

	client.SendMessage(OutgoingMessage{
		Type: "conflict_resolve_result",
		Path: msg.Path,
		Ok:   boolPtr(true),
		Meta: &ServerMetaPayload{
			Path:          msg.Path,
			ServerVersion: newFile.Version,
			ServerHash:    newFile.Hash,
			IsDeleted:     newFile.IsDeleted,
		},
	})
}

func (h *Handler) handleConflictResolveDelete(client *Client, msg IncomingMessage) {
	sf, fileErr := h.queries.GetFile(msg.Vault, msg.Path)

	baseVersion, _, _, err := protocolPayloadValues(msg, true, false)
	if err != nil {
		client.SendMessage(OutgoingMessage{Type: "conflict_resolve_result", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	var prevVersion int64
	if baseVersion != nil {
		prevVersion = *baseVersion
	}

	optResult := syncpkg.CheckFileDelete(sf, fileErr, prevVersion)

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
				ServerVersion: sf.Version,
				ServerHash:    sf.Hash,
				ServerContent: encoded,
				IsDeleted:     sf.IsDeleted,
				Encoding:      enc,
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
		Type: "conflict_resolve_result",
		Path: msg.Path,
		Ok:   boolPtr(true),
		Meta: &ServerMetaPayload{
			Path:          msg.Path,
			ServerVersion: newFile.Version,
			ServerHash:    newFile.Hash,
			IsDeleted:     newFile.IsDeleted,
		},
	})
}

func protocolPayloadValues(msg IncomingMessage, requireBaseVersion, requireLocalHash bool) (baseVersion *int64, baseHash string, localHash string, err error) {
	if msg.File == nil {
		return nil, "", "", errors.New("missing file payload")
	}

	if requireBaseVersion && msg.File.BaseVersion == nil {
		return nil, msg.File.BaseHash, msg.File.LocalHash, errors.New("missing file.baseVersion")
	}
	if requireLocalHash && msg.File.LocalHash == "" {
		return msg.File.BaseVersion, msg.File.BaseHash, "", errors.New("missing file.localHash")
	}

	return msg.File.BaseVersion, msg.File.BaseHash, msg.File.LocalHash, nil
}

func (h *Handler) makeDownloadEntry(vault string, sf db.File) (DownloadEntry, bool) {
	content, err := h.storage.ReadFile(vault, sf.Path)
	if err != nil {
		return DownloadEntry{}, false
	}
	enc, encoded := encodeContent(content)
	return DownloadEntry{
		Path:          sf.Path,
		Content:       encoded,
		ServerVersion: sf.Version,
		ServerHash:    sf.Hash,
		Encoding:      enc,
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
