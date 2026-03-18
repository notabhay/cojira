// Package cli provides shared CLI helpers for adding flags and normalising
// arguments across cojira subcommands (backed by cobra).
package cli

import (
	"os"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

var supportedOutputModes = map[string]bool{
	"human":   true,
	"json":    true,
	"summary": true,
	"auto":    true,
	"key":     true,
}

func resolveRequestedOutputMode(cmd *cobra.Command) string {
	mode, _ := cmd.Flags().GetString("output-mode")
	if mode == "" {
		mode = output.GetMode()
	}
	return mode
}

func resolveOutputMode(cmd *cobra.Command) string {
	mode := resolveRequestedOutputMode(cmd)
	if mode == "auto" {
		if output.IsTTY(int(os.Stdout.Fd())) {
			mode = "human"
		} else {
			mode = "json"
		}
	}
	return mode
}

// AddOutputFlags registers --output-mode (and optionally --quiet) on cmd.
func AddOutputFlags(cmd *cobra.Command, includeQuiet bool) {
	defaultMode := os.Getenv("COJIRA_OUTPUT_MODE")
	if defaultMode == "" {
		defaultMode = "human"
	}
	cmd.Flags().String("output-mode", defaultMode, "Output mode: human, json, summary, auto, key (default: human; key is command-specific)")
	if includeQuiet {
		cmd.Flags().Bool("quiet", false, "Suppress receipts/progress output (best-effort)")
	}
}

// AddHTTPRetryFlags registers --timeout, --retries, --retry-base-delay,
// --retry-max-delay, and --debug on cmd.
func AddHTTPRetryFlags(cmd *cobra.Command) {
	cmd.Flags().Float64("timeout", 30.0, "HTTP timeout in seconds (default: 30)")
	cmd.Flags().Int("retries", 5, "HTTP retries for 429/5xx (default: 5)")
	cmd.Flags().Float64("retry-base-delay", 0.5, "Base delay for exponential backoff in seconds (default: 0.5)")
	cmd.Flags().Float64("retry-max-delay", 8.0, "Max delay between retries in seconds (default: 8.0)")
	cmd.Flags().Bool("debug", false, "Enable debug logging to stderr")
}

// AddIdempotencyFlags registers --idempotency-key on cmd.
func AddIdempotencyFlags(cmd *cobra.Command) {
	cmd.Flags().String("idempotency-key", "", "Idempotency key for deduplication on retry. Skips if already completed.")
}

// ValidateOutputMode verifies that the requested output mode is supported.
func ValidateOutputMode(cmd *cobra.Command) error {
	mode := strings.TrimSpace(resolveRequestedOutputMode(cmd))
	if mode == "" {
		return nil
	}
	if supportedOutputModes[mode] {
		return nil
	}
	return &cerrors.CojiraError{
		Code:     cerrors.Unsupported,
		Message:  "Unsupported output mode: " + mode + ". Use one of: human, json, summary, auto, key.",
		ExitCode: 2,
	}
}

// SupportsKeyOutput reports whether the resolved leaf command supports key-mode output.
func SupportsKeyOutput(cmd *cobra.Command) bool {
	switch strings.TrimSpace(cmd.CommandPath()) {
	case "cojira jira create", "cojira jira clone", "cojira jira transition", "cojira jira bulk-transition", "create", "clone", "transition", "bulk-transition":
		return true
	default:
		return false
	}
}

// KeyModeUnsupportedError returns a consistent error for commands that do not support key-mode output.
func KeyModeUnsupportedError(cmd *cobra.Command) error {
	return &cerrors.CojiraError{
		Code:     cerrors.Unsupported,
		Message:  "--output-mode key is not supported by " + strings.TrimSpace(cmd.CommandPath()) + ". Supported commands: cojira jira create, cojira jira clone, cojira jira transition, cojira jira bulk-transition.",
		ExitCode: 2,
	}
}

// NormalizeOutputMode reads --output-mode from cmd, resolves "auto" to
// "human" or "json" depending on TTY, updates the global output mode,
// and returns the resolved mode string.
func NormalizeOutputMode(cmd *cobra.Command) string {
	mode := resolveOutputMode(cmd)
	output.SetMode(mode)
	return mode
}

// IsJSON returns true if the resolved output mode is "json".
func IsJSON(cmd *cobra.Command) bool {
	return resolveOutputMode(cmd) == "json"
}

// IsSummary returns true if the resolved output mode is "summary".
func IsSummary(cmd *cobra.Command) bool {
	return resolveOutputMode(cmd) == "summary"
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
	Timeout        float64
	Retries        int
	RetryBaseDelay float64
	RetryMaxDelay  float64
	Debug          bool
}

// BuildRetryConfig reads retry-related flags from cmd and returns a RetryConfig.
func BuildRetryConfig(cmd *cobra.Command) RetryConfig {
	timeout, _ := cmd.Flags().GetFloat64("timeout")
	retries, _ := cmd.Flags().GetInt("retries")
	baseDelay, _ := cmd.Flags().GetFloat64("retry-base-delay")
	maxDelay, _ := cmd.Flags().GetFloat64("retry-max-delay")
	debug, _ := cmd.Flags().GetBool("debug")
	return RetryConfig{
		Timeout:        timeout,
		Retries:        retries,
		RetryBaseDelay: baseDelay,
		RetryMaxDelay:  maxDelay,
		Debug:          debug,
	}
}
