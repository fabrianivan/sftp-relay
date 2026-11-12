package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersistLimitAndOrder(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.json")
	s, err := Open(p, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := s.Add(Record{File: string(rune('a' + i)), Status: Delivered}); err != nil {
			t.Fatal(err)
		}
	}
	re, err := Open(p, 3)
	if err != nil {
		t.Fatal(err)
	}
	recs := re.Records()
	if len(recs) != 3 || recs[0].File != "e" || recs[2].File != "c" {
		t.Fatalf("want newest-first [e d c], got %+v", recs)
	}
}

func TestPendingSurvivesRestart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.json")
	s, _ := Open(p, 10)
	key := Key("x.csv", 10, time.Unix(1000, 0))
	_ = s.MarkDelivered(key, Pending{SHA256: "abc", Target: "/in/x.csv"})

	re, _ := Open(p, 10)
	if got, ok := re.PendingFor(key); !ok || got.SHA256 != "abc" {
		t.Fatalf("pending lost: %+v %v", got, ok)
	}
	_ = re.ClearPending(key)
	if _, ok := re.PendingFor(key); ok {
		t.Fatal("pending not cleared")
	}
	if _, ok := re.PendingFor(Key("x.csv", 11, time.Unix(1000, 0))); ok {
		t.Fatal("a different version of the file must not match")
	}
}

func TestCorruptFileIsMovedAside(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.json")
	_ = os.WriteFile(p, []byte("{not json"), 0o600)
	s, err := Open(p, 10)
	if err == nil || s == nil {
		t.Fatalf("want warning and a usable store, got %v / %v", err, s)
	}
	entries, _ := os.ReadDir(dir)
	found := false
	for _, e := range entries {
		found = found || strings.HasPrefix(e.Name(), "history.json.corrupt-")
	}
	if !found {
		t.Fatal("corrupt file should be kept for inspection")
	}
}

func TestRetainPendingDropsOnlyStaleKeysWithPrefix(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.json")
	s, _ := Open(p, 10)
	for _, k := range []string{"local:a|1|1", "local:b|1|1", "server-file|1|1"} {
		if err := s.MarkDelivered(k, Pending{Target: "/in/x"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RetainPending("local:", map[string]bool{"local:a|1|1": true}); err != nil {
		t.Fatal(err)
	}
	again, _ := Open(p, 10)
	for k, want := range map[string]bool{"local:a|1|1": true, "local:b|1|1": false, "server-file|1|1": true} {
		if _, ok := again.PendingFor(k); ok != want {
			t.Errorf("%s present = %v, want %v", k, ok, want)
		}
	}
}
