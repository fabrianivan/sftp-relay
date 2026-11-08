// Package logging writes leveled, daily-rotated log files and streams
// entries to subscribers such as the GUI log viewer.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level is a log severity.
type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

func (l Level) String() string {
	switch l {
	case Debug:
		return "DEBUG"
	case Info:
		return "INFO"
	case Warn:
		return "WARN"
	default:
		return "ERROR"
	}
}

// Entry is one log line.
type Entry struct {
	Time    time.Time
	Level   Level
	Message string
}

// String formats an entry the way it is written to disk.
func (e Entry) String() string {
	return fmt.Sprintf("%s %-5s %s", e.Time.Format("2006-01-02 15:04:05.000"), e.Level, e.Message)
}

const recentSize = 1000

// Logger is safe for concurrent use.
type Logger struct {
	mu        sync.Mutex
	dir       string
	base, ext string
	retention int
	day       string
	file      *os.File
	console   io.Writer
	minLevel  Level
	now       func() time.Time

	recent []Entry
	subs   map[int]chan Entry
	nextID int
}

// Options configures a Logger.
type Options struct {
	Path          string // e.g. logs/sftp-relay.log; files become sftp-relay_2026-01-02.log
	RetentionDays int
	Console       io.Writer // optional mirror (stdout in headless mode)
	MinLevel      Level
	Now           func() time.Time
}

// New opens a logger. An empty Path logs only to Console and subscribers.
func New(o Options) (*Logger, error) {
	l := &Logger{
		retention: o.RetentionDays,
		console:   o.Console,
		minLevel:  o.MinLevel,
		now:       o.Now,
		subs:      map[int]chan Entry{},
	}
	if l.now == nil {
		l.now = time.Now
	}
	if o.Path != "" {
		l.dir = filepath.Dir(o.Path)
		l.ext = filepath.Ext(o.Path)
		l.base = strings.TrimSuffix(filepath.Base(o.Path), l.ext)
		if err := os.MkdirAll(l.dir, 0o700); err != nil {
			return nil, err
		}
		l.mu.Lock()
		err := l.rotateLocked(l.now())
		l.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	return l, nil
}

// Discard returns a logger that drops everything (useful in tests).
func Discard() *Logger {
	l, _ := New(Options{})
	return l
}

func (l *Logger) fileName(day string) string {
	return filepath.Join(l.dir, fmt.Sprintf("%s_%s%s", l.base, day, l.ext))
}

// rotateLocked opens the file for the current day and prunes old ones.
func (l *Logger) rotateLocked(now time.Time) error {
	day := now.Format("2006-01-02")
	if day == l.day && l.file != nil {
		return nil
	}
	f, err := os.OpenFile(l.fileName(day), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	l.file, l.day = f, day
	l.pruneLocked(now)
	return nil
}

func (l *Logger) pruneLocked(now time.Time) {
	if l.retention <= 0 || l.dir == "" {
		return
	}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}
	cutoff := now.AddDate(0, 0, -l.retention).Format("2006-01-02")
	prefix := l.base + "_"
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, l.ext) {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(name, prefix), l.ext)
		if _, err := time.Parse("2006-01-02", day); err != nil {
			continue
		}
		if day < cutoff {
			_ = os.Remove(filepath.Join(l.dir, name))
		}
	}
}

func (l *Logger) log(level Level, format string, a ...any) {
	if l == nil {
		return
	}
	e := Entry{Time: l.now(), Level: level, Message: fmt.Sprintf(format, a...)}

	l.mu.Lock()
	if level >= l.minLevel {
		line := e.String() + "\n"
		if l.dir != "" {
			if err := l.rotateLocked(e.Time); err == nil {
				_, _ = l.file.WriteString(line)
			}
		}
		if l.console != nil {
			_, _ = io.WriteString(l.console, line)
		}
	}
	l.recent = append(l.recent, e)
	if len(l.recent) > recentSize {
		l.recent = append([]Entry(nil), l.recent[len(l.recent)-recentSize:]...)
	}
	for _, ch := range l.subs {
		select {
		case ch <- e:
		default: // slow subscriber: drop rather than block transfers
		}
	}
	l.mu.Unlock()
}

func (l *Logger) Debugf(format string, a ...any) { l.log(Debug, format, a...) }
func (l *Logger) Infof(format string, a ...any)  { l.log(Info, format, a...) }
func (l *Logger) Warnf(format string, a ...any)  { l.log(Warn, format, a...) }
func (l *Logger) Errorf(format string, a ...any) { l.log(Error, format, a...) }

// Subscribe streams new entries. Call the returned function to unsubscribe.
func (l *Logger) Subscribe() (<-chan Entry, func()) {
	ch := make(chan Entry, 512)
	l.mu.Lock()
	id := l.nextID
	l.nextID++
	l.subs[id] = ch
	l.mu.Unlock()
	return ch, func() {
		l.mu.Lock()
		if c, ok := l.subs[id]; ok {
			delete(l.subs, id)
			close(c)
		}
		l.mu.Unlock()
	}
}

// Recent returns the most recent entries (oldest first).
func (l *Logger) Recent() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Entry(nil), l.recent...)
}

// Dir returns the folder log files are written to.
func (l *Logger) Dir() string { return l.dir }

// CurrentFile returns today's log file path.
func (l *Logger) CurrentFile() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dir == "" {
		return ""
	}
	return l.fileName(l.day)
}

// Close flushes and closes the log file.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Sync()
		_ = l.file.Close()
		l.file = nil
	}
	for id, ch := range l.subs {
		delete(l.subs, id)
		close(ch)
	}
}
