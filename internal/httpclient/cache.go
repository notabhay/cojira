package httpclient

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const cacheHeaderName = "X-Cojira-Cache"

// CacheConfig controls the shared on-disk HTTP response cache.
type CacheConfig struct {
	Disabled bool
	TTL      time.Duration
	Dir      string
	MaxEntries int
}

type cacheEntry struct {
	StatusCode   int         `json:"status_code"`
	Header       http.Header `json:"header"`
	Body         []byte      `json:"body"`
	StoredAtUnix int64       `json:"stored_at_unix"`
}

// DefaultCacheConfig returns the standard cache settings for read requests.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		TTL:        5 * time.Minute,
		Dir:        defaultCacheDir(),
		MaxEntries: defaultCacheMaxEntries(),
	}
}

func defaultCacheMaxEntries() int {
	value := strings.TrimSpace(os.Getenv("COJIRA_HTTP_CACHE_MAX_ENTRIES"))
	if value == "" {
		return 500
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 500
	}
	return parsed
}

func defaultCacheDir() string {
	if dir := strings.TrimSpace(os.Getenv("COJIRA_HTTP_CACHE_DIR")); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); dir != "" {
		return filepath.Join(dir, "cojira", "http")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".cache", "cojira", "http")
	}
	return filepath.Join(home, ".cache", "cojira", "http")
}

func normalizeCacheConfig(cfg CacheConfig) CacheConfig {
	defaults := DefaultCacheConfig()
	if cfg.TTL <= 0 {
		cfg.TTL = defaults.TTL
	}
	if strings.TrimSpace(cfg.Dir) == "" {
		cfg.Dir = defaults.Dir
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = defaults.MaxEntries
	}
	return cfg
}

func isCacheableMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead:
		return true
	default:
		return false
	}
}

func cachePath(dir, method, requestURL, varyKey string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(method) + "\n" + requestURL + "\n" + varyKey))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
}

func readCacheEntry(path string) (*cacheEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	if entry.Header == nil {
		entry.Header = http.Header{}
	}
	return &entry, nil
}

func writeCacheEntry(path string, entry cacheEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func pruneCacheEntries(dir string, ttl time.Duration, maxEntries int) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(paths) == 0 {
		return
	}
	type cacheFile struct {
		path     string
		storedAt int64
	}
	files := make([]cacheFile, 0, len(paths))
	now := time.Now()
	for _, path := range paths {
		entry, err := readCacheEntry(path)
		if err != nil {
			_ = os.Remove(path)
			continue
		}
		if ttl > 0 && now.Sub(time.Unix(entry.StoredAtUnix, 0)) >= ttl {
			_ = os.Remove(path)
			continue
		}
		files = append(files, cacheFile{path: path, storedAt: entry.StoredAtUnix})
	}
	if maxEntries <= 0 || len(files) <= maxEntries {
		return
	}
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].storedAt > files[j].storedAt
	})
	for _, item := range files[maxEntries:] {
		_ = os.Remove(item.path)
	}
}

func cloneHeader(header http.Header) http.Header {
	if header == nil {
		return http.Header{}
	}
	cloned := make(http.Header, len(header))
	for key, values := range header {
		copied := make([]string, len(values))
		copy(copied, values)
		cloned[key] = copied
	}
	return cloned
}

func responseFromCacheEntry(entry cacheEntry, cacheStatus string) *http.Response {
	header := cloneHeader(entry.Header)
	header.Set(cacheHeaderName, cacheStatus)
	return &http.Response{
		StatusCode: entry.StatusCode,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(entry.Body)),
	}
}

func shouldStoreResponse(resp *http.Response) bool {
	return resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300
}

func responseToCacheEntry(resp *http.Response) (cacheEntry, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return cacheEntry{}, err
	}
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return cacheEntry{
		StatusCode:   resp.StatusCode,
		Header:       cloneHeader(resp.Header),
		Body:         body,
		StoredAtUnix: time.Now().Unix(),
	}, nil
}

func isFresh(entry cacheEntry, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	return time.Since(time.Unix(entry.StoredAtUnix, 0)) < ttl
}

// RequestWithCache wraps a GET/HEAD request with a short-lived file-backed cache.
// The request function receives conditional headers (for revalidation) when a stale
// entry exists and should merge them into the outgoing request.
func RequestWithCache(method, requestURL, varyKey string, cfg CacheConfig, requestFn func(extraHeaders http.Header) (*http.Response, error)) (*http.Response, error) {
	cfg = normalizeCacheConfig(cfg)
	if cfg.Disabled || !isCacheableMethod(method) {
		resp, err := requestFn(nil)
		if resp != nil {
			resp.Header.Set(cacheHeaderName, "bypass")
		}
		return resp, err
	}

	path := cachePath(cfg.Dir, method, requestURL, varyKey)
	entry, err := readCacheEntry(path)
	if err == nil && isFresh(*entry, cfg.TTL) {
		return responseFromCacheEntry(*entry, "hit"), nil
	}

	conditionalHeaders := http.Header{}
	if err == nil {
		if etag := strings.TrimSpace(entry.Header.Get("ETag")); etag != "" {
			conditionalHeaders.Set("If-None-Match", etag)
		}
		if lastModified := strings.TrimSpace(entry.Header.Get("Last-Modified")); lastModified != "" {
			conditionalHeaders.Set("If-Modified-Since", lastModified)
		}
	}

	resp, reqErr := requestFn(conditionalHeaders)
	if reqErr != nil {
		return nil, reqErr
	}

	if resp.StatusCode == http.StatusNotModified && err == nil {
		revalidated := *entry
		revalidated.StoredAtUnix = time.Now().Unix()
		for key, values := range resp.Header {
			copied := make([]string, len(values))
			copy(copied, values)
			revalidated.Header[key] = copied
		}
		_ = writeCacheEntry(path, revalidated)
		return responseFromCacheEntry(revalidated, "revalidated"), nil
	}

	if shouldStoreResponse(resp) {
		entry, cacheErr := responseToCacheEntry(resp)
		if cacheErr == nil {
			_ = writeCacheEntry(path, entry)
			pruneCacheEntries(cfg.Dir, cfg.TTL, cfg.MaxEntries)
			resp.Header.Set(cacheHeaderName, "miss")
		}
	}
	return resp, nil
}
