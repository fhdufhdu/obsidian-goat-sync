package sync

import "testing"

type matrixFixture struct {
	ID           string
	Message      MatrixMessage
	ClientExists bool
	BaseVersion  *int64
	ServerState  ServerStateKind
	VersionMatch VersionMatch
	HashMatch    HashMatch
	Expected     MatrixAction
}

func ptr64(v int64) *int64 { return &v }

func matrixFixtures() []matrixFixture {
	return []matrixFixture{
		{ID: "M001", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionToPut},
		{ID: "M002", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: MatrixActionToUpdateMeta},
		{ID: "M003", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: MatrixActionConflict},
		{ID: "M004", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: MatrixActionToDeleteLocal},
		{ID: "M005", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: MatrixActionDeleteConflict},
		{ID: "M006", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: MatrixActionToDeleteLocal},
		{ID: "M007", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: MatrixActionToPut},
		{ID: "M008", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: MatrixActionToDeleteLocal},
		{ID: "M009", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: MatrixActionDeleteConflict},
		{ID: "M010", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: MatrixActionNone},
		{ID: "M011", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: MatrixActionToPut},
		{ID: "M012", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: MatrixActionToUpdateMeta},
		{ID: "M013", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: MatrixActionConflict},
		{ID: "M014", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionConflict},
		{ID: "M015", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionToDownload},
		{ID: "M016", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionNone},
		{ID: "M017", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionNone},
		{ID: "M018", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionAny, HashMatch: HashNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M019", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionToRemoveMeta},
		{ID: "M020", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionToUpdateMeta},
		{ID: "M021", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionPut},
		{ID: "M022", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: MatrixActionUpdateMeta},
		{ID: "M023", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: MatrixActionConflict},
		{ID: "M024", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: MatrixActionToDeleteLocal},
		{ID: "M025", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: MatrixActionDeleteConflict},
		{ID: "M026", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: MatrixActionToDeleteLocal},
		{ID: "M027", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: MatrixActionPut},
		{ID: "M028", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: MatrixActionToDeleteLocal},
		{ID: "M029", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: MatrixActionDeleteConflict},
		{ID: "M030", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: MatrixActionUpToDate},
		{ID: "M031", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: MatrixActionPut},
		{ID: "M032", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: MatrixActionUpdateMeta},
		{ID: "M033", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: MatrixActionConflict},
		{ID: "M034", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionConflict},
		{ID: "M035", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionOkUpdateMeta},
		{ID: "M036", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: MatrixActionOkUpdateMeta},
		{ID: "M037", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: MatrixActionConflict},
		{ID: "M038", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: MatrixActionToDeleteLocal},
		{ID: "M039", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: MatrixActionDeleteConflict},
		{ID: "M040", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: MatrixActionToDeleteLocal},
		{ID: "M041", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: MatrixActionOkUpdateMeta},
		{ID: "M042", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: MatrixActionToDeleteLocal},
		{ID: "M043", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: MatrixActionDeleteConflict},
		{ID: "M044", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionConflict},
		{ID: "M045", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: MatrixActionOkUpdateMeta},
		{ID: "M046", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: MatrixActionOkUpdateMeta},
		{ID: "M047", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: MatrixActionOkUpdateMeta},
		{ID: "M048", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: MatrixActionConflict},
		{ID: "M049", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionOkRemoveMeta},
		{ID: "M050", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M051", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionOkRemoveMeta},
		{ID: "M052", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashNotApplicable, Expected: MatrixActionOkUpdateMeta},
		{ID: "M053", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M054", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: MatrixActionOkRemoveMeta},
		{ID: "M055", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualDeletedFrom, HashMatch: HashNotApplicable, Expected: MatrixActionOkUpdateMeta},
		{ID: "M056", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(9), ServerState: ServerTombstone, VersionMatch: VersionNotEqualDeletedFrom, HashMatch: HashNotApplicable, Expected: MatrixActionDeleteConflict},
	}
}

func TestMatrixFixtures(t *testing.T) {
	for _, tc := range matrixFixtures() {
		t.Run(tc.ID, func(t *testing.T) {
			input := DecisionInputFromFixture(tc)
			got := Decide(input)
			if got.Action != tc.Expected {
				t.Fatalf("expected %s, got %s", tc.Expected, got.Action)
			}
		})
	}
}

func DecisionInputFromFixture(f matrixFixture) DecisionInput {
	serverVersion := int64(10)
	deletedFrom := DeletedFromVersion(serverVersion)
	localHash := "local"
	serverHash := "server"
	if f.HashMatch == HashEqual {
		localHash = serverHash
	}

	baseVersion := f.BaseVersion
	switch f.VersionMatch {
	case VersionEqualServer:
		baseVersion = ptr64(serverVersion)
	case VersionNotEqualServer:
		baseVersion = ptr64(serverVersion - 1)
	case VersionEqualDeletedFrom:
		baseVersion = ptr64(deletedFrom)
	case VersionNotEqualDeletedFrom:
		baseVersion = ptr64(deletedFrom - 1)
	}

	return DecisionInput{
		Message:            f.Message,
		ClientExists:       f.ClientExists,
		BaseVersion:        baseVersion,
		LocalHash:          localHash,
		ServerState:        f.ServerState,
		ServerVersion:      serverVersion,
		ServerHash:         serverHash,
		DeletedFromVersion: deletedFrom,
	}
}
