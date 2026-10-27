package ai

import (
	"context"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"time"
)

// maxRetries returns the retry budget from OMEGA_MAX_RETRIES, defaulting
// to 3. Non-numeric or negative values fall back to the default.
func maxRetries() int {
	if v := os.Getenv("OMEGA_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 3
}

// retryableStatus reports whether an HTTP status is worth retrying:
// 429 (rate limit) and any 5xx. Other 4xx are client errors and never
// retried.
func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// backoff returns the wait for a given attempt: 1s, 2s, 4s, ... plus a
// small random jitter so concurrent callers don't stampede in lockstep.
func backoff(attempt int) time.Duration {
	base := time.Duration(1<<attempt) * time.Second
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	jitter := time.Duration(rand.Int64N(int64(base / 4)))
	return base + jitter
}

// retryHTTP runs req with exponential backoff on transient failures:
// network errors, HTTP 429, and HTTP 5xx. Client errors (other 4xx) and
// context cancellation return immediately. Requests built from a
// bytes.Reader (all providers here) carry a GetBody that rewinds the
// payload for each attempt. The response body of each failed attempt is
// closed before retrying; the caller owns the final response body.
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
			// Context cancellation is not transient; stop.
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
