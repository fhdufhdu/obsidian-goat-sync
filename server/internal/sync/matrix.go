package sync

type MatrixMessage string

const (
	MessageSyncInit   MatrixMessage = "syncInit"
	MessageFileCheck  MatrixMessage = "fileCheck"
	MessageFilePut    MatrixMessage = "filePut"
	MessageFileDelete MatrixMessage = "fileDelete"
)

type ServerStateKind string

const (
	ServerMissing   ServerStateKind = "missing"
	ServerActive    ServerStateKind = "active"
	ServerTombstone ServerStateKind = "tombstone"
)

type VersionMatch string

const (
	VersionNotApplicable       VersionMatch = "n/a"
	VersionEqualServer         VersionMatch = "base==server"
	VersionNotEqualServer      VersionMatch = "base!=server"
	VersionEqualDeletedFrom    VersionMatch = "base==deletedFrom"
	VersionNotEqualDeletedFrom VersionMatch = "base!=deletedFrom"
	VersionAny                 VersionMatch = "any"
)

type HashMatch string

const (
	HashNotApplicable HashMatch = "n/a"
	HashEqual         HashMatch = "equal"
	HashDifferent     HashMatch = "different"
)

type MatrixAction string

const (
	MatrixActionNone           MatrixAction = "none"
	MatrixActionToPut          MatrixAction = "toPut"
	MatrixActionPut            MatrixAction = "put"
	MatrixActionToUpdateMeta   MatrixAction = "toUpdateMeta"
	MatrixActionUpdateMeta     MatrixAction = "updateMeta"
	MatrixActionOkUpdateMeta   MatrixAction = "okUpdateMeta"
	MatrixActionToDownload     MatrixAction = "toDownload"
	MatrixActionToDeleteLocal  MatrixAction = "toDeleteLocal"
	MatrixActionToRemoveMeta   MatrixAction = "toRemoveMeta"
	MatrixActionOkRemoveMeta   MatrixAction = "okRemoveMeta"
	MatrixActionUpToDate       MatrixAction = "upToDate"
	MatrixActionConflict       MatrixAction = "conflict"
	MatrixActionDeleteConflict MatrixAction = "deleteConflict"
)

type DecisionInput struct {
	Message            MatrixMessage
	ClientExists       bool
	BaseVersion        *int64
	LocalHash          string
	ServerState        ServerStateKind
	ServerVersion      int64
	ServerHash         string
	DeletedFromVersion int64
}

type DecisionResult struct {
	Action MatrixAction
}

func DeletedFromVersion(serverVersion int64) int64 {
	return serverVersion - 1
}

func Decide(input DecisionInput) DecisionResult {
	switch input.Message {
	case MessageSyncInit:
		return DecideSyncInit(input)
	case MessageFileCheck:
		return DecideFileCheck(input)
	case MessageFilePut:
		return DecideFilePut(input)
	case MessageFileDelete:
		return DecideFileDelete(input)
	default:
		return DecisionResult{Action: MatrixActionConflict}
	}
}

func DecideSyncInit(input DecisionInput) DecisionResult {
	return decideReadOrCheck(input, MatrixActionToPut, MatrixActionToUpdateMeta, MatrixActionNone)
}

func DecideFileCheck(input DecisionInput) DecisionResult {
	return decideReadOrCheck(input, MatrixActionPut, MatrixActionUpdateMeta, MatrixActionUpToDate)
}

func decideReadOrCheck(input DecisionInput, putAction, updateMetaAction, cleanAction MatrixAction) DecisionResult {
	hasBase := input.BaseVersion != nil

	if input.ClientExists {
		if !hasBase {
			switch input.ServerState {
			case ServerMissing:
				return DecisionResult{Action: putAction}
			case ServerActive:
				if input.LocalHash == input.ServerHash {
					return DecisionResult{Action: updateMetaAction}
				}
				return DecisionResult{Action: MatrixActionConflict}
			case ServerTombstone:
				if input.LocalHash == input.ServerHash {
					return DecisionResult{Action: MatrixActionToDeleteLocal}
				}
				return DecisionResult{Action: MatrixActionDeleteConflict}
			}
		}

		switch input.ServerState {
		case ServerMissing:
			return DecisionResult{Action: MatrixActionConflict}
		case ServerTombstone:
			if *input.BaseVersion == input.ServerVersion {
				if input.LocalHash == input.ServerHash {
					return DecisionResult{Action: MatrixActionToDeleteLocal}
				}
				return DecisionResult{Action: putAction}
			}
			if input.LocalHash == input.ServerHash {
				return DecisionResult{Action: MatrixActionToDeleteLocal}
			}
			return DecisionResult{Action: MatrixActionDeleteConflict}
		case ServerActive:
			if *input.BaseVersion == input.ServerVersion {
				if input.LocalHash == input.ServerHash {
					return DecisionResult{Action: cleanAction}
				}
				return DecisionResult{Action: putAction}
			}
			if input.LocalHash == input.ServerHash {
				return DecisionResult{Action: updateMetaAction}
			}
			return DecisionResult{Action: MatrixActionConflict}
		}
	}

	if !hasBase {
		switch input.ServerState {
		case ServerActive:
			return DecisionResult{Action: MatrixActionToDownload}
		default:
			return DecisionResult{Action: MatrixActionNone}
		}
	}

	switch input.ServerState {
	case ServerActive:
		return DecisionResult{Action: MatrixActionDeleteConflict}
	case ServerMissing:
		return DecisionResult{Action: MatrixActionToRemoveMeta}
	case ServerTombstone:
		return DecisionResult{Action: MatrixActionToUpdateMeta}
	default:
		return DecisionResult{Action: MatrixActionNone}
	}
}

func DecideFilePut(input DecisionInput) DecisionResult {
	hasBase := input.BaseVersion != nil
	if !input.ClientExists {
		return DecisionResult{Action: MatrixActionConflict}
	}

	if !hasBase {
		switch input.ServerState {
		case ServerMissing:
			return DecisionResult{Action: MatrixActionOkUpdateMeta}
		case ServerActive:
			if input.LocalHash == input.ServerHash {
				return DecisionResult{Action: MatrixActionOkUpdateMeta}
			}
			return DecisionResult{Action: MatrixActionConflict}
		case ServerTombstone:
			if input.LocalHash == input.ServerHash {
				return DecisionResult{Action: MatrixActionToDeleteLocal}
			}
			return DecisionResult{Action: MatrixActionDeleteConflict}
		}
	}

	switch input.ServerState {
	case ServerMissing:
		return DecisionResult{Action: MatrixActionConflict}
	case ServerTombstone:
		if *input.BaseVersion == input.ServerVersion {
			if input.LocalHash == input.ServerHash {
				return DecisionResult{Action: MatrixActionToDeleteLocal}
			}
			return DecisionResult{Action: MatrixActionOkUpdateMeta}
		}
		if input.LocalHash == input.ServerHash {
			return DecisionResult{Action: MatrixActionToDeleteLocal}
		}
		return DecisionResult{Action: MatrixActionDeleteConflict}
	case ServerActive:
		if *input.BaseVersion == input.ServerVersion {
			return DecisionResult{Action: MatrixActionOkUpdateMeta}
		}
		if input.LocalHash == input.ServerHash {
			return DecisionResult{Action: MatrixActionOkUpdateMeta}
		}
		return DecisionResult{Action: MatrixActionConflict}
	default:
		return DecisionResult{Action: MatrixActionConflict}
	}
}

func DecideFileDelete(input DecisionInput) DecisionResult {
	hasBase := input.BaseVersion != nil
	if !hasBase {
		switch input.ServerState {
		case ServerActive:
			return DecisionResult{Action: MatrixActionDeleteConflict}
		default:
			return DecisionResult{Action: MatrixActionOkRemoveMeta}
		}
	}

	switch input.ServerState {
	case ServerActive:
		if *input.BaseVersion == input.ServerVersion {
			return DecisionResult{Action: MatrixActionOkUpdateMeta}
		}
		return DecisionResult{Action: MatrixActionDeleteConflict}
	case ServerMissing:
		return DecisionResult{Action: MatrixActionOkRemoveMeta}
	case ServerTombstone:
		if *input.BaseVersion == input.DeletedFromVersion {
			return DecisionResult{Action: MatrixActionOkUpdateMeta}
		}
		return DecisionResult{Action: MatrixActionDeleteConflict}
	default:
		return DecisionResult{Action: MatrixActionOkRemoveMeta}
	}
}
