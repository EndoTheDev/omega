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

// slotConfig builds a config carrying the default provider (main) and
// one named instance "big".
func slotConfig(slot string) Config {
	cfg := DefaultConfig()
	cfg.Provider.ModelName = "main-model"
	cfg.Provider.APIKey = "default-key"
	cfg.Providers = map[string]ProviderConfig{
		"big": {Type: "openai", ModelName: "big-model", Host: "http://big.internal", APIKey: "big-key"},
	}
	cfg.ApprovalsModel = slot
	return cfg
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

// An instance prefix with no model part serves the instance's own
// model.
func TestApprovalSlotInstanceDefaultModel(t *testing.T) {
	cfg := slotConfig("big:")
	host, _, model := approvalSlot(cfg)
	if model != "big-model" {
		t.Errorf("model = %q, want the instance's model", model)
	}
	if host != "http://big.internal" {
		t.Errorf("host = %q, want the instance host", host)
	}
}

// A bare slot (and an unconfigured prefix over it) keeps the default
// provider wiring and carries the whole slot as the model.
func TestApprovalSlotBareStays(t *testing.T) {
	cfg := slotConfig("gemma3:27b")
	host, key, model := approvalSlot(cfg)
	if model != "gemma3:27b" || host != cfg.Provider.Host || key != "default-key" {
		t.Errorf("bare slot: %q %q %q (want default wiring + a bare model)", host, key, model)
	}

	cfg = slotConfig("unknown:m")
	host, key, model = approvalSlot(cfg)
	if model != "unknown:m" || host != cfg.Provider.Host || key != "default-key" {
		t.Errorf("unconfigured prefix: %q %q %q (want default wiring + a bare model)", host, key, model)
	}
}

// The reserved "default" prefix stays on the default provider's own
// wiring.
func TestApprovalSlotReservedDefault(t *testing.T) {
	cfg := slotConfig("default:main-model")
	host, key, model := approvalSlot(cfg)
	if host != cfg.Provider.Host || key != "default-key" || model != "main-model" {
		t.Errorf("reserved default: %q %q %q", host, key, model)
	}
}

// An empty slot keeps today's fail-closed shape (no model).
func TestApprovalSlotEmpty(t *testing.T) {
	host, key, model := approvalSlot(slotConfig(""))
	if model != "" || host != DefaultConfig().Provider.Host || key != "default-key" {
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
		t.Errorf("approvals section lost its host wiring")
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
