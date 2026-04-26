package sync

import "testing"

type matrixFixture struct {
	ID            string
	Message       MatrixMessage
	ClientExists  bool
	BaseVersion   *int64
	ServerState   ServerStateKind
	VersionMatch  VersionMatch
	HashMatch     HashMatch
	BaseRowExists bool
	BaseHashMatch HashMatch
	AutoMerge     AutoMergeState
	Expected      MatrixAction
}

func ptr64(v int64) *int64 { return &v }

func matrixFixtures() []matrixFixture {
	return []matrixFixture{
		{ID: "M001", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToPut},
		{ID: "M002", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToUpdateMeta},
		{ID: "M003", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionConflict},
		{ID: "M004", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDeleteLocal},
		{ID: "M005", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M006", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDeleteLocal},
		{ID: "M007", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToPut},
		{ID: "M008", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDeleteLocal},
		{ID: "M009", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M010", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionNone},
		{ID: "M011", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToPut},
		{ID: "M012", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToUpdateMeta},
		{ID: "M013", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: true, BaseHashMatch: HashEqual, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDownload},
		{ID: "M014", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: true, BaseHashMatch: HashDifferent, AutoMerge: AutoMergePossible, Expected: MatrixActionAutoMerge},
		{ID: "M015", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: true, BaseHashMatch: HashDifferent, AutoMerge: AutoMergeImpossible, Expected: MatrixActionConflict},
		{ID: "M016", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionConflict},
		{ID: "M017", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionConflict},
		{ID: "M018", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDownload},
		{ID: "M019", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionNone},
		{ID: "M020", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionNone},
		{ID: "M021", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionAny, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M022", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToRemoveMeta},
		{ID: "M023", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToUpdateMeta},
		{ID: "M024", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionPut},
		{ID: "M025", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionUpdateMeta},
		{ID: "M026", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionConflict},
		{ID: "M027", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDeleteLocal},
		{ID: "M028", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M029", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDeleteLocal},
		{ID: "M030", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionPut},
		{ID: "M031", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDeleteLocal},
		{ID: "M032", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M033", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionUpToDate},
		{ID: "M034", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionPut},
		{ID: "M035", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionUpdateMeta},
		{ID: "M036", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: true, BaseHashMatch: HashEqual, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDownload},
		{ID: "M037", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: true, BaseHashMatch: HashDifferent, AutoMerge: AutoMergePossible, Expected: MatrixActionAutoMerge},
		{ID: "M038", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: true, BaseHashMatch: HashDifferent, AutoMerge: AutoMergeImpossible, Expected: MatrixActionConflict},
		{ID: "M039", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionConflict},
		{ID: "M040", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionConflict},
		{ID: "M041", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionOkUpdateMeta},
		{ID: "M042", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionOkUpdateMeta},
		{ID: "M043", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionConflict},
		{ID: "M044", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDeleteLocal},
		{ID: "M045", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M046", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDeleteLocal},
		{ID: "M047", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionOkUpdateMeta},
		{ID: "M048", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDeleteLocal},
		{ID: "M049", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M050", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionConflict},
		{ID: "M051", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionOkUpdateMeta},
		{ID: "M052", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionOkUpdateMeta},
		{ID: "M053", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionOkUpdateMeta},
		{ID: "M054", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: true, BaseHashMatch: HashEqual, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionToDownload},
		{ID: "M055", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: true, BaseHashMatch: HashDifferent, AutoMerge: AutoMergePossible, Expected: MatrixActionAutoMerge},
		{ID: "M056", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: true, BaseHashMatch: HashDifferent, AutoMerge: AutoMergeImpossible, Expected: MatrixActionConflict},
		{ID: "M057", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionConflict},
		{ID: "M058", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionOkRemoveMeta},
		{ID: "M059", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M060", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionOkRemoveMeta},
		{ID: "M061", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionOkUpdateMeta},
		{ID: "M062", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionDeleteConflict},
		{ID: "M063", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionOkRemoveMeta},
		{ID: "M064", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualDeletedFrom, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionOkUpdateMeta},
		{ID: "M065", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualDeletedFrom, HashMatch: HashNotApplicable, BaseRowExists: false, BaseHashMatch: HashNotApplicable, AutoMerge: AutoMergeNotApplicable, Expected: MatrixActionDeleteConflict},
	}
}

func TestDecideSyncInitBaseAwareActiveDiverged(t *testing.T) {
	tests := []struct {
		name string
		in   DecisionInput
		want MatrixAction
	}{
		{
			name: "local unchanged from base downloads latest server",
			in: DecisionInput{
				Message:       MessageSyncInit,
				ClientExists:  true,
				BaseVersion:   ptr64(1),
				LocalHash:     "base",
				ServerState:   ServerActive,
				ServerVersion: 2,
				ServerHash:    "server",
				BaseRowExists: true,
				BaseHash:      "base",
			},
			want: MatrixActionToDownload,
		},
		{
			name: "both changed and clean merge required",
			in: DecisionInput{
				Message:       MessageSyncInit,
				ClientExists:  true,
				BaseVersion:   ptr64(1),
				LocalHash:     "local",
				ServerState:   ServerActive,
				ServerVersion: 2,
				ServerHash:    "server",
				BaseRowExists: true,
				BaseHash:      "base",
				AutoMerge:     AutoMergePossible,
			},
			want: MatrixActionAutoMerge,
		},
		{
			name: "missing base row remains conflict",
			in: DecisionInput{
				Message:       MessageSyncInit,
				ClientExists:  true,
				BaseVersion:   ptr64(1),
				LocalHash:     "local",
				ServerState:   ServerActive,
				ServerVersion: 2,
				ServerHash:    "server",
				BaseRowExists: false,
			},
			want: MatrixActionConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideSyncInit(tt.in)
			if got.Action != tt.want {
				t.Fatalf("action = %s, want %s", got.Action, tt.want)
			}
		})
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

	input := DecisionInput{
		Message:            f.Message,
		ClientExists:       f.ClientExists,
		BaseVersion:        baseVersion,
		LocalHash:          localHash,
		ServerState:        f.ServerState,
		ServerVersion:      serverVersion,
		ServerHash:         serverHash,
		DeletedFromVersion: deletedFrom,
	}
	if f.BaseHashMatch == HashEqual {
		input.BaseHash = input.LocalHash
	}
	if f.BaseHashMatch == HashDifferent {
		input.BaseHash = "base-hash"
		input.LocalHash = "local-hash"
	}
	input.BaseRowExists = f.BaseRowExists
	input.AutoMerge = f.AutoMerge
	return input
}
