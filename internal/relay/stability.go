package relay

import "time"

// tracker decides when a file has stopped changing.
//
// A file is ready when its modification time is at least `stable` old, or when
// its size and mtime have not changed across scans for at least `stable`
// (which also covers servers whose clock runs ahead of ours).
type tracker struct {
	seen map[string]observation
}

type observation struct {
	size  int64
	mod   time.Time
	since time.Time
}

func newTracker() *tracker { return &tracker{seen: map[string]observation{}} }

func (t *tracker) ready(name string, size int64, mod, now time.Time, stable time.Duration) bool {
	if stable <= 0 {
		return true
	}
	o, ok := t.seen[name]
	if !ok || o.size != size || !o.mod.Equal(mod) {
		t.seen[name] = observation{size: size, mod: mod, since: now}
		return now.Sub(mod) >= stable
	}
	return now.Sub(o.since) >= stable || now.Sub(mod) >= stable
}

// prune forgets files that are no longer in the source folder.
func (t *tracker) prune(present map[string]bool) {
	for name := range t.seen {
		if !present[name] {
			delete(t.seen, name)
		}
	}
}
