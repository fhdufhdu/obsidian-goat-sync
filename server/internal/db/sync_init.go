package db

type SyncInitFile struct {
	Path              string
	PrevServerVersion *int64
	PrevServerHash    string
	CurrentClientHash string
}
