// Package confluence provides the Confluence REST API client.
package confluence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/cojira/cojira/internal/httpclient"
)

// ClientConfig holds the parameters for creating a new Confluence client.
type ClientConfig struct {
	BaseURL     string
	Token       string
	UserAgent   string
	Timeout     time.Duration
	RetryConfig httpclient.RetryConfig
	Debug       bool
}

// Client is a Confluence REST API client using raw net/http.
type Client struct {
	baseURL     string
	restBase    string
	timeout     time.Duration
	retryConfig httpclient.RetryConfig
	debug       bool
	httpClient  *http.Client
	headers     http.Header
}

// NewClient creates a new Confluence REST API client.
func NewClient(cfg ClientConfig) (*Client, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return nil, &cerrors.CojiraError{
			Code:        cerrors.ConfigMissingEnv,
			Message:     fmt.Sprintf("CONFLUENCE_BASE_URL (or --base-url) is required. %s", cerrors.HintSetup()),
			Hint:        cerrors.HintSetup(),
			UserMessage: "I need your Confluence URL to continue. Run `cojira init` and paste a Confluence page URL.",
			Recovery:    map[string]any{"action": "run", "command": "cojira init", "requires_user": true},
			ExitCode:    2,
		}
	}

	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, &cerrors.CojiraError{
			Code:        cerrors.ConfigMissingEnv,
			Message:     fmt.Sprintf("CONFLUENCE_API_TOKEN environment variable is required. %s", cerrors.HintSetup()),
			Hint:        cerrors.HintSetup(),
			UserMessage: "I need your Confluence API token to continue. Run `cojira init` and paste your token.",
			Recovery:    map[string]any{"action": "run", "command": "cojira init", "requires_user": true},
			ExitCode:    2,
		}
	}

	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = "cojira/0.1"
	}

	baseURL = strings.TrimRight(baseURL, "/")
	restBase := baseURL + "/rest/api"

	headers := http.Header{
		"Accept":        {"application/json"},
		"Content-Type":  {"application/json"},
		"User-Agent":    {userAgent},
		"Authorization": {"Bearer " + token},
	}

	return &Client{
		baseURL:     baseURL,
		restBase:    restBase,
		timeout:     cfg.Timeout,
		retryConfig: cfg.RetryConfig,
		debug:       cfg.Debug,
		httpClient:  &http.Client{Timeout: cfg.Timeout},
		headers:     headers,
	}, nil
}

// BaseURL returns the base URL of the Confluence instance.
func (c *Client) BaseURL() string {
	return c.baseURL
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

	return c.httpClient.Do(req)
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

	if msg, ok := data["message"].(string); ok && msg != "" {
		return msg
	}

	raw, _ := json.Marshal(data)
	return string(raw)
}

// Request makes an HTTP request to the Confluence REST API.
func (c *Client) Request(method, path string, body []byte, params url.Values) (*http.Response, error) {
	requestURL := c.restBase + path
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
		if isTimeoutError(err) {
			timeout := c.timeout.Seconds()
			return nil, &cerrors.CojiraError{
				Code:     cerrors.Timeout,
				Message:  fmt.Sprintf("Request timed out: %s %s", method, requestURL),
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

	if resp.StatusCode >= 400 {
		message := c.formatError(resp)
		status := resp.StatusCode
		code := cerrors.HTTPError
		hint := ""
		if status == 401 {
			code = cerrors.HTTP401
			hint = cerrors.HintPermission()
		} else if status == 403 {
			code = cerrors.HTTP403
			hint = cerrors.HintPermission()
		} else if status == 429 {
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

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Timeout") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded")
}

// GetPageByID fetches a Confluence page by its numeric ID.
func (c *Client) GetPageByID(pageID string, expand string) (map[string]any, error) {
	params := url.Values{}
	if expand != "" {
		params.Set("expand", expand)
	}
	resp, err := c.Request("GET", "/content/"+pageID, nil, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeJSON(resp)
}

// GetPageByTitle fetches a Confluence page by space key and title.
func (c *Client) GetPageByTitle(spaceKey, title string) (map[string]any, error) {
	params := url.Values{}
	params.Set("spaceKey", spaceKey)
	params.Set("title", title)
	resp, err := c.Request("GET", "/content", nil, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	results, ok := data["results"].([]any)
	if !ok || len(results) == 0 {
		return nil, nil
	}
	page, ok := results[0].(map[string]any)
	if !ok {
		return nil, nil
	}
	return page, nil
}

// UpdatePage updates a Confluence page.
func (c *Client) UpdatePage(pageID string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.Request("PUT", "/content/"+pageID, body, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeJSON(resp)
}

// CreatePage creates a new Confluence page.
func (c *Client) CreatePage(payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.Request("POST", "/content", body, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeJSON(resp)
}

// SetPageLabel adds a label to a Confluence page.
func (c *Client) SetPageLabel(pageID, label string) error {
	payload := []map[string]string{{"name": label, "prefix": "global"}}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.Request("POST", "/content/"+pageID+"/label", body, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// GetChildren fetches child pages for a given page ID (fully paginated).
func (c *Client) GetChildren(pageID string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []map[string]any
	start := 0

	for {
		params := url.Values{}
		params.Set("limit", fmt.Sprintf("%d", limit))
		params.Set("start", fmt.Sprintf("%d", start))

		resp, err := c.Request("GET", "/content/"+pageID+"/child/page", nil, params)
		if err != nil {
			return nil, err
		}

		var data map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		results, ok := data["results"].([]any)
		if !ok || len(results) == 0 {
			break
		}

		for _, r := range results {
			if m, ok := r.(map[string]any); ok {
				out = append(out, m)
			}
		}

		if len(results) < limit {
			break
		}
		start += len(results)
	}

	return out, nil
}

// CQL runs a CQL search query.
func (c *Client) CQL(cql string, limit, start int) (map[string]any, error) {
	params := url.Values{}
	params.Set("cql", cql)
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", start))
	resp, err := c.Request("GET", "/content/search", nil, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeJSON(resp)
}

// GetCurrentUser returns the currently authenticated user.
func (c *Client) GetCurrentUser() (map[string]any, error) {
	resp, err := c.Request("GET", "/user/current", nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeJSON(resp)
}

// ListSpaces lists Confluence spaces.
func (c *Client) ListSpaces(limit, start int) (map[string]any, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", start))
	resp, err := c.Request("GET", "/space", nil, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeJSON(resp)
}

// MovePage moves a Confluence page under a new parent.
func (c *Client) MovePage(pageID, targetParentID string) (map[string]any, error) {
	// First get the current page to get the version and title
	page, err := c.GetPageByID(pageID, "version")
	if err != nil {
		return nil, err
	}

	version, _ := page["version"].(map[string]any)
	versionNum := 1.0
	if n, ok := version["number"].(float64); ok {
		versionNum = n
	}

	payload := map[string]any{
		"type":  "page",
		"title": page["title"],
		"version": map[string]any{
			"number": versionNum + 1,
		},
		"ancestors": []map[string]any{
			{"id": targetParentID},
		},
	}

	return c.UpdatePage(pageID, payload)
}

func decodeJSON(resp *http.Response) (map[string]any, error) {
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}
