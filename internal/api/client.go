package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/shaheeranser/watcher/internal/store"
)

// Client is the read-only client `watcher attach` uses. It can GET the list,
// one incident, health, and the event stream, and nothing else, which is what
// makes read-only a property of the transport rather than a promise (DASH-10).
type Client struct {
	base string
	http *http.Client
}

// NewClient dials a daemon's Unix socket.
func NewClient(socket string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &Client{base: "http://watcher", http: &http.Client{Transport: transport}}
}

// Health fetches the liveness and counter response.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	err := c.getJSON(ctx, Prefix+"/health", &h)
	return h, err
}

// Incidents fetches the incident list.
func (c *Client) Incidents(ctx context.Context) ([]IncidentRow, error) {
	var rows []IncidentRow
	if err := c.getJSON(ctx, Prefix+"/incidents", &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []IncidentRow{}
	}
	return rows, nil
}

// Incident fetches one incident's detail.
func (c *Client) Incident(ctx context.Context, id string) (IncidentDetail, error) {
	var d IncidentDetail
	err := c.getJSON(ctx, Prefix+"/incidents/"+url.PathEscape(id), &d)
	return d, err
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(path, resp)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// Events opens the Server-Sent Events stream. The returned stream owns the
// response body until Close.
func (c *Client) Events(ctx context.Context) (*EventStream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+Prefix+"/events", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		err := statusError(Prefix+"/events", resp)
		resp.Body.Close()
		return nil, err
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	return &EventStream{body: resp.Body, scanner: scanner}, nil
}

// EventStream reads Server-Sent Events off a live connection.
type EventStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
}

// Next blocks until the next mutation event. It returns io.EOF when the daemon
// closes the stream, which the caller treats as a disconnect and reconnects.
func (e *EventStream) Next() (store.Event, error) {
	var data strings.Builder
	for e.scanner.Scan() {
		line := e.scanner.Text()
		if line == "" {
			if data.Len() == 0 {
				continue // a keepalive comment block, not an event
			}
			var ev store.Event
			err := json.Unmarshal([]byte(data.String()), &ev)
			data.Reset()
			if err != nil {
				continue
			}
			return ev, nil
		}
		if payload, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(strings.TrimSpace(payload))
		}
	}
	if err := e.scanner.Err(); err != nil {
		return store.Event{}, err
	}
	return store.Event{}, io.EOF
}

// Close releases the stream.
func (e *EventStream) Close() error { return e.body.Close() }

func statusError(path string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	var eb errBody
	if json.Unmarshal(body, &eb) == nil && eb.Error != "" {
		return fmt.Errorf("%s: %s", path, eb.Error)
	}
	return fmt.Errorf("%s: status %d", path, resp.StatusCode)
}
