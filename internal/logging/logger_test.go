package logging

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDailyRotationAndRetention(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "relay_2020-01-01.log")
	_ = os.WriteFile(old, []byte("old\n"), 0o600)

	now := time.Date(2026, 5, 1, 23, 59, 0, 0, time.Local)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	l, err := New(Options{Path: filepath.Join(dir, "relay.log"), RetentionDays: 7, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Infof("before midnight")
	mu.Lock()
	now = now.Add(2 * time.Minute)
	mu.Unlock()
	l.Warnf("after midnight %d", 1)

	a, _ := os.ReadFile(filepath.Join(dir, "relay_2026-05-01.log"))
	b, _ := os.ReadFile(filepath.Join(dir, "relay_2026-05-02.log"))
	if !strings.Contains(string(a), "INFO  before midnight") || !strings.Contains(string(b), "WARN  after midnight 1") {
		t.Fatalf("rotation wrong:\n%s\n---\n%s", a, b)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("files past retention should be deleted")
	}
}

func TestSubscribersAndLevels(t *testing.T) {
	l, _ := New(Options{MinLevel: Warn})
	ch, stop := l.Subscribe()
	l.Debugf("hidden from files")
	l.Errorf("boom")
	got := []Entry{<-ch, <-ch}
	if got[0].Level != Debug || got[1].Message != "boom" {
		t.Fatalf("subscribers receive every entry, got %+v", got)
	}
	stop()
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after unsubscribe")
	}
	if len(l.Recent()) != 2 {
		t.Fatal("recent buffer")
	}
}

func TestConcurrentWrites(t *testing.T) {
	l, _ := New(Options{Path: filepath.Join(t.TempDir(), "c.log")})
	defer l.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				l.Infof("line %d", j)
			}
		}()
	}
	wg.Wait()
	data, _ := os.ReadFile(l.CurrentFile())
	if n := strings.Count(string(data), "\n"); n != 1600 {
		t.Fatalf("lines = %d, want 1600", n)
	}
}
