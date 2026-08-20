package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeConfigs(t *testing.T) {
	// Create temp directories
	tmpDir := t.TempDir()
	baseDir := filepath.Join(tmpDir, "base")
	teamDir := filepath.Join(tmpDir, "team")
	userDir := filepath.Join(tmpDir, "user")

	require.NoError(t, os.MkdirAll(baseDir, 0755))
	require.NoError(t, os.MkdirAll(teamDir, 0755))
	require.NoError(t, os.MkdirAll(userDir, 0755))

	// Base config
	baseConfig := `{
		"name": "base",
		"marketplaces": {
			"official": {"url": "https://github.com/official/plugins"},
			"shared": {"url": "https://github.com/shared/plugins"}
		},
		"enabled_plugins": ["plugin-a@official"]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "settings.json"), []byte(baseConfig), 0644))

	// Team config (overrides shared marketplace)
	teamConfig := `{
		"name": "team",
		"marketplaces": {
			"shared": {"url": "https://github.com/team/plugins"},
			"team-marketplace": {"url": "https://github.com/team/marketplace"}
		},
		"enabled_plugins": ["plugin-b@shared", "plugin-c@team-marketplace"]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(teamDir, "settings.json"), []byte(teamConfig), 0644))

	// User config (adds personal marketplace)
	userConfig := `{
		"name": "user",
		"marketplaces": {
			"my-plugins": {"url": "https://github.com/user/my-plugins"}
		},
		"enabled_plugins": ["plugin-d@my-plugins"]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "settings.json"), []byte(userConfig), 0644))

	// Merge
	result, err := MergeConfigs([]string{baseDir, teamDir, userDir}, MergeOptions{})
	require.NoError(t, err)

	// Verify marketplaces
	assert.Len(t, result.Marketplaces, 4)
	assert.Equal(t, "https://github.com/official/plugins", result.Marketplaces["official"].URL)         // Base
	assert.Equal(t, "https://github.com/team/plugins", result.Marketplaces["shared"].URL)               // Team override
	assert.Equal(t, "https://github.com/team/marketplace", result.Marketplaces["team-marketplace"].URL) // Team
	assert.Equal(t, "https://github.com/user/my-plugins", result.Marketplaces["my-plugins"].URL)        // User

	// Verify enabled plugins (union)
	assert.Len(t, result.EnabledPlugins, 4)
	assert.Contains(t, result.EnabledPlugins, "plugin-a@official")
	assert.Contains(t, result.EnabledPlugins, "plugin-b@shared")
	assert.Contains(t, result.EnabledPlugins, "plugin-c@team-marketplace")
	assert.Contains(t, result.EnabledPlugins, "plugin-d@my-plugins")
}

func TestMergeConfigsMultipleFilesPerDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple files in same directory
	file1 := `{"marketplaces": {"marketplace-a": {"url": "https://a.example.com"}}}`
	file2 := `{"marketplaces": {"marketplace-b": {"url": "https://b.example.com"}}}`

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "01-a.json"), []byte(file1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "02-b.json"), []byte(file2), 0644))

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)

	assert.Len(t, result.Marketplaces, 2)
	assert.Equal(t, "https://a.example.com", result.Marketplaces["marketplace-a"].URL)
	assert.Equal(t, "https://b.example.com", result.Marketplaces["marketplace-b"].URL)
}

func TestMergeConfigsPluginsUnion(t *testing.T) {
	tmpDir := t.TempDir()
	dir1 := filepath.Join(tmpDir, "dir1")
	dir2 := filepath.Join(tmpDir, "dir2")

	require.NoError(t, os.MkdirAll(dir1, 0755))
	require.NoError(t, os.MkdirAll(dir2, 0755))

	// Same plugin in both directories (should only appear once)
	config1 := `{"enabled_plugins": ["plugin-a@marketplace", "plugin-b@marketplace"]}`
	config2 := `{"enabled_plugins": ["plugin-b@marketplace", "plugin-c@marketplace"]}`

	require.NoError(t, os.WriteFile(filepath.Join(dir1, "settings.json"), []byte(config1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "settings.json"), []byte(config2), 0644))

	result, err := MergeConfigs([]string{dir1, dir2}, MergeOptions{})
	require.NoError(t, err)

	// Should have 3 unique plugins
	assert.Len(t, result.EnabledPlugins, 3)
	assert.Contains(t, result.EnabledPlugins, "plugin-a@marketplace")
	assert.Contains(t, result.EnabledPlugins, "plugin-b@marketplace")
	assert.Contains(t, result.EnabledPlugins, "plugin-c@marketplace")
}

func TestMergeConfigsMissingDir(t *testing.T) {
	// Missing directories should be skipped
	result, err := MergeConfigs([]string{"/nonexistent/dir"}, MergeOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.Marketplaces)
	assert.Empty(t, result.EnabledPlugins)
}

func TestMergeConfigsEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.Marketplaces)
	assert.Empty(t, result.EnabledPlugins)
}

func TestMergeConfigsEmptyInputDirs(t *testing.T) {
	result, err := MergeConfigs([]string{}, MergeOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.Marketplaces)
	assert.Empty(t, result.EnabledPlugins)
}

func TestMergeConfigsSkipsNonJsonFiles(t *testing.T) {
	tmpDir := t.TempDir()

	jsonConfig := `{"marketplaces": {"marketplace": {"url": "https://example.com"}}}`
	txtContent := `This is not JSON`

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(jsonConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte(txtContent), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(txtContent), 0644))

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)

	assert.Len(t, result.Marketplaces, 1)
	assert.Equal(t, "https://example.com", result.Marketplaces["marketplace"].URL)
}

func TestMergeConfigsInvalidJson(t *testing.T) {
	tmpDir := t.TempDir()

	invalidJson := `{"marketplaces": {invalid json`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "invalid.json"), []byte(invalidJson), 0644))

	_, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestMergeConfigsSkipsSubdirectories(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	mainConfig := `{"marketplaces": {"main": {"url": "https://main.example.com"}}}`
	subConfig := `{"marketplaces": {"sub": {"url": "https://sub.example.com"}}}`

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "main.json"), []byte(mainConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "sub.json"), []byte(subConfig), 0644))

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)

	// Should only include main, not sub
	assert.Len(t, result.Marketplaces, 1)
	assert.Equal(t, "https://main.example.com", result.Marketplaces["main"].URL)
}

func TestMergeConfigsVerboseLogging(t *testing.T) {
	tmpDir := t.TempDir()

	config := `{"marketplaces": {"marketplace": {"url": "https://example.com"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(config), 0644))

	var logs []string
	opts := MergeOptions{
		Verbose: true,
		Logger: func(format string, args ...interface{}) {
			logs = append(logs, format)
		},
	}

	_, err := MergeConfigs([]string{tmpDir}, opts)
	require.NoError(t, err)

	assert.NotEmpty(t, logs)
}

func TestMergeConfigsEmptyDirInList(t *testing.T) {
	tmpDir := t.TempDir()

	config := `{"marketplaces": {"marketplace": {"url": "https://example.com"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(config), 0644))

	// Include empty string in dirs list
	result, err := MergeConfigs([]string{"", tmpDir, ""}, MergeOptions{})
	require.NoError(t, err)

	assert.Len(t, result.Marketplaces, 1)
}

func TestWriteConfig(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "output", "merged.json")

	config := &SettingsConfig{
		Marketplaces: map[string]MarketplaceConfig{
			"test": {URL: "https://example.com"},
		},
		EnabledPlugins: []string{"plugin@test"},
	}

	err := WriteConfig(config, outputPath)
	require.NoError(t, err)

	// Verify file was created
	assert.FileExists(t, outputPath)

	// Verify content
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	var result settingsJSON
	require.NoError(t, json.Unmarshal(data, &result))

	assert.Len(t, result.Marketplaces, 1)
	assert.Equal(t, "https://example.com", result.Marketplaces["test"].URL)
	assert.Len(t, result.EnabledPlugins, 1)
	assert.Equal(t, "plugin@test", result.EnabledPlugins[0])
}

func TestWriteConfigCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "deep", "nested", "dir", "merged.json")

	config := &SettingsConfig{
		Marketplaces:   map[string]MarketplaceConfig{},
		EnabledPlugins: []string{},
	}

	err := WriteConfig(config, outputPath)
	require.NoError(t, err)

	assert.FileExists(t, outputPath)
}

func TestMergeAndWrite(t *testing.T) {
	tmpDir := t.TempDir()
	inputDir := filepath.Join(tmpDir, "input")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	config := `{
		"marketplaces": {"marketplace": {"url": "https://example.com"}},
		"enabled_plugins": ["plugin@marketplace"]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "config.json"), []byte(config), 0644))

	outputPath := filepath.Join(tmpDir, "output", "merged.json")

	err := MergeAndWrite([]string{inputDir}, outputPath, MergeOptions{})
	require.NoError(t, err)

	assert.FileExists(t, outputPath)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	var result settingsJSON
	require.NoError(t, json.Unmarshal(data, &result))

	assert.Len(t, result.Marketplaces, 1)
	assert.Equal(t, "https://example.com", result.Marketplaces["marketplace"].URL)
	assert.Len(t, result.EnabledPlugins, 1)
	assert.Equal(t, "plugin@marketplace", result.EnabledPlugins[0])
}

func TestMergeConfigsOverrideOrder(t *testing.T) {
	tmpDir := t.TempDir()
	dir1 := filepath.Join(tmpDir, "dir1")
	dir2 := filepath.Join(tmpDir, "dir2")
	dir3 := filepath.Join(tmpDir, "dir3")

	require.NoError(t, os.MkdirAll(dir1, 0755))
	require.NoError(t, os.MkdirAll(dir2, 0755))
	require.NoError(t, os.MkdirAll(dir3, 0755))

	// Same marketplace name in all directories with different URLs
	config1 := `{"marketplaces": {"marketplace": {"url": "https://dir1.example.com"}}}`
	config2 := `{"marketplaces": {"marketplace": {"url": "https://dir2.example.com"}}}`
	config3 := `{"marketplaces": {"marketplace": {"url": "https://dir3.example.com"}}}`

	require.NoError(t, os.WriteFile(filepath.Join(dir1, "config.json"), []byte(config1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "config.json"), []byte(config2), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir3, "config.json"), []byte(config3), 0644))

	// dir3 should win (last in list)
	result, err := MergeConfigs([]string{dir1, dir2, dir3}, MergeOptions{})
	require.NoError(t, err)

	assert.Equal(t, "https://dir3.example.com", result.Marketplaces["marketplace"].URL)
}

func TestMergeConfigsNilMarketplace(t *testing.T) {
	tmpDir := t.TempDir()

	// Config with nil marketplace value
	config := `{
		"marketplaces": {
			"valid": {"url": "https://example.com"},
			"invalid": null
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(config), 0644))

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)

	// Should only include valid marketplace
	assert.Len(t, result.Marketplaces, 1)
	assert.Equal(t, "https://example.com", result.Marketplaces["valid"].URL)
}

func TestMergeConfigsEmptyPluginString(t *testing.T) {
	tmpDir := t.TempDir()

	// Config with empty plugin string
	config := `{"enabled_plugins": ["valid-plugin@marketplace", "", "another-plugin@marketplace"]}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(config), 0644))

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)

	// Should only include non-empty plugins
	assert.Len(t, result.EnabledPlugins, 2)
	assert.Contains(t, result.EnabledPlugins, "valid-plugin@marketplace")
	assert.Contains(t, result.EnabledPlugins, "another-plugin@marketplace")
}

func TestMergeConfigsPluginsSorted(t *testing.T) {
	tmpDir := t.TempDir()
	dir1 := filepath.Join(tmpDir, "dir1")
	dir2 := filepath.Join(tmpDir, "dir2")

	require.NoError(t, os.MkdirAll(dir1, 0755))
	require.NoError(t, os.MkdirAll(dir2, 0755))

	config1 := `{"enabled_plugins": ["zebra@marketplace", "alpha@marketplace"]}`
	config2 := `{"enabled_plugins": ["middle@marketplace"]}`

	require.NoError(t, os.WriteFile(filepath.Join(dir1, "settings.json"), []byte(config1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "settings.json"), []byte(config2), 0644))

	result, err := MergeConfigs([]string{dir1, dir2}, MergeOptions{})
	require.NoError(t, err)

	// Should be sorted alphabetically
	assert.Equal(t, []string{"alpha@marketplace", "middle@marketplace", "zebra@marketplace"}, result.EnabledPlugins)
}
