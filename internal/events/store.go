package events

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultEventTTL = 24 * time.Hour

var appendMu sync.Mutex

// DefaultDir returns the default directory for persisted event streams.
func DefaultDir() string {
	if dir := strings.TrimSpace(os.Getenv("COJIRA_EVENT_DIR")); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); dir != "" {
		return filepath.Join(dir, "cojira", "events")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".cache", "cojira", "events")
	}
	return filepath.Join(home, ".cache", "cojira", "events")
}

// FilePath returns the path for a given event stream id.
func FilePath(streamID string) string {
	return filepath.Join(DefaultDir(), strings.TrimSpace(streamID)+".ndjson")
}

// Append appends a single JSON event line to the stream file and returns the
// file path used.
func Append(streamID string, payload map[string]any) (string, error) {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return "", errors.New("stream id is required")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	path := FilePath(streamID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	prune(DefaultDir(), defaultEventTTL)

	appendMu.Lock()
	defer appendMu.Unlock()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if err := enc.Encode(payload); err != nil {
		return "", err
	}
	return path, nil
}

// LatestStreamID returns the most recently modified stream id.
func LatestStreamID() (string, error) {
	prune(DefaultDir(), defaultEventTTL)
	paths, err := filepath.Glob(filepath.Join(DefaultDir(), "*.ndjson"))
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", os.ErrNotExist
	}
	sort.Slice(paths, func(i, j int) bool {
		ii, errI := os.Stat(paths[i])
		jj, errJ := os.Stat(paths[j])
		if errI != nil || errJ != nil {
			return paths[i] > paths[j]
		}
		if ii.ModTime().Equal(jj.ModTime()) {
			return paths[i] > paths[j]
		}
		return ii.ModTime().After(jj.ModTime())
	})
	base := filepath.Base(paths[0])
	return strings.TrimSuffix(base, filepath.Ext(base)), nil
}

// ReadAll returns every non-empty line from the stream file.
func ReadAll(streamID string) ([]string, error) {
	path := FilePath(streamID)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func prune(dir string, ttl time.Duration) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.ndjson"))
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-ttl)
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(path)
		}
	}
}
