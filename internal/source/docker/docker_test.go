package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shaheeranser/watcher/internal/source"
)

// fakeDaemon is a minimal Docker Engine API over a unix socket, standing in for
// the real daemon so the tests never need Docker (RT-NFR-5).
type fakeDaemon struct {
	mu         sync.Mutex
	containers map[string]*fakeContainer
	events     chan string
	socket     string
	server     *http.Server
}

type fakeContainer struct {
	id      string
	name    string
	labels  map[string]string
	tty     bool
	logs    []byte
	running bool
}

func startFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	d := &fakeDaemon{
		containers: make(map[string]*fakeContainer),
		events:     make(chan string, 32),
		socket:     socket,
	}
	d.server = &http.Server{Handler: http.HandlerFunc(d.handle)}
	go d.server.Serve(listener)
	t.Cleanup(func() { d.server.Close() })
	return d
}

func (d *fakeDaemon) add(c *fakeContainer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.containers[c.id] = c
}

func (d *fakeDaemon) remove(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.containers, id)
}

func (d *fakeDaemon) event(action, id string) {
	d.events <- `{"Type":"container","Action":"` + action + `","Actor":{"ID":"` + id + `"}}`
}

func (d *fakeDaemon) get(id string) (*fakeContainer, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.containers[id]
	return c, ok
}

func (d *fakeDaemon) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/_ping":
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "OK")
	case r.URL.Path == "/containers/json":
		d.handleList(w)
	case strings.HasPrefix(r.URL.Path, "/containers/") && strings.HasSuffix(r.URL.Path, "/json"):
		d.handleInspect(w, r)
	case strings.HasSuffix(r.URL.Path, "/logs"):
		d.handleLogs(w, r)
	case r.URL.Path == "/events":
		d.handleEvents(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (d *fakeDaemon) handleList(w http.ResponseWriter) {
	type listItem struct {
		ID     string            `json:"Id"`
		Names  []string          `json:"Names"`
		State  string            `json:"State"`
		Labels map[string]string `json:"Labels"`
	}
	d.mu.Lock()
	items := make([]listItem, 0, len(d.containers))
	for _, c := range d.containers {
		if !c.running {
			continue
		}
		items = append(items, listItem{
			ID:     c.id,
			Names:  []string{"/" + c.name},
			State:  "running",
			Labels: c.labels,
		})
	}
	d.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func (d *fakeDaemon) handleInspect(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/json")
	c, ok := d.get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	var body struct {
		ID     string `json:"Id"`
		Name   string `json:"Name"`
		Config struct {
			Tty    bool              `json:"Tty"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		State struct {
			Running bool   `json:"Running"`
			Status  string `json:"Status"`
		} `json:"State"`
	}
	body.ID = c.id
	body.Name = "/" + c.name
	body.Config.Tty = c.tty
	body.Config.Labels = c.labels
	body.State.Running = c.running
	body.State.Status = "running"

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

func (d *fakeDaemon) handleLogs(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/logs")
	c, ok := d.get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.WriteHeader(http.StatusOK)
	if len(c.logs) > 0 {
		w.Write(c.logs)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	// A follow stream stays open until the client goes away, exactly as the
	// real API does.
	<-r.Context().Done()
}

func (d *fakeDaemon) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	for {
		select {
		case ev := <-d.events:
			io.WriteString(w, ev+"\n")
			if flusher != nil {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func collect(t *testing.T, ch <-chan source.Line, n int, timeout time.Duration) []source.Line {
	t.Helper()
	var lines []source.Line
	deadline := time.After(timeout)
	for len(lines) < n {
		select {
		case line, ok := <-ch:
			if !ok {
				return lines
			}
			lines = append(lines, line)
		case <-deadline:
			return lines
		}
	}
	return lines
}

func expectNoLine(t *testing.T, ch <-chan source.Line, within time.Duration) {
	t.Helper()
	select {
	case line, ok := <-ch:
		if ok {
			t.Fatalf("unexpected line %q from %q", line.Raw, line.Source)
		}
	case <-time.After(within):
	}
}

func hexID(seed byte) string {
	return strings.Repeat(string([]byte{seed}), 64)
}

func TestDefaultScopeSelectsSiblingsAndExcludesSelf(t *testing.T) {
	d := startFakeDaemon(t)
	selfID := hexID('a')
	siblingID := hexID('b')
	otherID := hexID('c')

	d.add(&fakeContainer{id: selfID, name: "watcher", running: true,
		labels: map[string]string{labelComposeProject: "shop"},
		logs:   frame(streamStdout, "self crash\n")})
	d.add(&fakeContainer{id: siblingID, name: "web-1", running: true,
		labels: map[string]string{labelComposeProject: "shop", labelComposeService: "web"},
		logs:   frame(streamStdout, "web crash\n")})
	d.add(&fakeContainer{id: otherID, name: "unrelated", running: true,
		labels: map[string]string{labelComposeProject: "other"},
		logs:   frame(streamStdout, "other crash\n")})

	restore := selfContainerID
	selfContainerID = func() (string, bool) { return selfID, true }
	defer func() { selfContainerID = restore }()

	src, err := New("unix://"+d.socket, "", 0, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := src.Stream(ctx)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	lines := collect(t, ch, 1, 2*time.Second)
	if len(lines) != 1 {
		t.Fatalf("lines = %+v, want exactly one from the project sibling", lines)
	}
	if lines[0].Raw != "web crash" || lines[0].Source != "web" {
		t.Errorf("line = %+v, want {web crash, web}", lines[0])
	}
	expectNoLine(t, ch, 300*time.Millisecond)
}

func TestDefaultScopeOutsideComposeProjectFails(t *testing.T) {
	d := startFakeDaemon(t)

	restore := selfContainerID
	selfContainerID = func() (string, bool) { return "", false }
	defer func() { selfContainerID = restore }()

	_, err := New("unix://"+d.socket, "", 0, nil)
	if !errors.Is(err, ErrNotInComposeProject) {
		t.Fatalf("err = %v, want ErrNotInComposeProject", err)
	}
}

func TestExplicitProjectSelectorIgnoresOtherProjects(t *testing.T) {
	d := startFakeDaemon(t)
	shopID := hexID('d')
	otherID := hexID('e')
	d.add(&fakeContainer{id: shopID, name: "api", running: true,
		labels: map[string]string{labelComposeProject: "shop"},
		logs:   frame(streamStdout, "shop crash\n")})
	d.add(&fakeContainer{id: otherID, name: "other", running: true,
		labels: map[string]string{labelComposeProject: "other"},
		logs:   frame(streamStdout, "other crash\n")})

	src, err := New("unix://"+d.socket, "project=shop", 0, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _ := src.Stream(ctx)

	lines := collect(t, ch, 1, 2*time.Second)
	if len(lines) != 1 || lines[0].Raw != "shop crash" {
		t.Fatalf("lines = %+v, want only the shop container's line", lines)
	}
	expectNoLine(t, ch, 300*time.Millisecond)
}

func TestMissingSocketFailsFast(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.sock")
	if _, err := New("unix://"+missing, "project=shop", 0, nil); err == nil {
		t.Fatal("expected a failure for a missing docker socket")
	}
}

func TestUnsupportedHostRejected(t *testing.T) {
	if _, err := New("ssh://docker", "project=shop", 0, nil); err == nil {
		t.Fatal("expected an unsupported-host error")
	}
}

func TestLifecycleAttachesOnStartAndReattachesAfterRestart(t *testing.T) {
	d := startFakeDaemon(t)
	id := hexID('f')
	labels := map[string]string{labelComposeProject: "shop", labelComposeService: "api"}

	restore := selfContainerID
	selfContainerID = func() (string, bool) { return "", false }
	defer func() { selfContainerID = restore }()

	src, err := New("unix://"+d.socket, "project=shop", 0, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _ := src.Stream(ctx)

	// The container starts after the source is already watching.
	d.add(&fakeContainer{id: id, name: "api-1", running: true, labels: labels, logs: frame(streamStdout, "first crash\n")})
	d.event("start", id)

	first := collect(t, ch, 1, 2*time.Second)
	if len(first) != 1 || first[0].Raw != "first crash" || first[0].Source != "api" {
		t.Fatalf("first attach lines = %+v", first)
	}

	// It dies, then restarts with new output.
	d.remove(id)
	d.event("die", id)
	time.Sleep(100 * time.Millisecond)

	d.add(&fakeContainer{id: id, name: "api-1", running: true, labels: labels, logs: frame(streamStdout, "second crash\n")})
	d.event("start", id)

	second := collect(t, ch, 1, 2*time.Second)
	if len(second) != 1 || second[0].Raw != "second crash" {
		t.Fatalf("reattach lines = %+v, want the restarted container's line", second)
	}
}

func TestMultiplexedContainerUsesContainerNameWhenNoServiceLabel(t *testing.T) {
	d := startFakeDaemon(t)
	id := hexID('1')
	d.add(&fakeContainer{id: id, name: "plain", running: true,
		labels: map[string]string{labelComposeProject: "shop"},
		logs:   frame(streamStdout, "boom\n")})

	src, err := New("unix://"+d.socket, "project=shop", 0, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _ := src.Stream(ctx)

	lines := collect(t, ch, 1, 2*time.Second)
	if len(lines) != 1 || lines[0].Source != "plain" {
		t.Fatalf("lines = %+v, want source 'plain'", lines)
	}
}
