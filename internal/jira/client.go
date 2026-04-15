// Package jira provides the Jira REST API client.
package jira

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
)

const (
	DefaultAPIVersion = "2"
	GreenhopperBase   = "/rest/greenhopper/1.0"
	AgileBase         = "/rest/agile/1.0"
)

// ClientConfig holds the parameters for creating a new JiraClient.
type ClientConfig struct {
	BaseURL     string
	APIVersion  string
	Email       string
	Token       string
	AuthMode    string
	VerifySSL   bool
	UserAgent   string
	Timeout     time.Duration
	RetryConfig httpclient.RetryConfig
	Debug       bool
}

// Client is a Jira REST API client.
type Client struct {
	baseURL     string
	apiVersion  string
	restBase    string
	verifySSL   bool
	timeout     time.Duration
	retryConfig httpclient.RetryConfig
	debug       bool
	httpClient  *http.Client
	headers     http.Header
	auth        *basicAuth
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
	restBase := fmt.Sprintf("%s/rest/api/%s", baseURL, cfg.APIVersion)

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
		baseURL:     baseURL,
		apiVersion:  cfg.APIVersion,
		restBase:    restBase,
		verifySSL:   cfg.VerifySSL,
		timeout:     cfg.Timeout,
		retryConfig: cfg.RetryConfig,
		debug:       cfg.Debug,
		httpClient:  buildHTTPClient(cfg.Timeout, cfg.VerifySSL),
		headers:     headers,
		auth:        auth,
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
	if !c.debug {
		return
	}
	status := fmt.Sprintf("%d", statusCode)
	if statusCode == 0 {
		status = "error"
	}
	fmt.Fprintf(os.Stderr, "[debug] retry %d/%d after %.2fs (status=%s)\n",
		attempt, c.retryConfig.Retries, delay.Seconds(), status)
}

func (c *Client) doRequest(method, requestURL string, body []byte, params url.Values) (*http.Response, error) {
	if len(params) > 0 {
		requestURL = requestURL + "?" + params.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, requestURL, bodyReader)
	if err != nil {
		return nil, err
	}

	for key, values := range c.headers {
		for _, v := range values {
			req.Header.Set(key, v)
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

	cfg := c.retryConfig
	if method != "GET" && method != "HEAD" {
		cfg.RetryExceptions = false
	}

	requestFn := func() (*http.Response, error) {
		return c.doRequest(method, requestURL, body, params)
	}

	resp, err := httpclient.RequestWithRetry(requestFn, cfg, c.onRetry)
	if err != nil {
		// Classify the error.
		if isTimeoutError(err) {
			timeout := c.timeout.Seconds()
			return nil, &cerrors.CojiraError{
				Code:     cerrors.Timeout,
				Message:  fmt.Sprintf("Request timed out: %s %s", method, path(requestURL)),
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

// GetBoardIssues fetches issues from a Jira Agile board.
func (c *Client) GetBoardIssues(boardID string, jql string, limit, startAt int, fields string) (map[string]any, error) {
	boardURL := fmt.Sprintf("%s%s/board/%s/issue", c.baseURL, AgileBase, boardID)
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

// UpdateIssue updates a Jira issue with the given payload.
func (c *Client) UpdateIssue(issue string, payload map[string]any, notifyUsers bool) error {
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
func (c *Client) AddComment(issue string, bodyText string) (map[string]any, error) {
	body, err := json.Marshal(map[string]any{"body": bodyText})
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

// GetMyself returns the currently authenticated user.
func (c *Client) GetMyself() (map[string]any, error) {
	resp, err := c.Request("GET", "/myself", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
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

// UploadAttachment uploads a file attachment to a Jira issue.
func (c *Client) UploadAttachment(issue, filePath string) (map[string]any, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
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

	cfg := c.retryConfig
	if method != "GET" && method != "HEAD" {
		cfg.RetryExceptions = false
	}

	requestFn := func() (*http.Response, error) {
		finalURL := requestURL
		if len(params) > 0 {
			finalURL = finalURL + "?" + params.Encode()
		}

		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		req, err := http.NewRequest(method, finalURL, bodyReader)
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

	resp, err := httpclient.RequestWithRetry(requestFn, cfg, c.onRetry)
	if err != nil {
		if isTimeoutError(err) {
			timeout := c.timeout.Seconds()
			return nil, &cerrors.CojiraError{
				Code:     cerrors.Timeout,
				Message:  fmt.Sprintf("Request timed out: %s %s", method, path(requestURL)),
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

	return c.handleResponse(resp, method, requestURL)
}
