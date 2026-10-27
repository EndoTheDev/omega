package main

// cmdrun_session_test.go: the wake persistence flag (card t_72ecca67,
// M5). The spawn contract is `omega run --session <id>` - the parser
// is the whole engine-visible surface; the provider-bound persistence
// path rides the cron-proven TurnSink machinery.

import "testing"

func TestParseRunSession(t *testing.T) {
	id, rest, err := parseRunSession([]string{"--session", "canonical", "hello"})
	if err != nil || id != "canonical" || len(rest) != 1 || rest[0] != "hello" {
		t.Errorf("prefix form = %q %q %v", id, rest, err)
	}
	id, rest, err = parseRunSession([]string{"do", "a thing", "--session", "canonical"})
	if err != nil || id != "canonical" || rest[0] != "do" {
		t.Errorf("suffix form = %q %q %v", id, rest, err)
	}
	id, rest, err = parseRunSession([]string{"wake", "prompt"})
	if err != nil || id != "" || len(rest) != 2 {
		t.Errorf("absent form = %q %q %v", id, rest, err)
	}
	if _, _, err := parseRunSession([]string{"--session"}); err == nil {
		t.Errorf("dangling --session accepted")
	}
	if _, _, err := parseRunSession([]string{"--session", "   "}); err == nil {
		t.Errorf("blank --session value accepted")
	}
}
