package ws

import "encoding/json"

type FilePayload struct {
	Path        string `json:"path"`
	Exists      bool   `json:"exists"`
	BaseVersion *int64 `json:"baseVersion,omitempty"`
	BaseHash    string `json:"baseHash,omitempty"`
	LocalHash   string `json:"localHash,omitempty"`
}

type IncomingMessage struct {
	Type       string        `json:"type"`
	Vault      string        `json:"vault"`
	Path       string        `json:"path,omitempty"`
	Content    string        `json:"content,omitempty"`
	Encoding   string        `json:"encoding,omitempty"`
	File       *FilePayload  `json:"file,omitempty"`
	Files      []FilePayload `json:"files,omitempty"`
	Resolution string        `json:"resolution,omitempty"`
	Action     string        `json:"action,omitempty"`
}

type ServerMetaPayload struct {
	Path          string `json:"path,omitempty"`
	ServerVersion int64  `json:"serverVersion"`
	ServerHash    string `json:"serverHash,omitempty"`
	IsDeleted     bool   `json:"isDeleted"`
}

type DownloadEntry struct {
	Path          string `json:"path"`
	Content       string `json:"content"`
	ServerVersion int64  `json:"serverVersion"`
	ServerHash    string `json:"serverHash"`
	Encoding      string `json:"encoding,omitempty"`
}

type ConflictInfo struct {
	ServerVersion int64  `json:"serverVersion"`
	ServerHash    string `json:"serverHash"`
	ServerContent string `json:"serverContent"`
	IsDeleted     bool   `json:"isDeleted"`
	Encoding      string `json:"encoding,omitempty"`
}

type SyncConflictEntry struct {
	Path          string `json:"path"`
	BaseVersion   *int64 `json:"baseVersion,omitempty"`
	LocalHash     string `json:"localHash"`
	ServerVersion int64  `json:"serverVersion"`
	ServerHash    string `json:"serverHash"`
	ServerContent string `json:"serverContent"`
	IsDeleted     bool   `json:"isDeleted"`
	Encoding      string `json:"encoding,omitempty"`
}

type UpdateMetaEntry = ServerMetaPayload

type OutgoingMessage struct {
	Type          string              `json:"type"`
	Vault         string              `json:"vault,omitempty"`
	Path          string              `json:"path,omitempty"`
	Action        string              `json:"action,omitempty"`
	Ok            *bool               `json:"ok,omitempty"`
	Content       string              `json:"content,omitempty"`
	Encoding      string              `json:"encoding,omitempty"`
	ToPut         []string            `json:"toPut,omitempty"`
	Meta          *ServerMetaPayload  `json:"meta,omitempty"`
	Conflict      *ConflictInfo       `json:"conflict,omitempty"`
	ToDownload    []DownloadEntry     `json:"toDownload,omitempty"`
	ToUpdateMeta  []ServerMetaPayload `json:"toUpdateMeta,omitempty"`
	ToDeleteLocal []ServerMetaPayload `json:"toDeleteLocal,omitempty"`
	ToRemoveMeta  []ServerMetaPayload `json:"toRemoveMeta,omitempty"`
	Conflicts     []SyncConflictEntry `json:"conflicts,omitempty"`
	Error         string              `json:"error,omitempty"`
}

func boolPtr(b bool) *bool { return &b }

func MarshalMessage(msg OutgoingMessage) ([]byte, error) {
	return json.Marshal(msg)
}

func UnmarshalMessage(data []byte) (IncomingMessage, error) {
	var msg IncomingMessage
	err := json.Unmarshal(data, &msg)
	return msg, err
}
