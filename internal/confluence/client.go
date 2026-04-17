// Package confluence provides the Confluence REST API client.
package confluence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/httpclient"
	"github.com/notabhay/cojira/internal/logging"
)

// ClientConfig holds the parameters for creating a new Confluence client.
type ClientConfig struct {
	BaseURL     string
	APIBaseURL  string
	APIVersion  string
	Token       string
	UserAgent   string
	Context     context.Context
	VerifySSL   bool
	Timeout     time.Duration
	RetryConfig httpclient.RetryConfig
	CacheConfig httpclient.CacheConfig
	Logger      *slog.Logger
	Debug       bool
}

// Client is a Confluence REST API client using raw net/http.
type Client struct {
	baseURL      string
	apiBaseURL   string
	apiVersion   string
	restBase     string
	restBaseV1   string
	restBaseV2   string
	ctx          context.Context
	timeout      time.Duration
	retryConfig  httpclient.RetryConfig
	cacheConfig  httpclient.CacheConfig
	cacheVaryKey string
	logger       *slog.Logger
	debug        bool
	httpClient   *http.Client
	headers      http.Header
}

// NewClient creates a new Confluence REST API client.
func NewClient(cfg ClientConfig) (*Client, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return nil, &cerrors.CojiraError{
			Code:        cerrors.ConfigMissingEnv,
			Message:     fmt.Sprintf("CONFLUENCE_BASE_URL (or --base-url) is required. %s", cerrors.HintSetup()),
			Hint:        cerrors.HintSetup(),
			UserMessage: "I need your Confluence URL in `.env` or `~/.config/cojira/credentials`. Please update the file directly instead of pasting it here.",
			Recovery:    map[string]any{"action": "edit", "path": ".env", "global_path": "~/.config/cojira/credentials", "requires_user": true},
			ExitCode:    2,
		}
	}

	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, &cerrors.CojiraError{
			Code:        cerrors.ConfigMissingEnv,
			Message:     fmt.Sprintf("CONFLUENCE_API_TOKEN environment variable is required. %s", cerrors.HintSetup()),
			Hint:        cerrors.HintSetup(),
			UserMessage: "I need your Confluence token in `.env` or `~/.config/cojira/credentials`. Please update the file directly instead of pasting it here.",
			Recovery:    map[string]any{"action": "edit", "path": ".env", "global_path": "~/.config/cojira/credentials", "requires_user": true},
			ExitCode:    2,
		}
	}

	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = confluenceDefaultUserAgent()
	}

	baseURL = strings.TrimRight(baseURL, "/")
	apiBaseURL := strings.TrimRight(cfg.APIBaseURL, "/")
	if apiBaseURL == "" {
		apiBaseURL = baseURL
	}
	apiVersion := strings.TrimSpace(cfg.APIVersion)
	if apiVersion == "" {
		apiVersion = "1"
	}
	restBaseV1 := buildConfluenceRestBase(apiBaseURL, "1")
	restBaseV2 := buildConfluenceRestBase(apiBaseURL, "2")
	restBase := restBaseV1
	if apiVersion == "2" {
		restBase = restBaseV2
	}

	headers := http.Header{
		"Accept":        {"application/json"},
		"Content-Type":  {"application/json"},
		"User-Agent":    {userAgent},
		"Authorization": {"Bearer " + token},
	}

	return &Client{
		baseURL:      baseURL,
		apiBaseURL:   apiBaseURL,
		apiVersion:   apiVersion,
		restBase:     restBase,
		restBaseV1:   restBaseV1,
		restBaseV2:   restBaseV2,
		ctx:          cfg.Context,
		timeout:      cfg.Timeout,
		retryConfig:  cfg.RetryConfig,
		cacheConfig:  cfg.CacheConfig,
		cacheVaryKey: confluenceCacheVaryKey(token),
		logger:       cfg.Logger,
		debug:        cfg.Debug,
		httpClient:   buildConfluenceHTTPClient(cfg.Timeout, cfg.VerifySSL),
		headers:      headers,
	}, nil
}

// BaseURL returns the base URL of the Confluence instance.
func (c *Client) BaseURL() string {
	return c.baseURL
}

func buildConfluenceRestBase(apiBaseURL, version string) string {
	apiBaseURL = strings.TrimRight(apiBaseURL, "/")
	isAtlassianProxy := strings.Contains(apiBaseURL, "/ex/confluence/")
	switch version {
	case "2":
		if isAtlassianProxy {
			return apiBaseURL + "/wiki/api/v2"
		}
		return apiBaseURL + "/api/v2"
	default:
		if isAtlassianProxy {
			return apiBaseURL + "/wiki/rest/api"
		}
		return apiBaseURL + "/rest/api"
	}
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

func confluenceCacheVaryKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func mergeConfluenceQuery(requestURL string, params url.Values) string {
	if len(params) == 0 {
		return requestURL
	}
	return requestURL + "?" + params.Encode()
}

func (c *Client) doRequest(method, requestURL string, body []byte, params url.Values) (*http.Response, error) {
	return c.doRequestWithHeaders(method, mergeConfluenceQuery(requestURL, params), body, nil)
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

func (c *Client) requestV1(method, path string, body []byte, params url.Values) (*http.Response, error) {
	requestURL := c.restBaseV1 + path
	return c.requestWithURL(method, requestURL, body, params)
}

func (c *Client) requestV2(method, path string, body []byte, params url.Values) (*http.Response, error) {
	requestURL := c.restBaseV2 + path
	return c.requestWithURL(method, requestURL, body, params)
}

func (c *Client) requestWithURL(method, requestURL string, body []byte, params url.Values) (*http.Response, error) {
	method = strings.ToUpper(method)
	finalURL := mergeConfluenceQuery(requestURL, params)
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
		if isTimeoutError(err) {
			timeout := c.timeout.Seconds()
			return nil, &cerrors.CojiraError{
				Code:     cerrors.Timeout,
				Message:  fmt.Sprintf("Request timed out: %s %s", method, finalURL),
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

func (c *Client) requestWithExtraHeaders(method, requestURL string, body []byte, params url.Values, extraHeaders http.Header) (*http.Response, error) {
	method = strings.ToUpper(method)
	finalURL := mergeConfluenceQuery(requestURL, params)
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
				Message:  fmt.Sprintf("Request timed out: %s %s", method, finalURL),
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

	if resp.StatusCode >= 400 {
		message := c.formatError(resp)
		return nil, &cerrors.CojiraError{
			Code:     cerrors.HTTPError,
			Message:  fmt.Sprintf("HTTP %d: %s", resp.StatusCode, message),
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
	if c.apiVersion == "2" {
		params := url.Values{}
		params.Set("body-format", "storage")
		resp, err := c.requestV2("GET", "/pages/"+pageID, nil, params)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		page, err := decodeJSON(resp)
		if err != nil {
			return nil, err
		}
		return normalizeV2Page(page), nil
	}
	params := url.Values{}
	if expand != "" {
		params.Set("expand", expand)
	}
	resp, err := c.Request("GET", "/content/"+pageID, nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetContentByStatus fetches content with a given status such as current or trashed.
func (c *Client) GetContentByStatus(contentID, status, expand string) (map[string]any, error) {
	params := url.Values{}
	if strings.TrimSpace(status) != "" {
		params.Set("status", status)
	}
	if expand != "" {
		params.Set("expand", expand)
	}
	resp, err := c.requestV1("GET", "/content/"+contentID, nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetPageVersion fetches a historical version of a Confluence page.
func (c *Client) GetPageVersion(pageID string, version int, expand string) (map[string]any, error) {
	params := url.Values{}
	params.Set("status", "historical")
	params.Set("version", fmt.Sprintf("%d", version))
	if expand != "" {
		params.Set("expand", expand)
	}
	resp, err := c.requestV1("GET", "/content/"+pageID, nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetPageByTitle fetches a Confluence page by space key and title.
func (c *Client) GetPageByTitle(spaceKey, title string) (map[string]any, error) {
	if c.apiVersion == "2" {
		spaceID, err := c.getSpaceIDByKey(spaceKey)
		if err == nil && spaceID != "" {
			params := url.Values{}
			params.Set("space-id", spaceID)
			params.Set("title", title)
			params.Set("body-format", "storage")
			resp, err := c.requestV2("GET", "/pages", nil, params)
			if err == nil {
				defer func() { _ = resp.Body.Close() }()
				data, err := decodeJSON(resp)
				if err == nil {
					results, _ := data["results"].([]any)
					if len(results) > 0 {
						if page, ok := results[0].(map[string]any); ok {
							return normalizeV2Page(page), nil
						}
					}
					return nil, nil
				}
			}
		}
	}
	params := url.Values{}
	params.Set("spaceKey", spaceKey)
	params.Set("title", title)
	resp, err := c.Request("GET", "/content", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

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

// ListBlogPosts lists blog posts, optionally filtered by space.
func (c *Client) ListBlogPosts(spaceKey string, limit, start int) (map[string]any, error) {
	params := url.Values{}
	params.Set("type", "blogpost")
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", start))
	if strings.TrimSpace(spaceKey) != "" {
		params.Set("spaceKey", spaceKey)
	}
	resp, err := c.requestV1("GET", "/content", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListContent lists content with optional status and space filters.
func (c *Client) ListContent(contentType, spaceKey, status string, limit, start int) (map[string]any, error) {
	params := url.Values{}
	if strings.TrimSpace(contentType) != "" {
		params.Set("type", contentType)
	}
	if strings.TrimSpace(spaceKey) != "" {
		params.Set("spaceKey", spaceKey)
	}
	if strings.TrimSpace(status) != "" {
		params.Set("status", status)
	}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", start))
	resp, err := c.requestV1("GET", "/content", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UpdatePage updates a Confluence page.
func (c *Client) UpdatePage(pageID string, payload map[string]any) (map[string]any, error) {
	if c.apiVersion == "2" && normalizeMaybeString(payload["type"]) == "page" {
		v2Payload, err := c.translateV1PageUpdateToV2(pageID, payload)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(v2Payload)
		if err != nil {
			return nil, err
		}
		resp, err := c.requestV2("PUT", "/pages/"+pageID, body, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		page, err := decodeJSON(resp)
		if err != nil {
			return nil, err
		}
		return normalizeV2Page(page), nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.Request("PUT", "/content/"+pageID, body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// CreatePage creates a new Confluence page.
func (c *Client) CreatePage(payload map[string]any) (map[string]any, error) {
	if c.apiVersion == "2" && normalizeMaybeString(payload["type"]) == "page" {
		v2Payload, err := c.translateV1PageCreateToV2(payload)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(v2Payload)
		if err != nil {
			return nil, err
		}
		resp, err := c.requestV2("POST", "/pages", body, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		page, err := decodeJSON(resp)
		if err != nil {
			return nil, err
		}
		return normalizeV2Page(page), nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.Request("POST", "/content", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
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
	_ = resp.Body.Close()
	return nil
}

// DeletePageLabel removes a label from a Confluence page.
func (c *Client) DeletePageLabel(pageID, label string) error {
	params := url.Values{}
	params.Set("name", label)
	resp, err := c.Request("DELETE", "/content/"+pageID+"/label", nil, params)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// GetPageLabels fetches labels applied to a page.
func (c *Client) GetPageLabels(pageID string, limit, start int) (map[string]any, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", start))
	resp, err := c.Request("GET", "/content/"+pageID+"/label", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetPageHistory fetches history metadata for a page.
func (c *Client) GetPageHistory(pageID string) (map[string]any, error) {
	resp, err := c.Request("GET", "/content/"+pageID+"/history", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UpdateRestrictions replaces page restrictions.
func (c *Client) UpdateRestrictions(pageID string, payload []map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.Request("PUT", "/content/"+pageID+"/restriction", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListAttachments fetches attachments on a page.
func (c *Client) ListAttachments(pageID string, limit, start int) (map[string]any, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", start))
	resp, err := c.Request("GET", "/content/"+pageID+"/child/attachment", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UploadAttachment uploads a file to a page.
func (c *Client) UploadAttachment(pageID, filePath string) (map[string]any, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return c.uploadAttachmentReader(pageID, filepath.Base(filePath), file)
}

// UploadAttachmentBytes uploads in-memory attachment bytes to a page.
func (c *Client) UploadAttachmentBytes(pageID, filename string, data []byte) (map[string]any, error) {
	return c.uploadAttachmentReader(pageID, filename, bytes.NewReader(data))
}

func (c *Client) uploadAttachmentReader(pageID, filename string, reader io.Reader) (map[string]any, error) {
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

	resp, err := c.requestWithExtraHeaders(
		"POST",
		c.restBase+"/content/"+pageID+"/child/attachment",
		body.Bytes(),
		nil,
		http.Header{
			"Content-Type":      {writer.FormDataContentType()},
			"X-Atlassian-Token": {"nocheck"},
		},
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// DownloadAttachment downloads an attachment to disk.
func (c *Client) DownloadAttachment(downloadURL, outputPath string) error {
	if strings.HasPrefix(downloadURL, "/") {
		downloadURL = c.baseURL + downloadURL
	}
	resp, err := c.requestWithExtraHeaders("GET", downloadURL, nil, nil, nil)
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

// DownloadAttachmentContent fetches attachment bytes for hashing or sync logic.
func (c *Client) DownloadAttachmentContent(downloadURL string) ([]byte, error) {
	if strings.HasPrefix(downloadURL, "/") {
		downloadURL = c.baseURL + downloadURL
	}
	resp, err := c.requestWithExtraHeaders("GET", downloadURL, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

// DownloadPageExport downloads a page export in a document format such as pdf or word.
func (c *Client) DownloadPageExport(pageID, format string) ([]byte, string, error) {
	var exportURL string
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "pdf":
		exportURL = c.baseURL + "/spaces/flyingpdf/pdfpageexport.action?pageId=" + url.QueryEscape(pageID)
	case "word", "doc", "docx":
		exportURL = c.baseURL + "/exportword?pageId=" + url.QueryEscape(pageID)
	default:
		return nil, "", fmt.Errorf("unsupported export format: %s", format)
	}
	resp, err := c.requestWithExtraHeaders("GET", exportURL, nil, nil, nil)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, filenameFromDisposition(resp.Header.Get("Content-Disposition")), nil
}

// DeleteAttachment removes an attachment by content id.
func (c *Client) DeleteAttachment(attachmentID string) error {
	return c.DeleteContent(attachmentID)
}

// ListPageComments fetches comments on a page.
func (c *Client) ListPageComments(pageID string, limit, start int) (map[string]any, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", start))
	resp, err := c.Request("GET", "/content/"+pageID+"/child/comment", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// AddPageComment adds a storage-format comment to a page.
func (c *Client) AddPageComment(pageID, bodyText string) (map[string]any, error) {
	payload := map[string]any{
		"type": "comment",
		"container": map[string]any{
			"type": "page",
			"id":   pageID,
		},
		"body": map[string]any{
			"storage": map[string]any{
				"value":          bodyText,
				"representation": "storage",
			},
		},
	}
	return c.CreatePage(payload)
}

// GetContentByID fetches a Confluence content record by id using the v1 API.
func (c *Client) GetContentByID(contentID string, expand string) (map[string]any, error) {
	params := url.Values{}
	if expand != "" {
		params.Set("expand", expand)
	}
	resp, err := c.requestV1("GET", "/content/"+contentID, nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UpdatePageComment updates an existing storage-format page comment.
func (c *Client) UpdatePageComment(commentID, bodyText string) (map[string]any, error) {
	comment, err := c.GetContentByID(commentID, "version,container")
	if err != nil {
		return nil, err
	}
	versionNum := intFromAny(getNestedMapValue(comment, "version", "number"), 0)
	payload := map[string]any{
		"id":   commentID,
		"type": "comment",
		"version": map[string]any{
			"number": versionNum + 1,
		},
		"container": comment["container"],
		"body": map[string]any{
			"storage": map[string]any{
				"value":          bodyText,
				"representation": "storage",
			},
		},
	}
	return c.UpdatePage(commentID, payload)
}

// DeletePageComment deletes a page comment by content id.
func (c *Client) DeletePageComment(commentID string) error {
	return c.DeleteContent(commentID)
}

// ListInlineComments lists inline comments for a page.
func (c *Client) ListInlineComments(pageID string, limit int, cursor string, bodyFormat string) (map[string]any, error) {
	params := url.Values{}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	if strings.TrimSpace(cursor) != "" {
		params.Set("cursor", cursor)
	}
	if strings.TrimSpace(bodyFormat) != "" {
		params.Set("body-format", bodyFormat)
	}
	resp, err := c.requestV2("GET", "/pages/"+pageID+"/inline-comments", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListPageProperties lists content properties for a page.
func (c *Client) ListPageProperties(pageID string, limit int) (map[string]any, error) {
	params := url.Values{}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	resp, err := c.requestV2("GET", "/pages/"+pageID+"/properties", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetPageProperty fetches a page content property by id.
func (c *Client) GetPageProperty(pageID, propertyID string) (map[string]any, error) {
	resp, err := c.requestV2("GET", "/pages/"+pageID+"/properties/"+propertyID, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// CreatePageProperty creates a page content property.
func (c *Client) CreatePageProperty(pageID string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.requestV2("POST", "/pages/"+pageID+"/properties", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UpdatePageProperty updates a page content property.
func (c *Client) UpdatePageProperty(pageID, propertyID string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.requestV2("PUT", "/pages/"+pageID+"/properties/"+propertyID, body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// DeletePageProperty deletes a page content property.
func (c *Client) DeletePageProperty(pageID, propertyID string) error {
	resp, err := c.requestV2("DELETE", "/pages/"+pageID+"/properties/"+propertyID, nil, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// GetInlineComment fetches an inline comment by id.
func (c *Client) GetInlineComment(commentID string) (map[string]any, error) {
	resp, err := c.requestV2("GET", "/inline-comments/"+commentID, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// CreateInlineComment creates an inline comment.
func (c *Client) CreateInlineComment(payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.requestV2("POST", "/inline-comments", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UpdateInlineComment updates an inline comment.
func (c *Client) UpdateInlineComment(commentID string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.requestV2("PUT", "/inline-comments/"+commentID, body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// DeleteInlineComment deletes an inline comment permanently.
func (c *Client) DeleteInlineComment(commentID string) error {
	resp, err := c.requestV2("DELETE", "/inline-comments/"+commentID, nil, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// DeleteContent deletes a page or comment by content ID.
func (c *Client) DeleteContent(contentID string) error {
	if c.apiVersion == "2" {
		resp, err := c.requestV2("DELETE", "/pages/"+contentID, nil, nil)
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
	}
	resp, err := c.requestV1("DELETE", "/content/"+contentID, nil, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// ListContentVersions lists historical versions for a content item.
func (c *Client) ListContentVersions(contentID string, limit, start int) (map[string]any, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", start))
	resp, err := c.requestV1("GET", "/content/"+contentID+"/version", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// RestoreTrashedContent restores a trashed content item to current status.
func (c *Client) RestoreTrashedContent(contentID string, versionNumber int, message string, restoreTitle bool) (map[string]any, error) {
	payload := map[string]any{
		"operationKey": "restore",
		"params": map[string]any{
			"versionNumber": versionNumber,
			"message":       message,
			"restoreTitle":  restoreTitle,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.requestV1("POST", "/content/"+contentID+"/version", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
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
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()

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
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetCurrentUser returns the currently authenticated user.
func (c *Client) GetCurrentUser() (map[string]any, error) {
	resp, err := c.requestV1("GET", "/user/current", nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// ListSpaces lists Confluence spaces.
func (c *Client) ListSpaces(limit, start int) (map[string]any, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("start", fmt.Sprintf("%d", start))
	resp, err := c.requestV1("GET", "/space", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetSpace fetches a Confluence space by key.
func (c *Client) GetSpace(spaceKey string) (map[string]any, error) {
	resp, err := c.requestV1("GET", "/space/"+url.PathEscape(spaceKey), nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// CreateSpace creates a Confluence space.
func (c *Client) CreateSpace(payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.requestV1("POST", "/space", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UpdateSpace updates a Confluence space by key.
func (c *Client) UpdateSpace(spaceKey string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.requestV1("PUT", "/space/"+url.PathEscape(spaceKey), body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// DeleteSpace deletes a Confluence space by key.
func (c *Client) DeleteSpace(spaceKey string) error {
	resp, err := c.requestV1("DELETE", "/space/"+url.PathEscape(spaceKey), nil, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// ListTemplates lists content templates, optionally scoped to a space.
func (c *Client) ListTemplates(spaceKey string, limit, start int) (map[string]any, error) {
	params := url.Values{}
	if strings.TrimSpace(spaceKey) != "" {
		params.Set("spaceKey", spaceKey)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	if start > 0 {
		params.Set("start", fmt.Sprintf("%d", start))
	}
	resp, err := c.requestV1("GET", "/template/page", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// GetTemplate fetches a content template by id.
func (c *Client) GetTemplate(templateID string) (map[string]any, error) {
	resp, err := c.requestV1("GET", "/template/"+url.PathEscape(templateID), nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// CreateTemplate creates a content template.
func (c *Client) CreateTemplate(payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.requestV1("POST", "/template", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// UpdateTemplate updates a content template.
func (c *Client) UpdateTemplate(payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.requestV1("PUT", "/template", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

// DeleteTemplate removes a template by id.
func (c *Client) DeleteTemplate(templateID string) error {
	resp, err := c.requestV1("DELETE", "/template/"+url.PathEscape(templateID), nil, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// MovePage moves a Confluence page under a new parent.
func (c *Client) MovePage(pageID, targetParentID string) (map[string]any, error) {
	page, err := c.GetPageByID(pageID, "version,body.storage")
	if err != nil {
		return nil, err
	}

	version, _ := page["version"].(map[string]any)
	versionNum := 1.0
	if n, ok := version["number"].(float64); ok {
		versionNum = n
	}

	ancestors := []map[string]any{}
	if targetParentID != "" {
		ancestors = []map[string]any{{"id": targetParentID}}
	}

	payload := map[string]any{
		"id":    pageID,
		"type":  "page",
		"title": page["title"],
		"version": map[string]any{
			"number": versionNum + 1,
		},
		"ancestors": ancestors,
		"body": map[string]any{
			"storage": map[string]any{
				"value":          getNestedString(page, "body", "storage", "value"),
				"representation": "storage",
			},
		},
	}

	return c.UpdatePage(pageID, payload)
}

func (c *Client) getSpaceIDByKey(spaceKey string) (string, error) {
	resp, err := c.requestV1("GET", "/space/"+url.PathEscape(spaceKey), nil, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := decodeJSON(resp)
	if err != nil {
		return "", err
	}
	return normalizeMaybeString(data["id"]), nil
}

func getNestedMapValue(m map[string]any, keys ...string) any {
	var cur any = m
	for _, key := range keys {
		next, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = next[key]
	}
	return cur
}

func filenameFromDisposition(headerValue string) string {
	if strings.TrimSpace(headerValue) == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(headerValue)
	if err != nil {
		return ""
	}
	return params["filename"]
}

func (c *Client) translateV1PageCreateToV2(payload map[string]any) (map[string]any, error) {
	title := normalizeMaybeString(payload["title"])
	spaceKey := safeString(payload, "space", "key")
	spaceID, err := c.getSpaceIDByKey(spaceKey)
	if err != nil {
		return nil, err
	}
	parentID := ""
	if ancestors, ok := payload["ancestors"].([]map[string]any); ok && len(ancestors) > 0 {
		parentID = normalizeMaybeString(ancestors[0]["id"])
	} else if ancestorsAny, ok := payload["ancestors"].([]any); ok && len(ancestorsAny) > 0 {
		if parent, ok := ancestorsAny[0].(map[string]any); ok {
			parentID = normalizeMaybeString(parent["id"])
		}
	}
	bodyValue := getNestedString(payload, "body", "storage", "value")
	body := map[string]any{
		"representation": "storage",
		"value":          bodyValue,
	}
	out := map[string]any{
		"status":  "current",
		"title":   title,
		"spaceId": spaceID,
		"body":    body,
	}
	if parentID != "" {
		out["parentId"] = parentID
	}
	if subtype := normalizeMaybeString(payload["subtype"]); subtype != "" {
		out["subtype"] = subtype
	}
	return out, nil
}

func (c *Client) translateV1PageUpdateToV2(pageID string, payload map[string]any) (map[string]any, error) {
	current, err := c.GetPageByID(pageID, "version,space,ancestors")
	if err != nil {
		return nil, err
	}
	spaceID := safeString(current, "space", "id")
	if spaceID == "" {
		spaceID = safeString(current, "space", "key")
	}
	parentID := ""
	if ancestors := getNestedSlice(current, "ancestors"); len(ancestors) > 0 {
		if parent, ok := ancestors[len(ancestors)-1].(map[string]any); ok {
			parentID = normalizeMaybeString(parent["id"])
		}
	}
	if ancestorsAny, ok := payload["ancestors"].([]any); ok && len(ancestorsAny) > 0 {
		if parent, ok := ancestorsAny[0].(map[string]any); ok {
			parentID = normalizeMaybeString(parent["id"])
		}
	}
	out := map[string]any{
		"id":      pageID,
		"status":  "current",
		"title":   normalizeMaybeString(payload["title"]),
		"spaceId": spaceID,
		"body": map[string]any{
			"representation": "storage",
			"value":          getNestedString(payload, "body", "storage", "value"),
		},
	}
	if version := getNestedFloat(payload, "version", "number"); version > 0 {
		out["version"] = map[string]any{"number": int(version)}
	}
	if parentID != "" {
		out["parentId"] = parentID
	}
	return out, nil
}

func normalizeV2Page(page map[string]any) map[string]any {
	if page == nil {
		return nil
	}
	normalized := map[string]any{}
	for k, v := range page {
		normalized[k] = v
	}
	spaceID := normalizeMaybeString(page["spaceId"])
	if spaceID != "" {
		normalized["space"] = map[string]any{
			"id":  spaceID,
			"key": spaceID,
		}
	}
	if parentID := normalizeMaybeString(page["parentId"]); parentID != "" {
		normalized["ancestors"] = []any{map[string]any{"id": parentID}}
	}
	if body, ok := page["body"].(map[string]any); ok {
		if _, ok := body["storage"]; ok {
			normalized["body"] = body
		} else if value := normalizeMaybeString(body["value"]); value != "" {
			normalized["body"] = map[string]any{
				"storage": map[string]any{
					"value":          value,
					"representation": normalizeMaybeString(body["representation"]),
				},
			}
		}
	}
	return normalized
}

func safeString(m map[string]any, keys ...string) string {
	var cur any = m
	for _, key := range keys {
		next, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = next[key]
	}
	return normalizeMaybeString(cur)
}

func decodeJSON(resp *http.Response) (map[string]any, error) {
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}
