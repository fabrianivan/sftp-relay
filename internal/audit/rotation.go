package audit

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RotateOptions configures log rotation behavior.
type RotateOptions struct {
	LogPath       string // Path to the audit log file
	RetentionDays int    // Delete logs older than this (0 = keep forever)
	Compress      bool   // Compress rotated logs with gzip
}

// Rotate rotates the audit log file if it exists.
// The current log is renamed to audit-YYYY-MM-DD.log and optionally compressed.
// Old logs beyond retention days are deleted.
func Rotate(opts RotateOptions) error {
	// Check if log file exists
	info, err := os.Stat(opts.LogPath)
	if os.IsNotExist(err) {
		return nil // Nothing to rotate
	}
	if err != nil {
		return fmt.Errorf("stat log file: %w", err)
	}
	
	// Skip if file is empty
	if info.Size() == 0 {
		return nil
	}

	// Generate rotated filename
	timestamp := time.Now().Format("2006-01-02")
	dir := filepath.Dir(opts.LogPath)
	base := strings.TrimSuffix(filepath.Base(opts.LogPath), filepath.Ext(opts.LogPath))
	ext := filepath.Ext(opts.LogPath)
	
	rotatedPath := filepath.Join(dir, fmt.Sprintf("%s-%s%s", base, timestamp, ext))
	
	// If today's rotated file already exists, append a counter
	if _, err := os.Stat(rotatedPath); err == nil {
		for i := 1; i < 100; i++ {
			rotatedPath = filepath.Join(dir, fmt.Sprintf("%s-%s-%d%s", base, timestamp, i, ext))
			if _, err := os.Stat(rotatedPath); os.IsNotExist(err) {
				break
			}
		}
	}

	// Rename current log to rotated name
	if err := os.Rename(opts.LogPath, rotatedPath); err != nil {
		return fmt.Errorf("rotate log file: %w", err)
	}

	// Compress if requested
	if opts.Compress {
		if err := compressFile(rotatedPath); err != nil {
			// Non-fatal: log was rotated but compression failed
			return fmt.Errorf("compress rotated log: %w", err)
		}
	}

	// Clean up old logs
	if opts.RetentionDays > 0 {
		if err := cleanupOldLogs(dir, base, opts.RetentionDays); err != nil {
			// Non-fatal: rotation succeeded but cleanup failed
			return fmt.Errorf("cleanup old logs: %w", err)
		}
	}

	return nil
}

// compressFile compresses a file with gzip and removes the original.
func compressFile(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()

	gzPath := path + ".gz"
	dst, err := os.Create(gzPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	gz := gzip.NewWriter(dst)
	gz.Name = filepath.Base(path)
	gz.ModTime = time.Now()

	if _, err := io.Copy(gz, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(gzPath)
		return err
	}

	if err := gz.Close(); err != nil {
		_ = dst.Close()
		_ = os.Remove(gzPath)
		return err
	}

	if err := dst.Close(); err != nil {
		_ = os.Remove(gzPath)
		return err
	}

	// Remove original file after successful compression
	return os.Remove(path)
}

// cleanupOldLogs removes log files older than retentionDays.
func cleanupOldLogs(dir, basePattern string, retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	pattern := filepath.Join(dir, basePattern+"-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue // Skip files we can't stat
		}

		if info.ModTime().Before(cutoff) {
			if err := os.Remove(match); err != nil {
				// Log but continue with other files
				continue
			}
		}
	}

	return nil
}

// RotateIfNeeded rotates the log if it's older than the specified duration
// or larger than the specified size.
func RotateIfNeeded(opts RotateOptions, maxAge time.Duration, maxSize int64) error {
	info, err := os.Stat(opts.LogPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// Check age
	if maxAge > 0 && time.Since(info.ModTime()) > maxAge {
		return Rotate(opts)
	}

	// Check size
	if maxSize > 0 && info.Size() > maxSize {
		return Rotate(opts)
	}

	return nil
}
