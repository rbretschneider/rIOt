package db

import "testing"

func TestDUX_SEC022_FingerprintClassification(t *testing.T) {
	for _, tc := range []struct {
		v                 int64
		lc, av, pt, dirty bool
		action            string
	}{
		{0, false, false, false, false, "upgrade"}, {22, false, false, false, false, "upgrade"},
		{23, true, false, false, false, "upgrade"}, {23, true, true, false, false, "upgrade"},
		{29, true, true, true, false, "upgrade"}, {23, false, true, false, false, "operator_reconciliation_required"},
		{28, false, true, true, false, "operator_reconciliation_required"}, {23, true, false, true, false, "stop"},
		{23, true, false, false, true, "stop"}, {27, false, false, false, false, "stop"},
	} {
		_, action := ClassifyMigrationState(MigrationFingerprint{Version: tc.v, Lifecycle: tc.lc, Autovacuum: tc.av, Partitioned: tc.pt, Dirty: tc.dirty})
		if action != tc.action {
			t.Errorf("%+v: got %s", tc, action)
		}
	}
}
