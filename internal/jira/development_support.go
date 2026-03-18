package jira

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
)

type developmentIssueRef struct {
	IssueID string `json:"issue_id"`
	Key     string `json:"key"`
	Summary string `json:"summary,omitempty"`
}

type developmentSelection struct {
	ApplicationType string `json:"application_type"`
	DataType        string `json:"data_type"`
}

func isCloudJiraBase(baseURL string) bool {
	return strings.Contains(strings.ToLower(baseURL), ".atlassian.net")
}

func developmentBaseCandidates(client *Client) []string {
	base := client.BaseURL()
	latest := base + "/rest/dev-status/latest"
	v1 := base + "/rest/dev-status/1.0"
	if isCloudJiraBase(base) {
		return []string{latest, v1}
	}
	return []string{v1, latest}
}

func shouldTryNextDevelopmentBase(err error) bool {
	if ce, ok := err.(*cerrors.CojiraError); ok {
		switch ce.Code {
		case cerrors.HTTP404, cerrors.HTTP403:
			return true
		}
	}
	return false
}

func resolveDevelopmentIssue(client *Client, issue string) (developmentIssueRef, error) {
	payload, err := client.GetIssue(ResolveIssueIdentifier(issue), "summary", "")
	if err != nil {
		return developmentIssueRef{}, err
	}
	fields, _ := payload["fields"].(map[string]any)
	return developmentIssueRef{
		IssueID: strings.TrimSpace(fmt.Sprintf("%v", payload["id"])),
		Key:     strings.TrimSpace(fmt.Sprintf("%v", payload["key"])),
		Summary: strings.TrimSpace(fmt.Sprintf("%v", fields["summary"])),
	}, nil
}

func fetchDevelopmentSummary(client *Client, issue string) (developmentIssueRef, string, map[string]any, error) {
	issueRef, err := resolveDevelopmentIssue(client, issue)
	if err != nil {
		return developmentIssueRef{}, "", nil, err
	}
	if issueRef.IssueID == "" {
		return developmentIssueRef{}, "", nil, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  fmt.Sprintf("Issue %s did not return a numeric Jira issue id required for development lookups.", ResolveIssueIdentifier(issue)),
			ExitCode: 1,
		}
	}
	if isCloudJiraBase(client.BaseURL()) && !client.UsesBasicAuth() {
		return developmentIssueRef{}, "", nil, &cerrors.CojiraError{
			Code:     cerrors.HTTP401,
			Message:  "Jira development lookups on Atlassian Cloud require basic auth with JIRA_EMAIL + JIRA_API_TOKEN.",
			Hint:     "Set JIRA_EMAIL and use an Atlassian API token, or re-run cojira init/doctor to repair auth mode.",
			ExitCode: 1,
		}
	}

	params := url.Values{}
	params.Set("issueId", issueRef.IssueID)
	var lastErr error
	for _, apiBase := range developmentBaseCandidates(client) {
		resp, reqErr := client.RequestURL("GET", apiBase+"/issue/summary", nil, params)
		if reqErr != nil {
			lastErr = reqErr
			if shouldTryNextDevelopmentBase(reqErr) {
				continue
			}
			return developmentIssueRef{}, "", nil, reqErr
		}
		defer func() { _ = resp.Body.Close() }()
		payload, decodeErr := decodeJSON(resp)
		if decodeErr != nil {
			return developmentIssueRef{}, "", nil, decodeErr
		}
		return issueRef, apiBase, payload, nil
	}
	if lastErr != nil {
		return developmentIssueRef{}, "", nil, lastErr
	}
	return developmentIssueRef{}, "", nil, &cerrors.CojiraError{
		Code:     cerrors.FetchFailed,
		Message:  "Jira development summary lookup did not succeed against any known dev-status base.",
		ExitCode: 1,
	}
}

func fetchDevelopmentDetail(client *Client, apiBase string, issueID string, selection developmentSelection) (map[string]any, error) {
	params := url.Values{}
	params.Set("issueId", issueID)
	params.Set("applicationType", selection.ApplicationType)
	params.Set("dataType", selection.DataType)
	resp, err := client.RequestURL("GET", apiBase+"/issue/detail", nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp)
}

func developmentCounts(summary map[string]any) map[string]any {
	result := map[string]any{}
	summaryMap, _ := summary["summary"].(map[string]any)
	if summaryMap == nil {
		return result
	}
	for key, raw := range summaryMap {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		overall, _ := entry["overall"].(map[string]any)
		if overall == nil {
			continue
		}
		counts := map[string]any{
			"count":        intFromAny(overall["count"], 0),
			"last_updated": overall["lastUpdated"],
		}
		for detailKey, detailValue := range overall {
			if detailKey == "count" || detailKey == "lastUpdated" {
				continue
			}
			counts[detailKey] = detailValue
		}
		instances := []string{}
		byInstanceType, _ := entry["byInstanceType"].(map[string]any)
		for name := range byInstanceType {
			instances = append(instances, name)
		}
		sort.Strings(instances)
		counts["instance_types"] = instances
		result[key] = counts
	}
	return result
}

func developmentSelectionsFor(summary map[string]any, dataType string, explicitApp string) []developmentSelection {
	explicitApp = strings.TrimSpace(explicitApp)
	if explicitApp != "" {
		return []developmentSelection{{ApplicationType: explicitApp, DataType: dataType}}
	}
	summaryMap, _ := summary["summary"].(map[string]any)
	entry, _ := summaryMap[dataType].(map[string]any)
	if entry == nil {
		return nil
	}
	overall, _ := entry["overall"].(map[string]any)
	if intFromAny(overall["count"], 0) == 0 {
		return nil
	}
	byInstanceType, _ := entry["byInstanceType"].(map[string]any)
	if len(byInstanceType) == 0 {
		return []developmentSelection{{ApplicationType: developmentDefaultApplicationType(dataType), DataType: dataType}}
	}
	names := make([]string, 0, len(byInstanceType))
	for name := range byInstanceType {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]developmentSelection, 0, len(names))
	for _, name := range names {
		out = append(out, developmentSelection{ApplicationType: name, DataType: dataType})
	}
	return out
}

func developmentDefaultApplicationType(dataType string) string {
	switch dataType {
	case "build":
		return "bamboo"
	case "review":
		return "crucible"
	default:
		return "stash"
	}
}

func developmentDetailEntries(detail map[string]any) []any {
	entries, _ := detail["detail"].([]any)
	return entries
}
