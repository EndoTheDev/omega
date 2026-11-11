package store

// The store health probe (card t_5e2f3095): agent.StorePinger's Ping is
// what the /health body's database fact rides. It must answer on a live
// pool and must NOT answer healthy on a closed one - a wedged or closed
// database has to paint the row unhealthy, or the row is decoration.

import (
	"context"
	"testing"
	"time"
)

func TestPingHealthyStore(t *testing.T) {
	s, err := Open(t.TempDir() + "/ping.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("ping a live store: %v", err)
	}
}

// TestPingClosedStore: a closed handle answers with an error, never nil
// - the popover's "database" row is only worth showing when a dead
// database can redden it.
func TestPingClosedStore(t *testing.T) {
	s, err := Open(t.TempDir() + "/ping-closed.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Ping(ctx); err == nil {
		t.Fatal("a closed store pinged healthy")
	}
}
