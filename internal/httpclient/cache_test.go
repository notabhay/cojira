package httpclient

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestWithCacheFreshHit(t *testing.T) {
	cfg := CacheConfig{
		TTL: time.Hour,
		Dir: t.TempDir(),
	}

	callCount := 0
	requestFn := func(extraHeaders http.Header) (*http.Response, error) {
		callCount++
		require.Empty(t, extraHeaders.Get("If-None-Match"))
		header := http.Header{}
		header.Set("ETag", `"abc"`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	}

	resp1, err := RequestWithCache(http.MethodGet, "https://example.com/rest/api/2/issue/PROJ-1", "user-a", cfg, requestFn)
	require.NoError(t, err)
	body1, err := io.ReadAll(resp1.Body)
	require.NoError(t, err)
	require.NoError(t, resp1.Body.Close())
	assert.Equal(t, "miss", resp1.Header.Get(cacheHeaderName))

	resp2, err := RequestWithCache(http.MethodGet, "https://example.com/rest/api/2/issue/PROJ-1", "user-a", cfg, requestFn)
	require.NoError(t, err)
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	require.NoError(t, resp2.Body.Close())

	assert.Equal(t, 1, callCount)
	assert.Equal(t, `{"ok":true}`, string(body1))
	assert.Equal(t, `{"ok":true}`, string(body2))
	assert.Equal(t, "hit", resp2.Header.Get(cacheHeaderName))
}

func TestRequestWithCacheRevalidatesStaleEntry(t *testing.T) {
	cfg := CacheConfig{
		TTL: time.Minute,
		Dir: t.TempDir(),
	}

	callCount := 0
	requestFn := func(extraHeaders http.Header) (*http.Response, error) {
		callCount++
		if callCount == 1 {
			header := http.Header{}
			header.Set("ETag", `"abc"`)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader(`{"value":"cached"}`)),
			}, nil
		}
		assert.Equal(t, `"abc"`, extraHeaders.Get("If-None-Match"))
		header := http.Header{}
		header.Set("ETag", `"abc"`)
		return &http.Response{
			StatusCode: http.StatusNotModified,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}

	url := "https://example.com/rest/api/content/12345"
	varyKey := "user-b"

	resp1, err := RequestWithCache(http.MethodGet, url, varyKey, cfg, requestFn)
	require.NoError(t, err)
	_, err = io.ReadAll(resp1.Body)
	require.NoError(t, err)
	require.NoError(t, resp1.Body.Close())

	path := cachePath(cfg.Dir, http.MethodGet, url, varyKey)
	entry, err := readCacheEntry(path)
	require.NoError(t, err)
	assert.Equal(t, http.MethodGet, entry.Method)
	assert.Equal(t, url, entry.RequestURL)
	assert.Equal(t, varyKey, entry.VaryKey)
	entry.StoredAtUnix = time.Now().Add(-2 * time.Hour).Unix()
	require.NoError(t, writeCacheEntry(path, *entry))

	resp2, err := RequestWithCache(http.MethodGet, url, varyKey, cfg, requestFn)
	require.NoError(t, err)
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	require.NoError(t, resp2.Body.Close())

	assert.Equal(t, 2, callCount)
	assert.Equal(t, `{"value":"cached"}`, string(body2))
	assert.Equal(t, "revalidated", resp2.Header.Get(cacheHeaderName))
}

func TestRequestWithCacheInvalidatesVaryKeyOnWrite(t *testing.T) {
	cfg := CacheConfig{
		TTL: time.Hour,
		Dir: t.TempDir(),
	}
	url := "https://example.com/rest/api/2/issue/PROJ-1"
	varyKey := "user-d"

	getFn := func(extraHeaders http.Header) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	}
	resp, err := RequestWithCache(http.MethodGet, url, varyKey, cfg, getFn)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	entries, err := InspectCache(cfg, varyKey)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	writeFn := func(extraHeaders http.Header) (*http.Response, error) {
		require.Nil(t, extraHeaders)
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}
	resp, err = RequestWithCache(http.MethodPost, url, varyKey, cfg, writeFn)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, "bypass", resp.Header.Get(cacheHeaderName))

	entries, err = InspectCache(cfg, varyKey)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestRequestWithCacheBypassWhenDisabled(t *testing.T) {
	cfg := CacheConfig{
		Disabled: true,
		TTL:      time.Hour,
		Dir:      t.TempDir(),
	}

	callCount := 0
	requestFn := func(extraHeaders http.Header) (*http.Response, error) {
		callCount++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	}

	for i := 0; i < 2; i++ {
		resp, err := RequestWithCache(http.MethodGet, "https://example.com/rest/api/2/myself", "user-c", cfg, requestFn)
		require.NoError(t, err)
		_, err = io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, "bypass", resp.Header.Get(cacheHeaderName))
	}

	assert.Equal(t, 2, callCount)
}

func TestInspectCacheStatsAndClear(t *testing.T) {
	cfg := CacheConfig{
		TTL: time.Hour,
		Dir: t.TempDir(),
	}
	for i, varyKey := range []string{"user-a", "user-b"} {
		resp, err := RequestWithCache(http.MethodGet, "https://example.com/rest/api/2/issue/PROJ-"+string(rune('1'+i)), varyKey, cfg, func(extraHeaders http.Header) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		})
		require.NoError(t, err)
		_, err = io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}

	entries, err := InspectCache(cfg, "")
	require.NoError(t, err)
	require.Len(t, entries, 2)

	stats, err := CacheStatistics(cfg, "")
	require.NoError(t, err)
	assert.Equal(t, 2, stats.Entries)
	assert.Equal(t, 2, stats.UniqueVaryKey)
	assert.Greater(t, stats.Bytes, int64(0))

	removed, err := ClearCache(cfg, "user-a")
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	entries, err = InspectCache(cfg, "")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "user-b", entries[0].VaryKey)
}
