package ws

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"log"

	"obsidian-goat-sync/internal/db"
	"obsidian-goat-sync/internal/storage"
	syncpkg "obsidian-goat-sync/internal/sync"
)

type Handler struct {
	queries *db.Queries
	storage *storage.Storage
	hub     *Hub
}

type messageSender interface {
	SendMessage(OutgoingMessage)
}

type responseRecorder struct {
	messages []OutgoingMessage
	failed   bool
}

func (r *responseRecorder) SendMessage(msg OutgoingMessage) {
	if msg.Error != "" {
		r.failed = true
	}
	r.messages = append(r.messages, msg)
}

type rollbackResponseError struct{}

func (rollbackResponseError) Error() string {
	return "rollback after websocket error response"
}

func NewHandler(q *db.Queries, s *storage.Storage, hub *Hub) *Handler {
	return &Handler{queries: q, storage: s, hub: hub}
}

func (h *Handler) withQueries(q *db.Queries) *Handler {
	clone := *h
	clone.queries = q
	return &clone
}

func (h *Handler) HandleMessage(client *Client, data []byte) {
	msg, err := UnmarshalMessage(data)
	if err != nil {
		log.Printf("failed to parse message: %v", err)
		return
	}
	log.Printf("ws incoming raw: %s", string(data))

	if h == nil || h.queries == nil {
		log.Printf("unknown message type: %s", msg.Type)
		return
	}
	if !isKnownMessageType(msg.Type) {
		log.Printf("unknown message type: %s", msg.Type)
		return
	}

	var recorder responseRecorder
	var finalizers []func() error
	var rollbacks []func() error

	var sender messageSender = &recorder
	if client != nil {
		sender = client
	}

	err = h.queries.InTx(func(txq *db.Queries) error {
		if msg.Type != "" {
			if msg.Vault == "" {
				recorder.SendMessage(OutgoingMessage{Type: "error", Error: db.ErrInvalidVaultName.Error()})
				return rollbackResponseError{}
			}
			if err := txq.EnsureVault(msg.Vault); err != nil {
				recorder.SendMessage(OutgoingMessage{Type: "error", Vault: msg.Vault, Error: err.Error()})
				return rollbackResponseError{}
			}
		}
		txh := h.withQueries(txq)
		txh.dispatchMessage(&recorder, client, msg, &finalizers, &rollbacks)
		if recorder.failed {
			return rollbackResponseError{}
		}
		return nil
	})

	if err != nil {
		for _, rollback := range rollbacks {
			if rerr := rollback(); rerr != nil {
				log.Printf("ws rollback failed: %v", rerr)
			}
		}
		if _, ok := err.(rollbackResponseError); ok {
			for _, out := range recorder.messages {
				sender.SendMessage(out)
			}
			return
		}
		sender.SendMessage(OutgoingMessage{Type: "error", Vault: msg.Vault, Path: msg.Path, Error: err.Error()})
		return
	}

	for _, out := range recorder.messages {
		if out.Error != "" {
			for _, rollback := range rollbacks {
				if rerr := rollback(); rerr != nil {
					log.Printf("ws rollback failed: %v", rerr)
				}
			}
			for _, out := range recorder.messages {
				sender.SendMessage(out)
			}
			return
		}
	}

	for _, finalize := range finalizers {
		if ferr := finalize(); ferr != nil {
			log.Printf("ws finalize failed: %v", ferr)
			sender.SendMessage(OutgoingMessage{Type: "error", Vault: msg.Vault, Path: msg.Path, Error: ferr.Error()})
			return
		}
	}
	for _, out := range recorder.messages {
		sender.SendMessage(out)
	}
}

func isKnownMessageType(messageType string) bool {
	switch messageType {
	case "vaultCreate", "syncInit", "fileCheck", "filePut", "fileDelete", "conflictResolve", "mergePut":
		return true
	default:
		return false
	}
}

func (h *Handler) dispatchMessage(sender messageSender, client *Client, msg IncomingMessage, finalizers *[]func() error, rollbacks *[]func() error) {
	switch msg.Type {
	case "vaultCreate":
		h.handleVaultCreate(sender, msg)
	case "syncInit":
		h.handleSyncInit(sender, client, msg)
	case "fileCheck":
		h.handleFileCheck(sender, msg)
	case "filePut":
		h.handleFilePut(sender, msg, finalizers, rollbacks)
	case "fileDelete":
		h.handleFileDelete(sender, msg, finalizers, rollbacks)
	case "conflictResolve":
		h.handleConflictResolve(sender, client, msg, finalizers, rollbacks)
	case "mergePut":
		h.handleMergePut(sender, msg, finalizers, rollbacks)
	default:
		log.Printf("unknown message type: %s", msg.Type)
	}
}

func (h *Handler) handleMergePut(sender messageSender, msg IncomingMessage, finalizers *[]func() error, rollbacks *[]func() error) {
	sender.SendMessage(OutgoingMessage{Type: "mergePutResult", Path: msg.Path, Action: string(syncpkg.MatrixActionConflict), Error: "mergePut handler registered before merge behavior"})
}

func (h *Handler) handleVaultCreate(sender messageSender, msg IncomingMessage) {
	if err := h.queries.EnsureVault(msg.Vault); err != nil {
		sender.SendMessage(OutgoingMessage{Type: "error", Vault: msg.Vault, Error: err.Error()})
		return
	}
	if err := h.storage.CreateVaultDir(msg.Vault); err != nil {
		sender.SendMessage(OutgoingMessage{Type: "error", Vault: msg.Vault, Error: err.Error()})
		return
	}
	sender.SendMessage(OutgoingMessage{Type: "vault_created", Vault: msg.Vault})
}

func (h *Handler) handleSyncInit(sender messageSender, client *Client, msg IncomingMessage) {
	if client != nil {
		client.vault = msg.Vault
	}

	serverFiles, err := h.queries.ListActiveFiles(msg.Vault)
	if err != nil {
		sender.SendMessage(OutgoingMessage{Type: "error", Error: err.Error()})
		return
	}

	clientPaths := make(map[string]bool)
	for _, f := range msg.Files {
		clientPaths[f.Path] = true
	}

	var toDownload []DownloadEntry
	var toUpdateMeta []UpdateMetaEntry
	var toPut []string
	var toDeleteLocal []ServerMetaPayload
	var toRemoveMeta []ServerMetaPayload
	var conflicts []SyncConflictEntry

	for _, cf := range msg.Files {
		input, sf, _, err := h.decisionInputForPath(msg, cf, syncpkg.MessageSyncInit)
		if err != nil {
			sender.SendMessage(OutgoingMessage{Type: "error", Error: err.Error()})
			return
		}
		result := syncpkg.DecideSyncInit(input)

		switch result.Action {
		case syncpkg.MatrixActionToDownload:
			entry, ok := h.makeDownloadEntry(msg.Vault, sf)
			if ok {
				toDownload = append(toDownload, entry)
			}
		case syncpkg.MatrixActionToPut:
			toPut = append(toPut, cf.Path)
		case syncpkg.MatrixActionToUpdateMeta:
			toUpdateMeta = append(toUpdateMeta, *serverMeta(sf))
		case syncpkg.MatrixActionToDeleteLocal:
			toDeleteLocal = append(toDeleteLocal, *serverMeta(sf))
		case syncpkg.MatrixActionToRemoveMeta:
			meta := serverMeta(sf)
			if meta.Path == "" {
				meta = &ServerMetaPayload{Path: cf.Path}
			}
			toRemoveMeta = append(toRemoveMeta, *meta)
		case syncpkg.MatrixActionConflict, syncpkg.MatrixActionDeleteConflict:
			conflict, ok := h.makeSyncInitConflict(msg.Vault, cf, sf)
			if ok {
				conflicts = append(conflicts, conflict)
			}
		case syncpkg.MatrixActionNone:
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
	sender.SendMessage(OutgoingMessage{
		Type:          "syncResult",
		Vault:         msg.Vault,
		ToDownload:    toDownload,
		ToUpdateMeta:  toUpdateMeta,
		ToPut:         toPut,
		ToDeleteLocal: toDeleteLocal,
		ToRemoveMeta:  toRemoveMeta,
		Conflicts:     conflicts,
	})
}

func (h *Handler) handleFileCheck(sender messageSender, msg IncomingMessage) {
	if msg.File == nil {
		sender.SendMessage(OutgoingMessage{Type: "fileCheckResult", Path: msg.Path, Error: "missing file payload"})
		return
	}

	input, sf, _, err := h.decisionInputForPath(msg, *msg.File, syncpkg.MessageFileCheck)
	if err != nil {
		sender.SendMessage(OutgoingMessage{Type: "fileCheckResult", Path: msg.Path, Error: err.Error()})
		return
	}
	result := syncpkg.DecideFileCheck(input)

	resp := OutgoingMessage{
		Type: "fileCheckResult",
		Path: msg.Path,
	}

	switch result.Action {
	case syncpkg.MatrixActionPut:
		resp.Action = string(syncpkg.MatrixActionPut)
		resp.Meta = &ServerMetaPayload{
			Path:          msg.Path,
			ServerVersion: sf.Version,
			ServerHash:    sf.Hash,
		}
	case syncpkg.MatrixActionUpdateMeta:
		resp.Action = string(syncpkg.MatrixActionUpdateMeta)
		resp.Meta = &ServerMetaPayload{
			Path:          msg.Path,
			ServerVersion: sf.Version,
			ServerHash:    sf.Hash,
		}
	case syncpkg.MatrixActionToDownload:
		entry, ok := h.makeDownloadEntry(msg.Vault, sf)
		if !ok {
			sender.SendMessage(OutgoingMessage{Type: "fileCheckResult", Path: msg.Path, Error: "failed to read server file"})
			return
		}
		resp.Action = string(syncpkg.MatrixActionToDownload)
		resp.Content = entry.Content
		resp.Encoding = entry.Encoding
		resp.Meta = &ServerMetaPayload{
			Path:          entry.Path,
			ServerVersion: entry.ServerVersion,
			ServerHash:    entry.ServerHash,
		}
	case syncpkg.MatrixActionToDeleteLocal:
		resp.Action = string(syncpkg.MatrixActionToDeleteLocal)
		resp.Meta = &ServerMetaPayload{
			Path:          msg.Path,
			ServerVersion: sf.Version,
			ServerHash:    sf.Hash,
			IsDeleted:     sf.IsDeleted,
		}
	case syncpkg.MatrixActionConflict, syncpkg.MatrixActionDeleteConflict:
		content, rerr := h.readFileContent(msg.Vault, sf)
		if rerr != nil {
			sender.SendMessage(OutgoingMessage{Type: "fileCheckResult", Path: msg.Path, Action: string(result.Action), Error: rerr.Error()})
			return
		}
		enc, encoded := encodeContentWithRowEncoding(content, sf.Encoding)
		resp.Action = string(result.Action)
		resp.Conflict = &ConflictInfo{
			ServerVersion: sf.Version,
			ServerHash:    sf.Hash,
			ServerContent: encoded,
			IsDeleted:     sf.IsDeleted,
			Encoding:      enc,
		}
	case syncpkg.MatrixActionUpToDate:
		resp.Action = string(syncpkg.MatrixActionUpToDate)
	case syncpkg.MatrixActionToRemoveMeta:
		resp.Action = string(syncpkg.MatrixActionToRemoveMeta)
	default:
		resp.Action = string(syncpkg.MatrixActionUpToDate)
	}

	sender.SendMessage(resp)
}

func (h *Handler) makeSyncInitConflict(vault string, file FilePayload, sf db.File) (SyncConflictEntry, bool) {
	content, err := h.readFileContent(vault, sf)
	if err != nil {
		return SyncConflictEntry{}, false
	}
	enc, encoded := encodeContentWithRowEncoding(content, sf.Encoding)
	return SyncConflictEntry{
		Path:          file.Path,
		BaseVersion:   file.BaseVersion,
		LocalHash:     file.LocalHash,
		ServerVersion: sf.Version,
		ServerHash:    sf.Hash,
		ServerContent: encoded,
		IsDeleted:     sf.IsDeleted,
		Encoding:      enc,
	}, true
}

func (h *Handler) decisionInputForPath(msg IncomingMessage, payload FilePayload, message syncpkg.MatrixMessage) (syncpkg.DecisionInput, db.File, bool, error) {
	path := payload.Path
	if path == "" {
		path = msg.Path
	}
	sf, err := h.queries.GetFile(msg.Vault, path)
	if err != nil && err != sql.ErrNoRows {
		return syncpkg.DecisionInput{}, db.File{}, false, err
	}

	state := syncpkg.ServerMissing
	serverExists := false
	if err == nil {
		serverExists = true
		if sf.IsDeleted {
			state = syncpkg.ServerTombstone
		} else {
			state = syncpkg.ServerActive
		}
	}

	deletedFrom := int64(0)
	if serverExists && sf.IsDeleted {
		deletedFrom = sf.DeletedFromVersion()
	}

	return syncpkg.DecisionInput{
		Message:            message,
		ClientExists:       payload.Exists,
		BaseVersion:        payload.BaseVersion,
		LocalHash:          payload.LocalHash,
		ServerState:        state,
		ServerVersion:      sf.Version,
		ServerHash:         sf.Hash,
		DeletedFromVersion: deletedFrom,
	}, sf, serverExists, nil
}

func serverMeta(f db.File) *ServerMetaPayload {
	return &ServerMetaPayload{
		Path:          f.Path,
		ServerVersion: f.Version,
		ServerHash:    f.Hash,
		IsDeleted:     f.IsDeleted,
	}
}

func (h *Handler) sendConflictResult(sender messageSender, messageType, vault, path string, action syncpkg.MatrixAction, sf db.File) {
	content, err := h.readFileContent(vault, sf)
	if err != nil {
		sender.SendMessage(OutgoingMessage{Type: messageType, Path: path, Action: string(action), Error: err.Error()})
		return
	}
	enc, encoded := encodeContentWithRowEncoding(content, sf.Encoding)
	sender.SendMessage(OutgoingMessage{
		Type:   messageType,
		Path:   path,
		Action: string(action),
		Conflict: &ConflictInfo{
			ServerVersion: sf.Version,
			ServerHash:    sf.Hash,
			ServerContent: encoded,
			IsDeleted:     sf.IsDeleted,
			Encoding:      enc,
		},
	})
}

func (h *Handler) handleFilePut(sender messageSender, msg IncomingMessage, finalizers *[]func() error, rollbacks *[]func() error) {
	if msg.File == nil {
		sender.SendMessage(OutgoingMessage{Type: "filePutResult", Path: msg.Path, Action: string(syncpkg.MatrixActionConflict), Error: "missing file payload"})
		return
	}
	input, sf, serverExists, err := h.decisionInputForPath(msg, *msg.File, syncpkg.MessageFilePut)
	if err != nil {
		sender.SendMessage(OutgoingMessage{Type: "filePutResult", Path: msg.Path, Error: err.Error()})
		return
	}
	path := msg.File.Path
	if path == "" {
		path = msg.Path
	}

	result := syncpkg.DecideFilePut(input)
	switch result.Action {
	case syncpkg.MatrixActionOkUpdateMeta:
		fileContent := decodeContent(msg.Content, msg.Encoding)
		contentRef, err := h.stageContent(msg.Vault, path, fileContent, finalizers, rollbacks)
		if err != nil {
			sender.SendMessage(OutgoingMessage{Type: "filePutResult", Path: path, Error: err.Error()})
			return
		}

		var newFile db.File
		if !serverExists {
			newFile, err = h.queries.CreateFile(msg.Vault, path, msg.File.LocalHash, contentRef, msg.Encoding)
		} else if sf.IsDeleted {
			newFile, err = h.queries.CreateFileFromTombstone(msg.Vault, path, msg.File.LocalHash, contentRef, msg.Encoding, sf.Version)
		} else if msg.File.BaseVersion != nil && *msg.File.BaseVersion == sf.Version && msg.File.LocalHash != sf.Hash {
			newFile, err = h.queries.UpdateFile(msg.Vault, path, msg.File.LocalHash, contentRef, msg.Encoding)
		} else {
			newFile = sf
		}
		if err != nil {
			sender.SendMessage(OutgoingMessage{Type: "filePutResult", Path: path, Error: err.Error()})
			return
		}
		sender.SendMessage(OutgoingMessage{Type: "filePutResult", Path: path, Action: "okUpdateMeta", Meta: serverMeta(newFile)})
	case syncpkg.MatrixActionToDeleteLocal:
		sender.SendMessage(OutgoingMessage{Type: "filePutResult", Path: path, Action: "toDeleteLocal", Meta: serverMeta(sf)})
	case syncpkg.MatrixActionConflict, syncpkg.MatrixActionDeleteConflict:
		h.sendConflictResult(sender, "filePutResult", msg.Vault, path, result.Action, sf)
	default:
		sender.SendMessage(OutgoingMessage{Type: "filePutResult", Path: path, Action: string(result.Action)})
	}
}

func (h *Handler) handleFileDelete(sender messageSender, msg IncomingMessage, finalizers *[]func() error, rollbacks *[]func() error) {
	if msg.File == nil {
		sender.SendMessage(OutgoingMessage{Type: "fileDeleteResult", Path: msg.Path, Action: string(syncpkg.MatrixActionDeleteConflict), Error: "missing file payload"})
		return
	}

	input, sf, _, err := h.decisionInputForPath(msg, *msg.File, syncpkg.MessageFileDelete)
	if err != nil {
		sender.SendMessage(OutgoingMessage{Type: "fileDeleteResult", Path: msg.Path, Error: err.Error()})
		return
	}
	path := msg.File.Path
	if path == "" {
		path = msg.Path
	}

	result := syncpkg.DecideFileDelete(input)
	switch result.Action {
	case syncpkg.MatrixActionOkUpdateMeta:
		newFile := sf
		if !sf.IsDeleted {
			stage, err := h.storage.StageDelete(msg.Vault, path)
			if err != nil {
				sender.SendMessage(OutgoingMessage{Type: "fileDeleteResult", Path: path, Error: err.Error()})
				return
			}
			*finalizers = append(*finalizers, stage.Commit)
			*rollbacks = append(*rollbacks, stage.Rollback)
			var dErr error
			newFile, dErr = h.queries.DeleteFile(msg.Vault, path)
			if dErr != nil {
				sender.SendMessage(OutgoingMessage{Type: "fileDeleteResult", Path: path, Error: dErr.Error()})
				return
			}
		}
		sender.SendMessage(OutgoingMessage{
			Type:   "fileDeleteResult",
			Path:   path,
			Action: "okUpdateMeta",
			Meta:   serverMeta(newFile),
		})
	case syncpkg.MatrixActionOkRemoveMeta:
		sender.SendMessage(OutgoingMessage{
			Type:   "fileDeleteResult",
			Path:   path,
			Action: "okRemoveMeta",
			Meta:   serverMeta(sf),
		})
	case syncpkg.MatrixActionDeleteConflict:
		h.sendConflictResult(sender, "fileDeleteResult", msg.Vault, path, result.Action, sf)
	default:
		sender.SendMessage(OutgoingMessage{Type: "fileDeleteResult", Path: path, Action: string(result.Action)})
	}

}

func (h *Handler) handleConflictResolve(sender messageSender, client *Client, msg IncomingMessage, finalizers *[]func() error, rollbacks *[]func() error) {
	_ = client
	if msg.Resolution == "local" && msg.Action == "delete" {
		h.handleConflictResolveDelete(sender, msg, finalizers, rollbacks)
		return
	}
	h.handleConflictResolveUpdate(sender, msg, finalizers, rollbacks)
}

func (h *Handler) handleConflictResolveUpdate(sender messageSender, msg IncomingMessage, finalizers *[]func() error, rollbacks *[]func() error) {
	sf, err := h.queries.GetFile(msg.Vault, msg.Path)
	serverExists := err == nil && err != sql.ErrNoRows

	if err != nil && err != sql.ErrNoRows {
		sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	baseVersion, _, localHash, err := protocolPayloadValues(msg, true, true)
	if err != nil {
		sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	var prevVersion int64
	if baseVersion != nil {
		prevVersion = *baseVersion
	}

	optResult := syncpkg.CheckFileUpdate(sf, serverExists, prevVersion, localHash)

	if !optResult.OK {
		if optResult.ErrNoRows {
			sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: "file not found"})
			return
		}
		content, rerr := h.readFileContent(msg.Vault, sf)
		if rerr != nil {
			sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: rerr.Error()})
			return
		}
		enc, encoded := encodeContentWithRowEncoding(content, sf.Encoding)
		sender.SendMessage(OutgoingMessage{
			Type: "conflictResolveResult",
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
		if msg.Content != "" {
			fileContent := decodeContent(msg.Content, msg.Encoding)
			stage, err := h.storage.StageWrite(msg.Vault, msg.Path, fileContent)
			if err != nil {
				sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
				return
			}
			*finalizers = append(*finalizers, stage.Commit)
			*rollbacks = append(*rollbacks, stage.Rollback)
		}
		sender.SendMessage(OutgoingMessage{
			Type: "conflictResolveResult",
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
	contentRef, err := h.stageContent(msg.Vault, msg.Path, fileContent, finalizers, rollbacks)
	if err != nil {
		sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	newFile, uerr := h.queries.UpdateFile(msg.Vault, msg.Path, localHash, contentRef, msg.Encoding)
	if uerr != nil {
		sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: uerr.Error()})
		return
	}

	sender.SendMessage(OutgoingMessage{
		Type: "conflictResolveResult",
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

func (h *Handler) handleConflictResolveDelete(sender messageSender, msg IncomingMessage, finalizers *[]func() error, rollbacks *[]func() error) {
	sf, fileErr := h.queries.GetFile(msg.Vault, msg.Path)
	if fileErr != nil && fileErr != sql.ErrNoRows {
		sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: fileErr.Error()})
		return
	}

	baseVersion, _, _, err := protocolPayloadValues(msg, true, false)
	if err != nil {
		sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}

	var prevVersion int64
	if baseVersion != nil {
		prevVersion = *baseVersion
	}

	optResult := syncpkg.CheckFileDelete(sf, fileErr, prevVersion)

	if optResult.Noop {
		sender.SendMessage(OutgoingMessage{
			Type: "conflictResolveResult",
			Path: msg.Path,
			Ok:   boolPtr(true),
		})
		return
	}

	if !optResult.OK {
		content, rerr := h.readFileContent(msg.Vault, sf)
		if rerr != nil {
			sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: rerr.Error()})
			return
		}
		enc, encoded := encodeContentWithRowEncoding(content, sf.Encoding)
		sender.SendMessage(OutgoingMessage{
			Type: "conflictResolveResult",
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

	stage, err := h.storage.StageDelete(msg.Vault, msg.Path)
	if err != nil {
		sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: err.Error()})
		return
	}
	*finalizers = append(*finalizers, stage.Commit)
	*rollbacks = append(*rollbacks, stage.Rollback)

	newFile, derr := h.queries.DeleteFile(msg.Vault, msg.Path)
	if derr != nil {
		sender.SendMessage(OutgoingMessage{Type: "conflictResolveResult", Path: msg.Path, Ok: boolPtr(false), Error: derr.Error()})
		return
	}

	sender.SendMessage(OutgoingMessage{
		Type: "conflictResolveResult",
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
	content, err := h.readFileContent(vault, sf)
	if err != nil {
		return DownloadEntry{}, false
	}
	enc, encoded := encodeContentWithRowEncoding(content, sf.Encoding)
	return DownloadEntry{
		Path:          sf.Path,
		Content:       encoded,
		ServerVersion: sf.Version,
		ServerHash:    sf.Hash,
		Encoding:      enc,
	}, true
}

func (h *Handler) stageContent(vault, path string, data []byte, finalizers *[]func() error, rollbacks *[]func() error) (string, error) {
	contentRef, objectStage, err := h.storage.StageObjectWrite(data)
	if err != nil {
		return "", err
	}
	if err := objectStage.Commit(); err != nil {
		_ = objectStage.Rollback()
		return "", err
	}

	latestStage, err := h.storage.StageWrite(vault, path, data)
	if err != nil {
		return "", err
	}
	*finalizers = append(*finalizers, latestStage.Commit)
	*rollbacks = append(*rollbacks, latestStage.Rollback)
	return contentRef, nil
}

func (h *Handler) readFileContent(vault string, f db.File) ([]byte, error) {
	if f.ContentRef != "" {
		return h.storage.ReadObject(f.ContentRef)
	}
	return h.storage.ReadFile(vault, f.Path)
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

func encodeContentWithRowEncoding(data []byte, encoding string) (string, string) {
	if encoding == "base64" {
		return "base64", base64.StdEncoding.EncodeToString(data)
	}
	return encodeContent(data)
}

func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
