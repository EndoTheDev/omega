package core

// The command chain's contract (v0.14 chain law).

import (
	"context"
	"errors"
)

// ErrCommandMiss is the chain's miss sentinel. A handler returns it
// (through CommandMiss) for a name it does not own; the chain-on-miss
// wrapper in agent.RegisterCommand is the only caller that inspects it
// and the only error it falls through.
var ErrCommandMiss = errors.New("command miss")

// commandMiss is the miss answer.
type commandMiss struct{ name string }

func (m commandMiss) Error() string { return "no handler for " + m.name }

func (m commandMiss) Unwrap() error { return ErrCommandMiss }

// CommandMiss is a handler's "not mine" answer for name. It is the only
// error the command chain falls through on; a handler that owns the
// name must return its own error instead, or answer.
func CommandMiss(name string) error { return commandMiss{name: name} }

// commandSessionKey carries the invoking surface's session id to a command handler.
type commandSessionKey struct{}

// WithCommandSession binds the invoking surface's session id to a command context.
func WithCommandSession(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, commandSessionKey{}, sessionID)
}

// CommandSession is the invoking surface's session id, or "" when the caller bound.
func CommandSession(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(commandSessionKey{}).(string)
	return id
}
