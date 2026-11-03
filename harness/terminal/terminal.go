// Package terminal implements the terminal module capsule (card
// t_55a3fc4b, Endo's ruling 2026-11-01): one live shell (pty) per
// dock pane, streamed to the webui's xterm.js Terminal tab over SSE.
//
// TRUST MODEL (the settled ruling - it supersedes the card body's
// trust paragraphs): NO confirm-gate on open, NO disabled-by-default.
// The webui terminal is a shell on this box with zero restrictions -
// Endo's box behind the tailnet. Do not add opt-ins or gates to this
// module; the owner explicitly rejected them.
//
// Lifecycle (VS Code parity, one terminal per pane): the pane's tab
// claims a session on mount, types into it, streams its output; the
// shell keeps running while another tab is active (the dock unmounts
// the outlet, the session idles server-side and a remount reattaches
// to it). Sessions die by: DELETE (the kill affordance), the cap law
// (maxSessions - the oldest idle orphan is reaped first), the orphan
// prune (noOrphanTTL with no subscriber and no activity), or the
// module's lifecycle stop.
//
// Removal proof: TestTerminalRemovalProof (cmd/omega/removal_test.go).
package terminal

import (
	"bytes"
	"errors"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// The hard shapes. maxSessions is the cap law: one terminal per pane
// was ruled, so more terminals = more panes - four covers the split
// pane count a screen can plausibly host, and the cap is what stops a
// reload loop from farming shells.
const (
	maxSessions = 4
	// SCROLLBACK CAPS (the bounded terminal-runtime rules): the server
	// owns at most 5,000 lines OR 8 MiB of a shell's output, whichever
	// hits first, evicting the OLDEST. The bounded history is the
	// server's truth - the client may show less.
	scrollbackMax   = 8 * 1024 * 1024
	scrollbackLines = 5000
	// replayTail: the restoration window - a reattach reads ONLY the
	// bounded tail of the retained history (never the whole thing; the
	// client may show less than the server keeps).
	replayTail = 256 * 1024
	// noOrphanTTL: a session no pane is attached to dies this long
	// after its last activity - a shell nobody watches and nobody
	// typed into is an orphan, not state to keep.
	noOrphanTTL = 15 * time.Minute
	// pruneEvery: the orphan prune's tick.
	pruneEvery = time.Minute
	// subBuffer: SSE frames buffered per attached pane before the
	// pane counts as stalled (the session keeps running; the stalled
	// pane reconnects and replays).
	subBuffer = 512
	// maxInput: the input route's body ceiling (a paste, not a file
	// dump); larger pastes come in chunked frames.
	maxInput = 64 * 1024
)

// process is the live shell seam a session drives: the pty master on
// real runs, a pipe pair under test. The handlers never know which
// side they hold.
type process interface {
	// Read/Write/Close: the master - the server reads shell output
	// from it and writes keystrokes to it.
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
	// Resize sets the child's window size (TIOCSWINSZ).
	Resize(rows, cols uint16) error
	// Done closes when the child exited AND its pid is reaped.
	Done() <-chan struct{}
	// Kill takes the child down hard (its whole process group).
	Kill() error
	// ExitCode is valid only after Done closes.
	ExitCode() int
}

// spawnFunc is the spawn seam (real pty on unix, pipe pair under
// test).
type spawnFunc func(workdir string) (process, error)

// event is one SSE frame source. kind: 'o' shell output (base64 in
// the frame), 'x' child exit (code in the frame).
type event struct {
	kind byte
	data []byte
	code int
}

// subscription is one attached pane; the writer loop owns its channel.
type subscription struct {
	ch chan event
	// missed: the session dropped this stalled pane - it reconnects
	// and replays. Own mutex: the publisher sets it under the
	// session's lock, the SSE handler reads it bare.
	mu     sync.Mutex
	missed bool
}

// setMissed is the publisher's drop mark.
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

// Session is one live shell: the child process, the scrollback buffer
// (the bounded history), the attached panes. mu guards
// buf/lines/subs/exited/lastUse and the process handoff; the read and
// watch loops take it per publish.
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
	// claimed is the open's ownership mark: Claim sets it while the
	// pane's stream has not subscribed yet, subscribe() clears it. It
	// makes the claim/attach gap honest - reattach and the birth of a
	// fresh spawn both mark under s.mu (the Store's scan reads them
	// there too), so a second racing pane never takes an owned shell.
	claimed bool
}

// subscribe registers a pane atomically with the replay snapshot: the
// snapshot and the registration happen under one lock, so no output
// can appear twice (in the snapshot AND on the channel) and none can
// fall in the gap. Returns the subscription, the replay tail, and the
// session's exit state AT THE SNAPSHOT (a pane reattaching to an
// already-exited shell gets the state directly - the exit broadcast
// happened in the past, waiting for it would be a 20s silence).
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

// replayTailOf returns the restoration window: ONLY the bounded tail
// of the retained history (never the whole scrollback), cut on a
// codepoint boundary. The replay rides the SSE stream straight to the
// pane's xterm - nothing here writes to the pty, so the restoration
// cannot trigger fresh shell replies.
func replayTailOf(buf []byte) []byte {
	if len(buf) > replayTail {
		buf = buf[len(buf)-replayTail:]
		for len(buf) > 0 && buf[0]&0xC0 == 0x80 {
			buf = buf[1:]
		}
	}
	return buf
}

// unsubscribe detaches a pane (tab switch, reconnect, page reload).
func (s *Session) unsubscribe(sub *subscription) {
	s.mu.Lock()
	delete(s.subs, sub)
	s.mu.Unlock()
}

// publish broadcasts one event to every attached pane. A stalled pane
// (full buffer) is marked missed and detaches - never blocks the
// shell, never drops another pane.
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

// readLoop moves shell output into the replay buffer and to the
// panes. Runs until the master closes (child hangup, Kill, removal).
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

// record appends one output chunk. PTY output is INCREMENTAL - chunks
// append, snapshots are the only materialization boundary (the
// subscriber's tail copy under subscribe()). The retained history is
// never echoed back into the pty (the replay path is SSE-only and
// never touches the writer), so a running interactive program cannot
// receive its own old output as new input. The session owns the copy -
// the caller's buffer is scratch.
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

// trimLocked enforces the scrollback caps (5,000 lines OR 8 MiB,
// whichever hits first). The OLDEST output evicts, and an eviction
// never cuts a UTF-8 codepoint: byte evictions skip the continuation
// bytes (\x80..\xBF land mid-codepoint by construction), line
// evictions cut after a '\n' - 0x0A is one of the few bytes that never
// appears inside a multi-byte sequence, so a line boundary is always a
// codepoint boundary.
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

// watchLoop broadcasts the exit once the child is reaped.
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

// remove kills and forgets the session (DELETE route, cap law, prune,
// lifecycle stop). Idempotent.
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

// write moves one input frame into the shell (an exited shell
// refuses - the Gone at the wire). Touches lastUse: typing is life.
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

// resize sets the shell's window (the pane's ResizeObserver). Resizing
// an exited shell is nothing to do - the refusal is the Gone.
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

// errExited is input/resize against a dead shell.
var errExited = errors.New("terminal: the shell has exited")

// Store owns the session table. Safe for concurrent route handlers.
type Store struct {
	spawn   spawnFunc
	workdir string
	now     func() time.Time

	mu       sync.Mutex
	sessions map[string]*Session
	order    []string // creation order - the cap law's reap list
	stop     chan struct{}
}

// NewStore builds the store. spawn is the seam (spawnProcess on
// unix); workdir is the shell's cwd (omega's working directory); now
// is the clock (nil = time.Now; tests inject).
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

// Start arms the orphan prune (the lifecycle handle's start - Mount
// starts nothing, the host StartAll does). Stop ends it.
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

// Stop ends the prune and kills every session (the lifecycle fence:
// the dismount kills its shells).
func (st *Store) Stop() error {
	select {
	case <-st.stop:
		// already stopped
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

// Claim hands out a session: the newest live one with no attached
// pane (a pane remounting after a sibling-tab switch or a page
// reload reattaches to its previous shell), else a fresh spawn. The
// scan, the mark and the spawn ride one lock: reattach claims mark
// the session taken (fresh spawns are born taken) and the pane's
// stream subscribe clears the mark later - so two panes racing Open
// never take the same shell, even though subscribe happens after the
// lock comes off. The bool is the claim report: false = freshly
// spawned, true = reattached.
func (st *Store) Claim() (*Session, bool, error) {
	st.mu.Lock()
	// reattach: newest live session with zero subscribers and no
	// standing claim
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

	// cap law: kill the oldest idle orphan, else the oldest session
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
	proc, err := st.spawn(st.workdir)
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

// reapCandidateLocked picks the cap law's victim: the oldest exited
// session first (a dead shell holds only a replay buffer), then the
// oldest with no subscribers (an idle shell nobody owns). Live
// attached sessions are the panes' shells - NEVER reaped silently;
// nothing reapable = the cap refusal. The want funcs read under
// s.mu: watchLoop and remove write exited/removed there.
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

// Get returns the session by id (unknown = nil).
func (st *Store) Get(id string) *Session {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.sessions[id]
}

// Drop removes one session (DELETE route). Missing = no-op report.
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

// listing is the GET /terminal/sessions row.
type listing struct {
	ID       string `json:"id"`
	Exited   bool   `json:"exited"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Created  string `json:"created_at"`
}

// List snapshots the table, oldest first.
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

// prune reaps orphans: exited or unattached sessions idle past the
// TTL. A live untouched session also goes - nobody subscribed and
// nobody typed for noOrphanTTL is the orphan shape for a running
// shell too (the pane that owned it vanished; when it comes back it
// spawns fresh, which is honest).
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

// capError is the spawn refusal past the cap with nothing reapable
// (four live attached sessions - the panes own them). 409 at the wire.
type capError struct{ live int }

func (e *capError) Error() string {
	return "terminal: session cap reached; close a terminal pane first"
}
