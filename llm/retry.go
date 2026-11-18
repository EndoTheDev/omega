package llm

import (
	"context"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"time"
)

// maxRetries returns the retry budget from OMEGA_MAX_RETRIES, defaulting to 3.
func maxRetries() int {
	if v := os.Getenv("OMEGA_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 3
}

// retryableStatus reports whether an HTTP status is worth retrying. Other 4xx are
// client errors and never retried.
func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// backoff returns the wait for a given attempt: 1s, 2s, 4s,. plus a small random jitter
// so concurrent callers don't stampede in lockstep.
func backoff(attempt int) time.Duration {
	base := time.Duration(1<<attempt) * time.Second
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	jitter := time.Duration(rand.Int64N(int64(base / 4)))
	return base + jitter
}

// MaxRetries reports the OMEGA_MAX_RETRIES budget: the same value retryHTTP uses.
// Stream-level retries that must react to a 200 OK response (a failure the HTTP layer
// cannot see) read this so one knob governs both layers.
func MaxRetries() int { return maxRetries() }

// loadBackoff returns the wait for a COLD-LOAD retry attempt n.
func loadBackoff(attempt int) time.Duration {
	base := 5 * time.Second
	for i := 0; i < attempt; i++ {
		base *= 3
		if base > 45*time.Second {
			base = 45 * time.Second
			break
		}
	}
	jitter := time.Duration(rand.Int64N(int64(base / 4)))
	return base + jitter
}

// Backoff returns the exponential backoff wait for retry attempt n, identical.
func Backoff(attempt int) time.Duration {
	return backoff(attempt)
}

// LoadBackoff returns the backoff wait for a cold-load ("load" verdict) stream retry.
func LoadBackoff(attempt int) time.Duration {
	return loadBackoff(attempt)
}

// retryHTTP runs req with exponential backoff on transient failures. The response body
// of each failed attempt is closed before retrying; the caller owns the final response
// body.
func retryHTTP(ctx context.Context, req *http.Request) (*http.Response, error) {
	max := maxRetries()
	var resp *http.Response
	var err error
	for attempt := 0; ; attempt++ {
		if attempt > 0 && req.GetBody != nil {
			if body, gerr := req.GetBody(); gerr == nil {
				req.Body = body
			}
		}
		resp, err = httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
		} else if !retryableStatus(resp.StatusCode) {
			return resp, nil
		}
		if attempt >= max {
			return resp, err
		}
		if resp != nil {
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff(attempt)):
		}
	}
}
