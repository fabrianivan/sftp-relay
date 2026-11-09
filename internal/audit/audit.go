// Package audit provides comprehensive audit logging for SFTP Relay operations.
// It tracks who did what, when, and with what result - essential for compliance
// and forensic investigation.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// EventType categorizes audit events.
type EventType string

const (
	EventTransferStarted   EventType = "TRANSFER_STARTED"
	EventTransferCompleted EventType = "TRANSFER_COMPLETED"
	EventTransferFailed    EventType = "TRANSFER_FAILED"
	EventFileArchived      EventType = "FILE_ARCHIVED"
	EventConfigChanged     EventType = "CONFIG_CHANGED"
	EventConnectionFailed  EventType = "CONNECTION_FAILED"
	EventConnectionSuccess EventType = "CONNECTION_SUCCESS"
	EventScanStarted       EventType = "SCAN_STARTED"
	EventScanCompleted     EventType = "SCAN_COMPLETED"
	EventEngineStarted     EventType = "ENGINE_STARTED"
	EventEngineStopped     EventType = "ENGINE_STOPPED"
	EventAuthSuccess       EventType = "AUTH_SUCCESS"
	EventAuthFailure       EventType = "AUTH_FAILURE"
)

// Status represents the outcome of an audited action.
type Status string

const (
	StatusSuccess Status = "SUCCESS"
	StatusFailed  Status = "FAILED"
	StatusStarted Status = "STARTED"
)

// Event is a single audit log entry.
type Event struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	EventType EventType              `json:"event_type"`
	Status    Status                 `json:"status"`
	Actor     Actor                  `json:"actor"`
	Source    *Endpoint              `json:"source,omitempty"`
	Target    *Endpoint              `json:"target,omitempty"`
	File      *FileInfo              `json:"file,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Duration  *time.Duration         `json:"duration_ms,omitempty"`
}

// Actor represents who/what initiated the action.
type Actor struct {
	Type     string `json:"type"`      // "user", "system", "scheduler"
	User     string `json:"user"`      // OS username
	Hostname string `json:"hostname"`  // machine name
	PID      int    `json:"pid"`       // process ID
	Mode     string `json:"mode"`      // "gui", "headless", "once"
}

// Endpoint represents an SFTP server in the audit context.
type Endpoint struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Directory string `json:"directory"`
}

// FileInfo contains details about a transferred file.
type FileInfo struct {
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"mod_time"`
	SHA256       string    `json:"sha256,omitempty"`
	SourcePath   string    `json:"source_path"`
	TargetPath   string    `json:"target_path,omitempty"`
	ArchivePath  string    `json:"archive_path,omitempty"`
}

// Logger writes audit events to persistent storage.
type Logger struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	actor    Actor
	disabled bool
}

// Options configures the audit logger.
type Options struct {
	LogPath  string // Path to audit log file
	Disabled bool   // Set true to disable audit logging
	Mode     string // "gui", "headless", "once"
}

// New creates a new audit logger.
func New(opts Options) (*Logger, error) {
	if opts.Disabled {
		return &Logger{disabled: true}, nil
	}

	if opts.LogPath == "" {
		return nil, fmt.Errorf("audit log path is required")
	}

	// Ensure directory exists
	dir := filepath.Dir(opts.LogPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create audit log directory: %w", err)
	}

	// Open log file in append mode
	f, err := os.OpenFile(opts.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}

	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME") // Windows
	}

	return &Logger{
		file: f,
		path: opts.LogPath,
		actor: Actor{
			Type:     "system",
			User:     username,
			Hostname: hostname,
			PID:      os.Getpid(),
			Mode:     opts.Mode,
		},
	}, nil
}

// Log writes an audit event.
func (l *Logger) Log(ctx context.Context, event Event) error {
	if l.disabled {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Fill in standard fields
	event.ID = generateID()
	event.Timestamp = time.Now().UTC()
	event.Actor = l.actor

	// Serialize to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal audit event: %w", err)
	}

	// Write with newline
	if _, err := l.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}

	return nil
}

// TransferStarted logs the beginning of a file transfer.
func (l *Logger) TransferStarted(ctx context.Context, source, target *Endpoint, file *FileInfo) error {
	return l.Log(ctx, Event{
		EventType: EventTransferStarted,
		Status:    StatusStarted,
		Source:    source,
		Target:    target,
		File:      file,
	})
}

// TransferCompleted logs a successful file transfer.
func (l *Logger) TransferCompleted(ctx context.Context, source, target *Endpoint, file *FileInfo, duration time.Duration) error {
	return l.Log(ctx, Event{
		EventType: EventTransferCompleted,
		Status:    StatusSuccess,
		Source:    source,
		Target:    target,
		File:      file,
		Duration:  &duration,
		Details: map[string]interface{}{
			"bytes_per_second": float64(file.Size) / duration.Seconds(),
		},
	})
}

// TransferFailed logs a failed file transfer.
func (l *Logger) TransferFailed(ctx context.Context, source, target *Endpoint, file *FileInfo, err error, duration time.Duration) error {
	return l.Log(ctx, Event{
		EventType: EventTransferFailed,
		Status:    StatusFailed,
		Source:    source,
		Target:    target,
		File:      file,
		Error:     err.Error(),
		Duration:  &duration,
	})
}

// FileArchived logs when a file is moved to the archive.
func (l *Logger) FileArchived(ctx context.Context, source *Endpoint, file *FileInfo) error {
	return l.Log(ctx, Event{
		EventType: EventFileArchived,
		Status:    StatusSuccess,
		Source:    source,
		File:      file,
	})
}

// ConfigChanged logs configuration updates.
func (l *Logger) ConfigChanged(ctx context.Context, changes map[string]interface{}) error {
	return l.Log(ctx, Event{
		EventType: EventConfigChanged,
		Status:    StatusSuccess,
		Details:   changes,
	})
}

// ConnectionSuccess logs successful SFTP connections.
func (l *Logger) ConnectionSuccess(ctx context.Context, endpoint *Endpoint) error {
	return l.Log(ctx, Event{
		EventType: EventConnectionSuccess,
		Status:    StatusSuccess,
		Source:    endpoint,
	})
}

// ConnectionFailed logs failed SFTP connection attempts.
func (l *Logger) ConnectionFailed(ctx context.Context, endpoint *Endpoint, err error) error {
	return l.Log(ctx, Event{
		EventType: EventConnectionFailed,
		Status:    StatusFailed,
		Source:    endpoint,
		Error:     err.Error(),
	})
}

// EngineStarted logs when the relay engine starts.
func (l *Logger) EngineStarted(ctx context.Context, config map[string]interface{}) error {
	return l.Log(ctx, Event{
		EventType: EventEngineStarted,
		Status:    StatusSuccess,
		Details:   config,
	})
}

// EngineStopped logs when the relay engine stops.
func (l *Logger) EngineStopped(ctx context.Context, reason string) error {
	return l.Log(ctx, Event{
		EventType: EventEngineStopped,
		Status:    StatusSuccess,
		Details:   map[string]interface{}{"reason": reason},
	})
}

// Close flushes and closes the audit log file.
func (l *Logger) Close() error {
	if l.disabled || l.file == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	return l.file.Close()
}

// generateID creates a unique event ID.
func generateID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
}

// Noop returns a disabled logger for testing.
func Noop() *Logger {
	return &Logger{disabled: true}
}
