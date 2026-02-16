package server

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Benbentwo/ue5/pkg"
)

// QueryLogs reads the log file and applies filters.
func QueryLogs(req *GetLogsRequest) (*LogsResponse, error) {
	logFile := LogFile(req.ProjectPath)

	f, err := os.Open(logFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	// Parse the "since" filter if provided
	var sinceTime time.Time
	if req.Since != "" {
		sinceTime, err = parseSince(req.Since)
		if err != nil {
			return nil, fmt.Errorf("invalid since value: %w", err)
		}
	}

	// Compile pattern regex if provided
	var patternRe *regexp.Regexp
	if req.Pattern != "" {
		patternRe, err = regexp.Compile(req.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern: %w", err)
		}
	}

	// Read all lines from the log file
	var allLines []LogLineEvent
	totalLines := 0

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		totalLines++
		line := scanner.Text()

		event, err := parseLogFileLine(line)
		if err != nil {
			continue
		}

		// Apply filters
		if !matchesFilter(event, req.Level, req.Category, "") {
			continue
		}

		// Apply pattern filter
		if patternRe != nil && !patternRe.MatchString(event.Raw) {
			continue
		}

		// Apply since filter
		if !sinceTime.IsZero() && event.Timestamp.Before(sinceTime) {
			continue
		}

		allLines = append(allLines, event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading log file: %w", err)
	}

	// Apply line limit (take the last N lines)
	filteredCount := len(allLines)
	if req.Lines > 0 && len(allLines) > req.Lines {
		allLines = allLines[len(allLines)-req.Lines:]
	}

	return &LogsResponse{
		Lines:         allLines,
		TotalLines:    totalLines,
		FilteredLines: filteredCount,
	}, nil
}

// parseLogFileLine parses a line from the log file format:
// "2026-02-15T10:00:00.123Z stdout | <raw line>"
func parseLogFileLine(line string) (LogLineEvent, error) {
	// Find the first space (after timestamp)
	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 4 {
		return LogLineEvent{}, fmt.Errorf("malformed log line")
	}

	timestamp, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return LogLineEvent{}, fmt.Errorf("invalid timestamp: %w", err)
	}

	stream := parts[1]
	// parts[2] is "|"
	raw := parts[3]

	// Parse the raw content for level/category
	parsed := pkg.ParseLine(raw)

	levelStr := "info"
	switch parsed.Level {
	case pkg.LogLevelError:
		levelStr = "error"
	case pkg.LogLevelWarning:
		levelStr = "warning"
	}

	return LogLineEvent{
		Timestamp: timestamp,
		Stream:    stream,
		Raw:       raw,
		Level:     levelStr,
		Category:  parsed.Category,
	}, nil
}

// matchesFilter checks if a log line event matches the given filter criteria.
func matchesFilter(event LogLineEvent, level, category, pattern string) bool {
	if level != "" && !strings.EqualFold(event.Level, level) {
		return false
	}
	if category != "" && !strings.EqualFold(event.Category, category) {
		return false
	}
	if pattern != "" && !strings.Contains(event.Raw, pattern) {
		return false
	}
	return true
}

// parseSince parses a duration string (e.g., "5m", "1h") or RFC3339 timestamp.
func parseSince(since string) (time.Time, error) {
	// Try parsing as a duration first
	d, err := time.ParseDuration(since)
	if err == nil {
		return time.Now().Add(-d), nil
	}

	// Try parsing as RFC3339
	t, err := time.Parse(time.RFC3339, since)
	if err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("could not parse '%s' as duration or RFC3339 timestamp", since)
}
