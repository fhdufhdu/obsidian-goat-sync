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
		{ID: "M001", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionToPut},
		{ID: "M002", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionToUpdateMeta},
		{ID: "M003", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M004", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M005", Message: MessageSyncInit, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M006", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M007", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionToPut},
		{ID: "M008", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M009", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M010", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionNone},
		{ID: "M011", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionToPut},
		{ID: "M012", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionToUpdateMeta},
		{ID: "M013", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M014", Message: MessageSyncInit, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionConflict},
		{ID: "M015", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionToDownload},
		{ID: "M016", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionNone},
		{ID: "M017", Message: MessageSyncInit, ClientExists: false, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionNone},
		{ID: "M018", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionAny, HashMatch: HashNotApplicable, Expected: ActionDeleteConflict},
		{ID: "M019", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionToRemoveMeta},
		{ID: "M020", Message: MessageSyncInit, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionToUpdateMeta},
		{ID: "M021", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionPut},
		{ID: "M022", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionUpdateMeta},
		{ID: "M023", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M024", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M025", Message: MessageFileCheck, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M026", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M027", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionPut},
		{ID: "M028", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M029", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M030", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionUpToDate},
		{ID: "M031", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionPut},
		{ID: "M032", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionUpdateMeta},
		{ID: "M033", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M034", Message: MessageFileCheck, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionConflict},
		{ID: "M035", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionOkUpdateMeta},
		{ID: "M036", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionOkUpdateMeta},
		{ID: "M037", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M038", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M039", Message: MessageFilePut, ClientExists: true, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M040", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M041", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionOkUpdateMeta},
		{ID: "M042", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionToDeleteLocal},
		{ID: "M043", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionDeleteConflict},
		{ID: "M044", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionConflict},
		{ID: "M045", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashEqual, Expected: ActionOkUpdateMeta},
		{ID: "M046", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashDifferent, Expected: ActionOkUpdateMeta},
		{ID: "M047", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashEqual, Expected: ActionOkUpdateMeta},
		{ID: "M048", Message: MessageFilePut, ClientExists: true, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashDifferent, Expected: ActionConflict},
		{ID: "M049", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionOkRemoveMeta},
		{ID: "M050", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerActive, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionDeleteConflict},
		{ID: "M051", Message: MessageFileDelete, ClientExists: false, BaseVersion: nil, ServerState: ServerTombstone, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionOkRemoveMeta},
		{ID: "M052", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionEqualServer, HashMatch: HashNotApplicable, Expected: ActionOkUpdateMeta},
		{ID: "M053", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerActive, VersionMatch: VersionNotEqualServer, HashMatch: HashNotApplicable, Expected: ActionDeleteConflict},
		{ID: "M054", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerMissing, VersionMatch: VersionNotApplicable, HashMatch: HashNotApplicable, Expected: ActionOkRemoveMeta},
		{ID: "M055", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(10), ServerState: ServerTombstone, VersionMatch: VersionEqualDeletedFrom, HashMatch: HashNotApplicable, Expected: ActionOkUpdateMeta},
		{ID: "M056", Message: MessageFileDelete, ClientExists: false, BaseVersion: ptr64(9), ServerState: ServerTombstone, VersionMatch: VersionNotEqualDeletedFrom, HashMatch: HashNotApplicable, Expected: ActionDeleteConflict},
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
