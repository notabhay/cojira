package jira

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

func safeJoinUnder(base string, elems ...string) (string, error) {
	baseClean := filepath.Clean(base)
	joined := baseClean
	for _, elem := range elems {
		if elem == "" {
			continue
		}
		cleaned := filepath.Clean(elem)
		if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path %q escapes the base directory", elem)
		}
		joined = filepath.Join(joined, cleaned)
	}

	baseAbs, err := filepath.Abs(baseClean)
	if err != nil {
		return "", err
	}
	joinedAbs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(baseAbs, joinedAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the base directory", joined)
	}
	return joined, nil
}

func findMatchingDirs(root string, pattern string) ([]string, error) {
	root = filepath.Clean(root)
	pattern = normalizeSlashPattern(pattern)
	var matches []string
	err := filepath.WalkDir(root, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		rel = normalizeSlashPattern(rel)
		if rel == "" {
			return nil
		}
		ok, err := matchDoublestarPattern(pattern, rel)
		if err != nil {
			return err
		}
		if ok {
			matches = append(matches, current)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func normalizeSlashPattern(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = filepath.ToSlash(filepath.Clean(value))
	value = strings.TrimPrefix(value, "./")
	if value == "." {
		return ""
	}
	return strings.Trim(value, "/")
}

func matchDoublestarPattern(pattern, candidate string) (bool, error) {
	patternSegments := splitSlashPattern(pattern)
	candidateSegments := splitSlashPattern(candidate)
	return matchDoublestarSegments(patternSegments, candidateSegments)
}

func splitSlashPattern(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, "/")
}

func matchDoublestarSegments(patterns, candidates []string) (bool, error) {
	if len(patterns) == 0 {
		return len(candidates) == 0, nil
	}
	if patterns[0] == "**" {
		if len(patterns) == 1 {
			return true, nil
		}
		for i := 0; i <= len(candidates); i++ {
			matched, err := matchDoublestarSegments(patterns[1:], candidates[i:])
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
		return false, nil
	}
	if len(candidates) == 0 {
		return false, nil
	}
	matched, err := path.Match(patterns[0], candidates[0])
	if err != nil {
		return false, err
	}
	if !matched {
		return false, nil
	}
	return matchDoublestarSegments(patterns[1:], candidates[1:])
}
