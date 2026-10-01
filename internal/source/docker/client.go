// Package docker streams container logs from the Docker Engine API over the
// Docker socket. It reads only: it lists and inspects containers, follows their
// log streams, and watches lifecycle events so it can attach to containers as
// they start and detach as they stop. It never shells out to the docker binary.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client is a minimal Docker Engine API client over a unix or tcp socket.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a client for a Docker host of the form unix:///path or
// tcp://host:port.
func NewClient(host string) (*Client, error) {
	switch {
	case strings.HasPrefix(host, "unix://"):
		path := strings.TrimPrefix(host, "unix://")
		if path == "" {
			return nil, fmt.Errorf("docker host %q names no socket path", host)
		}
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", path)
			},
		}
		return &Client{baseURL: "http://docker", http: &http.Client{Transport: transport}}, nil
	case strings.HasPrefix(host, "tcp://"), strings.HasPrefix(host, "http://"):
		addr := strings.TrimPrefix(strings.TrimPrefix(host, "tcp://"), "http://")
		if addr == "" {
			return nil, fmt.Errorf("docker host %q names no address", host)
		}
		return &Client{baseURL: "http://" + addr, http: &http.Client{}}, nil
	default:
		return nil, fmt.Errorf("unsupported docker host %q (want unix:// or tcp://)", host)
	}
}

// Ping verifies the daemon is reachable and the socket is authorized.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.get(ctx, "/_ping")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker ping: status %d", resp.StatusCode)
	}
	return nil
}

// List returns the running containers the daemon reports.
func (c *Client) List(ctx context.Context) ([]Container, error) {
	var raw []listContainer
	if err := c.decode(ctx, "/containers/json", &raw); err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.container())
	}
	return out, nil
}

// Inspect returns one container by id or name.
func (c *Client) Inspect(ctx context.Context, id string) (Container, error) {
	var raw inspectContainer
	if err := c.decode(ctx, "/containers/"+id+"/json", &raw); err != nil {
		return Container{}, err
	}
	return raw.container(), nil
}

// Logs opens a follow stream of a container's stdout and stderr. The caller
// demultiplexes it according to the container's TTY setting. since is a
// lookback duration; zero starts from the current time.
func (c *Client) Logs(ctx context.Context, id string, since time.Duration) (io.ReadCloser, error) {
	from := time.Now().Add(-since).Unix()
	path := "/containers/" + id + "/logs?follow=1&stdout=1&stderr=1&since=" + strconv.FormatInt(from, 10)
	return c.stream(ctx, path)
}

// Events opens the daemon's event stream.
func (c *Client) Events(ctx context.Context) (io.ReadCloser, error) {
	return c.stream(ctx, "/events")
}

// get issues a request and returns the response, leaving the body to the
// caller.
func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build docker request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker request %s: %w", path, err)
	}
	return resp, nil
}

// stream opens a long-lived response and fails on any non-2xx status.
func (c *Client) stream(ctx context.Context, path string) (io.ReadCloser, error) {
	resp, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("docker %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

// decode reads a bounded JSON document from an endpoint.
func (c *Client) decode(ctx context.Context, path string, out any) error {
	resp, err := c.get(ctx, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode docker %s: %w", path, err)
	}
	return nil
}
