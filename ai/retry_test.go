package ai

import (
	"strconv"
	"testing"
	"time"
)

func TestRetryableStatus(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{429, true},
		{500, true},
		{503, true},
		{599, true},
		{200, false},
		{400, false},
		{404, false},
		{418, false},
	}

	for _, tc := range cases {
		t.Run("status_"+strconv.Itoa(tc.code), func(t *testing.T) {
			got := retryableStatus(tc.code)
			if got != tc.want {
				t.Errorf("retryableStatus(%d) = %v, want %v", tc.code, got, tc.want)
			}
		})
	}
}

func TestMaxRetries(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		if got := maxRetries(); got != 3 {
			t.Errorf("maxRetries() = %d, want 3", got)
		}
	})

	t.Run("set_5", func(t *testing.T) {
		t.Setenv("OMEGA_MAX_RETRIES", "5")
		if got := maxRetries(); got != 5 {
			t.Errorf("maxRetries() = %d, want 5", got)
		}
	})

	t.Run("set_0", func(t *testing.T) {
		t.Setenv("OMEGA_MAX_RETRIES", "0")
		if got := maxRetries(); got != 0 {
			t.Errorf("maxRetries() = %d, want 0", got)
		}
	})

	t.Run("negative_falls_back", func(t *testing.T) {
		t.Setenv("OMEGA_MAX_RETRIES", "-1")
		if got := maxRetries(); got != 3 {
			t.Errorf("maxRetries() = %d, want 3 (fallback)", got)
		}
	})

	t.Run("non_numeric_falls_back", func(t *testing.T) {
		t.Setenv("OMEGA_MAX_RETRIES", "abc")
		if got := maxRetries(); got != 3 {
			t.Errorf("maxRetries() = %d, want 3 (fallback)", got)
		}
	})
}

func TestBackoff(t *testing.T) {
	cases := []struct {
		attempt int
		base    time.Duration // minimum (no jitter)
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
	}

	for _, tc := range cases {
		t.Run("attempt_"+strconv.Itoa(tc.attempt), func(t *testing.T) {
			got := backoff(tc.attempt)
			// base <= got < base*1.25 (jitter is [0, base/4))
			if got < tc.base {
				t.Errorf("backoff(%d) = %v, want >= %v", tc.attempt, got, tc.base)
			}
			upper := tc.base + tc.base/4
			if got >= upper {
				t.Errorf("backoff(%d) = %v, want < %v (jitter cap)", tc.attempt, got, upper)
			}
		})
	}
}