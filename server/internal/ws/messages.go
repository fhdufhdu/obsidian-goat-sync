package ws

import "encoding/json"

type SyncInitFile struct {
	Path              string `json:"path"`
	PrevServerVersion *int64 `json:"prevServerVersion,omitempty"`
	PrevServerHash    string `json:"prevServerHash,omitempty"`
	CurrentClientHash string `json:"currentClientHash"`
}

type IncomingMessage struct {
	Type              string         `json:"type"`
	Vault             string         `json:"vault"`
	Path              string         `json:"path,omitempty"`
	Content           string         `json:"content,omitempty"`
	Encoding          string         `json:"encoding,omitempty"`
	CurrentClientHash string         `json:"currentClientHash,omitempty"`
	PrevServerVersion *int64         `json:"prevServerVersion,omitempty"`
	PrevServerHash    string         `json:"prevServerHash,omitempty"`
	Files             []SyncInitFile `json:"files,omitempty"`
	Resolution        string         `json:"resolution,omitempty"`
	Action            string         `json:"action,omitempty"`
}

type DownloadEntry struct {
	Path                 string `json:"path"`
	Content              string `json:"content"`
	CurrentServerVersion int64  `json:"currentServerVersion"`
	CurrentServerHash    string `json:"currentServerHash"`
	Encoding             string `json:"encoding,omitempty"`
}

type UpdateMetaEntry struct {
	Path                 string `json:"path"`
	CurrentServerVersion int64  `json:"currentServerVersion"`
	CurrentServerHash    string `json:"currentServerHash"`
}

type ConflictInfo struct {
	CurrentServerVersion   int64  `json:"currentServerVersion"`
	CurrentServerHash      string `json:"currentServerHash"`
	CurrentServerContent   string `json:"currentServerContent"`
	Encoding               string `json:"encoding,omitempty"`
}

type SyncConflictEntry struct {
	Path                 string `json:"path"`
	PrevServerVersion    *int64 `json:"prevServerVersion,omitempty"`
	CurrentClientHash    string `json:"currentClientHash"`
	CurrentServerVersion int64  `json:"currentServerVersion"`
	CurrentServerHash    string `json:"currentServerHash"`
	CurrentServerContent string `json:"currentServerContent"`
	Encoding             string `json:"encoding,omitempty"`
}

type OutgoingMessage struct {
	Type                 string              `json:"type"`
	Vault                string              `json:"vault,omitempty"`
	Path                 string              `json:"path,omitempty"`
	Ok                   *bool               `json:"ok,omitempty"`
	Noop                 bool                `json:"noop,omitempty"`
	CurrentServerVersion int64               `json:"currentServerVersion,omitempty"`
	CurrentServerHash    string              `json:"currentServerHash,omitempty"`
	Action               string              `json:"action,omitempty"`
	Content              string              `json:"content,omitempty"`
	Encoding             string              `json:"encoding,omitempty"`
	Conflict             *ConflictInfo       `json:"conflict,omitempty"`
	ToUpload             []string            `json:"toUpload,omitempty"`
	ToUpdate             []string            `json:"toUpdate,omitempty"`
	ToDownload           []DownloadEntry     `json:"toDownload,omitempty"`
	ToDelete             []string            `json:"toDelete,omitempty"`
	ToUpdateMeta         []UpdateMetaEntry   `json:"toUpdateMeta,omitempty"`
	Conflicts            []SyncConflictEntry `json:"conflicts,omitempty"`
	Error                string              `json:"error,omitempty"`
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
