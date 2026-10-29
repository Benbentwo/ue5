package pkg

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.1.0", "0.1.1", -1},
		{"0.1.1", "0.1.0", 1},
		{"0.2.0", "0.1.9", 1},
		{"1.0.0", "0.9.9", 1},
		{"dev", "0.1.0", -1},
		{"0.1.0", "dev", 1},
		{"0.3.3", "0.3.4", -1},
		{"0.3.4", "0.3.4", 0},
		{"1.0", "1.0.0", 0},
		{"1.0.0", "1.0", 0},
	}

	for _, tt := range tests {
		result := compareVersions(tt.v1, tt.v2)
		if result != tt.expected {
			t.Errorf("compareVersions(%q, %q) = %d, expected %d", tt.v1, tt.v2, result, tt.expected)
		}
	}
}

func TestGetAssetPatternForPlatform(t *testing.T) {
	pattern := getAssetPatternForPlatform()
	if pattern == "" {
		t.Error("getAssetPatternForPlatform returned empty string")
	}
	// Should end with .tar.gz
	if len(pattern) < 7 || pattern[len(pattern)-7:] != ".tar.gz" {
		t.Errorf("expected pattern to end with .tar.gz, got %q", pattern)
	}
}
