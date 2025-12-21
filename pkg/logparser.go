package pkg

import (
	"regexp"
	"strconv"
	"strings"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	LogLevelInfo LogLevel = iota
	LogLevelWarning
	LogLevelError
)

// ParsedLine contains the parsed information from a log line
type ParsedLine struct {
	Level    LogLevel
	Category string // e.g., "LogInit", "LogCompile" (for UE logs)
	Message  string
	File     string // For compiler errors
	Line     int    // For compiler errors
	Column   int    // For compiler errors
}

// Compiled regex patterns for log parsing
var (
	// UE Engine log format: LogCategory:Warning: message or LogCategory: message
	// Matches: LogInit: Display: message, LogCompile:Error: message, LogTemp: message
	ueLogPattern = regexp.MustCompile(`^(Log\w+):\s*(Warning|Error|Display)?:?\s*(.*)$`)

	// Clang/GCC compiler errors: file.cpp:123:45: error: message
	clangErrorPattern = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s*(fatal error|error|warning|note):\s*(.*)$`)

	// MSVC compiler errors: file.cpp(123,45): error C1234: message or file.cpp(123): error C1234: message
	msvcErrorPattern = regexp.MustCompile(`^(.+?)\((\d+)(?:,(\d+))?\)\s*:\s*(fatal error|error|warning)\s+[A-Z]+\d+:\s*(.*)$`)

	// UBT progress indicator: [1/45] Compile file.cpp
	ubtProgressPattern = regexp.MustCompile(`^\[(\d+)/(\d+)\]\s*(.*)$`)

	// UE verbose log with timestamp: [2024.01.15-12.34.56:789][  0]LogCategory: message
	ueTimestampPattern = regexp.MustCompile(`^\[\d{4}\.\d{2}\.\d{2}-\d{2}\.\d{2}\.\d{2}:\d{3}\]\[\s*\d+\](Log\w+):\s*(Warning|Error|Display)?:?\s*(.*)$`)

	// Linker errors often contain "error:" or "error LNK"
	linkerErrorPattern = regexp.MustCompile(`(?i)(error\s+LNK\d+|undefined reference|cannot find|unresolved external)`)

	// UBT error messages - must be followed by colon or specific UBT patterns
	ubtErrorPattern = regexp.MustCompile(`^(ERROR:|FAILED:|error:)`)
)

// ParseLine parses a log line and returns structured information
func ParseLine(line string) ParsedLine {
	// Try UE timestamp format first (most specific)
	if matches := ueTimestampPattern.FindStringSubmatch(line); matches != nil {
		return parseUELog(matches[1], matches[2], matches[3])
	}

	// Try standard UE log format
	if matches := ueLogPattern.FindStringSubmatch(line); matches != nil {
		return parseUELog(matches[1], matches[2], matches[3])
	}

	// Try Clang/GCC compiler error format
	if matches := clangErrorPattern.FindStringSubmatch(line); matches != nil {
		return parseClangError(matches)
	}

	// Try MSVC compiler error format
	if matches := msvcErrorPattern.FindStringSubmatch(line); matches != nil {
		return parseMSVCError(matches)
	}

	// Try UBT progress indicator
	if matches := ubtProgressPattern.FindStringSubmatch(line); matches != nil {
		return ParsedLine{
			Level:   LogLevelInfo,
			Message: line,
		}
	}

	// Check for linker errors
	if linkerErrorPattern.MatchString(line) {
		return ParsedLine{
			Level:   LogLevelError,
			Message: line,
		}
	}

	// Check for UBT error messages
	if ubtErrorPattern.MatchString(line) {
		return ParsedLine{
			Level:   LogLevelError,
			Message: line,
		}
	}

	// Check for specific known error patterns
	if strings.Contains(line, "A conflicting instance of UnrealBuildTool is already running") {
		return ParsedLine{
			Level:   LogLevelError,
			Message: line,
		}
	}

	// Default to info level
	return ParsedLine{
		Level:   LogLevelInfo,
		Message: line,
	}
}

// parseUELog parses Unreal Engine log format
func parseUELog(category, level, message string) ParsedLine {
	parsed := ParsedLine{
		Category: category,
		Message:  message,
		Level:    LogLevelInfo,
	}

	switch strings.ToLower(level) {
	case "error":
		parsed.Level = LogLevelError
	case "warning":
		parsed.Level = LogLevelWarning
	case "display", "":
		parsed.Level = LogLevelInfo
	}

	return parsed
}

// parseClangError parses Clang/GCC compiler error format
func parseClangError(matches []string) ParsedLine {
	line, _ := strconv.Atoi(matches[2])
	col, _ := strconv.Atoi(matches[3])
	severity := strings.ToLower(matches[4])

	parsed := ParsedLine{
		File:    matches[1],
		Line:    line,
		Column:  col,
		Message: matches[5],
		Level:   LogLevelInfo,
	}

	switch severity {
	case "error", "fatal error":
		parsed.Level = LogLevelError
	case "warning":
		parsed.Level = LogLevelWarning
	case "note":
		parsed.Level = LogLevelInfo
	}

	return parsed
}

// parseMSVCError parses MSVC compiler error format
func parseMSVCError(matches []string) ParsedLine {
	line, _ := strconv.Atoi(matches[2])
	col := 0
	if matches[3] != "" {
		col, _ = strconv.Atoi(matches[3])
	}
	severity := strings.ToLower(matches[4])

	parsed := ParsedLine{
		File:    matches[1],
		Line:    line,
		Column:  col,
		Message: matches[5],
		Level:   LogLevelInfo,
	}

	switch severity {
	case "error", "fatal error":
		parsed.Level = LogLevelError
	case "warning":
		parsed.Level = LogLevelWarning
	}

	return parsed
}
