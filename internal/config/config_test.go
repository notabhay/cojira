package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadProjectConfigValid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ConfigFilename)
	err := os.WriteFile(cfgPath, []byte(`{
  "jira": {"default_project": "PROJ", "default_jql_scope": "project = PROJ"},
  "confluence": {"default_space": "TEAM", "root_page_id": "12345"}
}`), 0o644)
	require.NoError(t, err)

	loaded, err := LoadProjectConfig([]string{cfgPath})
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, cfgPath, loaded.Path)
	assert.Equal(t, "PROJ", loaded.GetValue([]string{"jira", "default_project"}, nil))
	assert.Equal(t, "12345", loaded.GetValue([]string{"confluence", "root_page_id"}, nil))
}

func TestLoadProjectConfigInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ConfigFilename)
	require.NoError(t, os.WriteFile(cfgPath, []byte("{"), 0o644))

	_, err := LoadProjectConfig([]string{cfgPath})
	require.Error(t, err)
	var cfgErr *ConfigError
	require.True(t, errors.As(err, &cfgErr))
	assert.Equal(t, CodeConfigInvalid, cfgErr.Code)
}

func TestLoadProjectConfigEmpty(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ConfigFilename)
	require.NoError(t, os.WriteFile(cfgPath, []byte("   "), 0o644))

	_, err := LoadProjectConfig([]string{cfgPath})
	require.Error(t, err)
	var cfgErr *ConfigError
	require.True(t, errors.As(err, &cfgErr))
	assert.Equal(t, CodeConfigInvalid, cfgErr.Code)
}

func TestLoadProjectConfigNotObject(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ConfigFilename)
	require.NoError(t, os.WriteFile(cfgPath, []byte(`[1, 2, 3]`), 0o644))

	_, err := LoadProjectConfig([]string{cfgPath})
	require.Error(t, err)
	var cfgErr *ConfigError
	require.True(t, errors.As(err, &cfgErr))
	assert.Equal(t, CodeConfigInvalid, cfgErr.Code)
}

func TestLoadProjectConfigNoFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ConfigFilename)

	loaded, err := LoadProjectConfig([]string{cfgPath})
	assert.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestLoadProjectConfigInvalidSection(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ConfigFilename)
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"jira": "not a map"}`), 0o644))

	_, err := LoadProjectConfig([]string{cfgPath})
	require.Error(t, err)
	var cfgErr *ConfigError
	require.True(t, errors.As(err, &cfgErr))
	assert.Equal(t, CodeConfigInvalid, cfgErr.Code)
	assert.Contains(t, cfgErr.Message, "jira")
}

func TestLoadProjectConfigPicksFirstExisting(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	path1 := filepath.Join(dir1, ConfigFilename) // does not exist
	path2 := filepath.Join(dir2, ConfigFilename)
	require.NoError(t, os.WriteFile(path2, []byte(`{"jira": {"x": 1}}`), 0o644))

	loaded, err := LoadProjectConfig([]string{path1, path2})
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, path2, loaded.Path)
}

func TestGetSection(t *testing.T) {
	cfg := &ProjectConfig{
		Data: map[string]any{
			"jira": map[string]any{"key": "val"},
			"bad":  "not a map",
		},
	}

	assert.Equal(t, map[string]any{"key": "val"}, cfg.GetSection("jira"))
	assert.Equal(t, map[string]any{}, cfg.GetSection("missing"))
	assert.Equal(t, map[string]any{}, cfg.GetSection("bad"))
}

func TestGetValue(t *testing.T) {
	cfg := &ProjectConfig{
		Data: map[string]any{
			"jira": map[string]any{
				"default_project": "PROJ",
			},
			"scalar": 42,
		},
	}

	assert.Equal(t, "PROJ", cfg.GetValue([]string{"jira", "default_project"}, nil))
	assert.Equal(t, "fallback", cfg.GetValue([]string{"jira", "missing"}, "fallback"))
	assert.Equal(t, "fallback", cfg.GetValue([]string{"scalar", "sub"}, "fallback"))
	assert.Equal(t, "fallback", cfg.GetValue([]string{"nope"}, "fallback"))
}

func TestGetAlias(t *testing.T) {
	cfg := &ProjectConfig{
		Data: map[string]any{
			"aliases": map[string]any{
				"my-board":   "jira board-issues 45434 --all",
				"empty":      "",
				"whitespace": "   ",
				"not-string": 42,
			},
		},
	}

	assert.Equal(t, "jira board-issues 45434 --all", cfg.GetAlias("my-board"))
	assert.Equal(t, "", cfg.GetAlias("empty"))
	assert.Equal(t, "", cfg.GetAlias("whitespace"))
	assert.Equal(t, "", cfg.GetAlias("not-string"))
	assert.Equal(t, "", cfg.GetAlias("missing"))
}

func TestGetValueNested(t *testing.T) {
	cfg := &ProjectConfig{
		Data: map[string]any{
			"a": map[string]any{
				"b": map[string]any{
					"c": "deep",
				},
			},
		},
	}

	assert.Equal(t, "deep", cfg.GetValue([]string{"a", "b", "c"}, nil))
	assert.Nil(t, cfg.GetValue([]string{"a", "b", "d"}, nil))
}

func TestNullSectionsAreValid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ConfigFilename)
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"jira": null, "aliases": null}`), 0o644))

	loaded, err := LoadProjectConfig([]string{cfgPath})
	require.NoError(t, err)
	require.NotNil(t, loaded)
}
