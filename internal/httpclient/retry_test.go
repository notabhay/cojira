package httpclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noSleep(d time.Duration) {}

func TestRetriesOnStatusAndRespectsRetryAfter(t *testing.T) {
	var sleeps []time.Duration

	responses := []*http.Response{
		{StatusCode: 429, Header: http.Header{"Retry-After": {"2"}}},
		{StatusCode: 200, Header: http.Header{}},
	}
	idx := 0

	requestFn := func() (*http.Response, error) {
		r := responses[idx]
		idx++
		return r, nil
	}

	type retryCall struct {
		attempt int
		delay   time.Duration
		status  int
	}
	var calls []retryCall

	cfg := RetryConfig{
		Retries:           3,
		BaseDelay:         100 * time.Millisecond,
		MaxDelay:          10 * time.Second,
		MaxRetryAfter:     300 * time.Second,
		JitterRatio:       0.0,
		RespectRetryAfter: true,
		RetryExceptions:   true,
		RetryStatuses:     map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true},
		Sleep:             func(d time.Duration) { sleeps = append(sleeps, d) },
	}

	resp, err := RequestWithRetry(requestFn, cfg, func(attempt int, delay time.Duration, statusCode int) {
		calls = append(calls, retryCall{attempt, delay, statusCode})
	})

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, []time.Duration{2 * time.Second}, sleeps)
	require.Len(t, calls, 1)
	assert.Equal(t, 1, calls[0].attempt)
	assert.Equal(t, 2*time.Second, calls[0].delay)
	assert.Equal(t, 429, calls[0].status)
}

func TestRetryAfterIsCapped(t *testing.T) {
	var sleeps []time.Duration

	responses := []*http.Response{
		{StatusCode: 429, Header: http.Header{"Retry-After": {"1000"}}},
		{StatusCode: 200, Header: http.Header{}},
	}
	idx := 0

	cfg := RetryConfig{
		Retries:           1,
		BaseDelay:         100 * time.Millisecond,
		MaxDelay:          10 * time.Second,
		MaxRetryAfter:     300 * time.Second,
		JitterRatio:       0.0,
		RespectRetryAfter: true,
		RetryExceptions:   true,
		RetryStatuses:     map[int]bool{429: true},
		Sleep:             func(d time.Duration) { sleeps = append(sleeps, d) },
	}

	resp, err := RequestWithRetry(func() (*http.Response, error) {
		r := responses[idx]
		idx++
		return r, nil
	}, cfg, nil)

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, []time.Duration{300 * time.Second}, sleeps)
}

func TestRetriesExceptionsWhenEnabled(t *testing.T) {
	callCount := 0

	cfg := RetryConfig{
		Retries:         1,
		BaseDelay:       0,
		MaxDelay:        0,
		JitterRatio:     0.0,
		RetryExceptions: true,
		RetryStatuses:   map[int]bool{429: true},
		Sleep:           noSleep,
	}

	resp, err := RequestWithRetry(func() (*http.Response, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("boom")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}}, nil
	}, cfg, nil)

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 2, callCount)
}

func TestDoesNotRetryExceptionsWhenDisabled(t *testing.T) {
	cfg := RetryConfig{
		Retries:         3,
		RetryExceptions: false,
		RetryStatuses:   map[int]bool{429: true},
		Sleep:           noSleep,
	}

	_, err := RequestWithRetry(func() (*http.Response, error) {
		return nil, errors.New("boom")
	}, cfg, nil)

	require.Error(t, err)
	assert.Equal(t, "boom", err.Error())
}

func TestNoRetryOnSuccess(t *testing.T) {
	callCount := 0
	cfg := RetryConfig{
		Retries:       3,
		RetryStatuses: map[int]bool{429: true},
		Sleep:         noSleep,
	}

	resp, err := RequestWithRetry(func() (*http.Response, error) {
		callCount++
		return &http.Response{StatusCode: 200, Header: http.Header{}}, nil
	}, cfg, nil)

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 1, callCount)
}

func TestReturnsLastResponseAfterMaxRetries(t *testing.T) {
	cfg := RetryConfig{
		Retries:           2,
		BaseDelay:         0,
		MaxDelay:          0,
		JitterRatio:       0.0,
		RespectRetryAfter: false,
		RetryExceptions:   true,
		RetryStatuses:     map[int]bool{503: true},
		Sleep:             noSleep,
	}

	resp, err := RequestWithRetry(func() (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: http.Header{}}, nil
	}, cfg, nil)

	require.NoError(t, err)
	assert.Equal(t, 503, resp.StatusCode)
}

func TestExponentialBackoffWithoutJitter(t *testing.T) {
	var sleeps []time.Duration
	callCount := 0

	cfg := RetryConfig{
		Retries:           3,
		BaseDelay:         100 * time.Millisecond,
		MaxDelay:          10 * time.Second,
		JitterRatio:       0.0,
		RespectRetryAfter: false,
		RetryExceptions:   true,
		RetryStatuses:     map[int]bool{500: true},
		Sleep:             func(d time.Duration) { sleeps = append(sleeps, d) },
	}

	resp, err := RequestWithRetry(func() (*http.Response, error) {
		callCount++
		if callCount <= 3 {
			return &http.Response{StatusCode: 500, Header: http.Header{}}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}}, nil
	}, cfg, nil)

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	require.Len(t, sleeps, 3)
	// 100ms * 2^0 = 100ms, 100ms * 2^1 = 200ms, 100ms * 2^2 = 400ms
	assert.Equal(t, 100*time.Millisecond, sleeps[0])
	assert.Equal(t, 200*time.Millisecond, sleeps[1])
	assert.Equal(t, 400*time.Millisecond, sleeps[2])
}

func TestBackoffCappedByMaxDelay(t *testing.T) {
	var sleeps []time.Duration
	callCount := 0

	cfg := RetryConfig{
		Retries:           5,
		BaseDelay:         1 * time.Second,
		MaxDelay:          3 * time.Second,
		JitterRatio:       0.0,
		RespectRetryAfter: false,
		RetryStatuses:     map[int]bool{500: true},
		Sleep:             func(d time.Duration) { sleeps = append(sleeps, d) },
	}

	_, _ = RequestWithRetry(func() (*http.Response, error) {
		callCount++
		if callCount <= 5 {
			return &http.Response{StatusCode: 500, Header: http.Header{}}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}}, nil
	}, cfg, nil)

	// All sleeps should be <= 3s
	for _, s := range sleeps {
		assert.LessOrEqual(t, s, 3*time.Second)
	}
}

func TestOnRetryCallbackForExceptions(t *testing.T) {
	type retryCall struct {
		attempt int
		status  int
	}
	var calls []retryCall
	callCount := 0

	cfg := RetryConfig{
		Retries:         2,
		BaseDelay:       0,
		MaxDelay:        0,
		JitterRatio:     0.0,
		RetryExceptions: true,
		RetryStatuses:   map[int]bool{429: true},
		Sleep:           noSleep,
	}

	_, _ = RequestWithRetry(func() (*http.Response, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("net error")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}}, nil
	}, cfg, func(attempt int, delay time.Duration, statusCode int) {
		calls = append(calls, retryCall{attempt, statusCode})
	})

	require.Len(t, calls, 1)
	assert.Equal(t, 1, calls[0].attempt)
	assert.Equal(t, 0, calls[0].status) // 0 indicates exception, not HTTP status
}

type trackingReadCloser struct {
	closed bool
}

func (t *trackingReadCloser) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (t *trackingReadCloser) Close() error {
	t.closed = true
	return nil
}

func TestRetryableResponseBodyClosedBeforeRetry(t *testing.T) {
	body := &trackingReadCloser{}
	responses := []*http.Response{
		{StatusCode: 503, Header: http.Header{}, Body: body},
		{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))},
	}
	idx := 0

	resp, err := RequestWithRetry(func() (*http.Response, error) {
		r := responses[idx]
		idx++
		return r, nil
	}, RetryConfig{
		Retries:       1,
		RetryStatuses: map[int]bool{503: true},
		Sleep:         noSleep,
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.True(t, body.closed)
}
