package main

import "testing"

// TestCmdTest verifies that the cmdTest smoke test runs the full agent
// pipeline against a fake provider and returns nil on success. cmdTest
// itself validates the event sequence internally, so a nil return means
// every event arrived in the expected order.
func TestCmdTest(t *testing.T) {
	if err := cmdTest(); err != nil {
		t.Fatalf("cmdTest() = %v, want nil", err)
	}
}