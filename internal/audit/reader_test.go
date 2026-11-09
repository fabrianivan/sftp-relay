package audit

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestReaderQuery(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	// Create some test events
	logger, _ := New(Options{LogPath: logPath, Mode: "test"})
	ctx := context.Background()

	source := &Endpoint{Host: "source.example.com", Port: 22}
	target := &Endpoint{Host: "target.example.com", Port: 22}
	file1 := &FileInfo{Name: "report.csv", Size: 1024}
	file2 := &FileInfo{Name: "data.xml", Size: 2048}

	logger.TransferStarted(ctx, source, target, file1)
	time.Sleep(time.Millisecond)
	logger.TransferCompleted(ctx, source, target, file1, time.Second)
	time.Sleep(time.Millisecond)
	logger.TransferStarted(ctx, source, target, file2)
	time.Sleep(time.Millisecond)
	logger.TransferFailed(ctx, source, target, file2, fmt.Errorf("connection lost"), time.Second)

	logger.Close()

	// Query all events
	reader := NewReader(logPath)
	events, err := reader.All()
	if err != nil {
		t.Fatalf("All() failed: %v", err)
	}
	if len(events) != 4 {
		t.Errorf("expected 4 events, got %d", len(events))
	}

	// Query by event type
	completed, err := reader.Query(Filter{
		EventTypes: []EventType{EventTransferCompleted},
	})
	if err != nil {
		t.Fatalf("Query() failed: %v", err)
	}
	if len(completed) != 1 {
		t.Errorf("expected 1 completed event, got %d", len(completed))
	}

	// Query by filename
	csvEvents, err := reader.Query(Filter{
		Filename: "report.csv",
	})
	if err != nil {
		t.Fatalf("Query() by filename failed: %v", err)
	}
	if len(csvEvents) != 2 {
		t.Errorf("expected 2 events for report.csv, got %d", len(csvEvents))
	}

	// Query by status
	failed, err := reader.Query(Filter{
		Status: []Status{StatusFailed},
	})
	if err != nil {
		t.Fatalf("Query() by status failed: %v", err)
	}
	if len(failed) != 1 {
		t.Errorf("expected 1 failed event, got %d", len(failed))
	}
}

func TestReaderRecent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	logger, _ := New(Options{LogPath: logPath, Mode: "test"})
	for i := 0; i < 10; i++ {
		logger.Log(context.Background(), Event{
			EventType: EventScanCompleted,
			Status:    StatusSuccess,
		})
	}
	logger.Close()

	reader := NewReader(logPath)
	recent, err := reader.Recent(3)
	if err != nil {
		t.Fatalf("Recent() failed: %v", err)
	}
	if len(recent) != 3 {
		t.Errorf("expected 3 recent events, got %d", len(recent))
	}
}

func TestReaderTimeRange(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	logger, _ := New(Options{LogPath: logPath, Mode: "test"})
	ctx := context.Background()

	now := time.Now()
	logger.Log(ctx, Event{EventType: EventScanStarted, Status: StatusStarted})
	time.Sleep(10 * time.Millisecond)
	middle := time.Now()
	time.Sleep(10 * time.Millisecond)
	logger.Log(ctx, Event{EventType: EventScanCompleted, Status: StatusSuccess})

	logger.Close()

	reader := NewReader(logPath)

	// Query events after middle time
	afterMiddle, err := reader.Query(Filter{StartTime: &middle})
	if err != nil {
		t.Fatalf("Query() with time range failed: %v", err)
	}
	if len(afterMiddle) != 1 {
		t.Errorf("expected 1 event after middle time, got %d", len(afterMiddle))
	}

	// Query events before middle time
	beforeMiddle, err := reader.Query(Filter{EndTime: &middle})
	if err != nil {
		t.Fatalf("Query() with time range failed: %v", err)
	}
	if len(beforeMiddle) != 1 {
		t.Errorf("expected 1 event before middle time, got %d", len(beforeMiddle))
	}

	// Query events in range
	end := now.Add(time.Hour)
	inRange, err := reader.Query(Filter{StartTime: &now, EndTime: &end})
	if err != nil {
		t.Fatalf("Query() with time range failed: %v", err)
	}
	if len(inRange) != 2 {
		t.Errorf("expected 2 events in range, got %d", len(inRange))
	}
}

func TestReaderLimit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	logger, _ := New(Options{LogPath: logPath, Mode: "test"})
	for i := 0; i < 100; i++ {
		logger.Log(context.Background(), Event{
			EventType: EventScanCompleted,
			Status:    StatusSuccess,
		})
	}
	logger.Close()

	reader := NewReader(logPath)
	limited, err := reader.Query(Filter{Limit: 10})
	if err != nil {
		t.Fatalf("Query() with limit failed: %v", err)
	}
	if len(limited) != 10 {
		t.Errorf("expected 10 events, got %d", len(limited))
	}
}

func TestReaderTransferHistory(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	logger, _ := New(Options{LogPath: logPath, Mode: "test"})
	ctx := context.Background()

	file := &FileInfo{Name: "important.pdf", Size: 5000}
	logger.TransferStarted(ctx, nil, nil, file)
	logger.TransferCompleted(ctx, nil, nil, file, time.Second)
	logger.FileArchived(ctx, nil, file)

	// Different file
	otherFile := &FileInfo{Name: "other.txt", Size: 100}
	logger.TransferStarted(ctx, nil, nil, otherFile)

	logger.Close()

	reader := NewReader(logPath)
	history, err := reader.TransferHistory("important.pdf")
	if err != nil {
		t.Fatalf("TransferHistory() failed: %v", err)
	}
	if len(history) != 3 {
		t.Errorf("expected 3 events for important.pdf, got %d", len(history))
	}
}

func TestReaderExport(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	logger, _ := New(Options{LogPath: logPath, Mode: "test"})
	logger.Log(context.Background(), Event{
		EventType: EventScanCompleted,
		Status:    StatusSuccess,
	})
	logger.Close()

	reader := NewReader(logPath)
	var buf bytes.Buffer
	if err := reader.Export(&buf, Filter{}); err != nil {
		t.Fatalf("Export() failed: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("expected non-empty export")
	}
}

func TestReaderNonexistentFile(t *testing.T) {
	reader := NewReader("/nonexistent/audit.log")
	events, err := reader.All()
	if err != nil {
		t.Errorf("All() should not error on nonexistent file: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected empty events, got %d", len(events))
	}
}
