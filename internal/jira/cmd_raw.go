package jira

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

var jiraRawAllowlist = []string{
	"/issue",
	"/search",
	"/field",
	"/myself",
	"/project",
	"/user",
}

// NewRawCmd creates the "raw" passthrough subcommand.
func NewRawCmd() *cobra.Command {
		cmd := &cobra.Command{
			Use:     "raw <method> <path>",
			Aliases: []string{"api"},
			Short:   "Send an authenticated Jira REST API request to an allowlisted path",
			Long: `Send an authenticated Jira REST API request to an allowlisted Jira resource.

Method comes first, then an API-relative path.

Examples:
  cojira jira raw GET /issue/PROJ-123
  cojira jira raw GET /search?jql=project%20=%20PROJ

Do not include /rest/api/2 in the path. Use /issue/PROJ-123, not /rest/api/2/issue/PROJ-123.

Accepted API-relative prefixes:
  /issue
  /search
  /field
  /myself
  /project
  /user`,
		Args: cobra.ExactArgs(2),
		RunE: runRaw,
	}
	cmd.Flags().String("body", "", "Request body file for POST/PUT")
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runRaw(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	method := strings.ToUpper(strings.TrimSpace(args[0]))
	pathArg := strings.TrimSpace(args[1])
	bodyFile, _ := cmd.Flags().GetString("body")
	outputFile, _ := cmd.Flags().GetString("output")

	if !allowedRawMethod(method) {
		return rawValidationError(mode, method, pathArg, "Unsupported method. Use GET, POST, PUT, or DELETE.")
	}

	apiPath, params, err := parseAndValidateJiraRawPath(method, pathArg)
	if err != nil {
		return rawValidationError(mode, method, pathArg, err.Error())
	}

	var body []byte
	if bodyFile != "" {
		text, err := readTextFile(bodyFile)
		if err != nil {
			return err
		}
		body = []byte(text)
	}

	resp, err := client.Request(method, apiPath, body, params)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "jira", "raw",
				map[string]any{"method": method, "path": pathArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error fetching %s %s: %v\n", method, pathArg, err)
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	payload, hasBody, err := decodeOptionalRawResponse(resp)
	if err != nil {
		return err
	}

	result := payload
	if !hasBody {
		result = map[string]any{"no_content": true}
	}

	if outputFile != "" {
		jsonStr, err := output.JSONDumps(result)
		if err != nil {
			return err
		}
		if err := writeFile(outputFile, jsonStr); err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "raw",
				map[string]any{"method": method, "path": apiPath},
				map[string]any{"saved_to": outputFile},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Saved %s %s to %s.\n", method, apiPath, outputFile)
			return nil
		}
		fmt.Printf("Saved raw response to: %s\n", outputFile)
		return nil
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "raw",
			map[string]any{"method": method, "path": apiPath},
			result, nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Println(rawSummary(method, apiPath, result))
		return nil
	}

	jsonStr, err := output.JSONDumps(result)
	if err != nil {
		return err
	}
	fmt.Println(jsonStr)
	return nil
}

func allowedRawMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "DELETE":
		return true
	default:
		return false
	}
}

func parseAndValidateJiraRawPath(method string, pathArg string) (string, url.Values, error) {
	parsed, err := url.Parse(pathArg)
	if err != nil {
		return "", nil, fmt.Errorf("Invalid API path: %v", err)
	}
	if parsed.IsAbs() || parsed.Host != "" || parsed.Scheme != "" {
		return "", nil, fmt.Errorf("Absolute URLs are not allowed. Use an API-relative path like /issue/PROJ-1")
	}
	if !strings.HasPrefix(parsed.Path, "/") {
		return "", nil, fmt.Errorf("API paths must start with '/'.")
	}
	if err := validateJiraRawRoute(method, parsed.Path); err != nil {
		return "", nil, err
	}
	return parsed.Path, parsed.Query(), nil
}

func validateJiraRawRoute(method, path string) error {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		return fmt.Errorf("Unsupported API path. Allowed prefixes: %s", strings.Join(jiraRawAllowlist, ", "))
	}
	switch segments[0] {
	case "issue":
		switch len(segments) {
		case 1:
			if method == "GET" || method == "POST" {
				return nil
			}
		case 2:
			if method == "GET" || method == "PUT" || method == "DELETE" {
				return nil
			}
		case 3:
			if segments[2] == "transitions" && (method == "GET" || method == "POST") {
				return nil
			}
		}
	case "search":
		if method == "GET" || method == "POST" {
			return nil
		}
	case "field", "myself", "project", "user":
		if method == "GET" {
			return nil
		}
	}
	return fmt.Errorf("Unsupported API path for %s. Allowed prefixes: %s", method, strings.Join(jiraRawAllowlist, ", "))
}

func rawValidationError(mode string, method, pathArg, message string) error {
	if mode == "json" {
		errObj, _ := output.ErrorObj(cerrors.OpFailed, message, "", "", nil)
		ec := 2
		return output.PrintJSON(output.BuildEnvelope(
			false, "jira", "raw",
			map[string]any{"method": method, "path": pathArg},
			nil, nil, []any{errObj}, "", "", "", &ec,
		))
	}
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: message, ExitCode: 2}
}

func rawSummary(method, path string, payload any) string {
	if obj, ok := payload.(map[string]any); ok {
		if issues, ok := obj["issues"].([]any); ok {
			return fmt.Sprintf("%s %s returned %d issue(s).", method, path, len(issues))
		}
		if results, ok := obj["results"].([]any); ok {
			return fmt.Sprintf("%s %s returned %d item(s).", method, path, len(results))
		}
		return fmt.Sprintf("%s %s completed.", method, path)
	}
	if arr, ok := payload.([]any); ok {
		return fmt.Sprintf("%s %s returned %d item(s).", method, path, len(arr))
	}
	return fmt.Sprintf("%s %s completed.", method, path)
}

func decodeOptionalRawResponse(resp *http.Response) (any, bool, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, false, nil
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return string(body), true, nil
	}
	return payload, true, nil
}
