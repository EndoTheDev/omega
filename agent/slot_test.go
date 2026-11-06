package agent

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// The per-subsystem model slot (card t_fc0b2f2d): the grammar and the
// ONE resolution rule - a colon prefix naming a CONFIGURED instance
// (or "default") routes the consult to that instance, anything else is
// the whole slot as a bare model on the receiver.

func TestParseModelSlot(t *testing.T) {
	cases := []struct {
		slot     string
		instance string
		model    string
	}{
		{"", "", ""},
		{"  ", "", ""},
		{"big:gemma3:27b", "big", "gemma3:27b"},
		{"default:gemma3:27b", "default", "gemma3:27b"},
		{"big:", "big", ""},             // the instance's own default model
		{"gemma3:27b", "gemma3", "27b"}, // colon = the split; a miss on the
		// instance lookup stays bare (below)
		{"llama3.2:70b", "llama3.2", "70b"}, // dot ids split too; lookup decides
		{"gemma3", "", "gemma3"},            // no colon = a bare model name
		{":gemma3:27b", "", ":gemma3:27b"},  // empty prefix is not a route
	}
	for _, c := range cases {
		inst, model := ParseModelSlot(c.slot)
		if inst != c.instance || model != c.model {
			t.Errorf("ParseModelSlot(%q) = (%q, %q), want (%q, %q)",
				c.slot, inst, model, c.instance, c.model)
		}
	}
}

func streamScript(content string) []core.ProviderStreamEvent {
	return []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response_chunk", Content: content},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
}

// ResolveModelSlot routes a configured instance prefix to the instance
// provider and keeps the model part; a bare slot (or an unknown
// prefix) stays on the receiver.
func TestResolveModelSlotKnownInstances(t *testing.T) {
	base := NewFakeProvider("main", streamScript("base")...)
	big := NewFakeProvider("big-model", streamScript("big")...)
	small := NewFakeProvider("small-model", streamScript("small")...)
	base.SetInstances(map[string]Provider{"big": big, "small": small})

	cases := []struct {
		slot        string
		wantModel   string
		wantContent string
	}{
		{"", "", "base"},
		{"gemma3:27b", "gemma3:27b", "base"},         // bare tag stays
		{"big:gemma3:27b", "gemma3:27b", "big"},      // instance + its model
		{"big:", "", "big"},                          // the instance's own default model
		{"default:gemma3:27b", "gemma3:27b", "base"}, // the reserved default
		{"unknown:m", "unknown:m", "base"},           // unconfigured prefix = bare
	}
	for _, c := range cases {
		prov, model := ResolveModelSlot(base, c.slot)
		if model != c.wantModel {
			t.Errorf("ResolveModelSlot(%q) model = %q, want %q", c.slot, model, c.wantModel)
		}
		if got := drainFake(t, prov); got != c.wantContent {
			t.Errorf("ResolveModelSlot(%q) routed wrong provider: %q", c.slot, got)
		}
	}
}

// A provider without the named-instances upgrade takes every slot as a
// bare model.
func TestResolveModelSlotNoInstances(t *testing.T) {
	prov := NewFakeProvider("main", streamScript("base")...)
	out, model := ResolveModelSlot(prov, "big:gemma3:27b")
	if model != "big:gemma3:27b" {
		t.Errorf("model = %q", model)
	}
	if got := drainFake(t, out); got != "base" {
		t.Errorf("routed wrong provider: %q", got)
	}
}

func drainFake(t *testing.T, prov Provider) string {
	t.Helper()
	var b string
	for ev := range prov.Stream(context.Background(),
		[]core.Message{core.NewUser("x")}, nil) {
		if chunk, ok := ev.(core.ResponseChunk); ok {
			b += chunk.Content
		}
	}
	return b
}
