// The logging half of the observability capsule. When disabled, a NopLogger is used so
// callers never need nil checks.
package observability

import (
	"fmt"
	"github.com/EndoTheDev/omega/agent"
	"log"
	"os"
	"sync"
	"time"
)

type FileLogger struct {
	mu   sync.Mutex
	path string
	file *os.File
	log  *log.Logger
	lastErr   string
	lastErrAt time.Time
}

// NewFileLogger opens the log file in append mode.
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

// Path returns the file this logger writes to. The inspector log tail reads the same
// path the logger holds open, so a /logs read and the log writes can never disagree
// about what "the server's log" is.
func (l *FileLogger) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

func (l *FileLogger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Printf(format, args...)
}

func (l *FileLogger) Errorf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Printf("ERROR: "+format, args...)
	l.lastErr = fmt.Sprintf(format, args...)
	l.lastErrAt = time.Now().Truncate(time.Second)
}

// LastError returns the newest ERROR-level entry and when it was written.
func (l *FileLogger) LastError() (string, time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastErr == "" {
		return "", time.Time{}, false
	}
	return l.lastErr, l.lastErrAt, true
}

func (l *FileLogger) Warnf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Printf("WARN: "+format, args...)
}

func (l *FileLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

type NopLogger struct{}

func (NopLogger) Printf(format string, args ...any) {}

func (NopLogger) Warnf(format string, args ...any) {}

func (NopLogger) Errorf(format string, args ...any) {}

func (NopLogger) Close() error { return nil }

// NewLogger creates a LoggerProvider from config. Returns a NopLogger when disabled or
// when File is empty - an empty path is the construction default (no cwd-relative
// fallback; resolution is cmd/omega's job), so it must never open a file in the process
// CWD.
func NewLogger(cfg Config) (agent.LoggerProvider, error) {
	if !cfg.Enabled || cfg.File == "" {
		return NopLogger{}, nil
	}
	return NewFileLogger(cfg.File)
}
