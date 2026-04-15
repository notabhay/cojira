package errors

import "fmt"

// HintSetup returns the hint for missing setup.
func HintSetup() string {
	return "Use 'cojira bootstrap', then edit '.env' or '~/.config/cojira/credentials' manually. Do not paste tokens into chat."
}

// HintTimeout returns the hint for timeout errors.
// If timeoutS is nil the hint is generic; otherwise it includes the duration.
func HintTimeout(timeoutS *float64) string {
	if timeoutS == nil {
		return "Retry with --timeout 60 or check network connectivity."
	}
	return fmt.Sprintf("Request timed out after %gs. Retry with --timeout 60 or check network connectivity.", *timeoutS)
}

// HintIdentifier returns the hint for unresolved identifiers.
func HintIdentifier(formats string) string {
	return fmt.Sprintf("Valid formats: %s", formats)
}

// HintPermission returns the hint for permission errors.
func HintPermission() string {
	return "The token may lack required permissions. Verify token scopes/permissions and try again."
}

// HintBaseURL returns the hint for base URL issues.
func HintBaseURL() string {
	return "The base URL may be wrong. If Jira has a context path (e.g. /jira), include it in JIRA_BASE_URL."
}

// HintAuthMode returns the hint for auth mode issues.
func HintAuthMode() string {
	return "If you use a Personal Access Token, try removing JIRA_EMAIL or setting JIRA_AUTH_MODE=bearer."
}

// HintRateLimit returns the hint for rate limit errors.
func HintRateLimit() string {
	return "The server is rate limiting requests. Wait a bit and retry (or add a small delay between operations)."
}

// CoerceHTTPStatusHint returns an appropriate hint for the given HTTP status code,
// or empty string if no specific hint applies.
func CoerceHTTPStatusHint(statusCode int) string {
	switch statusCode {
	case 401, 403:
		return HintPermission()
	case 404:
		return HintBaseURL()
	case 429:
		return HintRateLimit()
	default:
		return ""
	}
}
