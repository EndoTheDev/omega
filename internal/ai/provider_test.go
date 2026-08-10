package ai

import "testing"

func TestNewProvider(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		wantErr bool
	}{
		{"default ollama", "", false},
		{"explicit ollama", "ollama", false},
		{"openai", "openai", false},
		{"unknown", "grok", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewProvider(tc.typ, "model", "", "")
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p == nil {
				t.Fatal("expected non-nil provider")
			}
		})
	}
}
