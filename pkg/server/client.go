package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/charmbracelet/log"
)

// Client provides a high-level interface for communicating with the daemon.
type Client struct {
	socketPath string
	timeout    time.Duration
}

// NewClient creates a new client that connects to the default socket path.
func NewClient() *Client {
	return &Client{
		socketPath: SocketPath(),
		timeout:    10 * time.Second,
	}
}

// Send sends a request and returns the response.
func (c *Client) Send(req Request) (*Response, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close() //nolint:errcheck

	// Set deadlines
	_ = conn.SetWriteDeadline(time.Now().Add(c.timeout))
	_ = conn.SetReadDeadline(time.Now().Add(c.timeout))

	// Send request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
		return nil, fmt.Errorf("connection closed without response")
	}

	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp, nil
}

// Stream sends a streaming request and returns a channel of responses.
// The channel is closed when the server closes the connection or an error occurs.
func (c *Client) Stream(req Request) (<-chan *Response, func(), error) {
	conn, err := c.connect()
	if err != nil {
		return nil, nil, err
	}

	// Send request
	_ = conn.SetWriteDeadline(time.Now().Add(c.timeout))
	data, err := json.Marshal(req)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Remove read deadline for streaming
	_ = conn.SetReadDeadline(time.Time{})

	ch := make(chan *Response, 64)
	cancel := func() {
		_ = conn.Close()
	}

	go func() {
		defer close(ch)
		defer conn.Close() //nolint:errcheck

		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			var resp Response
			if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
				log.Error("Failed to parse streaming response", "error", err)
				continue
			}
			ch <- &resp
		}
	}()

	return ch, cancel, nil
}

// Ping checks if the daemon is running and responsive.
func (c *Client) Ping() (*PongResponse, error) {
	resp, err := c.Send(Request{
		ID:   generateID(),
		Type: ReqPing,
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("ping failed: %s", resp.Error)
	}
	return resp.Pong, nil
}

// IsRunning checks if the daemon is accessible.
func (c *Client) IsRunning() bool {
	_, err := c.Ping()
	return err == nil
}

func (c *Client) connect() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon at %s: %w", c.socketPath, err)
	}
	return conn, nil
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
