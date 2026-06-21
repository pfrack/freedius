package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

// IPCClient connects to a running daemon's Unix socket and streams events
// and logs via SSE. It provides the same channel interface as the in-memory
// EventBus/LogSink for the TUI to consume.
type IPCClient struct {
	socketPath string
	httpClient *http.Client
	events     chan proxy.RequestEvent
	logs       chan proxy.LogEntry
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewIPCClient creates a new IPC client connected to the daemon's Unix socket.
func NewIPCClient(socketPath string) (*IPCClient, error) {
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}
	httpClient := &http.Client{Transport: transport}

	// Verify connection with a stats request.
	resp, err := httpClient.Get("http://localhost/v1/stats")
	if err != nil {
		return nil, fmt.Errorf("freedius: cannot connect to daemon at %s: %w", socketPath, err)
	}
	_ = resp.Body.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := &IPCClient{
		socketPath: socketPath,
		httpClient: httpClient,
		events:     make(chan proxy.RequestEvent, 1000),
		logs:       make(chan proxy.LogEntry, 1000),
		cancel:     cancel,
	}

	c.wg.Add(2)
	go c.streamEvents(ctx)
	go c.streamLogs(ctx)

	return c, nil
}

// Events returns the channel of request events from the daemon.
func (c *IPCClient) Events() <-chan proxy.RequestEvent {
	return c.events
}

// Logs returns the channel of log entries from the daemon.
func (c *IPCClient) Logs() <-chan proxy.LogEntry {
	return c.logs
}

// Stats returns the current proxy stats from the daemon.
func (c *IPCClient) Stats() (StatsSnapshot, error) {
	resp, err := c.httpClient.Get("http://localhost/v1/stats")
	if err != nil {
		return StatsSnapshot{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var stats StatsSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return StatsSnapshot{}, err
	}
	return stats, nil
}

// Config returns the current config from the daemon.
func (c *IPCClient) Config() (*config.Config, error) {
	resp, err := c.httpClient.Get("http://localhost/v1/config")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var cfg config.Config
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Close stops the IPC client and waits for goroutines to exit.
func (c *IPCClient) Close() error {
	c.cancel()
	c.wg.Wait()
	return nil
}

func (c *IPCClient) streamEvents(ctx context.Context) {
	defer c.wg.Done()
	c.streamSSE(ctx, "http://localhost/v1/events?since=0", func(data []byte) {
		var e proxy.RequestEvent
		if err := json.Unmarshal(data, &e); err == nil {
			select {
			case c.events <- e:
			default:
			}
		}
	})
}

func (c *IPCClient) streamLogs(ctx context.Context) {
	defer c.wg.Done()
	c.streamSSE(ctx, "http://localhost/v1/logs?since=0", func(data []byte) {
		var e proxy.LogEntry
		if err := json.Unmarshal(data, &e); err == nil {
			select {
			case c.logs <- e:
			default:
			}
		}
	})
}

// streamSSE connects to an SSE endpoint and reads events using
// bufio.Reader.ReadBytes('\n') per lessons.md §2 (NOT bufio.Scanner).
func (c *IPCClient) streamSSE(ctx context.Context, url string, onData func([]byte)) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue // Frame boundary.
		}
		if line[0] == ':' {
			continue // Comment.
		}

		if bytes.HasPrefix(line, []byte("data: ")) {
			data := line[6:]
			onData(data)
		}
		// We ignore "event:" lines — the daemon sends one event type
		// per endpoint, so the data payload is sufficient.
	}
}
