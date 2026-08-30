// Package logging provides the logging seam (agent.LoggerProvider)
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

	"github.com/EndoTheDev/omega/agent"
)

// FileLogger implements agent.LoggerProvider with a file backend.
type FileLogger struct {
	mu   sync.Mutex
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
		file: f,
		log:  log.New(f, "", log.LstdFlags),
	}, nil
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

// NopLogger implements agent.LoggerProvider with no-ops.
// Used when logging is disabled.
type NopLogger struct{}

// Printf is a no-op.
func (NopLogger) Printf(format string, args ...any) {}

// Errorf is a no-op.
func (NopLogger) Errorf(format string, args ...any) {}

// Close is a no-op.
func (NopLogger) Close() error { return nil }

// NewLogger creates a LoggerProvider from config. Returns a NopLogger
// when disabled, or a FileLogger writing to the configured file.
func NewLogger(cfg Config) (agent.LoggerProvider, error) {
	if !cfg.Enabled {
		return NopLogger{}, nil
	}
	return NewFileLogger(cfg.File)
}