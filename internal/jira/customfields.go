package jira

import (
	"strconv"
	"strings"
)

type fieldMetadataClient interface {
	GetEditMeta(issue string) (map[string]any, error)
	GetIssue(issue string, fields string, expand string) (map[string]any, error)
	GetCreateMetaIssueTypeFields(projectKey, issueTypeID string) ([]map[string]any, error)
	ListCreateMetaIssueTypes(projectKey string) ([]map[string]any, error)
	ListFields() ([]map[string]any, error)
}

var builtinFieldAliases = map[string]string{
	"summary":      "summary",
	"description":  "description",
	"labels":       "labels",
	"components":   "components",
	"versions":     "versions",
	"fixversions":  "fixVersions",
	"fix versions": "fixVersions",
	"issuetype":    "issuetype",
	"issue type":   "issuetype",
	"priority":     "priority",
	"resolution":   "resolution",
	"project":      "project",
	"parent":       "parent",
	"reporter":     "reporter",
	"assignee":     "assignee",
	"duedate":      "duedate",
	"due date":     "duedate",
	"environment":  "environment",
	"timetracking": "timetracking",
	"epic link":    "Epic Link",
	"epiclink":     "Epic Link",
	"sprint":       "Sprint",
	"rank":         "Rank",
	"story points": "Story Points",
	"storypoint":   "Story Points",
	"storypoints":  "Story Points",
}

var directStandardFields = map[string]bool{
	"summary":      true,
	"description":  true,
	"labels":       true,
	"components":   true,
	"versions":     true,
	"fixVersions":  true,
	"issuetype":    true,
	"priority":     true,
	"resolution":   true,
	"project":      true,
	"parent":       true,
	"reporter":     true,
	"assignee":     true,
	"duedate":      true,
	"environment":  true,
	"timetracking": true,
}

func canonicalFieldAlias(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return ""
	}
	if canonical, ok := builtinFieldAliases[key]; ok {
		return canonical
	}
	return strings.TrimSpace(name)
}

func isCustomFieldID(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "customfield_")
}

type fieldResolver struct {
	client       fieldMetadataClient
	issueID      string
	projectKey   string
	issueTypeID  string
	issueTypeRaw string
	editMeta     map[string]any
	editLoaded   bool
	createMeta   []map[string]any
	createLoaded bool
	allFields    []map[string]any
	fieldsLoaded bool
}

type resolvedField struct {
	ID    string
	Entry map[string]any
}

func newIssueFieldResolver(client fieldMetadataClient, issueID string) *fieldResolver {
	return &fieldResolver{client: client, issueID: strings.TrimSpace(issueID)}
}

func newCreateFieldResolver(client fieldMetadataClient, fields map[string]any) *fieldResolver {
	resolver := &fieldResolver{client: client}
	if fields == nil {
		return resolver
	}
	if project, ok := fields["project"].(map[string]any); ok {
		resolver.projectKey = normalizeMaybeString(project["key"])
		if resolver.projectKey == "" {
			resolver.projectKey = normalizeMaybeString(project["id"])
		}
	}
	if issueType, ok := fields["issuetype"].(map[string]any); ok {
		resolver.issueTypeID = normalizeMaybeString(issueType["id"])
		resolver.issueTypeRaw = normalizeMaybeString(issueType["name"])
	}
	return resolver
}

func (r *fieldResolver) Resolve(field string) (string, error) {
	resolved, err := r.ResolveWithEntry(field)
	if err != nil {
		return "", err
	}
	return resolved.ID, nil
}

func (r *fieldResolver) ResolveWithEntry(field string) (resolvedField, error) {
	field = canonicalFieldAlias(field)
	if field == "" || isCustomFieldID(field) {
		return resolvedField{ID: field}, nil
	}
	lower := strings.ToLower(strings.TrimSpace(field))
	if canonical, ok := builtinFieldAliases[lower]; ok && directStandardFields[canonical] {
		return resolvedField{ID: canonical}, nil
	}

	if r.issueID != "" {
		if resolved, ok, err := r.lookupEditMeta(field); err != nil {
			return resolvedField{}, err
		} else if ok {
			return resolved, nil
		}
		if resolved, ok, err := r.lookupCreateMetaFromIssue(field); err != nil {
			return resolvedField{}, err
		} else if ok {
			return resolved, nil
		}
	}

	if resolved, ok, err := r.lookupCreateMeta(field); err != nil {
		return resolvedField{}, err
	} else if ok {
		return resolved, nil
	}

	if resolved, ok, err := r.lookupAllFields(field); err != nil {
		return resolvedField{}, err
	} else if ok {
		return resolved, nil
	}
	return resolvedField{ID: field}, nil
}

func (r *fieldResolver) lookupEditMeta(field string) (resolvedField, bool, error) {
	if r.client == nil || r.issueID == "" {
		return resolvedField{}, false, nil
	}
	if !r.editLoaded {
		editMeta, err := r.client.GetEditMeta(r.issueID)
		if err != nil {
			return resolvedField{}, false, err
		}
		r.editMeta = editMeta
		r.editLoaded = true
	}
	fields, _ := r.editMeta["fields"].(map[string]any)
	if fieldID, entry := findEditMetaField(fields, field); fieldID != "" {
		return resolvedField{ID: fieldID, Entry: entry}, true, nil
	}
	return resolvedField{}, false, nil
}

func (r *fieldResolver) lookupCreateMetaFromIssue(field string) (resolvedField, bool, error) {
	if r.client == nil || r.issueID == "" {
		return resolvedField{}, false, nil
	}
	if r.projectKey != "" && (r.issueTypeID != "" || r.issueTypeRaw != "") {
		return r.lookupCreateMeta(field)
	}
	issue, err := r.client.GetIssue(r.issueID, "project,issuetype", "")
	if err != nil {
		return resolvedField{}, false, err
	}
	r.projectKey, r.issueTypeID = issueProjectAndType(issue)
	if r.issueTypeID == "" {
		fields, _ := issue["fields"].(map[string]any)
		if issueType, ok := fields["issuetype"].(map[string]any); ok {
			r.issueTypeRaw = normalizeMaybeString(issueType["name"])
		}
	}
	return r.lookupCreateMeta(field)
}

func (r *fieldResolver) lookupCreateMeta(field string) (resolvedField, bool, error) {
	if r.client == nil || strings.TrimSpace(r.projectKey) == "" {
		return resolvedField{}, false, nil
	}
	if r.issueTypeID == "" && r.issueTypeRaw != "" {
		items, err := r.client.ListCreateMetaIssueTypes(r.projectKey)
		if err != nil {
			return resolvedField{}, false, err
		}
		for _, item := range items {
			if strings.EqualFold(normalizeMaybeString(item["name"]), r.issueTypeRaw) {
				r.issueTypeID = normalizeMaybeString(item["id"])
				break
			}
		}
	}
	if r.issueTypeID == "" {
		return resolvedField{}, false, nil
	}
	if !r.createLoaded {
		items, err := r.client.GetCreateMetaIssueTypeFields(r.projectKey, r.issueTypeID)
		if err != nil {
			return resolvedField{}, false, err
		}
		r.createMeta = items
		r.createLoaded = true
	}
	if fieldID, entry := findCreateMetaField(r.createMeta, field); fieldID != "" {
		return resolvedField{ID: fieldID, Entry: entry}, true, nil
	}
	return resolvedField{}, false, nil
}

func (r *fieldResolver) lookupAllFields(field string) (resolvedField, bool, error) {
	if r.client == nil {
		return resolvedField{}, false, nil
	}
	if !r.fieldsLoaded {
		items, err := r.client.ListFields()
		if err != nil {
			return resolvedField{}, false, err
		}
		r.allFields = items
		r.fieldsLoaded = true
	}
	for _, item := range r.allFields {
		id := normalizeMaybeString(item["id"])
		name := normalizeMaybeString(item["name"])
		if strings.EqualFold(id, field) || strings.EqualFold(name, field) {
			return resolvedField{ID: id, Entry: item}, true, nil
		}
	}
	return resolvedField{}, false, nil
}

func resolveFieldMapKeys(fields map[string]any, resolver *fieldResolver) (map[string]any, error) {
	if len(fields) == 0 || resolver == nil {
		return fields, nil
	}
	resolved := make(map[string]any, len(fields))
	for key, value := range fields {
		fieldID, err := resolver.Resolve(key)
		if err != nil {
			return nil, err
		}
		resolved[fieldID] = value
	}
	return resolved, nil
}

func applyResolvedSetOp(field, op, value string, entry map[string]any, fields, currentFields map[string]any) error {
	if op == OpSet && isCustomFieldID(field) {
		if coerced, ok := coerceCustomFieldValue(value, entry); ok {
			fields[field] = coerced
			return nil
		}
	}
	return applySetOp(field, op, value, fields, currentFields)
}

func coerceCustomFieldValue(value string, entry map[string]any) (any, bool) {
	schema, _ := entry["schema"].(map[string]any)
	schemaType := strings.ToLower(normalizeMaybeString(schema["type"]))
	switch schemaType {
	case "number", "float", "double":
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, false
		}
		return parsed, true
	case "integer", "int":
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return nil, false
		}
		return parsed, true
	}
	return nil, false
}
