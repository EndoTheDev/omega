package llm

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
		{5, 30 * time.Second}, // capped from 32s
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

// TestLoadBackoff pins the cold-load retry pacing (t_163d1e87): a
// "load" verdict waits on LOAD-scale timescales - 5s/15s/45s - so a
// default 3-retry budget spans ~65s and outlasts a real upstream model
// load, unlike the 1s/2s/4s 429 pacing that ended the window before a
// cold remote model (~1 minute observed) ever finished loading.
func TestLoadBackoff(t *testing.T) {
	cases := []struct {
		attempt int
		base    time.Duration // minimum (no jitter)
	}{
		{0, 5 * time.Second},
		{1, 15 * time.Second},
		{2, 45 * time.Second},
		{3, 45 * time.Second}, // capped: a load either finishes or the budget dies
	}

	for _, tc := range cases {
		t.Run("attempt_"+strconv.Itoa(tc.attempt), func(t *testing.T) {
			got := loadBackoff(tc.attempt)
			if got < tc.base {
				t.Errorf("loadBackoff(%d) = %v, want >= %v", tc.attempt, got, tc.base)
			}
			upper := tc.base + tc.base/4
			if got >= upper {
				t.Errorf("loadBackoff(%d) = %v, want < %v (jitter cap)", tc.attempt, got, upper)
			}
		})
	}

	// the full default window: 3 retries wait at least 65s in total -
	// the observed cold-load latency fits inside the budget instead of
	// outliving it
	var total time.Duration
	for attempt := 0; attempt < 3; attempt++ {
		total += loadBackoff(attempt)
	}
	if total < 65*time.Second {
		t.Errorf("3-retry load window = %v, want >= 65s (outlast a ~1min cold load)", total)
	}
}
