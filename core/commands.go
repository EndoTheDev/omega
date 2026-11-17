package core

// The command chain's contract (v0.14 chain law): modules register
// commands through api.RegisterCommand, which wraps each handler around
// the previous one. A handler that does not own the name hands the call
// down the chain; a handler that DOES own it answers - or refuses with
// its own message. Before the miss sentinel those two were the same
// thing (any error fell through), so an owner's honest refusal
// ("no active session to export") surfaced to the user as a lie:
// "no handler for /export".
//
// ErrCommandMiss is the one error that means "not mine". Every other
// error stops the chain and surfaces as itself.

import (
	"context"
	"errors"
)

// ErrCommandMiss is the chain's miss sentinel. A handler returns it
// (through CommandMiss) for a name it does not own; the chain-on-miss
// wrapper in agent.RegisterCommand is the only caller that inspects it
// and the only error it falls through on.
var ErrCommandMiss = errors.New("command miss")

// commandMiss is the miss answer: its text preserves the wording every
// surface has printed since the chain existed ("no handler for /name"),
// while errors.Is reaches the sentinel through Unwrap.
type commandMiss struct{ name string }

func (m commandMiss) Error() string { return "no handler for " + m.name }

func (m commandMiss) Unwrap() error { return ErrCommandMiss }

// CommandMiss is a handler's "not mine" answer for name. It is the only
// error the command chain falls through on; a handler that owns the
// name must return its own error instead, or answer.
func CommandMiss(name string) error { return commandMiss{name: name} }

// commandSessionKey carries the invoking surface's session id to a
// command handler. It is unexported so the only ways in and out are the
// two functions below.
type commandSessionKey struct{}

// WithCommandSession binds the invoking surface's session id to a
// command context: the palette sends the tab's session on the dispatch
// wire, so a session-scoped command (/export, /label, /branch) acts on
// it without the id riding in args.
func WithCommandSession(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, commandSessionKey{}, sessionID)
}

// CommandSession is the invoking surface's session id, or "" when the
// caller bound none (the terminal loop, which resolves the session from
// args the way it always has).
func CommandSession(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(commandSessionKey{}).(string)
	return id
}
