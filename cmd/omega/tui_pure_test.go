package main

import (
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
)

// msgEqual compares two ai.Message values by type and content field.
// Interface types with slices can't use ==/!=, so we compare structurally.
func msgEqual(a, b ai.Message) bool {
	switch va := a.(type) {
	case ai.User:
		vb, ok := b.(ai.User)
		return ok && va.Content == vb.Content
	case ai.Assistant:
		vb, ok := b.(ai.Assistant)
		return ok && va.Content == vb.Content
	case ai.System:
		vb, ok := b.(ai.System)
		return ok && va.Content == vb.Content
	case ai.ToolResult:
		vb, ok := b.(ai.ToolResult)
		return ok && va.Content == vb.Content
	default:
		return false
	}
}

func TestSummarizeForBranch(t *testing.T) {
	t.Run("keepFirstPlusKeepLastGeLength returns unchanged", func(t *testing.T) {
		msgs := []ai.Message{ai.NewUser("a"), ai.NewUser("b"), ai.NewUser("c")}
		got := summarizeForBranch(msgs, 1, 2)
		if len(got) != len(msgs) {
			t.Fatalf("len = %d, want %d (unchanged)", len(got), len(msgs))
		}
		for i := range got {
			if !msgEqual(got[i], msgs[i]) {
				t.Fatalf("msg %d differs, want unchanged", i)
			}
		}
	})

	t.Run("normal inserts summary between head and tail", func(t *testing.T) {
		msgs := make([]ai.Message, 10)
		for i := range msgs {
			msgs[i] = ai.NewUser("m" + string(rune('a'+i)))
		}
		got := summarizeForBranch(msgs, 2, 3)
		if want := 2 + 1 + 3; len(got) != want {
			t.Fatalf("len = %d, want %d", len(got), want)
		}
		// First two are the original head.
		for i := 0; i < 2; i++ {
			if !msgEqual(got[i], msgs[i]) {
				t.Fatalf("head msg %d differs", i)
			}
		}
		// Middle is a synthetic system message.
		sys, ok := got[2].(ai.System)
		if !ok {
			t.Fatalf("summary msg = %T, want ai.System", got[2])
		}
		if !strings.Contains(sys.Content, "omitted") {
			t.Fatalf("summary content = %q, want it to mention omitted", sys.Content)
		}
		if !strings.Contains(sys.Content, "5 messages omitted") {
			t.Fatalf("summary content = %q, want '5 messages omitted'", sys.Content)
		}
		// Last three are the original tail.
		for i := 0; i < 3; i++ {
			if !msgEqual(got[3+i], msgs[len(msgs)-3+i]) {
				t.Fatalf("tail msg %d differs", i)
			}
		}
	})

	t.Run("empty slice returns empty", func(t *testing.T) {
		got := summarizeForBranch(nil, 2, 3)
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})

	t.Run("single message returns unchanged", func(t *testing.T) {
		msgs := []ai.Message{ai.NewUser("only")}
		got := summarizeForBranch(msgs, 2, 3)
		if len(got) != 1 || !msgEqual(got[0], msgs[0]) {
			t.Fatalf("got = %v, want unchanged single message", got)
		}
	})
}

func TestTruncate(t *testing.T) {
	t.Run("short string unchanged", func(t *testing.T) {
		if got := truncate("hi", 10); got != "hi" {
			t.Fatalf("truncate(\"hi\", 10) = %q, want \"hi\"", got)
		}
	})

	t.Run("exact length unchanged", func(t *testing.T) {
		s := "hello"
		if got := truncate(s, len(s)); got != s {
			t.Fatalf("truncate(%q, %d) = %q, want %q", s, len(s), got, s)
		}
	})

	t.Run("long string truncated with ellipsis", func(t *testing.T) {
		s := "hello world"
		got := truncate(s, 5)
		want := "hello..."
		if got != want {
			t.Fatalf("truncate(%q, 5) = %q, want %q", s, got, want)
		}
	})

	t.Run("empty string unchanged", func(t *testing.T) {
		if got := truncate("", 5); got != "" {
			t.Fatalf("truncate(\"\", 5) = %q, want \"\"", got)
		}
	})
}

func TestFirstLineOfDesc(t *testing.T) {
	t.Run("normal returns first line", func(t *testing.T) {
		s := "Reads a file\nMore details\nEven more"
		if got := firstLineOfDesc(s); got != "Reads a file" {
			t.Fatalf("firstLineOfDesc(%q) = %q, want \"Reads a file\"", s, got)
		}
	})

	t.Run("leading blank lines skipped", func(t *testing.T) {
		s := "\n\n  \nFirst real line\nSecond"
		if got := firstLineOfDesc(s); got != "First real line" {
			t.Fatalf("firstLineOfDesc(%q) = %q, want \"First real line\"", s, got)
		}
	})

	t.Run("all blank returns original", func(t *testing.T) {
		s := "\n\n  \n\t"
		if got := firstLineOfDesc(s); got != s {
			t.Fatalf("firstLineOfDesc(%q) = %q, want original", s, got)
		}
	})

	t.Run("single line returns itself", func(t *testing.T) {
		s := "only line"
		if got := firstLineOfDesc(s); got != s {
			t.Fatalf("firstLineOfDesc(%q) = %q, want %q", s, got, s)
		}
	})
}