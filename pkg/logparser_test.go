package pkg

import (
	"testing"
)

func TestParseUELogFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected LogLevel
		category string
	}{
		{
			name:     "UE Error log",
			input:    "LogCompile:Error: Failed to compile shader",
			expected: LogLevelError,
			category: "LogCompile",
		},
		{
			name:     "UE Warning log",
			input:    "LogInit:Warning: Module not found",
			expected: LogLevelWarning,
			category: "LogInit",
		},
		{
			name:     "UE Display log",
			input:    "LogInit: Display: Initializing engine",
			expected: LogLevelInfo,
			category: "LogInit",
		},
		{
			name:     "UE simple log",
			input:    "LogTemp: Some debug message",
			expected: LogLevelInfo,
			category: "LogTemp",
		},
		{
			name:     "UE log with timestamp",
			input:    "[2024.01.15-12.34.56:789][  0]LogCore:Error: Critical error occurred",
			expected: LogLevelError,
			category: "LogCore",
		},
		{
			name:     "UE log with timestamp warning",
			input:    "[2024.01.15-12.34.56:789][  0]LogStreaming:Warning: Failed to read file",
			expected: LogLevelWarning,
			category: "LogStreaming",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseLine(tt.input)
			if result.Level != tt.expected {
				t.Errorf("ParseLine(%q).Level = %v, want %v", tt.input, result.Level, tt.expected)
			}
			if result.Category != tt.category {
				t.Errorf("ParseLine(%q).Category = %q, want %q", tt.input, result.Category, tt.category)
			}
		})
	}
}

func TestParseClangErrors(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected LogLevel
		file     string
		line     int
		col      int
	}{
		{
			name:     "Clang error",
			input:    "/path/to/file.cpp:123:45: error: undeclared identifier 'foo'",
			expected: LogLevelError,
			file:     "/path/to/file.cpp",
			line:     123,
			col:      45,
		},
		{
			name:     "Clang warning",
			input:    "src/MyClass.cpp:42:10: warning: unused variable 'x'",
			expected: LogLevelWarning,
			file:     "src/MyClass.cpp",
			line:     42,
			col:      10,
		},
		{
			name:     "Clang note",
			input:    "include/header.h:10:5: note: declared here",
			expected: LogLevelInfo,
			file:     "include/header.h",
			line:     10,
			col:      5,
		},
		{
			name:     "Clang fatal error",
			input:    "/usr/include/stdio.h:1:1: fatal error: file not found",
			expected: LogLevelError,
			file:     "/usr/include/stdio.h",
			line:     1,
			col:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseLine(tt.input)
			if result.Level != tt.expected {
				t.Errorf("ParseLine(%q).Level = %v, want %v", tt.input, result.Level, tt.expected)
			}
			if result.File != tt.file {
				t.Errorf("ParseLine(%q).File = %q, want %q", tt.input, result.File, tt.file)
			}
			if result.Line != tt.line {
				t.Errorf("ParseLine(%q).Line = %d, want %d", tt.input, result.Line, tt.line)
			}
			if result.Column != tt.col {
				t.Errorf("ParseLine(%q).Column = %d, want %d", tt.input, result.Column, tt.col)
			}
		})
	}
}

func TestParseMSVCErrors(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected LogLevel
		file     string
		line     int
	}{
		{
			name:     "MSVC error with column",
			input:    "C:\\Source\\MyProject\\MyClass.cpp(123,45): error C2065: 'foo': undeclared identifier",
			expected: LogLevelError,
			file:     "C:\\Source\\MyProject\\MyClass.cpp",
			line:     123,
		},
		{
			name:     "MSVC error without column",
			input:    "C:\\Source\\MyClass.cpp(42): error C2146: syntax error",
			expected: LogLevelError,
			file:     "C:\\Source\\MyClass.cpp",
			line:     42,
		},
		{
			name:     "MSVC warning",
			input:    "D:\\Code\\Test.cpp(10,5): warning C4996: deprecated function",
			expected: LogLevelWarning,
			file:     "D:\\Code\\Test.cpp",
			line:     10,
		},
		{
			name:     "MSVC fatal error",
			input:    "Source\\Main.cpp(1,1): fatal error C1083: Cannot open include file",
			expected: LogLevelError,
			file:     "Source\\Main.cpp",
			line:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseLine(tt.input)
			if result.Level != tt.expected {
				t.Errorf("ParseLine(%q).Level = %v, want %v", tt.input, result.Level, tt.expected)
			}
			if result.File != tt.file {
				t.Errorf("ParseLine(%q).File = %q, want %q", tt.input, result.File, tt.file)
			}
			if result.Line != tt.line {
				t.Errorf("ParseLine(%q).Line = %d, want %d", tt.input, result.Line, tt.line)
			}
		})
	}
}

func TestParseUBTProgress(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "UBT compile progress",
			input: "[1/45] Compile Module.MyGame.cpp",
		},
		{
			name:  "UBT link progress",
			input: "[45/45] Link MyGameEditor-Mac-Development",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseLine(tt.input)
			if result.Level != LogLevelInfo {
				t.Errorf("ParseLine(%q).Level = %v, want LogLevelInfo", tt.input, result.Level)
			}
		})
	}
}

func TestParseLinkerErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "MSVC linker error",
			input: "error LNK2019: unresolved external symbol",
		},
		{
			name:  "GCC undefined reference",
			input: "undefined reference to `MyClass::MyFunction()'",
		},
		{
			name:  "Linker cannot find",
			input: "ld: cannot find -lsomelib",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseLine(tt.input)
			if result.Level != LogLevelError {
				t.Errorf("ParseLine(%q).Level = %v, want LogLevelError", tt.input, result.Level)
			}
		})
	}
}

func TestParseUBTErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "UBT ERROR prefix with colon",
			input: "ERROR: Build failed",
		},
		{
			name:  "UBT FAILED prefix with colon",
			input: "FAILED: Build command failed",
		},
		{
			name:  "error colon prefix",
			input: "error: something went wrong",
		},
		{
			name:  "Conflicting UBT instance",
			input: "A conflicting instance of UnrealBuildTool is already running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseLine(tt.input)
			if result.Level != LogLevelError {
				t.Errorf("ParseLine(%q).Level = %v, want LogLevelError", tt.input, result.Level)
			}
		})
	}
}

func TestParseFallback(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "Plain info message",
			input: "Building 45 actions with 8 processes",
		},
		{
			name:  "Build time message",
			input: "Total build time: 123.45 seconds",
		},
		{
			name:  "Word containing error but not an error",
			input: "Processing errorHandler.cpp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseLine(tt.input)
			if result.Level != LogLevelInfo {
				t.Errorf("ParseLine(%q).Level = %v, want LogLevelInfo", tt.input, result.Level)
			}
		})
	}
}

func TestNoFalsePositives(t *testing.T) {
	// Lines that contain "error" but should NOT be classified as errors
	nonErrors := []string{
		"Processing errorHandler.cpp",
		"Compiling ErrorReporter.h",
		"Found 0 errors",
		"Error handling module loaded",
		"myErrorCode = SUCCESS",
	}

	for _, line := range nonErrors {
		t.Run(line, func(t *testing.T) {
			result := ParseLine(line)
			if result.Level == LogLevelError {
				t.Errorf("ParseLine(%q) incorrectly classified as error", line)
			}
		})
	}
}
