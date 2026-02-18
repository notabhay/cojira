package httpclient

import (
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryConfig controls retry behavior for HTTP requests.
type RetryConfig struct {
	Retries           int
	BaseDelay         time.Duration
	MaxDelay          time.Duration
	MaxRetryAfter     time.Duration
	JitterRatio       float64
	RespectRetryAfter bool
	RetryExceptions   bool
	RetryStatuses     map[int]bool
	// Sleep overrides time.Sleep for testing. If nil, uses time.Sleep.
	Sleep func(time.Duration)
}

// DefaultRetryConfig returns a RetryConfig with sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		Retries:           5,
		BaseDelay:         500 * time.Millisecond,
		MaxDelay:          8 * time.Second,
		MaxRetryAfter:     300 * time.Second,
		JitterRatio:       0.1,
		RespectRetryAfter: true,
		RetryExceptions:   true,
		RetryStatuses: map[int]bool{
			429: true, 500: true, 502: true, 503: true, 504: true,
		},
	}
}

// OnRetryFunc is called before each retry sleep. Attempt is 1-based.
// StatusCode is 0 if the retry is due to an error (not an HTTP status).
type OnRetryFunc func(attempt int, delay time.Duration, statusCode int)

// RequestFunc makes an HTTP request and returns a response.
type RequestFunc func() (*http.Response, error)

func computeBackoff(attempt int, baseDelay, maxDelay time.Duration, jitterRatio float64) time.Duration {
	delay := float64(baseDelay) * math.Pow(2, float64(attempt))
	if delay > float64(maxDelay) {
		delay = float64(maxDelay)
	}
	if jitterRatio > 0 {
		jitter := delay * jitterRatio
		delay += (rand.Float64()*2 - 1) * jitter
		if delay < 0 {
			delay = 0
		}
	}
	return time.Duration(delay)
}

func parseRetryAfter(header string) (time.Duration, bool) {
	value := strings.TrimSpace(header)
	if value == "" {
		return 0, false
	}

	// Try as seconds (integer).
	if seconds, err := strconv.Atoi(value); err == nil {
		d := time.Duration(seconds) * time.Second
		if d < 0 {
			d = 0
		}
		return d, true
	}

	// Try as HTTP-date.
	t, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delta := time.Until(t)
	if delta < 0 {
		delta = 0
	}
	return delta, true
}

func (c *RetryConfig) sleep(d time.Duration) {
	if c.Sleep != nil {
		c.Sleep(d)
	} else {
		time.Sleep(d)
	}
}

// RequestWithRetry calls requestFn and retries on configured HTTP status codes.
// Returns the final response (even if still retryable after max retries).
// Does not raise for HTTP statuses; callers decide how to handle non-2xx.
func RequestWithRetry(requestFn RequestFunc, config RetryConfig, onRetry OnRetryFunc) (*http.Response, error) {
	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt <= config.Retries; attempt++ {
		resp, lastErr = requestFn()

		if lastErr != nil {
			if !config.RetryExceptions || attempt >= config.Retries {
				return nil, lastErr
			}

			delay := computeBackoff(attempt, config.BaseDelay, config.MaxDelay, config.JitterRatio)
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}
			if onRetry != nil {
				onRetry(attempt+1, delay, 0)
			}
			config.sleep(delay)
			continue
		}

		if !config.RetryStatuses[resp.StatusCode] {
			return resp, nil
		}

		if attempt >= config.Retries {
			return resp, nil
		}

		var delay time.Duration
		retryAfterUsed := false

		if config.RespectRetryAfter {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if d, ok := parseRetryAfter(ra); ok {
					delay = d
					if delay > config.MaxRetryAfter {
						delay = config.MaxRetryAfter
					}
					retryAfterUsed = true
				}
			}
		}

		if !retryAfterUsed {
			delay = computeBackoff(attempt, config.BaseDelay, config.MaxDelay, config.JitterRatio)
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}
		}

		if onRetry != nil {
			onRetry(attempt+1, delay, resp.StatusCode)
		}
		config.sleep(delay)
	}

	if resp != nil {
		return resp, nil
	}
	return nil, lastErr
}
