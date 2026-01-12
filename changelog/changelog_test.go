package changelog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionLine(t *testing.T) {
	// Test the version line regex
	if !versionRE.MatchString("# [v1.0.0]") {
		t.Error("versionRE failed to match")
	}
}

func TestParseBulletOverride(t *testing.T) {
	line := "- added override [[PR]](https://github.com/OffchainLabs/prysm/pull/1)"
	res := parseBullet(line, "NOPE")
	if line != res {
		t.Error("parseBullet did not recognize the override")
	}
}

func TestSectionRe(t *testing.T) {
	cases := []struct {
		value string
		valid bool
	}{
		{value: "####### Fixes", valid: false}, // there's no H7
		{value: " Fixes", valid: false},
		{value: "# Fixes", valid: true},
		{value: "## Fixes", valid: true},
		{value: "### Fixes", valid: true},
		{value: "#### Fixes", valid: true},
		{value: "##### Fixes", valid: true},
		{value: "###### Fixes", valid: true},
	}
	for _, c := range cases {
		if valid := sectionRE.MatchString(c.value); valid != c.valid {
			t.Errorf("sectionRE failed to match %v", c)
		}
	}
}

func TestParseFragment_CustomSections(t *testing.T) {
	// 1. Setup default vs custom maps
	defaultMap := make(map[string]bool)
	for _, s := range Sections {
		defaultMap[s] = true
	}

	customMap := make(map[string]bool)
	for _, s := range Sections {
		customMap[s] = true
	}
	customMap["Configuration"] = true
	customMap["SpecialFeature"] = true

	tests := []struct {
		name          string
		lines         []string
		validSections map[string]bool
		expectKey     string
		expectCount   int
	}{
		{
			name:          "Standard Fixed Section (Default)",
			lines:         []string{"### Fixed", "- bug fix"},
			validSections: defaultMap,
			expectKey:     "Fixed",
			expectCount:   1,
		},
		{
			name:          "Configuration Section (Default - Should Fail/Ignore)",
			lines:         []string{"### Configuration", "- added flag"},
			validSections: defaultMap,
			expectKey:     "Configuration",
			expectCount:   0, // Should be ignored as it's not in defaultMap
		},
		{
			name:          "Configuration Section (Custom - Should Pass)",
			lines:         []string{"### Configuration", "- added flag"},
			validSections: customMap,
			expectKey:     "Configuration",
			expectCount:   1,
		},
		{
			name:          "SpecialFeature Section (Custom - Should Pass)",
			lines:         []string{"### SpecialFeature", "- cool stuff"},
			validSections: customMap,
			expectKey:     "SpecialFeature",
			expectCount:   1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// We pass a dummy PR string "PR-LINK"
			res := ParseFragment(tc.lines, "PR-LINK", tc.validSections)
			if len(res[tc.expectKey]) != tc.expectCount {
				t.Errorf("expected %d entries for section '%s', got %d", tc.expectCount, tc.expectKey, len(res[tc.expectKey]))
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Create a temp dir to mock the repo structure
	tmpDir := t.TempDir()
	changelogDir := filepath.Join(tmpDir, "changelog")
	err := os.Mkdir(changelogDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Define config content
	configContent := []byte("sections:\n  - Added\n  - CustomOne\n  - Configuration")
	configPath := filepath.Join(changelogDir, ".unclog.yaml")
	err = os.WriteFile(configPath, configContent, 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt to load
	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected config to be non-nil")
	}

	// Verify contents
	expectedLen := 3
	if len(cfg.Sections) != expectedLen {
		t.Errorf("expected %d sections, got %d", expectedLen, len(cfg.Sections))
	}

	foundConfig := false
	for _, s := range cfg.Sections {
		if s == "Configuration" {
			foundConfig = true
		}
	}
	if !foundConfig {
		t.Error("expected 'Configuration' in loaded sections")
	}
}

func TestLoadConfig_Malformed(t *testing.T) {
	tmpDir := t.TempDir()
	changelogDir := filepath.Join(tmpDir, "changelog")
	err := os.Mkdir(changelogDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Write invalid YAML content (tab indentation is forbidden in YAML, or just garbage)
	configContent := []byte("sections:\n\t- Added") // Tabs are illegal in YAML
	configPath := filepath.Join(changelogDir, ".unclog.yaml")
	err = os.WriteFile(configPath, configContent, 0644)
	if err != nil {
		t.Fatal(err)
	}

	_, err = LoadConfig(tmpDir)
	if err == nil {
		t.Error("expected error when loading malformed yaml, got nil")
	}
}

func TestValidSections_Logic(t *testing.T) {
	// Setup a custom allowed list
	allowed := map[string]bool{
		"Added": true,
		"Fixed": true,
	}

	tests := []struct {
		name      string
		fragments map[string][]string // The parsed fragments to validate
		expectErr bool
	}{
		{
			name: "Valid Sections Only",
			fragments: map[string][]string{
				"Added": {"- item 1"},
				"Fixed": {"- item 2"},
			},
			expectErr: false,
		},
		{
			name: "Contains Ignored (Should always pass)",
			fragments: map[string][]string{
				"Added":   {"- item 1"},
				"Ignored": {"- skipped reason"},
			},
			expectErr: false,
		},
		{
			name: "Contains Invalid Section",
			fragments: map[string][]string{
				"Added":   {"- item 1"},
				"Removed": {"- item 2"}, // 'Removed' is not in our 'allowed' map
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidSections(tc.fragments, allowed)
			if tc.expectErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestSectionHeader_Variations(t *testing.T) {
	// We want to verify that H1-H6 are accepted, but H7 or weird formatting is rejected
	// Note: The regex is `^#{1,6} (\w+)\s?$`

	defaultMap := map[string]bool{"Fixed": true}

	tests := []struct {
		line      string
		expectSec string // Empty string means no match
	}{
		{"# Fixed", "Fixed"},
		{"## Fixed", "Fixed"},
		{"### Fixed", "Fixed"},
		{"#### Fixed", "Fixed"},
		{"##### Fixed", "Fixed"},
		{"###### Fixed", "Fixed"},
		{"####### Fixed", ""},   // Too many hashes
		{"Fixed", ""},           // No hashes
		{"### Fixed ", "Fixed"}, // Trailing space allowed by \s?
		{"### Fixed  ", ""},     // Too many trailing spaces (regex is strict on \s?)
		{"###  Fixed", ""},      // Too many spaces after hash
		{"### fixed", ""},       // Case sensitive check (parseSection checks against map keys)
	}

	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			res := parseSection(tc.line, defaultMap)
			if res != tc.expectSec {
				t.Errorf("input '%s': expected '%s', got '%s'", tc.line, tc.expectSec, res)
			}
		})
	}
}
