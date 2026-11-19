package core

import (
	"context"
	"errors"
)

// ErrCommandMiss is the chain's miss sentinel. A handler returns it (through CommandMiss) for
// a name it does not own; the chain-on-miss wrapper in agent.RegisterCommand is the only
// caller that inspects it and the only error it falls through.
var ErrCommandMiss = errors.New("command miss")

type commandMiss struct{ name string }

func (m commandMiss) Error() string { return "no handler for " + m.name }

func (m commandMiss) Unwrap() error { return ErrCommandMiss }

// CommandMiss is a handler's "not mine" answer for name - the only error the command chain
// falls through on. A handler that owns the name must answer or return its own error.
func CommandMiss(name string) error { return commandMiss{name: name} }

type commandSessionKey struct{}

func WithCommandSession(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, commandSessionKey{}, sessionID)
}

func CommandSession(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(commandSessionKey{}).(string)
	return id
}
