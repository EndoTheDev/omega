package agent_loop

import "testing"

func TestIsOverflowError(t *testing.T) {
	tests := []struct {
		name string
		err  string
		want bool
	}{
		// Known overflow phrases → true.
		{"context length", "context length exceeded", true},
		{"context_length", "context_length exceeded", true},
		{"too long", "input is too long", true},
		{"token limit", "token limit reached", true},
		{"maximum context", "maximum context window exceeded", true},

		// Exact phrases alone → true.
		{"exact context length", "context length", true},
		{"exact context_length", "context_length", true},
		{"exact too long", "too long", true},
		{"exact token limit", "token limit", true},
		{"exact maximum context", "maximum context", true},

		// Case insensitivity → true.
		{"capitalized", "Context Length", true},
		{"all caps", "CONTEXT LENGTH", true},
		{"mixed case too long", "Too Long", true},
		{"mixed case token limit", "Token LIMIT", true},
		{"mixed case maximum context", "Maximum Context", true},

		// Embedded in longer strings → true.
		{"embedded context length", "error: the context length is too long for this model", true},
		{"embedded context_length", "the context_length is too long", true},
		{"embedded too long", "your request is too long for the model", true},
		{"embedded token limit", "you have hit the token limit for this session", true},
		{"embedded maximum context", "this exceeds the maximum context allowed", true},

		// Non-overflow phrases → false.
		{"network error", "network error", false},
		{"timeout", "timeout", false},
		{"unauthorized", "unauthorized", false},
		{"empty string", "", false},
		{"not found", "model not found", false},
		{"rate limited", "rate limited, try again later", false},
		{"internal server error", "internal server error", false},

		// Near-miss phrases that should not match → false.
		{"context only", "context window", false},
		{"length only", "maximum length", false},
		{"long only", "long response", false},
		{"token only", "token count", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isOverflowError(tt.err); got != tt.want {
				t.Errorf("isOverflowError(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}