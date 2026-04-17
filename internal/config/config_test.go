package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadProjectConfigMergesAncestorConfigs(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(child, 0o755))

	rootCfg := filepath.Join(root, ConfigFilename)
	childCfg := filepath.Join(root, "a", ConfigFilename)
	require.NoError(t, os.WriteFile(rootCfg, []byte(`{"jira":{"default_project":"ROOT"},"aliases":{"base":"jira whoami"}}`), 0o644))
	require.NoError(t, os.WriteFile(childCfg, []byte(`{"jira":{"default_jql_scope":"project = CHILD"},"aliases":{"local":"jira info PROJ-1"}}`), 0o644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(child))
	defer func() { _ = os.Chdir(origDir) }()

	loaded, err := LoadProjectConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "ROOT", loaded.GetValue([]string{"jira", "default_project"}, nil))
	assert.Equal(t, "project = CHILD", loaded.GetValue([]string{"jira", "default_jql_scope"}, nil))
	assert.Equal(t, "jira whoami", loaded.GetAlias("base"))
	assert.Equal(t, "jira info PROJ-1", loaded.GetAlias("local"))
}

func TestGetStringMapAndObject(t *testing.T) {
	cfg := &ProjectConfig{
		Data: map[string]any{
			"jira": map[string]any{
				"saved_queries": map[string]any{
					"mine": "assignee = currentUser()",
					"bad":  42,
				},
				"templates": map[string]any{
					"bug": map[string]any{"summary_prefix": "[Bug]"},
				},
			},
		},
	}

	assert.Equal(t, map[string]string{"mine": "assignee = currentUser()"}, cfg.GetStringMap([]string{"jira", "saved_queries"}))
	assert.Equal(t, map[string]any{"summary_prefix": "[Bug]"}, cfg.GetObject([]string{"jira", "templates", "bug"}))
	assert.Equal(t, map[string]any{}, cfg.GetObject([]string{"jira", "templates", "missing"}))
}

func TestLoadWritableProjectConfigCreatesInCwdWhenMissing(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origDir) }()

	cfg, err := LoadWritableProjectConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, ConfigFilename, filepath.Base(cfg.Path))
	assert.True(t, strings.HasSuffix(cfg.Path, filepath.Join(filepath.Base(dir), ConfigFilename)))
	assert.Equal(t, map[string]any{}, cfg.Data)
}

func TestNearestConfigPathPrefersClosestAncestor(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(child, 0o755))
	rootCfg := filepath.Join(root, ConfigFilename)
	childCfg := filepath.Join(root, "a", ConfigFilename)
	require.NoError(t, os.WriteFile(rootCfg, []byte(`{"jira":{"default_project":"ROOT"}}`), 0o644))
	require.NoError(t, os.WriteFile(childCfg, []byte(`{"jira":{"default_project":"CHILD"}}`), 0o644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(child))
	defer func() { _ = os.Chdir(origDir) }()

	assert.Equal(t, ConfigFilename, filepath.Base(NearestConfigPath()))
	assert.True(t, strings.HasSuffix(NearestConfigPath(), filepath.Join("a", ConfigFilename)))
}

func TestWriteProjectConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ConfigFilename)
	data := map[string]any{
		"jira": map[string]any{
			"default_project": "PROJ",
		},
	}

	require.NoError(t, WriteProjectConfig(path, data))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"default_project": "PROJ"`)
	assert.True(t, strings.HasSuffix(string(raw), "\n"))
}
