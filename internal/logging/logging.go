package logging

import (
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

// Config controls structured debug logger creation.
type Config struct {
	Enabled   bool
	Component string
	Format    string
	Writer    io.Writer
}

// NewDebugLogger returns a structured debug logger or nil when disabled.
func NewDebugLogger(enabled bool, component string) *slog.Logger {
	return NewLogger(Config{
		Enabled:   enabled,
		Component: component,
	})
}

// NewLogger creates a slog-backed logger using text or JSON output.
func NewLogger(cfg Config) *slog.Logger {
	if !cfg.Enabled {
		return nil
	}

	writer := cfg.Writer
	if writer == nil {
		writer = os.Stderr
	}

	format := strings.ToLower(strings.TrimSpace(cfg.Format))
	if format == "" {
		format = strings.ToLower(strings.TrimSpace(os.Getenv("COJIRA_DEBUG_FORMAT")))
	}

	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(writer, opts)
	} else {
		handler = slog.NewTextHandler(writer, opts)
	}

	logger := slog.New(handler)
	if strings.TrimSpace(cfg.Component) != "" {
		logger = logger.With("component", cfg.Component)
	}
	return logger
}

// SafeTarget returns a log-safe target string for an HTTP URL.
func SafeTarget(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	target := parsed.Host + parsed.Path
	if parsed.RawQuery != "" {
		target += "?" + parsed.RawQuery
	}
	return target
}
