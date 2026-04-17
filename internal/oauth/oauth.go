package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/dotenv"
	cerrors "github.com/notabhay/cojira/internal/errors"
)

const (
	defaultAtlassianTokenURL = "https://auth.atlassian.com/oauth/token"
)

var atlassianAccessibleResourcesURL = "https://api.atlassian.com/oauth/token/accessible-resources"

type ResolvedAuth struct {
	AccessToken string
	CloudID     string
	APIBaseURL  string
}

type refreshedToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	TokenType    string
}

// ResolveAtlassianOAuth2 resolves an OAuth 2 access token from environment
// variables and optionally discovers the Atlassian Cloud resource to use for
// API proxy requests.
func ResolveAtlassianOAuth2(ctx context.Context, product, baseURL, prefix string) (*ResolvedAuth, error) {
	return ResolveAtlassianOAuth2WithOverrides(ctx, product, baseURL, prefix, nil)
}

// ResolveAtlassianOAuth2WithOverrides resolves OAuth settings using explicit
// override values before falling back to environment variables.
func ResolveAtlassianOAuth2WithOverrides(ctx context.Context, product, baseURL, prefix string, overrides map[string]string) (*ResolvedAuth, error) {
	accessToken := firstEnvWithOverrides(overrides, prefix+"_OAUTH_ACCESS_TOKEN", "ATLASSIAN_OAUTH_ACCESS_TOKEN")
	refreshToken := firstEnvWithOverrides(overrides, prefix+"_OAUTH_REFRESH_TOKEN", "ATLASSIAN_OAUTH_REFRESH_TOKEN")
	clientID := firstEnvWithOverrides(overrides, prefix+"_OAUTH_CLIENT_ID", "ATLASSIAN_OAUTH_CLIENT_ID")
	clientSecret := firstEnvWithOverrides(overrides, prefix+"_OAUTH_CLIENT_SECRET", "ATLASSIAN_OAUTH_CLIENT_SECRET")
	tokenURL := firstEnvWithOverrides(overrides, prefix+"_OAUTH_TOKEN_URL", "ATLASSIAN_OAUTH_TOKEN_URL")
	if tokenURL == "" {
		tokenURL = defaultAtlassianTokenURL
	}
	cloudID := firstEnvWithOverrides(overrides, prefix+"_OAUTH_CLOUD_ID", "ATLASSIAN_OAUTH_CLOUD_ID")
	expiry := parseExpiry(firstEnvWithOverrides(overrides, prefix+"_OAUTH_EXPIRY", "ATLASSIAN_OAUTH_EXPIRY"))

	if strings.TrimSpace(accessToken) == "" && strings.TrimSpace(refreshToken) == "" {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.ConfigMissingEnv,
			Message:  fmt.Sprintf("%s OAuth 2 is enabled, but no access or refresh token was provided.", strings.ToUpper(product)),
			ExitCode: 2,
		}
	}

	if strings.TrimSpace(refreshToken) != "" && (strings.TrimSpace(accessToken) == "" || tokenExpired(expiry)) {
		if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.ConfigMissingEnv,
				Message:  fmt.Sprintf("%s OAuth 2 refresh requires client id and client secret.", strings.ToUpper(product)),
				ExitCode: 2,
			}
		}
		token, err := refreshAtlassianToken(ctx, tokenURL, clientID, clientSecret, refreshToken)
		if err != nil {
			return nil, err
		}
		accessToken = token.AccessToken
		if token.RefreshToken != "" {
			refreshToken = token.RefreshToken
		} else {
			token.RefreshToken = refreshToken
		}
		expiry = refreshedExpiry(token.ExpiresIn)
		persistOAuthValues(prefix, token, expiry)
	}

	if strings.TrimSpace(accessToken) == "" {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.ConfigMissingEnv,
			Message:  fmt.Sprintf("%s OAuth 2 did not yield an access token.", strings.ToUpper(product)),
			ExitCode: 2,
		}
	}

	resolved := &ResolvedAuth{AccessToken: accessToken, CloudID: cloudID}
	if resolved.CloudID == "" {
		resource, err := resolveAccessibleResource(ctx, product, baseURL, accessToken)
		if err == nil && resource != nil {
			resolved.CloudID = resource.ID
		}
	}
	if resolved.CloudID != "" {
		resolved.APIBaseURL = atlassianAPIBase(product, resolved.CloudID)
	}
	return resolved, nil
}

func firstEnvWithOverrides(overrides map[string]string, keys ...string) string {
	for _, key := range keys {
		if overrides != nil {
			if value := strings.TrimSpace(overrides[key]); value != "" {
				return value
			}
		}
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func refreshedExpiry(expiresIn int) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return time.Now().UTC().Add(time.Duration(expiresIn) * time.Second)
}

func persistOAuthValues(prefix string, token *refreshedToken, expiry time.Time) {
	if token == nil {
		return
	}
	_ = os.Setenv(prefix+"_OAUTH_ACCESS_TOKEN", token.AccessToken)
	if token.RefreshToken != "" {
		_ = os.Setenv(prefix+"_OAUTH_REFRESH_TOKEN", token.RefreshToken)
	}
	if !expiry.IsZero() {
		_ = os.Setenv(prefix+"_OAUTH_EXPIRY", expiry.Format(time.RFC3339))
	}

	target := strings.TrimSpace(os.Getenv("COJIRA_OAUTH_PERSIST_PATH"))
	if target == "" {
		target = dotenv.CredentialsPath()
	}
	if target == "" {
		return
	}

	existing := map[string]string{}
	if data, err := os.ReadFile(target); err == nil {
		existing = dotenv.ParseLines(string(data))
	}
	existing[prefix+"_OAUTH_ACCESS_TOKEN"] = token.AccessToken
	if token.RefreshToken != "" {
		existing[prefix+"_OAUTH_REFRESH_TOKEN"] = token.RefreshToken
	}
	if !expiry.IsZero() {
		existing[prefix+"_OAUTH_EXPIRY"] = expiry.Format(time.RFC3339)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return
	}
	keys := make([]string, 0, len(existing))
	for key := range existing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", key, existing[key]))
	}
	_ = os.WriteFile(target, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func refreshAtlassianToken(ctx context.Context, tokenURL, clientID, clientSecret, refreshToken string) (*refreshedToken, error) {
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     clientID,
		"client_secret": clientSecret,
		"refresh_token": refreshToken,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.HTTPError,
			Message:  fmt.Sprintf("OAuth 2 token refresh failed: %v", err),
			ExitCode: 1,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.HTTPError,
			Message:  fmt.Sprintf("OAuth 2 token refresh returned invalid JSON: %v", err),
			ExitCode: 1,
		}
	}
	if resp.StatusCode >= 400 {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.HTTPError,
			Message:  fmt.Sprintf("OAuth 2 token refresh failed: HTTP %d", resp.StatusCode),
			ExitCode: 1,
		}
	}

	accessToken, _ := data["access_token"].(string)
	refreshOut, _ := data["refresh_token"].(string)
	tokenType, _ := data["token_type"].(string)
	expiresIn := intFromAny(data["expires_in"])
	if strings.TrimSpace(accessToken) == "" {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.HTTPError,
			Message:  "OAuth 2 token refresh response did not include an access token.",
			ExitCode: 1,
		}
	}
	return &refreshedToken{
		AccessToken:  accessToken,
		RefreshToken: refreshOut,
		ExpiresIn:    expiresIn,
		TokenType:    tokenType,
	}, nil
}

type accessibleResource struct {
	ID     string   `json:"id"`
	URL    string   `json:"url"`
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

func resolveAccessibleResource(ctx context.Context, product, baseURL, accessToken string) (*accessibleResource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, atlassianAccessibleResourcesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("accessible-resources HTTP %d", resp.StatusCode)
	}

	var resources []accessibleResource
	if err := json.NewDecoder(resp.Body).Decode(&resources); err != nil {
		return nil, err
	}
	if len(resources) == 0 {
		return nil, nil
	}

	baseHost := parseHost(baseURL)
	for _, resource := range resources {
		if baseHost != "" && parseHost(resource.URL) == baseHost {
			return &resource, nil
		}
	}
	for _, resource := range resources {
		if resourceSupportsProduct(product, resource.Scopes) {
			return &resource, nil
		}
	}
	return &resources[0], nil
}

func resourceSupportsProduct(product string, scopes []string) bool {
	for _, scope := range scopes {
		normalized := strings.ToLower(strings.TrimSpace(scope))
		switch product {
		case "jira":
			if strings.Contains(normalized, "jira") {
				return true
			}
		case "confluence":
			if strings.Contains(normalized, "confluence") {
				return true
			}
		}
	}
	return false
}

func atlassianAPIBase(product, cloudID string) string {
	switch product {
	case "jira":
		return "https://api.atlassian.com/ex/jira/" + cloudID
	case "confluence":
		return "https://api.atlassian.com/ex/confluence/" + cloudID
	default:
		return ""
	}
}

func parseHost(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}

func parseExpiry(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts
	}
	return time.Time{}
}

func tokenExpired(expiry time.Time) bool {
	if expiry.IsZero() {
		return false
	}
	return time.Now().After(expiry.Add(-1 * time.Minute))
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	}
	return 0
}
