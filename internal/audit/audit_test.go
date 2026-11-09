package audit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditLogger(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	logger, err := New(Options{
		LogPath: logPath,
		Mode:    "test",
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer logger.Close()

	ctx := context.Background()

	// Test transfer events
	source := &Endpoint{Host: "source.example.com", Port: 22, Username: "user", Directory: "/outbox"}
	target := &Endpoint{Host: "target.example.com", Port: 22, Username: "user", Directory: "/inbox"}
	file := &FileInfo{Name: "test.csv", Size: 1024, SourcePath: "/outbox/test.csv", TargetPath: "/inbox/test.csv"}

	if err := logger.TransferStarted(ctx, source, target, file); err != nil {
		t.Errorf("TransferStarted() failed: %v", err)
	}

	if err := logger.TransferCompleted(ctx, source, target, file, time.Second); err != nil {
		t.Errorf("TransferCompleted() failed: %v", err)
	}

	// Verify log file was created
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Fatal("audit log file was not created")
	}

	// Read and parse log entries using reader
	reader := NewReader(logPath)
	events, err := reader.All()
	if err != nil {
		t.Fatalf("failed to read audit log: %v", err)
	}

	// Should have 2 entries
	if len(events) != 2 {
		t.Errorf("expected 2 log entries, got %d", len(events))
	}

	// Check first entry
	event := events[0]

	if event.EventType != EventTransferStarted {
		t.Errorf("expected EventTransferStarted, got %s", event.EventType)
	}
	if event.Actor.Mode != "test" {
		t.Errorf("expected mode 'test', got %s", event.Actor.Mode)
	}
	if event.File.Name != "test.csv" {
		t.Errorf("expected file name 'test.csv', got %s", event.File.Name)
	}
}

func TestDisabledLogger(t *testing.T) {
	logger, err := New(Options{
		LogPath:  "/tmp/should-not-exist.log",
		Disabled: true,
	})
	if err != nil {
		t.Fatalf("New() with Disabled should not fail: %v", err)
	}

	// Should not create file
	if err := logger.TransferStarted(context.Background(), nil, nil, nil); err != nil {
		t.Errorf("disabled logger should not error: %v", err)
	}

	if _, err := os.Stat("/tmp/should-not-exist.log"); !os.IsNotExist(err) {
		t.Error("disabled logger should not create file")
	}
}

func TestNoop(t *testing.T) {
	logger := Noop()
	if !logger.disabled {
		t.Error("Noop() should return disabled logger")
	}

	if err := logger.Log(context.Background(), Event{}); err != nil {
		t.Errorf("Noop logger should not error: %v", err)
	}
}

func TestConfigChanged(t *testing.T) {
	dir := t.TempDir()
	logger, _ := New(Options{LogPath: filepath.Join(dir, "audit.log"), Mode: "test"})
	defer logger.Close()

	changes := map[string]interface{}{
		"pollInterval": 60,
		"maxParallel":  4,
	}

	if err := logger.ConfigChanged(context.Background(), changes); err != nil {
		t.Errorf("ConfigChanged() failed: %v", err)
	}
}

func TestConnectionEvents(t *testing.T) {
	dir := t.TempDir()
	logger, _ := New(Options{LogPath: filepath.Join(dir, "audit.log"), Mode: "test"})
	defer logger.Close()

	endpoint := &Endpoint{Host: "test.example.com", Port: 22, Username: "user"}

	if err := logger.ConnectionSuccess(context.Background(), endpoint); err != nil {
		t.Errorf("ConnectionSuccess() failed: %v", err)
	}

	if err := logger.ConnectionFailed(context.Background(), endpoint, os.ErrPermission); err != nil {
		t.Errorf("ConnectionFailed() failed: %v", err)
	}
}

func TestEngineEvents(t *testing.T) {
	dir := t.TempDir()
	logger, _ := New(Options{LogPath: filepath.Join(dir, "audit.log"), Mode: "test"})
	defer logger.Close()

	config := map[string]interface{}{
		"source": "sftp://source:22",
		"target": "sftp://target:22",
	}

	if err := logger.EngineStarted(context.Background(), config); err != nil {
		t.Errorf("EngineStarted() failed: %v", err)
	}

	if err := logger.EngineStopped(context.Background(), "user requested"); err != nil {
		t.Errorf("EngineStopped() failed: %v", err)
	}
}

func TestFilePermissions(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	logger, _ := New(Options{LogPath: logPath, Mode: "test"})
	logger.Close()

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("failed to stat audit log: %v", err)
	}

	// Audit log should be readable/writable only by owner (0600)
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %o", info.Mode().Perm())
	}
}
