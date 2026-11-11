// Package logging provides the logging seam (agent.LoggerProvider)
// backed by a file logger that writes to OMEGA_HOME/omega.log.
//
// Seam: logging (exclusive).
//
// When disabled, a NopLogger is used so callers never need nil checks.
package logging

import (
	"fmt"
	"github.com/EndoTheDev/omega/agent"
	"log"
	"os"
	"sync"
	"time"
)

// FileLogger implements agent.LoggerProvider with a file backend.
type FileLogger struct {
	mu   sync.Mutex
	path string
	file *os.File
	log  *log.Logger
	// lastErr / lastErrAt are the newest ERROR-level entry (the health
	// popover's "last error" fact, agent.LoggerLastError). The message
	// is the formatted text WITHOUT the in-band "ERROR: " marker - the
	// row already says what it is.
	lastErr   string
	lastErrAt time.Time
}

// NewFileLogger opens the log file (append mode) and returns a
// FileLogger writing to it with timestamps.
func NewFileLogger(path string) (*FileLogger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return &FileLogger{
		path: path,
		file: f,
		log:  log.New(f, "", log.LstdFlags),
	}, nil
}

// Path returns the file this logger writes to. The inspector log tail
// (GET /logs, card t_db430fea) reads the same path the logger holds
// open, so a /logs read and the log writes can never disagree about
// what "the server's log" is.
func (l *FileLogger) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// Printf writes an info-level log entry.
func (l *FileLogger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Printf(format, args...)
}

// Errorf writes an error-level log entry.
func (l *FileLogger) Errorf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Printf("ERROR: "+format, args...)
	// The health fact keeps the newest entry only: the file tail is the
	// history (GET /logs), the popover asks "what broke last".
	l.lastErr = fmt.Sprintf(format, args...)
	l.lastErrAt = time.Now().Truncate(time.Second)
}

// LastError returns the newest ERROR-level entry and when it was
// written (agent.LoggerLastError - the health popover's last-error
// fact). ok=false when no error has been logged since this logger
// opened.
func (l *FileLogger) LastError() (string, time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastErr == "" {
		return "", time.Time{}, false
	}
	return l.lastErr, l.lastErrAt, true
}

// Warnf writes a warning-level log entry. The in-band "WARN: " marker
// (the "ERROR: " convention's sibling, card t_221dc498) is what the
// logs tab's severity filter classifies on - the marker IS the
// severity metadata; harness/logging's test pins the exact shapes.
func (l *FileLogger) Warnf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Printf("WARN: "+format, args...)
}

// Close flushes and closes the log file.
func (l *FileLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

// NopLogger implements agent.LoggerProvider with no-ops.
// Used when logging is disabled.
type NopLogger struct{}

// Printf is a no-op.
func (NopLogger) Printf(format string, args ...any) {}

// Warnf is a no-op.
func (NopLogger) Warnf(format string, args ...any) {}

// Errorf is a no-op.
func (NopLogger) Errorf(format string, args ...any) {}

// Close is a no-op.
func (NopLogger) Close() error { return nil }

// NewLogger creates a LoggerProvider from config. Returns a NopLogger
// when disabled or when File is empty - an empty path is the
// construction default (no cwd-relative fallback; resolution is
// cmd/omega's job), so it must never open a file in the process CWD.
// A FileLogger writing to the configured file otherwise.
func NewLogger(cfg Config) (agent.LoggerProvider, error) {
	if !cfg.Enabled || cfg.File == "" {
		return NopLogger{}, nil
	}
	return NewFileLogger(cfg.File)
}
