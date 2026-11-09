package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRotate(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	// Create a log file with some content
	if err := os.WriteFile(logPath, []byte("test log content\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Rotate it
	err := Rotate(RotateOptions{
		LogPath:       logPath,
		RetentionDays: 7,
		Compress:      false,
	})
	if err != nil {
		t.Fatalf("Rotate() failed: %v", err)
	}

	// Original file should be gone or recreated
	if _, err := os.Stat(logPath); err == nil {
		// File was recreated (which is ok)
	}

	// Rotated file should exist
	today := time.Now().Format("2006-01-02")
	rotatedPath := filepath.Join(dir, "audit-"+today+".log")
	if _, err := os.Stat(rotatedPath); os.IsNotExist(err) {
		t.Fatal("rotated file was not created")
	}

	// Content should be preserved
	content, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "test log content\n" {
		t.Errorf("content mismatch: got %q", string(content))
	}
}

func TestRotateWithCompression(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	// Create a log file
	if err := os.WriteFile(logPath, []byte("test log content for compression\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Rotate with compression
	err := Rotate(RotateOptions{
		LogPath:       logPath,
		RetentionDays: 0,
		Compress:      true,
	})
	if err != nil {
		t.Fatalf("Rotate() with compression failed: %v", err)
	}

	// Compressed file should exist
	today := time.Now().Format("2006-01-02")
	gzPath := filepath.Join(dir, "audit-"+today+".log.gz")
	if _, err := os.Stat(gzPath); os.IsNotExist(err) {
		t.Fatal("compressed file was not created")
	}

	// Original rotated file should be removed after compression
	rotatedPath := filepath.Join(dir, "audit-"+today+".log")
	if _, err := os.Stat(rotatedPath); !os.IsNotExist(err) {
		t.Error("original rotated file should be removed after compression")
	}
}

func TestRotateEmptyFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	// Create an empty log file
	if err := os.WriteFile(logPath, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	// Rotate should do nothing for empty files
	err := Rotate(RotateOptions{
		LogPath:       logPath,
		RetentionDays: 0,
		Compress:      false,
	})
	if err != nil {
		t.Fatalf("Rotate() failed: %v", err)
	}

	// Empty file should still exist (not rotated)
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("empty file was rotated (should be skipped)")
	}
}

func TestRotateNonexistent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nonexistent.log")

	// Rotate nonexistent file should not error
	err := Rotate(RotateOptions{
		LogPath:       logPath,
		RetentionDays: 0,
		Compress:      false,
	})
	if err != nil {
		t.Errorf("Rotate() should not error on nonexistent file: %v", err)
	}
}

func TestCleanupOldLogs(t *testing.T) {
	dir := t.TempDir()

	// Create some old log files
	oldDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	recentDate := time.Now().AddDate(0, 0, -3).Format("2006-01-02")

	oldPath := filepath.Join(dir, "audit-"+oldDate+".log")
	recentPath := filepath.Join(dir, "audit-"+recentDate+".log")

	if err := os.WriteFile(oldPath, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recentPath, []byte("recent"), 0600); err != nil {
		t.Fatal(err)
	}

	// Set old mod time
	oldTime := time.Now().AddDate(0, 0, -10)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Clean up logs older than 7 days
	err := cleanupOldLogs(dir, "audit", 7)
	if err != nil {
		t.Fatalf("cleanupOldLogs() failed: %v", err)
	}

	// Old file should be deleted
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old log file should have been deleted")
	}

	// Recent file should still exist
	if _, err := os.Stat(recentPath); os.IsNotExist(err) {
		t.Error("recent log file should not have been deleted")
	}
}

func TestRotateIfNeeded(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	// Create a log file
	content := make([]byte, 1024*10) // 10KB
	if err := os.WriteFile(logPath, content, 0600); err != nil {
		t.Fatal(err)
	}

	// Should rotate if size exceeds threshold
	err := RotateIfNeeded(RotateOptions{
		LogPath:       logPath,
		RetentionDays: 0,
		Compress:      false,
	}, 0, 1024*5) // 5KB threshold
	if err != nil {
		t.Fatalf("RotateIfNeeded() failed: %v", err)
	}

	// Rotated file should exist
	today := time.Now().Format("2006-01-02")
	rotatedPath := filepath.Join(dir, "audit-"+today+".log")
	if _, err := os.Stat(rotatedPath); os.IsNotExist(err) {
		t.Error("log should have been rotated based on size")
	}
}
