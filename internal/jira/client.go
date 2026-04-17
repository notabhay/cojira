// Package jira provides the Jira REST API client.
package jira

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/dotenv"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/httpclient"
	"github.com/notabhay/cojira/internal/logging"
)

const (
	DefaultAPIVersion = "2"
	GreenhopperBase   = "/rest/greenhopper/1.0"
	AgileBase         = "/rest/agile/1.0"
	ServiceDeskBase   = "/rest/servicedeskapi"
)

// ClientConfig holds the parameters for creating a new JiraClient.
type ClientConfig struct {
	BaseURL     string
	APIBaseURL  string
	APIVersion  string
	Email       string
	Token       string
	AuthMode    string
	VerifySSL   bool
	UserAgent   string
	Context     context.Context
	Timeout     time.Duration
	RetryConfig httpclient.RetryConfig
	CacheConfig httpclient.CacheConfig
	Logger      *slog.Logger
	Debug       bool
}

// Client is a Jira REST API client.
type Client struct {
	baseURL      string
	apiBaseURL   string
	apiVersion   string
	restBase     string
	verifySSL    bool
	timeout      time.Duration
	ctx          context.Context
	retryConfig  httpclient.RetryConfig
	cacheConfig  httpclient.CacheConfig
	cacheVaryKey string
	logger       *slog.Logger
	debug        bool
	httpClient   *http.Client
	headers      http.Header
	auth         *basicAuth
}

type basicAuth struct {
	username string
	password string
}

// NewClient creates a new Jira REST API client.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, &cerrors.CojiraError{
			Code:        cerrors.ConfigMissingEnv,
			Message:     fmt.Sprintf("JIRA_BASE_URL is required. %s", cerrors.HintSetup()),
			Hint:        cerrors.HintSetup(),
			UserMessage: "I need your Jira URL in `.env` or `~/.config/cojira/credentials`. Please update the file directly instead of pasting it here.",
			Recovery:    map[string]any{"action": "edit", "path": ".env", "global_path": "~/.config/cojira/credentials", "requires_user": true},
			ExitCode:    2,
		}
	}
	if cfg.Token == "" {
		return nil, &cerrors.CojiraError{
			Code:        cerrors.ConfigMissingEnv,
			Message:     fmt.Sprintf("JIRA_API_TOKEN is required. %s", cerrors.HintSetup()),
			Hint:        cerrors.HintSetup(),
			UserMessage: "I need your Jira token in `.env` or `~/.config/cojira/credentials`. Please update the file directly instead of pasting it here.",
			Recovery:    map[string]any{"action": "edit", "path": ".env", "global_path": "~/.config/cojira/credentials", "requires_user": true},
			ExitCode:    2,
		}
	}

	if cfg.APIVersion == "" {
		cfg.APIVersion = DefaultAPIVersion
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUserAgent()
	}

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	apiBaseURL := strings.TrimRight(cfg.APIBaseURL, "/")
	if apiBaseURL == "" {
		apiBaseURL = baseURL
	}
	restBase := fmt.Sprintf("%s/rest/api/%s", apiBaseURL, cfg.APIVersion)

	headers := http.Header{
		"Accept":       {"application/json"},
		"Content-Type": {"application/json"},
		"User-Agent":   {cfg.UserAgent},
	}

	var auth *basicAuth
	effectiveEmail := cfg.Email
	if dotenv.IsPlaceholder(effectiveEmail, "email") {
		effectiveEmail = ""
	}

	mode := strings.TrimSpace(strings.ToLower(cfg.AuthMode))
	if mode == "bearer" || effectiveEmail == "" {
		headers.Set("Authorization", "Bearer "+cfg.Token)
	} else {
		auth = &basicAuth{username: effectiveEmail, password: cfg.Token}
	}

	return &Client{
		baseURL:      baseURL,
		apiBaseURL:   apiBaseURL,
		apiVersion:   cfg.APIVersion,
		restBase:     restBase,
		verifySSL:    cfg.VerifySSL,
		timeout:      cfg.Timeout,
		ctx:          cfg.Context,
		retryConfig:  cfg.RetryConfig,
		cacheConfig:  cfg.CacheConfig,
		cacheVaryKey: cacheVaryKey(headers.Get("Authorization"), effectiveEmail, cfg.Token, mode),
		logger:       cfg.Logger,
		debug:        cfg.Debug,
		httpClient:   buildHTTPClient(cfg.Timeout, cfg.VerifySSL),
		headers:      headers,
		auth:         auth,
	}, nil
}

// BaseURL returns the base URL of the Jira instance.
func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) formatError(resp *http.Response) string {
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) == 0 {
		return fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		text := strings.TrimSpace(string(body))
		if text == "" {
			return fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return text
	}

	var messages []string
	if errorMessages, ok := data["errorMessages"].([]any); ok {
		for _, m := range errorMessages {
			if s, ok := m.(string); ok {
				messages = append(messages, s)
			}
		}
	}
	if errorsMap, ok := data["errors"].(map[string]any); ok {
		for key, value := range errorsMap {
			if s, ok := value.(string); ok {
				messages = append(messages, fmt.Sprintf("%s: %s", key, s))
			}
		}
	}
	if msg, ok := data["message"].(string); ok {
		messages = append(messages, msg)
	}

	if len(messages) > 0 {
		return strings.Join(messages, "; ")
	}
	raw, _ := json.Marshal(data)
	return string(raw)
}

func (c *Client) onRetry(attempt int, delay time.Duration, statusCode int) {
	if c.logger == nil {
		return
	}
	c.logger.Debug("http.retry",
		"attempt", attempt,
		"max_retries", c.retryConfig.Retries,
		"delay_ms", delay.Milliseconds(),
		"status", statusCode,
	)
}

func cacheVaryKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func mergeQuery(requestURL string, params url.Values) string {
	if len(params) == 0 {
		return requestURL
	}
	return requestURL + "?" + params.Encode()
}

func (c *Client) doRequest(method, requestURL string, body []byte, params url.Values) (*http.Response, error) {
	return c.doRequestWithHeaders(method, mergeQuery(requestURL, params), body, nil)
}

func (c *Client) doRequestWithHeaders(method, requestURL string, body []byte, extraHeaders http.Header) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	target := logging.SafeTarget(requestURL)
	if c.logger != nil {
		c.logger.Debug("http.request",
			"method", method,
			"target", target,
			"has_body", body != nil,
			"conditional", extraHeaders.Get("If-None-Match") != "" || extraHeaders.Get("If-Modified-Since") != "",
		)
	}

	var req *http.Request
	var err error
	if c.ctx != nil {
		req, err = http.NewRequestWithContext(c.ctx, method, requestURL, bodyReader)
	} else {
		req, err = http.NewRequest(method, requestURL, bodyReader)
	}
	if err != nil {
		return nil, err
	}

	for key, values := range c.headers {
		for _, v := range values {
			req.Header.Set(key, v)
		}
	}
	for key, values := range extraHeaders {
		req.Header.Del(key)
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}

	if c.auth != nil {
		req.SetBasicAuth(c.auth.username, c.auth.password)
	}

	return c.httpClient.Do(req)
}

func (c *Client) handleResponse(resp *http.Response, method, path string) (*http.Response, error) {
	if resp.StatusCode >= 400 {
		message := c.formatError(resp)
		status := resp.StatusCode
		code := cerrors.HTTPError
		hint := ""
		switch status {
		case 401:
			code = cerrors.HTTP401
			hint = cerrors.HintPermission()
		case 403:
			code = cerrors.HTTP403
			hint = cerrors.HintPermission()
		case 429:
			code = cerrors.HTTP429
			hint = cerrors.HintRateLimit()
		}
		return nil, &cerrors.CojiraError{
			Code:     code,
			Message:  fmt.Sprintf("HTTP %d: %s", status, message),
			Hint:     hint,
			ExitCode: 1,
		}
	}
	return resp, nil
}

// Request makes an HTTP request to the Jira REST API (relative path).
func (c *Client) Request(method, path string, body []byte, params url.Values) (*http.Response, error) {
	requestURL := c.restBase + path
	return c.requestWithURL(method, requestURL, body, params)
}

// RequestURL makes an HTTP request to an absolute URL using the same auth/retry settings.
func (c *Client) RequestURL(method, requestURL string, body []byte, params url.Values) (*http.Response, error) {
	return c.requestWithURL(method, requestURL, body, params)
}

func (c *Client) requestWithURL(method, requestURL string, body []byte, params url.Values) (*http.Response, error) {
	method = strings.ToUpper(method)
	finalURL := mergeQuery(requestURL, params)
	target := logging.SafeTarget(finalURL)
	startedAt := time.Now()

	cfg := c.retryConfig
	if method != "GET" && method != "HEAD" {
		cfg.RetryExceptions = false
	}

	requestFn := func(extraHeaders http.Header) (*http.Response, error) {
		return c.doRequestWithHeaders(method, finalURL, body, extraHeaders)
	}

	resp, err := httpclient.RequestWithCache(method, finalURL, c.cacheVaryKey, c.cacheConfig, func(extraHeaders http.Header) (*http.Response, error) {
		return httpclient.RequestWithRetryURL(finalURL, func() (*http.Response, error) {
			return requestFn(extraHeaders)
		}, cfg, c.onRetry)
	})
	if err != nil {
		if c.logger != nil {
			c.logger.Debug("http.error",
				"method", method,
				"target", target,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"error", err.Error(),
			)
		}
		// Classify the error.
		if isTimeoutError(err) {
			timeout := c.timeout.Seconds()
			return nil, &cerrors.CojiraError{
				Code:     cerrors.Timeout,
				Message:  fmt.Sprintf("Request timed out: %s %s", method, path(finalURL)),
				Hint:     cerrors.HintTimeout(&timeout),
				ExitCode: 1,
			}
		}
		return nil, &cerrors.CojiraError{
			Code:     cerrors.HTTPError,
			Message:  fmt.Sprintf("Network error: %v", err),
			ExitCode: 1,
		}
	}

	if c.logger != nil {
		c.logger.Debug("http.response",
			"method", method,
			"target", target,
			"status", resp.StatusCode,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"cache", resp.Header.Get("X-Cojira-Cache"),
		)
	}

	return c.handleResponse(resp, method, requestURL)
}

func path(requestURL string) string {
	u, err := url.Parse(requestURL)
	if err != nil {
		return requestURL
	}
	return u.Path
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Timeout") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded")
}

// GetIssue fetches a Jira issue by key or ID.
func (c *Client) GetIssue(issue string, fields string, expand string) (map[string]any, error) {
	params := url.Values{}
	if fields != "" {
		params.Set("fields", fields)
	}
	if expand != "" {
		params.Set("expand", expand)
	}
	resp, err := c.Request("GET", "/issue/"+issue, nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// Search runs a JQL search query.
func (c *Client) Search(jql string, limit, startAt int, fields string, expand string) (map[string]any, error) {
	params := url.Values{}
	params.Set("jql", jql)
	params.Set("maxResults", fmt.Sprintf("%d", limit))
	params.Set("startAt", fmt.Sprintf("%d", startAt))
	if fields != "" {
		params.Set("fields", fields)
	}
	if expand != "" {
		params.Set("expand", expand)
	}
	resp, err := c.Request("GET", "/search", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ValidateJQL parses a JQL query using Jira's validation endpoint.
func (c *Client) ValidateJQL(jql string) (map[string]any, error) {
	body, err := json.Marshal(map[string]any{
		"queries":         []string{jql},
		"validation":      "strict",
		"failFast":        false,
		"includeWarnings": true,
	})
	if err != nil {
		return nil, err
	}
	resp, err := c.Request("POST", "/jql/parse", body, nil)
	if err != nil {
		var ce *cerrors.CojiraError
		if errors.As(err, &ce) && (ce.Code == cerrors.HTTP404 || (ce.Code == cerrors.HTTPError && strings.Contains(ce.Message, "HTTP 404"))) {
			return c.validateJQLViaSearch(jql)
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

func (c *Client) validateJQLViaSearch(jql string) (map[string]any, error) {
	params := url.Values{}
	params.Set("jql", jql)
	params.Set("maxResults", "0")
	params.Set("validateQuery", "strict")
	resp, err := c.Request("GET", "/search", nil, params)
	if err != nil {
		var ce *cerrors.CojiraError
		if errors.As(err, &ce) && ce.Code == cerrors.HTTPError && strings.Contains(ce.Message, "HTTP 400") {
			return map[string]any{
				"queries": []map[string]any{{
					"query":    jql,
					"errors":   []string{ce.Message},
					"warnings": []string{"Used search validation fallback because /jql/parse is unavailable on this Jira."},
					"source":   "search-fallback",
				}},
			}, nil
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := decodeJSON(resp); err != nil {
		return nil, err
	}
	return map[string]any{
		"queries": []map[string]any{{
			"query":    jql,
			"errors":   []string{},
			"warnings": []string{"Used search validation fallback because /jql/parse is unavailable on this Jira."},
			"source":   "search-fallback",
		}},
	}, nil
}

// GetJQLAutoCompleteData returns Jira JQL autocomplete metadata.
func (c *Client) GetJQLAutoCompleteData() (map[string]any, error) {
	resp, err := c.Request("GET", "/jql/autocompletedata", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetBoardIssues fetches issues from a Jira Agile board.
func (c *Client) GetBoardIssues(boardID string, jql string, limit, startAt int, fields string) (map[string]any, error) {
	boardURL := fmt.Sprintf("%s%s/board/%s/issue", c.apiBaseURL, AgileBase, boardID)
	params := url.Values{}
	params.Set("maxResults", fmt.Sprintf("%d", limit))
	params.Set("startAt", fmt.Sprintf("%d", startAt))
	if jql != "" {
		params.Set("jql", jql)
	}
	if fields != "" {
		params.Set("fields", fields)
	}
	resp, err := c.RequestURL("GET", boardURL, nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetBoardConfiguration fetches Jira Agile board configuration metadata.
func (c *Client) GetBoardConfiguration(boardID string) (map[string]any, error) {
	requestURL := fmt.Sprintf("%s%s/board/%s/configuration", c.apiBaseURL, AgileBase, boardID)
	resp, err := c.RequestURL("GET", requestURL, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListBoards lists Jira agile boards.
func (c *Client) ListBoards(boardType, name, projectKey string, limit, startAt int) (map[string]any, error) {
	requestURL := fmt.Sprintf("%s%s/board", c.apiBaseURL, AgileBase)
	params := url.Values{}
	params.Set("maxResults", fmt.Sprintf("%d", limit))
	params.Set("startAt", fmt.Sprintf("%d", startAt))
	if strings.TrimSpace(boardType) != "" {
		params.Set("type", boardType)
	}
	if strings.TrimSpace(name) != "" {
		params.Set("name", name)
	}
	if strings.TrimSpace(projectKey) != "" {
		params.Set("projectKeyOrId", projectKey)
	}
	resp, err := c.RequestURL("GET", requestURL, nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListDashboards lists Jira dashboards visible to the current user.
func (c *Client) ListDashboards(limit, startAt int) (map[string]any, error) {
	params := url.Values{}
	params.Set("maxResults", fmt.Sprintf("%d", limit))
	params.Set("startAt", fmt.Sprintf("%d", startAt))
	resp, err := c.Request("GET", "/dashboard", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetDashboard fetches dashboard metadata by id.
func (c *Client) GetDashboard(dashboardID string) (map[string]any, error) {
	resp, err := c.Request("GET", "/dashboard/"+dashboardID, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListDashboardGadgets lists gadgets configured on a dashboard.
func (c *Client) ListDashboardGadgets(dashboardID string, limit, startAt int) (map[string]any, error) {
	params := url.Values{}
	params.Set("maxResults", fmt.Sprintf("%d", limit))
	params.Set("startAt", fmt.Sprintf("%d", startAt))
	resp, err := c.Request("GET", "/dashboard/"+dashboardID+"/gadget", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UpdateIssue updates a Jira issue with the given payload.
func (c *Client) UpdateIssue(issue string, payload map[string]any, notifyUsers bool) error {
	if c.apiVersion == "3" {
		if err := normalizeADFPayload(payload); err != nil {
			return err
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	params := url.Values{}
	params.Set("notifyUsers", fmt.Sprintf("%t", notifyUsers))
	resp, err := c.Request("PUT", "/issue/"+issue, body, params)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// TransitionIssue performs a workflow transition on a Jira issue.
func (c *Client) TransitionIssue(issue string, payload map[string]any, notifyUsers bool) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	params := url.Values{}
	params.Set("notifyUsers", fmt.Sprintf("%t", notifyUsers))
	resp, err := c.Request("POST", "/issue/"+issue+"/transitions", body, params)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// ListTransitions returns the available workflow transitions for an issue.
func (c *Client) ListTransitions(issue string) (map[string]any, error) {
	resp, err := c.Request("GET", "/issue/"+issue+"/transitions", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// CreateIssue creates a new Jira issue.
func (c *Client) CreateIssue(payload map[string]any) (map[string]any, error) {
	if c.apiVersion == "3" {
		if err := normalizeADFPayload(payload); err != nil {
			return nil, err
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.Request("POST", "/issue", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListComments fetches comments for a Jira issue.
func (c *Client) ListComments(issue string, limit, startAt int) (map[string]any, error) {
	params := url.Values{}
	params.Set("maxResults", fmt.Sprintf("%d", limit))
	params.Set("startAt", fmt.Sprintf("%d", startAt))
	resp, err := c.Request("GET", "/issue/"+issue+"/comment", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// AddComment adds a comment to a Jira issue.
func (c *Client) AddComment(issue string, bodyValue any) (map[string]any, error) {
	if c.apiVersion == "3" {
		if text, ok := bodyValue.(string); ok {
			bodyValue = plainTextADFDocument(text)
		}
	}
	body, err := json.Marshal(map[string]any{"body": bodyValue})
	if err != nil {
		return nil, err
	}
	resp, err := c.Request("POST", "/issue/"+issue+"/comment", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UpdateComment updates an existing Jira comment.
func (c *Client) UpdateComment(issue, commentID string, bodyValue any) (map[string]any, error) {
	if c.apiVersion == "3" {
		if text, ok := bodyValue.(string); ok {
			bodyValue = plainTextADFDocument(text)
		}
	}
	body, err := json.Marshal(map[string]any{"body": bodyValue})
	if err != nil {
		return nil, err
	}
	resp, err := c.Request("PUT", "/issue/"+issue+"/comment/"+commentID, body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// DeleteComment removes a Jira comment from an issue.
func (c *Client) DeleteComment(issue, commentID string) error {
	resp, err := c.Request("DELETE", "/issue/"+issue+"/comment/"+commentID, nil, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// CreateIssueLink creates a relationship between two Jira issues.
func (c *Client) CreateIssueLink(payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.Request("POST", "/issueLink", body, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// DeleteIssueLink removes an issue link by id.
func (c *Client) DeleteIssueLink(linkID string) error {
	resp, err := c.Request("DELETE", "/issueLink/"+linkID, nil, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// ListIssueLinkTypes returns the configured Jira issue link types.
func (c *Client) ListIssueLinkTypes() ([]map[string]any, error) {
	resp, err := c.Request("GET", "/issueLinkType", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := decodeJSON(resp)
	if err != nil {
		return nil, err
	}
	raw, _ := data["issueLinkTypes"].([]any)
	return coerceJSONArray(raw), nil
}

// AssignIssue updates the assignee on a Jira issue.
func (c *Client) AssignIssue(issue string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.Request("PUT", "/issue/"+issue+"/assignee", body, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// SearchUsers looks up users by query, falling back for older Jira variants.
func (c *Client) SearchUsers(query string, limit int) ([]map[string]any, error) {
	tryParams := []url.Values{
		func() url.Values {
			params := url.Values{}
			params.Set("query", query)
			params.Set("maxResults", fmt.Sprintf("%d", limit))
			return params
		}(),
		func() url.Values {
			params := url.Values{}
			params.Set("username", query)
			params.Set("maxResults", fmt.Sprintf("%d", limit))
			return params
		}(),
	}

	var lastErr error
	for _, params := range tryParams {
		resp, err := c.Request("GET", "/user/search", nil, params)
		if err != nil {
			lastErr = err
			continue
		}
		defer func() { _ = resp.Body.Close() }()
		return decodeJSONArray(resp)
	}

	return nil, lastErr
}

// ListProjects returns visible Jira projects.
func (c *Client) ListProjects() ([]map[string]any, error) {
	resp, err := c.Request("GET", "/project", nil, nil)
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		return decodeJSONArray(resp)
	}

	searchResp, searchErr := c.Request("GET", "/project/search", nil, nil)
	if searchErr != nil {
		return nil, err
	}
	defer func() { _ = searchResp.Body.Close() }()

	data, decodeErr := decodeJSON(searchResp)
	if decodeErr != nil {
		return nil, decodeErr
	}
	for _, key := range []string{"values", "results"} {
		if arr, ok := data[key].([]any); ok {
			return coerceJSONArray(arr), nil
		}
	}
	return []map[string]any{}, nil
}

// ListFields returns all available Jira fields.
func (c *Client) ListFields() ([]map[string]any, error) {
	resp, err := c.Request("GET", "/field", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var result []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListCreateMetaIssueTypes returns issue types available for creating issues in a project.
func (c *Client) ListCreateMetaIssueTypes(projectKey string) ([]map[string]any, error) {
	resp, err := c.Request("GET", "/issue/createmeta/"+url.PathEscape(projectKey)+"/issuetypes", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := decodeJSON(resp)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"values", "results", "issueTypes"} {
		if arr, ok := data[key].([]any); ok {
			return coerceJSONArray(arr), nil
		}
	}
	return []map[string]any{}, nil
}

// GetCreateMetaIssueTypeFields returns create metadata fields for a project and issue type.
func (c *Client) GetCreateMetaIssueTypeFields(projectKey, issueTypeID string) ([]map[string]any, error) {
	resp, err := c.Request("GET", "/issue/createmeta/"+url.PathEscape(projectKey)+"/issuetypes/"+url.PathEscape(issueTypeID), nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := decodeJSON(resp)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"values", "results", "fields"} {
		if arr, ok := data[key].([]any); ok {
			return coerceJSONArray(arr), nil
		}
	}
	return []map[string]any{}, nil
}

// GetEditMeta returns edit metadata for an issue, including allowed values.
func (c *Client) GetEditMeta(issue string) (map[string]any, error) {
	resp, err := c.Request("GET", "/issue/"+issue+"/editmeta", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetMyself returns the currently authenticated user.
func (c *Client) GetMyself() (map[string]any, error) {
	resp, err := c.Request("GET", "/myself", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetWatchers returns watcher metadata for an issue.
func (c *Client) GetWatchers(issue string) (map[string]any, error) {
	resp, err := c.Request("GET", "/issue/"+issue+"/watchers", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// AddWatcher adds a watcher to an issue. Jira expects a JSON string body.
func (c *Client) AddWatcher(issue string, watcher string) error {
	body, err := json.Marshal(watcher)
	if err != nil {
		return err
	}
	resp, err := c.Request("POST", "/issue/"+issue+"/watchers", body, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// RemoveWatcher removes a watcher from an issue.
func (c *Client) RemoveWatcher(issue, paramKey, value string) error {
	params := url.Values{}
	params.Set(paramKey, value)
	if paramKey == "username" {
		params.Set("userName", value)
	}
	resp, err := c.Request("DELETE", "/issue/"+issue+"/watchers", nil, params)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// ListWorklogs fetches worklogs for an issue.
func (c *Client) ListWorklogs(issue string, limit, startAt int) (map[string]any, error) {
	params := url.Values{}
	params.Set("maxResults", fmt.Sprintf("%d", limit))
	params.Set("startAt", fmt.Sprintf("%d", startAt))
	resp, err := c.Request("GET", "/issue/"+issue+"/worklog", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// AddWorklog creates a worklog on an issue.
func (c *Client) AddWorklog(issue string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.Request("POST", "/issue/"+issue+"/worklog", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UpdateWorklog updates an existing worklog.
func (c *Client) UpdateWorklog(issue, worklogID string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.Request("PUT", "/issue/"+issue+"/worklog/"+worklogID, body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// DeleteWorklog deletes an existing worklog.
func (c *Client) DeleteWorklog(issue, worklogID string) error {
	resp, err := c.Request("DELETE", "/issue/"+issue+"/worklog/"+worklogID, nil, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// DeleteIssue deletes a Jira issue.
func (c *Client) DeleteIssue(issue string, deleteSubtasks bool) error {
	params := url.Values{}
	if deleteSubtasks {
		params.Set("deleteSubtasks", "true")
	}
	resp, err := c.Request("DELETE", "/issue/"+issue, nil, params)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// ListBoardSprints lists sprints on a board.
func (c *Client) ListBoardSprints(boardID, state string, limit, startAt int) (map[string]any, error) {
	requestURL := fmt.Sprintf("%s%s/board/%s/sprint", c.apiBaseURL, AgileBase, boardID)
	params := url.Values{}
	params.Set("maxResults", fmt.Sprintf("%d", limit))
	params.Set("startAt", fmt.Sprintf("%d", startAt))
	if strings.TrimSpace(state) != "" {
		params.Set("state", state)
	}
	resp, err := c.RequestURL("GET", requestURL, nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetSprint fetches sprint metadata by ID.
func (c *Client) GetSprint(sprintID string) (map[string]any, error) {
	requestURL := fmt.Sprintf("%s%s/sprint/%s", c.apiBaseURL, AgileBase, sprintID)
	resp, err := c.RequestURL("GET", requestURL, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// CreateSprint creates a sprint.
func (c *Client) CreateSprint(payload map[string]any) (map[string]any, error) {
	requestURL := fmt.Sprintf("%s%s/sprint", c.apiBaseURL, AgileBase)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.RequestURL("POST", requestURL, body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UpdateSprint updates an existing sprint.
func (c *Client) UpdateSprint(sprintID string, payload map[string]any) (map[string]any, error) {
	requestURL := fmt.Sprintf("%s%s/sprint/%s", c.apiBaseURL, AgileBase, sprintID)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.RequestURL("PUT", requestURL, body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// DeleteSprint deletes a sprint.
func (c *Client) DeleteSprint(sprintID string) error {
	requestURL := fmt.Sprintf("%s%s/sprint/%s", c.apiBaseURL, AgileBase, sprintID)
	resp, err := c.RequestURL("DELETE", requestURL, nil, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// AddIssuesToSprint assigns issues to a sprint.
func (c *Client) AddIssuesToSprint(sprintID string, issues []string) error {
	requestURL := fmt.Sprintf("%s%s/sprint/%s/issue", c.apiBaseURL, AgileBase, sprintID)
	body, err := json.Marshal(map[string]any{"issues": issues})
	if err != nil {
		return err
	}
	resp, err := c.RequestURL("POST", requestURL, body, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// GetBacklogIssues lists issues currently in a board backlog.
func (c *Client) GetBacklogIssues(boardID, jql string, limit, startAt int, fields, expand string) (map[string]any, error) {
	requestURL := fmt.Sprintf("%s%s/board/%s/backlog", c.apiBaseURL, AgileBase, boardID)
	params := url.Values{}
	params.Set("maxResults", fmt.Sprintf("%d", limit))
	params.Set("startAt", fmt.Sprintf("%d", startAt))
	if strings.TrimSpace(jql) != "" {
		params.Set("jql", jql)
	}
	if strings.TrimSpace(fields) != "" {
		params.Set("fields", fields)
	}
	if strings.TrimSpace(expand) != "" {
		params.Set("expand", expand)
	}
	resp, err := c.RequestURL("GET", requestURL, nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// RankIssues reorders issues before or after another issue.
func (c *Client) RankIssues(issues []string, rankBeforeIssue, rankAfterIssue string, rankCustomFieldID int) error {
	requestURL := fmt.Sprintf("%s%s/issue/rank", c.apiBaseURL, AgileBase)
	payload := map[string]any{"issues": issues}
	if strings.TrimSpace(rankBeforeIssue) != "" {
		payload["rankBeforeIssue"] = rankBeforeIssue
	}
	if strings.TrimSpace(rankAfterIssue) != "" {
		payload["rankAfterIssue"] = rankAfterIssue
	}
	if rankCustomFieldID > 0 {
		payload["rankCustomFieldId"] = rankCustomFieldID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.RequestURL("PUT", requestURL, body, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *Client) requestServiceDesk(method, path string, body []byte, params url.Values) (*http.Response, error) {
	requestURL := strings.TrimRight(c.apiBaseURL, "/") + ServiceDeskBase + path
	return c.RequestURL(method, requestURL, body, params)
}

// ListServiceDesks lists Jira Service Management desks visible to the current user.
func (c *Client) ListServiceDesks(limit, startAt int) (map[string]any, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", startAt))
	resp, err := c.requestServiceDesk("GET", "/servicedesk", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListServiceDeskQueues lists queues for a service desk.
func (c *Client) ListServiceDeskQueues(serviceDeskID string, limit, startAt int) (map[string]any, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", startAt))
	resp, err := c.requestServiceDesk("GET", "/servicedesk/"+url.PathEscape(serviceDeskID)+"/queue", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListQueueIssues lists requests/issues in a service desk queue.
func (c *Client) ListQueueIssues(serviceDeskID, queueID string, limit, startAt int) (map[string]any, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", startAt))
	resp, err := c.requestServiceDesk("GET", "/servicedesk/"+url.PathEscape(serviceDeskID)+"/queue/"+url.PathEscape(queueID)+"/issue", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListCustomerRequests lists customer requests visible to the current user.
func (c *Client) ListCustomerRequests(limit, startAt int) (map[string]any, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", startAt))
	resp, err := c.requestServiceDesk("GET", "/request", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetCustomerRequest fetches a JSM request by issue key or id.
func (c *Client) GetCustomerRequest(requestID string) (map[string]any, error) {
	resp, err := c.requestServiceDesk("GET", "/request/"+url.PathEscape(requestID), nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListRequestApprovals lists approvals on a JSM request.
func (c *Client) ListRequestApprovals(requestID string) (map[string]any, error) {
	resp, err := c.requestServiceDesk("GET", "/request/"+url.PathEscape(requestID)+"/approval", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// DecideApproval approves or declines a JSM approval.
func (c *Client) DecideApproval(requestID, approvalID, decision string) (map[string]any, error) {
	body, err := json.Marshal(map[string]any{"decision": decision})
	if err != nil {
		return nil, err
	}
	resp, err := c.requestServiceDesk("POST", "/request/"+url.PathEscape(requestID)+"/approval/"+url.PathEscape(approvalID), body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListRequestSLAs lists SLA state for a JSM request.
func (c *Client) ListRequestSLAs(requestID string) (map[string]any, error) {
	resp, err := c.requestServiceDesk("GET", "/request/"+url.PathEscape(requestID)+"/sla", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// MoveIssuesToBoard moves backlog issues onto a board, optionally ranking them.
func (c *Client) MoveIssuesToBoard(boardID string, issues []string, rankBeforeIssue, rankAfterIssue string, rankCustomFieldID int) error {
	requestURL := fmt.Sprintf("%s%s/board/%s/issue", c.apiBaseURL, AgileBase, boardID)
	payload := map[string]any{"issues": issues}
	if strings.TrimSpace(rankBeforeIssue) != "" {
		payload["rankBeforeIssue"] = rankBeforeIssue
	}
	if strings.TrimSpace(rankAfterIssue) != "" {
		payload["rankAfterIssue"] = rankAfterIssue
	}
	if rankCustomFieldID > 0 {
		payload["rankCustomFieldId"] = rankCustomFieldID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.RequestURL("POST", requestURL, body, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// MoveIssuesToBacklog removes issues from active or future sprints.
func (c *Client) MoveIssuesToBacklog(issues []string) error {
	requestURL := fmt.Sprintf("%s%s/backlog/issue", c.apiBaseURL, AgileBase)
	body, err := json.Marshal(map[string]any{"issues": issues})
	if err != nil {
		return err
	}
	resp, err := c.RequestURL("POST", requestURL, body, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// MoveIssuesToBacklogForBoard moves issues into a specific board backlog.
func (c *Client) MoveIssuesToBacklogForBoard(boardID string, issues []string, rankBeforeIssue, rankAfterIssue string, rankCustomFieldID int) error {
	requestURL := fmt.Sprintf("%s%s/backlog/%s/issue", c.apiBaseURL, AgileBase, boardID)
	payload := map[string]any{"issues": issues}
	if strings.TrimSpace(rankBeforeIssue) != "" {
		payload["rankBeforeIssue"] = rankBeforeIssue
	}
	if strings.TrimSpace(rankAfterIssue) != "" {
		payload["rankAfterIssue"] = rankAfterIssue
	}
	if rankCustomFieldID > 0 {
		payload["rankCustomFieldId"] = rankCustomFieldID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.RequestURL("POST", requestURL, body, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// GetEpicIssues lists issues assigned to an epic.
func (c *Client) GetEpicIssues(epicID, jql string, limit, startAt int, fields, expand string) (map[string]any, error) {
	requestURL := fmt.Sprintf("%s%s/epic/%s/issue", c.apiBaseURL, AgileBase, url.PathEscape(epicID))
	params := url.Values{}
	params.Set("maxResults", fmt.Sprintf("%d", limit))
	params.Set("startAt", fmt.Sprintf("%d", startAt))
	if strings.TrimSpace(jql) != "" {
		params.Set("jql", jql)
	}
	if strings.TrimSpace(fields) != "" {
		params.Set("fields", fields)
	}
	if strings.TrimSpace(expand) != "" {
		params.Set("expand", expand)
	}
	resp, err := c.RequestURL("GET", requestURL, nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// MoveIssuesToEpic assigns issues to an epic.
func (c *Client) MoveIssuesToEpic(epicID string, issues []string) error {
	requestURL := fmt.Sprintf("%s%s/epic/%s/issue", c.apiBaseURL, AgileBase, url.PathEscape(epicID))
	body, err := json.Marshal(map[string]any{"issues": issues})
	if err != nil {
		return err
	}
	resp, err := c.RequestURL("POST", requestURL, body, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// RemoveIssuesFromEpic clears epic assignments for issues.
func (c *Client) RemoveIssuesFromEpic(issues []string) error {
	requestURL := fmt.Sprintf("%s%s/epic/none/issue", c.apiBaseURL, AgileBase)
	body, err := json.Marshal(map[string]any{"issues": issues})
	if err != nil {
		return err
	}
	resp, err := c.RequestURL("POST", requestURL, body, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// ListAttachments returns attachment metadata from an issue.
func (c *Client) ListAttachments(issue string) ([]map[string]any, error) {
	data, err := c.GetIssue(issue, "attachment", "")
	if err != nil {
		return nil, err
	}
	fields, _ := data["fields"].(map[string]any)
	arr, _ := fields["attachment"].([]any)
	return coerceJSONArray(arr), nil
}

// DeleteAttachment removes an attachment by id.
func (c *Client) DeleteAttachment(attachmentID string) error {
	resp, err := c.Request("DELETE", "/attachment/"+attachmentID, nil, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// UploadAttachment uploads a file attachment to a Jira issue.
func (c *Client) UploadAttachment(issue, filePath string) (map[string]any, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return c.uploadAttachmentReader(issue, filepath.Base(filePath), file)
}

// UploadAttachmentBytes uploads in-memory attachment content to a Jira issue.
func (c *Client) UploadAttachmentBytes(issue, filename string, data []byte) (map[string]any, error) {
	return c.uploadAttachmentReader(issue, filename, bytes.NewReader(data))
}

func (c *Client) uploadAttachmentReader(issue, filename string, reader io.Reader) (map[string]any, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, reader); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	params := url.Values{}
	resp, err := c.requestWithExtraHeaders("POST", c.restBase+"/issue/"+issue+"/attachments", body.Bytes(), params, http.Header{
		"Content-Type":      {writer.FormDataContentType()},
		"X-Atlassian-Token": {"no-check"},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	items, err := decodeJSONArray(resp)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return map[string]any{}, nil
	}
	return items[0], nil
}

// DownloadAttachment downloads an attachment URL to disk.
func (c *Client) DownloadAttachment(contentURL, outputPath string) error {
	resp, err := c.requestWithExtraHeaders("GET", contentURL, nil, nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, resp.Body)
	return err
}

// DownloadAttachmentContent fetches attachment bytes for hashing or custom sync logic.
func (c *Client) DownloadAttachmentContent(contentURL string) ([]byte, error) {
	resp, err := c.requestWithExtraHeaders("GET", contentURL, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

func decodeJSON(resp *http.Response) (map[string]any, error) {
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func decodeJSONArray(resp *http.Response) ([]map[string]any, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result []map[string]any
	if err := json.Unmarshal(body, &result); err == nil {
		return result, nil
	}

	var wrapper map[string]any
	if err := json.Unmarshal(body, &wrapper); err == nil {
		for _, key := range []string{"values", "results"} {
			if arr, ok := wrapper[key].([]any); ok {
				return coerceJSONArray(arr), nil
			}
		}
	}

	return nil, fmt.Errorf("expected JSON array response")
}

func coerceJSONArray(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func (c *Client) requestWithExtraHeaders(method, requestURL string, body []byte, params url.Values, extraHeaders http.Header) (*http.Response, error) {
	method = strings.ToUpper(method)
	finalURL := mergeQuery(requestURL, params)
	target := logging.SafeTarget(finalURL)
	startedAt := time.Now()

	cfg := c.retryConfig
	if method != "GET" && method != "HEAD" {
		cfg.RetryExceptions = false
	}

	requestFn := func() (*http.Response, error) {
		return c.doRequestWithHeaders(method, finalURL, body, extraHeaders)
	}

	resp, err := httpclient.RequestWithRetryURL(finalURL, requestFn, cfg, c.onRetry)
	if err != nil {
		if c.logger != nil {
			c.logger.Debug("http.error",
				"method", method,
				"target", target,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"error", err.Error(),
			)
		}
		if isTimeoutError(err) {
			timeout := c.timeout.Seconds()
			return nil, &cerrors.CojiraError{
				Code:     cerrors.Timeout,
				Message:  fmt.Sprintf("Request timed out: %s %s", method, path(finalURL)),
				Hint:     cerrors.HintTimeout(&timeout),
				ExitCode: 1,
			}
		}
		return nil, &cerrors.CojiraError{
			Code:     cerrors.HTTPError,
			Message:  fmt.Sprintf("Network error: %v", err),
			ExitCode: 1,
		}
	}

	if c.logger != nil {
		c.logger.Debug("http.response",
			"method", method,
			"target", target,
			"status", resp.StatusCode,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"cache", resp.Header.Get("X-Cojira-Cache"),
		)
	}

	return c.handleResponse(resp, method, finalURL)
}
