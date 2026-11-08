package relay

import (
	"testing"
	"time"
)

func TestTrackerWaitsForFilesToSettle(t *testing.T) {
	tr := newTracker()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	stable := 10 * time.Second

	// Just written: not ready.
	if tr.ready("a", 100, t0, t0.Add(time.Second), stable) {
		t.Fatal("fresh file must not be ready")
	}
	// Grew since last scan: still not ready.
	if tr.ready("a", 200, t0.Add(5*time.Second), t0.Add(6*time.Second), stable) {
		t.Fatal("growing file must not be ready")
	}
	// Unchanged for long enough: ready.
	if !tr.ready("a", 200, t0.Add(5*time.Second), t0.Add(17*time.Second), stable) {
		t.Fatal("settled file should be ready")
	}
}

func TestTrackerOldFilesAreReadyImmediately(t *testing.T) {
	tr := newTracker()
	now := time.Now()
	if !tr.ready("old", 1, now.Add(-time.Hour), now, 10*time.Second) {
		t.Fatal("a file untouched for an hour is ready")
	}
}

func TestTrackerHandlesServerClockAhead(t *testing.T) {
	tr := newTracker()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	future := now.Add(5 * time.Minute) // server clock ahead
	if tr.ready("f", 1, future, now, 10*time.Second) {
		t.Fatal("first sighting should wait")
	}
	if !tr.ready("f", 1, future, now.Add(11*time.Second), 10*time.Second) {
		t.Fatal("unchanged across scans should become ready even with clock skew")
	}
}

func TestTrackerPrune(t *testing.T) {
	tr := newTracker()
	now := time.Now()
	tr.ready("gone", 1, now, now, time.Second)
	tr.ready("kept", 1, now, now, time.Second)
	tr.prune(map[string]bool{"kept": true})
	if _, ok := tr.seen["gone"]; ok {
		t.Fatal("gone should be pruned")
	}
	if _, ok := tr.seen["kept"]; !ok {
		t.Fatal("kept should stay")
	}
}

func TestTrackerDisabled(t *testing.T) {
	if !newTracker().ready("x", 1, time.Now(), time.Now(), 0) {
		t.Fatal("stable=0 disables the check")
	}
}
