// Package cli provides shared CLI helpers for adding flags and normalising
// arguments across cojira subcommands (backed by cobra).
package cli

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/config"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// AddOutputFlags registers --output-mode (and optionally --quiet) on cmd.
func AddOutputFlags(cmd *cobra.Command, includeQuiet bool) {
	defaultMode := os.Getenv("COJIRA_OUTPUT_MODE")
	if defaultMode == "" {
		defaultMode = "human"
	}
	cmd.Flags().String("output-mode", defaultMode, "Output mode: human, json, ndjson, summary, auto (default: human)")
	defaultColor := os.Getenv("COJIRA_COLOR")
	if defaultColor == "" {
		defaultColor = "auto"
	}
	cmd.Flags().String("color", defaultColor, "Color mode: auto, always, never (default: auto)")
	if includeQuiet {
		cmd.Flags().Bool("quiet", false, "Suppress receipts/progress output (best-effort)")
	}
}

// AddHTTPRetryFlags registers --timeout, --retries, --retry-base-delay,
// --retry-max-delay, and --debug on cmd.
func AddHTTPRetryFlags(cmd *cobra.Command) {
	addHTTPRetryFlags(cmd.Flags())
}

// AddPersistentHTTPRetryFlags registers retry flags as inherited flags.
func AddPersistentHTTPRetryFlags(cmd *cobra.Command) {
	addHTTPRetryFlags(cmd.PersistentFlags())
}

// AddHTTPCacheFlags registers --no-cache and --cache-ttl on cmd.
func AddHTTPCacheFlags(cmd *cobra.Command) {
	addHTTPCacheFlags(cmd.Flags())
}

// AddPersistentHTTPCacheFlags registers cache flags as inherited flags.
func AddPersistentHTTPCacheFlags(cmd *cobra.Command) {
	addHTTPCacheFlags(cmd.PersistentFlags())
}

func addHTTPRetryFlags(flags *pflag.FlagSet) {
	flags.Float64("timeout", 30.0, "HTTP timeout in seconds (default: 30)")
	flags.Int("retries", 5, "HTTP retries for 429/5xx (default: 5)")
	flags.Float64("retry-base-delay", 0.5, "Base delay for exponential backoff in seconds (default: 0.5)")
	flags.Float64("retry-max-delay", 8.0, "Max delay between retries in seconds (default: 8.0)")
	flags.Float64("client-rate-limit", 0.0, "Proactive client-side request rate limit in requests/sec (0 disables)")
	flags.Int("client-burst", 4, "Client-side burst size when --client-rate-limit is enabled")
	flags.Bool("debug", false, "Enable debug logging to stderr")
}

func addHTTPCacheFlags(flags *pflag.FlagSet) {
	flags.Bool("no-cache", false, "Disable the shared HTTP cache for GET/HEAD requests")
	flags.Duration("cache-ttl", 5*time.Minute, "TTL for cached GET/HEAD responses (default: 5m)")
}

// AddIdempotencyFlags registers --idempotency-key on cmd.
func AddIdempotencyFlags(cmd *cobra.Command) {
	cmd.Flags().String("idempotency-key", "", "Idempotency key for deduplication on retry. Skips if already completed.")
}

// NormalizeOutputMode reads --output-mode from cmd, resolves "auto" to
// "human" or "json" depending on TTY, updates the global output mode,
// and returns the resolved mode string.
func NormalizeOutputMode(cmd *cobra.Command) string {
	mode, _ := cmd.Flags().GetString("output-mode")
	stream, _ := cmd.Flags().GetBool("stream")
	if stream {
		mode = "ndjson"
	}
	if mode == "auto" {
		if output.IsTTY(int(os.Stdout.Fd())) {
			mode = "human"
		} else {
			mode = "json"
		}
	}
	if mode != "human" && mode != "json" && mode != "ndjson" && mode != "summary" {
		mode = "human"
	}
	output.SetMode(mode)
	colorMode, _ := cmd.Flags().GetString("color")
	switch colorMode {
	case "always", "never", "auto":
		output.SetColorMode(colorMode)
	default:
		output.SetColorMode("auto")
	}
	selectExpr, _ := cmd.Flags().GetString("select")
	output.SetSelect(strings.TrimSpace(selectExpr))
	return mode
}

// IsJSON returns true if the resolved output mode is "json".
func IsJSON(cmd *cobra.Command) bool {
	mode, _ := cmd.Flags().GetString("output-mode")
	stream, _ := cmd.Flags().GetBool("stream")
	return mode == "json" || mode == "ndjson" || stream
}

// IsSummary returns true if the resolved output mode is "summary".
func IsSummary(cmd *cobra.Command) bool {
	mode, _ := cmd.Flags().GetString("output-mode")
	return mode == "summary"
}

// ApplyPlanFlag sets --dry-run and --diff to true when --plan is set.
func ApplyPlanFlag(cmd *cobra.Command) {
	plan, _ := cmd.Flags().GetBool("plan")
	if !plan {
		return
	}
	if cmd.Flags().Lookup("dry-run") != nil {
		_ = cmd.Flags().Set("dry-run", "true")
	}
	if cmd.Flags().Lookup("diff") != nil {
		_ = cmd.Flags().Set("diff", "true")
	}
}

// RetryConfig holds HTTP retry configuration.
// This is a local definition; the httpclient package may define its own
// once it's ported and this can be unified.
type RetryConfig struct {
	Context         context.Context
	Timeout         float64
	Retries         int
	RetryBaseDelay  float64
	RetryMaxDelay   float64
	Debug           bool
	NoCache         bool
	CacheTTL        time.Duration
	ClientRateLimit float64
	ClientBurst     int
}

// SelectedProfile resolves the profile from --profile, COJIRA_PROFILE, or the
// project config default_profile value.
func SelectedProfile(cmd *cobra.Command) (string, error) {
	requested := ""
	if cmd != nil {
		requested, _ = cmd.Flags().GetString("profile")
	}
	return config.ResolveProfileName(requested)
}

// ProfileEnvOverrides resolves the effective profile override map for the
// current command.
func ProfileEnvOverrides(cmd *cobra.Command) (map[string]string, string, error) {
	requested := ""
	if cmd != nil {
		requested, _ = cmd.Flags().GetString("profile")
	}
	return config.ProfileEnvOverrides(requested)
}

// BuildRetryConfig reads retry-related flags from cmd and returns a RetryConfig.
func BuildRetryConfig(cmd *cobra.Command) RetryConfig {
	timeout, _ := cmd.Flags().GetFloat64("timeout")
	retries, _ := cmd.Flags().GetInt("retries")
	baseDelay, _ := cmd.Flags().GetFloat64("retry-base-delay")
	maxDelay, _ := cmd.Flags().GetFloat64("retry-max-delay")
	debug, _ := cmd.Flags().GetBool("debug")
	noCache, _ := cmd.Flags().GetBool("no-cache")
	cacheTTL, _ := cmd.Flags().GetDuration("cache-ttl")
	clientRateLimit, _ := cmd.Flags().GetFloat64("client-rate-limit")
	clientBurst, _ := cmd.Flags().GetInt("client-burst")
	return RetryConfig{
		Context:         cmd.Context(),
		Timeout:         timeout,
		Retries:         retries,
		RetryBaseDelay:  baseDelay,
		RetryMaxDelay:   maxDelay,
		Debug:           debug,
		NoCache:         noCache,
		CacheTTL:        cacheTTL,
		ClientRateLimit: clientRateLimit,
		ClientBurst:     clientBurst,
	}
}
