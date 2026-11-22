// Package terminal implements the terminal module capsule. Do not add opt-ins or gates
// to this module; the owner explicitly rejected them.
package terminal

import (
	"bytes"
	"errors"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

const (
	maxSessions     = 4
	scrollbackMax   = 8 * 1024 * 1024
	scrollbackLines = 5000
	// replayTail: the restoration window - a reattach reads ONLY the
	// bounded tail of the retained history (never the whole thing; the
	// client may show less than the server keeps).
	replayTail = 256 * 1024
	// noOrphanTTL: a session no pane is attached to dies this long after its last use.
	noOrphanTTL = 15 * time.Minute
	pruneEvery  = time.Minute
	// subBuffer: SSE frames buffered per attached pane before the pane counts as stalled.
	subBuffer = 512
	// maxInput: the input route's body ceiling (a paste, not a file dump).
	maxInput = 64 * 1024
)

type process interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
	Resize(rows, cols uint16) error
	// Done closes when the child exited AND its pid is reaped.
	Done() <-chan struct{}
	Kill() error
	// ExitCode is valid only after Done closes.
	ExitCode() int
}

type spawnFunc func(workdir string) (process, error)

type event struct {
	kind byte
	data []byte
	code int
}

// subscription is one attached pane; the writer loop owns its channel.
type subscription struct {
	ch chan event
	mu     sync.Mutex
	missed bool
}

func (sub *subscription) setMissed() {
	sub.mu.Lock()
	sub.missed = true
	sub.mu.Unlock()
}

// checkMissed reads the drop mark (and clears it: checked once).
func (sub *subscription) checkMissed() bool {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	m := sub.missed
	sub.missed = false
	return m
}

type Session struct {
	id   string
	proc process

	mu        sync.Mutex
	createdAt time.Time
	buf       []byte
	lines     int // complete '\n'-terminated lines retained in buf
	subs      map[*subscription]struct{}
	exited    bool
	exitCode  int
	lastUse   time.Time
	removed   bool
	// claimed is the open's ownership mark. It makes the claim/attach gap honest -
	// reattach and the birth of a fresh spawn both mark under s.mu (the Store's scan reads
	// them there too), so a second racing pane never takes an owned shell.
	claimed bool
}

// subscribe registers a pane atomically with the replay snapshot.
func (s *Session) subscribe() (*subscription, []byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub := &subscription{ch: make(chan event, subBuffer)}
	if s.subs == nil {
		s.subs = map[*subscription]struct{}{}
	}
	s.subs[sub] = struct{}{}
	s.lastUse = time.Now()
	s.claimed = false // the stream adopted the claim: the sub owns it now
	tail := replayTailOf(s.buf)
	snap := make([]byte, len(tail))
	copy(snap, tail)
	return sub, snap, s.exited
}

// replayTailOf returns the restoration window: ONLY the bounded tail of the retained
// history (never the whole scrollback), cut on a codepoint boundary. The replay rides
// the SSE stream straight to the pane's xterm - nothing here writes to the pty, so the
// restoration cannot trigger fresh shell replies.
func replayTailOf(buf []byte) []byte {
	if len(buf) > replayTail {
		buf = buf[len(buf)-replayTail:]
		for len(buf) > 0 && buf[0]&0xC0 == 0x80 {
			buf = buf[1:]
		}
	}
	return buf
}

func (s *Session) unsubscribe(sub *subscription) {
	s.mu.Lock()
	delete(s.subs, sub)
	s.mu.Unlock()
}

// publish broadcasts one event to every attached pane. A stalled pane (full buffer) is
// marked missed and detaches - never blocks the shell, never drops another pane.
func (s *Session) publish(ev event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.removed {
		return
	}
	for sub := range s.subs {
		select {
		case sub.ch <- ev:
		default:
			sub.setMissed()
			delete(s.subs, sub)
		}
	}
}

func (s *Session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.proc.Read(buf)
		if n > 0 {
			s.record(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// record appends one output chunk. The retained history is never echoed back into the
// pty (the replay path is SSE-only and never touches the writer), so a running
// interactive program cannot receive its own old output as new input. The session owns
// the copy - the caller's buffer is scratch.
func (s *Session) record(chunk []byte) {
	chunkCopy := make([]byte, len(chunk))
	copy(chunkCopy, chunk)
	s.mu.Lock()
	if s.removed {
		s.mu.Unlock()
		return
	}
	s.buf = append(s.buf, chunkCopy...)
	s.lines += bytes.Count(chunkCopy, []byte{'\n'})
	s.trimLocked()
	s.lastUse = time.Now()
	s.mu.Unlock()
	s.publish(event{kind: 'o', data: chunkCopy})
}

// trimLocked enforces the scrollback caps (5,000 lines OR 8 MiB, whichever hits first).
// The OLDEST output evicts, and an eviction never cuts a UTF-8 codepoint: byte
// evictions skip the continuation bytes (\x80.\xBF land mid-codepoint by construction),
// line evictions cut after a '\n' - 0x0A is one of the few bytes that never appears
// inside a multi-byte sequence, so a line boundary is always a codepoint boundary.
func (s *Session) trimLocked() {
	if over := len(s.buf) - scrollbackMax; over > 0 {
		s.lines -= bytes.Count(s.buf[:over], []byte{'\n'})
		s.buf = s.buf[over:]
		for len(s.buf) > 0 && s.buf[0]&0xC0 == 0x80 {
			s.buf = s.buf[1:]
		}
	}
	for s.lines > scrollbackLines {
		i := bytes.IndexByte(s.buf, '\n')
		if i < 0 {
			break // an unterminated giant chunk: the byte cap owns it
		}
		s.buf = s.buf[i+1:]
		s.lines--
	}
}

func (s *Session) watchLoop() {
	<-s.proc.Done()
	s.mu.Lock()
	if s.removed {
		s.mu.Unlock()
		return
	}
	s.exited = true
	code := s.proc.ExitCode()
	s.exitCode = code
	s.mu.Unlock()
	s.publish(event{kind: 'x', code: code})
}

func (s *Session) remove() {
	s.mu.Lock()
	if s.removed {
		s.mu.Unlock()
		return
	}
	s.removed = true
	subs := make([]*subscription, 0, len(s.subs))
	for sub := range s.subs {
		subs = append(subs, sub)
	}
	s.subs = map[*subscription]struct{}{}
	s.mu.Unlock()
	for _, sub := range subs {
		close(sub.ch) // the waiters see the close: connection dropped
	}
	_ = s.proc.Kill()
	_ = s.proc.Close()
}

func (s *Session) write(b []byte) (int, error) {
	s.mu.Lock()
	if s.removed {
		s.mu.Unlock()
		return 0, errExited
	}
	exited := s.exited
	s.lastUse = time.Now()
	s.mu.Unlock()
	if exited {
		return 0, errExited
	}
	n, err := s.proc.Write(b)
	return n, err
}

func (s *Session) resize(rows, cols uint16) error {
	s.mu.Lock()
	if s.removed {
		s.mu.Unlock()
		return errExited
	}
	exited := s.exited
	s.lastUse = time.Now()
	s.mu.Unlock()
	if exited {
		return errExited
	}
	return s.proc.Resize(rows, cols)
}

var errExited = errors.New("terminal: the shell has exited")

// Store is safe for concurrent route handlers.
type Store struct {
	spawn   spawnFunc
	workdir string
	now     func() time.Time

	mu       sync.Mutex
	sessions map[string]*Session
	order    []string // creation order - the cap law's reap list
	stop     chan struct{}
}

func NewStore(spawn spawnFunc, workdir string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{
		spawn:    spawn,
		workdir:  workdir,
		now:      now,
		sessions: map[string]*Session{},
		stop:     make(chan struct{}),
	}
}

func (st *Store) Start() {
	go func() {
		t := time.NewTicker(pruneEvery)
		defer t.Stop()
		for {
			select {
			case <-st.stop:
				return
			case <-t.C:
				st.prune()
			}
		}
	}()
}

func (st *Store) Stop() error {
	select {
	case <-st.stop:
	default:
		close(st.stop)
	}
	st.mu.Lock()
	all := make([]*Session, 0, len(st.sessions))
	for _, s := range st.sessions {
		all = append(all, s)
	}
	st.sessions = map[string]*Session{}
	st.order = nil
	st.mu.Unlock()
	for _, s := range all {
		s.remove()
	}
	return nil
}

// Claim hands out a session. The scan, the mark and the spawn ride one lock: reattach
// claims mark the session taken (fresh spawns are born taken) and the pane's stream
// subscribe clears the mark later - so two panes racing Open never take the same shell,
// even though subscribe happens after the lock comes off. workdir is the SPAWN
// workspace: the claim's caller resolves it (session workspace, else the
// default_workspace knob, else "" - the construction fallback, omega's process cwd). A
// reattach ignores it: the shell already lives, its cwd is the user's state, never
// rewritten under them.
func (st *Store) Claim(workdir string) (*Session, bool, error) {
	if workdir == "" {
		workdir = st.workdir
	}
	st.mu.Lock()
	for i := len(st.order) - 1; i >= 0; i-- {
		s := st.sessions[st.order[i]]
		if s == nil {
			continue
		}
		s.mu.Lock()
		attachable := !s.exited && !s.removed && !s.claimed && len(s.subs) == 0
		if attachable {
			s.claimed = true // taken until the pane's stream subscribes
		}
		s.mu.Unlock()
		if attachable {
			st.mu.Unlock()
			return s, true, nil
		}
	}

	for len(st.sessions) >= maxSessions {
		victim := st.reapCandidateLocked()
		if victim == nil {
			err := &capError{live: len(st.sessions)}
			st.mu.Unlock()
			return nil, false, err
		}
		st.mu.Unlock()
		victim.remove()
		st.mu.Lock()
		st.forget(victim.id)
	}

	id, err := agent.NewSessionID()
	if err != nil {
		st.mu.Unlock()
		return nil, false, err
	}
	proc, err := st.spawn(workdir)
	if err != nil {
		st.mu.Unlock()
		return nil, false, err
	}
	s := &Session{id: id, proc: proc, createdAt: st.now(), lastUse: st.now(), claimed: true}
	st.sessions[id] = s
	st.order = append(st.order, id)
	st.mu.Unlock()
	go s.readLoop()
	go s.watchLoop()
	return s, false, nil
}

// reapCandidateLocked picks the cap law's victim. Live attached sessions are the panes'
// shells - NEVER reaped silently; nothing reapable = the cap refusal.
func (st *Store) reapCandidateLocked() *Session {
	for _, want := range []func(*Session) bool{
		func(s *Session) bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.exited && !s.removed
		},
		func(s *Session) bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return !s.exited && !s.removed && len(s.subs) == 0
		},
	} {
		for i := 0; i < len(st.order); i++ {
			s := st.sessions[st.order[i]]
			if s == nil {
				continue
			}
			if want(s) {
				return s
			}
		}
	}
	return nil
}

func (st *Store) forget(id string) {
	delete(st.sessions, id)
	for i, o := range st.order {
		if o == id {
			st.order = append(st.order[:i], st.order[i+1:]...)
			break
		}
	}
}

func (st *Store) Get(id string) *Session {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.sessions[id]
}

func (st *Store) Drop(id string) bool {
	st.mu.Lock()
	s := st.sessions[id]
	if s != nil {
		st.forget(id)
	}
	st.mu.Unlock()
	if s != nil {
		s.remove()
	}
	return s != nil
}

type listing struct {
	ID       string `json:"id"`
	Exited   bool   `json:"exited"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Created  string `json:"created_at"`
}

func (st *Store) List() []listing {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]listing, 0, len(st.sessions))
	for _, id := range st.order {
		s := st.sessions[id]
		if s == nil {
			continue
		}
		s.mu.Lock()
		row := listing{ID: s.id, Exited: s.exited, Created: s.createdAt.UTC().Format(time.RFC3339)}
		if s.exited {
			code := s.exitCode
			row.ExitCode = &code
		}
		s.mu.Unlock()
		out = append(out, row)
	}
	return out
}

func (st *Store) prune() {
	cutoff := st.now().Add(-noOrphanTTL)
	st.mu.Lock()
	victims := []*Session{}
	for _, id := range st.order {
		s := st.sessions[id]
		if s == nil {
			continue
		}
		s.mu.Lock()
		orphan := s.lastUse.Before(cutoff) && len(s.subs) == 0
		s.mu.Unlock()
		if orphan {
			victims = append(victims, s)
		}
	}
	for _, v := range victims {
		st.forget(v.id)
	}
	st.mu.Unlock()
	for _, v := range victims {
		v.remove()
	}
}

type capError struct{ live int }

func (e *capError) Error() string {
	return "terminal: session cap reached; close a terminal pane first"
}
