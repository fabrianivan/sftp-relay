package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Filter specifies criteria for querying audit events.
type Filter struct {
	EventTypes []EventType   // Filter by event types
	Status     []Status      // Filter by status
	StartTime  *time.Time    // Events after this time
	EndTime    *time.Time    // Events before this time
	Filename   string        // Filter by file name (substring match)
	Hostname   string        // Filter by hostname
	Limit      int           // Maximum number of events to return (0 = unlimited)
}

// Reader reads and queries audit log files.
type Reader struct {
	path string
}

// NewReader creates a new audit log reader.
func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// Query reads events matching the filter.
func (r *Reader) Query(filter Filter) ([]Event, error) {
	f, err := os.Open(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Event{}, nil // Empty log is not an error
		}
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			// Skip malformed lines
			continue
		}

		if !matchesFilter(event, filter) {
			continue
		}

		events = append(events, event)

		if filter.Limit > 0 && len(events) >= filter.Limit {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read audit log: %w", err)
	}

	return events, nil
}

// All returns all audit events.
func (r *Reader) All() ([]Event, error) {
	return r.Query(Filter{})
}

// Recent returns the N most recent audit events.
func (r *Reader) Recent(n int) ([]Event, error) {
	events, err := r.All()
	if err != nil {
		return nil, err
	}

	if len(events) <= n {
		return events, nil
	}

	return events[len(events)-n:], nil
}

// TransferHistory returns all transfer-related events for a specific file.
func (r *Reader) TransferHistory(filename string) ([]Event, error) {
	return r.Query(Filter{
		EventTypes: []EventType{
			EventTransferStarted,
			EventTransferCompleted,
			EventTransferFailed,
			EventFileArchived,
		},
		Filename: filename,
	})
}

// FailedTransfers returns all failed transfer events within a time range.
func (r *Reader) FailedTransfers(start, end time.Time) ([]Event, error) {
	return r.Query(Filter{
		EventTypes: []EventType{EventTransferFailed},
		StartTime:  &start,
		EndTime:    &end,
	})
}

// Export writes matching events to a writer in JSON format.
func (r *Reader) Export(w io.Writer, filter Filter) error {
	events, err := r.Query(filter)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	return encoder.Encode(events)
}

// matchesFilter checks if an event matches the filter criteria.
func matchesFilter(event Event, filter Filter) bool {
	// Event type filter
	if len(filter.EventTypes) > 0 {
		match := false
		for _, et := range filter.EventTypes {
			if event.EventType == et {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// Status filter
	if len(filter.Status) > 0 {
		match := false
		for _, s := range filter.Status {
			if event.Status == s {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// Time range filter
	if filter.StartTime != nil && event.Timestamp.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && event.Timestamp.After(*filter.EndTime) {
		return false
	}

	// Filename filter (substring match)
	if filter.Filename != "" && event.File != nil {
		if !contains(event.File.Name, filter.Filename) {
			return false
		}
	}

	// Hostname filter
	if filter.Hostname != "" && event.Actor.Hostname != filter.Hostname {
		return false
	}

	return true
}

// contains is a simple substring check.
func contains(s, substr string) bool {
	return len(substr) == 0 || len(s) >= len(substr) && (s == substr || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
