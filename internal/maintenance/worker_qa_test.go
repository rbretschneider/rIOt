package maintenance

import (
	"context"
	"errors"
	"testing"
)

// Added by QA Engineer (PATCH-FLOW-001).
//
// TestWorker_PF_AC032_SourcesErrorIsTreatedAsChanged returns a different
// fingerprint on its second call ("" then "x"), so the "changed" decision is
// reached by the fingerprints differing and the error handling is never the
// deciding factor. Mutation testing confirmed it: dropping either error check
// still passed. These tests hold the fingerprint equal so only the error can
// cause the refresh.

// sourcesWithErrors returns a Sources func that reports (fingerprint, errs[i])
// on call i, with the same fingerprint every time.
func sourcesWithErrors(fingerprint string, errs ...error) func() (string, error) {
	calls := 0
	return func() (string, error) {
		i := calls
		calls++
		if i >= len(errs) {
			i = len(errs) - 1
		}
		return fingerprint, errs[i]
	}
}

// Covers AC-032, FR-040: an unreadable "before" fingerprint is "changed" even when
// the "after" fingerprint happens to equal it.
func TestWorker_QA_AC032_BeforeFingerprintErrorRefreshesOnceEvenIfFingerprintsMatch(t *testing.T) {
	w, r, _ := newWorker()
	w.Sources = sourcesWithErrors("same", errors.New("stat failed"), nil)

	rec, err := w.Execute(context.Background(), testRequest())

	if err != nil || rec.Outcome != "success" {
		t.Fatalf("run must succeed: %+v %v", rec, err)
	}
	if len(r.calls) != callsWithExtra || !isRefresh(r.calls[callPostRefresh]) {
		t.Errorf("[AC-032] a failed first fingerprint must fail toward exactly one extra refresh, got %d calls: %v", len(r.calls), phases(r))
	}
}

// Covers AC-032, FR-040: an unreadable "after" fingerprint is "changed" even when
// the "before" fingerprint equals what was returned.
func TestWorker_QA_AC032_AfterFingerprintErrorRefreshesOnceEvenIfFingerprintsMatch(t *testing.T) {
	w, r, _ := newWorker()
	w.Sources = sourcesWithErrors("same", nil, errors.New("stat failed"))

	rec, err := w.Execute(context.Background(), testRequest())

	if err != nil || rec.Outcome != "success" {
		t.Fatalf("run must succeed: %+v %v", rec, err)
	}
	if len(r.calls) != callsWithExtra || !isRefresh(r.calls[callPostRefresh]) {
		t.Errorf("[AC-032] a failed second fingerprint must fail toward exactly one extra refresh, got %d calls: %v", len(r.calls), phases(r))
	}
}

// Covers AC-033, FR-043: two readable, equal fingerprints are the only case that
// skips the refresh (guards the two tests above against an over-eager fix).
func TestWorker_QA_AC033_EqualReadableFingerprintsSkipTheRefresh(t *testing.T) {
	w, r, _ := newWorker()
	w.Sources = sourcesWithErrors("same", nil, nil)

	rec, err := w.Execute(context.Background(), testRequest())

	if err != nil || rec.Outcome != "success" {
		t.Fatalf("run must succeed: %+v %v", rec, err)
	}
	if len(r.calls) != callsWithoutExtra {
		t.Errorf("[AC-033] no change must mean no extra refresh, got %d calls: %v", len(r.calls), phases(r))
	}
}
