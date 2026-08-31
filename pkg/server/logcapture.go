package server

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Benbentwo/ue5/pkg"
	"github.com/charmbracelet/log"
)

const (
	ringBufferSize = 10000
)

// LogCapture manages writing process output to a log file and maintains
// a ring buffer for quick access to recent lines.
type LogCapture struct {
	file       *os.File
	ring       *RingBuffer
	mu         sync.RWMutex
	watchers   []chan LogLineEvent
	watchersMu sync.Mutex
	closed     bool
}

// RingBuffer is a fixed-size circular buffer of LogLineEvents.
type RingBuffer struct {
	lines []LogLineEvent
	head  int
	size  int
	cap   int
}

// NewRingBuffer creates a new ring buffer with the given capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		lines: make([]LogLineEvent, capacity),
		cap:   capacity,
	}
}

// Push adds a line to the ring buffer.
func (rb *RingBuffer) Push(line LogLineEvent) {
	rb.lines[rb.head] = line
	rb.head = (rb.head + 1) % rb.cap
	if rb.size < rb.cap {
		rb.size++
	}
}

// Last returns the last n lines from the ring buffer, oldest first.
func (rb *RingBuffer) Last(n int) []LogLineEvent {
	if n > rb.size {
		n = rb.size
	}
	result := make([]LogLineEvent, n)
	start := (rb.head - n + rb.cap) % rb.cap
	for i := 0; i < n; i++ {
		result[i] = rb.lines[(start+i)%rb.cap]
	}
	return result
}

// Size returns the number of lines in the buffer.
func (rb *RingBuffer) Size() int {
	return rb.size
}

// NewLogCapture creates a new log capture writing to the given file path.
func NewLogCapture(logFilePath string) (*LogCapture, error) {
	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", logFilePath, err)
	}

	return &LogCapture{
		file: file,
		ring: NewRingBuffer(ringBufferSize),
	}, nil
}

// CaptureStream reads lines from a reader and writes them to the log file and ring buffer.
// This should be called in a goroutine, once per stream (stdout, stderr).
func (lc *LogCapture) CaptureStream(r io.Reader, streamLabel string) {
	scanner := bufio.NewScanner(r)
	// Increase scanner buffer for long UE log lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		lc.mu.RLock()
		if lc.closed {
			lc.mu.RUnlock()
			return
		}
		lc.mu.RUnlock()

		raw := scanner.Text()
		now := time.Now()

		// Parse the line using the existing log parser
		parsed := pkg.ParseLine(raw)

		// Determine level string
		levelStr := "info"
		switch parsed.Level {
		case pkg.LogLevelError:
			levelStr = "error"
		case pkg.LogLevelWarning:
			levelStr = "warning"
		}

		event := LogLineEvent{
			Timestamp: now,
			Stream:    streamLabel,
			Raw:       raw,
			Level:     levelStr,
			Category:  parsed.Category,
		}

		// Write to log file
		logLine := fmt.Sprintf("%s %s | %s\n", now.Format(time.RFC3339Nano), streamLabel, raw)
		lc.mu.Lock()
		_, _ = lc.file.WriteString(logLine)
		lc.ring.Push(event)
		lc.mu.Unlock()

		// Fan out to watchers
		lc.fanOut(event)
	}

	// A closed-pipe error means the process exited and the pipe was torn down
	// while we were still blocked in Read — normal shutdown, not a failure.
	if err := scanner.Err(); err != nil && !pkg.IsStreamClosedErr(err) {
		log.Error("Error reading stream", "stream", streamLabel, "error", err)
	}
}

// Subscribe creates a new watcher channel that receives log lines matching the filter.
func (lc *LogCapture) Subscribe(filter *StreamLogsRequest) chan LogLineEvent {
	ch := make(chan LogLineEvent, 256)

	lc.watchersMu.Lock()
	lc.watchers = append(lc.watchers, ch)
	lc.watchersMu.Unlock()

	// Wrap the raw channel with a filtered one if needed
	if filter.Level == "" && filter.Category == "" && filter.Pattern == "" {
		return ch
	}

	filtered := make(chan LogLineEvent, 256)
	go func() {
		defer close(filtered)
		for event := range ch {
			if matchesFilter(event, filter.Level, filter.Category, filter.Pattern) {
				filtered <- event
			}
		}
	}()
	return filtered
}

// Close closes the log capture, flushing and closing the log file.
func (lc *LogCapture) Close() {
	lc.mu.Lock()
	lc.closed = true
	if lc.file != nil {
		_ = lc.file.Sync()
		_ = lc.file.Close()
	}
	lc.mu.Unlock()

	lc.watchersMu.Lock()
	for _, ch := range lc.watchers {
		close(ch)
	}
	lc.watchers = nil
	lc.watchersMu.Unlock()
}

// RecentLines returns the last n lines from the ring buffer.
func (lc *LogCapture) RecentLines(n int) []LogLineEvent {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.ring.Last(n)
}

func (lc *LogCapture) fanOut(event LogLineEvent) {
	lc.watchersMu.Lock()
	defer lc.watchersMu.Unlock()

	active := lc.watchers[:0]
	for _, ch := range lc.watchers {
		select {
		case ch <- event:
			active = append(active, ch)
		default:
			// Channel full — close it to unblock any filter goroutines and prevent leaks
			close(ch)
		}
	}
	lc.watchers = active
}
