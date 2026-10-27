// Package logging provides the logging seam (core.LoggerProvider)
// backed by a file logger that writes to OMEGA_HOME/omega.log.
//
// Seam: logging (exclusive).
//
// When disabled, a NopLogger is used so callers never need nil checks.
package logging

import (
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// FileLogger implements core.LoggerProvider with a file backend.
type FileLogger struct {
	mu   sync.Mutex
	path string
	file *os.File
	log  *log.Logger
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
}

// Close flushes and closes the log file.
func (l *FileLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

// NopLogger implements core.LoggerProvider with no-ops.
// Used when logging is disabled.
type NopLogger struct{}

// Printf is a no-op.
func (NopLogger) Printf(format string, args ...any) {}

// Errorf is a no-op.
func (NopLogger) Errorf(format string, args ...any) {}

// Close is a no-op.
func (NopLogger) Close() error { return nil }

// NewLogger creates a LoggerProvider from config. Returns a NopLogger
// when disabled or when File is empty - an empty path is the
// construction default (no cwd-relative fallback; resolution is
// cmd/omega's job), so it must never open a file in the process CWD.
// A FileLogger writing to the configured file otherwise.
func NewLogger(cfg Config) (core.LoggerProvider, error) {
	if !cfg.Enabled || cfg.File == "" {
		return NopLogger{}, nil
	}
	return NewFileLogger(cfg.File)
}