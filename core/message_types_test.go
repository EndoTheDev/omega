package core

import (
	"testing"
)

func TestNewSystem(t *testing.T) {
	m := NewSystem("you are helpful")
	if m.Content != "you are helpful" {
		t.Errorf("Content = %q, want %q", m.Content, "you are helpful")
	}
	if m.Timestamp == "" {
		t.Error("Timestamp is empty, want non-empty")
	}
}

func TestNewUser(t *testing.T) {
	m := NewUser("hello world")
	if m.Content != "hello world" {
		t.Errorf("Content = %q, want %q", m.Content, "hello world")
	}
	if m.Timestamp == "" {
		t.Error("Timestamp is empty, want non-empty")
	}
	if len(m.Images) != 0 {
		t.Errorf("Images = %v, want empty", m.Images)
	}
}

func TestNewAssistant(t *testing.T) {
	m := NewAssistant("response text")
	if m.Content != "response text" {
		t.Errorf("Content = %q, want %q", m.Content, "response text")
	}
	if m.Timestamp == "" {
		t.Error("Timestamp is empty, want non-empty")
	}
}

func TestNewToolResult(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		m := NewToolResult("output", "tc1", false)
		if m.Content != "output" {
			t.Errorf("Content = %q, want %q", m.Content, "output")
		}
		if m.ToolCallID != "tc1" {
			t.Errorf("ToolCallID = %q, want %q", m.ToolCallID, "tc1")
		}
		if m.IsError {
			t.Error("IsError = true, want false")
		}
		if m.Timestamp == "" {
			t.Error("Timestamp is empty, want non-empty")
		}
	})

	t.Run("error", func(t *testing.T) {
		m := NewToolResult("boom", "tc2", true)
		if m.Content != "boom" {
			t.Errorf("Content = %q, want %q", m.Content, "boom")
		}
		if m.ToolCallID != "tc2" {
			t.Errorf("ToolCallID = %q, want %q", m.ToolCallID, "tc2")
		}
		if !m.IsError {
			t.Error("IsError = false, want true")
		}
		if m.Timestamp == "" {
			t.Error("Timestamp is empty, want non-empty")
		}
	})
}

func TestNewModelChange(t *testing.T) {
	m := NewModelChange("glm-5.2")
	if m.Model != "glm-5.2" {
		t.Errorf("Model = %q, want %q", m.Model, "glm-5.2")
	}
	if m.Timestamp == "" {
		t.Error("Timestamp is empty, want non-empty")
	}
}

func TestNewThinkingLevelChange(t *testing.T) {
	m := NewThinkingLevelChange("high")
	if m.Level != "high" {
		t.Errorf("Level = %q, want %q", m.Level, "high")
	}
	if m.Timestamp == "" {
		t.Error("Timestamp is empty, want non-empty")
	}
}
