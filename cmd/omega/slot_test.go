package main

import (
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/approval"
	"github.com/EndoTheDev/omega/harness/skills"
	"github.com/EndoTheDev/omega/harness/store"
)

// The per-subsystem model slots (card t_fc0b2f2d): host-side routing
// tests - the compaction section carries the slot, and the approval
// decision slot resolves into the reviewer's host/key/model wiring.

// slotConfig builds a config carrying the primary provider (the row
// named "default", the agent's main endpoint) and one named instance
// "big". Every provider is a row (card t_bf21ee07): there is no second
// shape for the primary.
func slotConfig(slot string) Config {
	cfg := DefaultConfig()
	cfg.Provider.ModelName = "main-model"
	cfg.Provider.setInstance(ProviderInstance{
		Name: PrimaryProviderInstance, Type: "ollama", APIKey: "default-key",
	})
	cfg.Provider.setInstance(ProviderInstance{
		Name: "big", Type: "openai", Host: "http://big.internal", APIKey: "big-key",
	})
	cfg.ApprovalsModel = slot
	return cfg
}

// primaryHost is the primary row's endpoint - the wiring a bare slot
// rides.
func primaryHost(c Config) string {
	inst, _ := c.defaultInstance()
	return inst.Host
}

func TestApprovalSlotRoutesInstance(t *testing.T) {
	cfg := slotConfig("big:big-model")
	host, key, model := approvalSlot(cfg)
	if model != "big-model" {
		t.Errorf("model = %q, want big-model", model)
	}
	if host != "http://big.internal" {
		t.Errorf("host = %q, want the instance host", host)
	}
	if key != "big-key" {
		t.Errorf("key = %q, want the instance credential", key)
	}
}

// An instance prefix with no model part names an endpoint and no model:
// the consult fails closed (the gate escalates) rather than guessing a
// model the instance no longer carries.
func TestApprovalSlotInstanceWithoutModelFailsClosed(t *testing.T) {
	cfg := slotConfig("big:")
	host, key, model := approvalSlot(cfg)
	if model != "" {
		t.Errorf("model = %q, want the fail-closed empty model", model)
	}
	if host != "http://big.internal" || key != "big-key" {
		t.Errorf("host/key = %q %q, want the instance wiring", host, key)
	}
}

// A bare slot (and an unconfigured prefix over it) keeps the primary
// provider wiring and carries the whole slot as the model.
func TestApprovalSlotBareStays(t *testing.T) {
	cfg := slotConfig("gemma3:27b")
	host, key, model := approvalSlot(cfg)
	if model != "gemma3:27b" || host != primaryHost(cfg) || key != "default-key" {
		t.Errorf("bare slot: %q %q %q (want primary wiring + a bare model)", host, key, model)
	}

	cfg = slotConfig("unknown:m")
	host, key, model = approvalSlot(cfg)
	if model != "unknown:m" || host != primaryHost(cfg) || key != "default-key" {
		t.Errorf("unconfigured prefix: %q %q %q (want primary wiring + a bare model)", host, key, model)
	}
}

// The prefix naming the primary row resolves that row's wiring and the
// model part - there is no special "reserved" word, only the row name.
func TestApprovalSlotPrimaryPrefix(t *testing.T) {
	cfg := slotConfig("default:main-model")
	host, key, model := approvalSlot(cfg)
	if host != primaryHost(cfg) || key != "default-key" || model != "main-model" {
		t.Errorf("primary prefix: %q %q %q", host, key, model)
	}
}

// An empty slot keeps today's fail-closed shape (no model).
func TestApprovalSlotEmpty(t *testing.T) {
	host, key, model := approvalSlot(slotConfig(""))
	if model != "" || host != primaryHost(slotConfig("")) || key != "default-key" {
		t.Errorf("empty slot: %q %q %q", host, key, model)
	}
}

func TestCompactionSectionCarrier(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CompactionModel = "big:big-model"
	got := compactionSection(cfg)
	if got.Model != "big:big-model" {
		t.Errorf("compaction section model = %q, want the top-level slot", got.Model)
	}
	if got.Enabled != cfg.Compaction.Enabled {
		t.Error("compactionSection lost the compaction knobs")
	}
}

// The ConfigSection routes carry the slots: compaction rides the
// carrier field, approvals gets the resolved wiring, sessions and
// skills keep their capsule fields.
func TestConfigSectionSlots(t *testing.T) {
	cfg := slotConfig("big:big-model")
	cfg.CompactionModel = "big:big-model"
	cfg.SummarizerModel = "small-model"
	cfg.SkillsModel = "small-model"

	if sec, ok := cfg.ConfigSection("compactor"); !ok {
		t.Fatal("no compactor section")
	} else if cc := sec.(agent.CompactionConfig); cc.Model != "big:big-model" {
		t.Errorf("compactor section model = %q", cc.Model)
	}
	if sec, ok := cfg.ConfigSection("approvals"); !ok {
		t.Fatal("no approvals section")
	} else if ac := sec.(approval.Config); ac.HTTPHost == "" {
		t.Error("approvals section lost its host wiring")
	}
	if sec, ok := cfg.ConfigSection("sessions"); !ok {
		t.Fatal("no sessions section")
	} else if hc := sec.(store.HygieneConfig); hc.SummarizerModel != "small-model" {
		t.Errorf("sessions section summarizer slot = %q", hc.SummarizerModel)
	}
	if sec, ok := cfg.ConfigSection("skills"); !ok {
		t.Fatal("no skills section")
	} else if sc := sec.(skills.Config); sc.CuratorModel != "small-model" {
		t.Errorf("skills section curator slot = %q", sc.CuratorModel)
	}
}
