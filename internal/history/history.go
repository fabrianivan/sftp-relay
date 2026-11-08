// Package history keeps a persistent journal of relayed files.
//
// Besides powering the History tab, it remembers files that were delivered
// but could not be archived yet, so a retry only archives them instead of
// uploading them a second time.
package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Status of a processed file.
type Status string

const (
	Delivered Status = "delivered" // copied to target and archived
	Archived  Status = "archived"  // archive-only mode (no target configured)
	Failed    Status = "failed"
)

// Record is one journal entry.
type Record struct {
	Time       time.Time `json:"time"`
	File       string    `json:"file"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256,omitempty"`
	DurationMS int64     `json:"durationMs,omitempty"`
	Status     Status    `json:"status"`
	Target     string    `json:"target,omitempty"`
	Archive    string    `json:"archive,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Duration returns the transfer time.
func (r Record) Duration() time.Duration { return time.Duration(r.DurationMS) * time.Millisecond }

// Pending is a file that reached the target but is still in the source folder.
type Pending struct {
	SHA256    string    `json:"sha256"`
	Delivered time.Time `json:"delivered"`
	Target    string    `json:"target"`
}

type state struct {
	Records []Record           `json:"records"`
	Pending map[string]Pending `json:"pending"`
}

// Store is safe for concurrent use.
type Store struct {
	mu    sync.Mutex
	path  string
	limit int
	st    state
}

// Key identifies one version of a source file.
func Key(name string, size int64, mod time.Time) string {
	return fmt.Sprintf("%s|%d|%d", name, size, mod.Unix())
}

// Open loads the journal at path (empty path = in-memory only).
// A corrupt file is moved aside instead of blocking startup.
func Open(path string, limit int) (*Store, error) {
	if limit <= 0 {
		limit = 1000
	}
	s := &Store{path: path, limit: limit, st: state{Pending: map[string]Pending{}}}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		bad := path + ".corrupt-" + time.Now().Format("20060102-150405")
		_ = os.Rename(path, bad)
		return s, fmt.Errorf("history file was unreadable and was moved to %s: %w", bad, err)
	}
	if st.Pending == nil {
		st.Pending = map[string]Pending{}
	}
	s.st = st
	return s, nil
}

// Add appends a record and persists the journal.
func (s *Store) Add(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Records = append(s.st.Records, r)
	if over := len(s.st.Records) - s.limit; over > 0 {
		s.st.Records = append([]Record(nil), s.st.Records[over:]...)
	}
	return s.saveLocked()
}

// MarkDelivered remembers that a file version reached the target.
func (s *Store) MarkDelivered(key string, p Pending) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Pending[key] = p
	return s.saveLocked()
}

// PendingFor returns delivery info for a file that still needs archiving.
func (s *Store) PendingFor(key string) (Pending, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.st.Pending[key]
	return p, ok
}

// ClearPending forgets a pending entry once the file has been archived.
func (s *Store) ClearPending(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.st.Pending[key]; !ok {
		return nil
	}
	delete(s.st.Pending, key)
	return s.saveLocked()
}

// Records returns a copy of the journal, newest first.
func (s *Store) Records() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, len(s.st.Records))
	for i, r := range s.st.Records {
		out[len(out)-1-i] = r
	}
	return out
}

// Path returns the journal file location.
func (s *Store) Path() string { return s.path }

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
