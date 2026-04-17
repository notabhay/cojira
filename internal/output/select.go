package output

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ApplySelect applies the global selection expression to data. The selection
// syntax is a comma-separated list of dotted paths with optional numeric array
// indexes, for example: "result.issues.0.key,result.total".
func ApplySelect(data any) any {
	expr := strings.TrimSpace(GetSelect())
	if expr == "" || data == nil {
		return data
	}

	var generic any
	raw, err := json.Marshal(data)
	if err != nil {
		return data
	}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return data
	}

	paths := splitSelectPaths(expr)
	if len(paths) == 0 {
		return data
	}
	if len(paths) == 1 {
		if value, ok := selectPath(generic, paths[0]); ok {
			return value
		}
		return nil
	}

	result := map[string]any{}
	for _, path := range paths {
		if value, ok := selectPath(generic, path); ok {
			result[path] = value
		}
	}
	return result
}

func splitSelectPaths(expr string) []string {
	parts := strings.Split(expr, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func selectPath(data any, path string) (any, bool) {
	current := data
	for _, segment := range strings.Split(path, ".") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return nil, false
		}
		next, ok := selectSegment(current, segment)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func selectSegment(current any, segment string) (any, bool) {
	name := segment
	indexes := []int{}
	for {
		open := strings.Index(name, "[")
		if open < 0 {
			break
		}
		close := strings.Index(name[open:], "]")
		if close < 0 {
			return nil, false
		}
		close += open
		if open == 0 && len(name) == close+1 {
			parsed, err := strconv.Atoi(name[open+1 : close])
			if err != nil {
				return nil, false
			}
			name = ""
			indexes = append(indexes, parsed)
			break
		}
		parsed, err := strconv.Atoi(name[open+1 : close])
		if err != nil {
			return nil, false
		}
		name = name[:open]
		indexes = append(indexes, parsed)
		break
	}

	if name != "" {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[name]
		if !ok {
			return nil, false
		}
	}

	for _, index := range indexes {
		items, ok := current.([]any)
		if !ok || index < 0 || index >= len(items) {
			return nil, false
		}
		current = items[index]
	}
	return current, true
}
