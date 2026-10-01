package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/shaheeranser/watcher/internal/guard"
	"github.com/shaheeranser/watcher/internal/source"
)

const (
	lineBuffer           = 1024
	socketTimeout        = 10 * time.Second
	reconcileInterval    = 15 * time.Second
	streamBackoffInitial = 250 * time.Millisecond
	streamBackoffMax     = 5 * time.Second
	eventBackoffInitial  = 250 * time.Millisecond
	eventBackoffMax      = 5 * time.Second
)

// ErrNotInComposeProject means the default scope was asked for but Watcher is
// not running in a container that belongs to a Compose project. The caller
// falls back to stdin on a bare-metal run and reports it when Docker was
// requested explicitly.
var ErrNotInComposeProject = errors.New("not running in a Docker Compose project")

// selfContainerID resolves Watcher's own container id. It is a variable so a
// test can place Watcher "inside" a container without one.
var selfContainerID = guard.SelfContainerID

// Docker streams logs from the containers in its selector's scope, attaching as
// containers start and detaching as they stop.
type Docker struct {
	client   *Client
	selector Selector
	since    time.Duration
	selfID   string
	log      *slog.Logger
}

// New builds a Docker source. It fails fast when the socket is missing or
// unauthorized (RT-DOCK-8) and, for the default scope, when Watcher is not in a
// Compose project. It never fails a run that does not request Docker, because
// the caller only calls New when it wants the source.
func New(host, selectorRaw string, since time.Duration, log *slog.Logger) (*Docker, error) {
	client, err := NewClient(host)
	if err != nil {
		return nil, err
	}

	log = loggerOrDiscard(log)
	ctx, cancel := context.WithTimeout(context.Background(), socketTimeout)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		return nil, fmt.Errorf("docker socket unreachable: %w", err)
	}

	selector, err := ParseSelector(selectorRaw)
	if err != nil {
		return nil, err
	}

	selfID, _ := selfContainerID()
	if selector.OwnProject() {
		project, err := ownProject(ctx, client, selfID)
		if err != nil {
			return nil, err
		}
		selector = Selector{mode: modeProject, value: project}
	}

	return &Docker{client: client, selector: selector, since: since, selfID: selfID, log: log}, nil
}

// ownProject resolves the Compose project of the container Watcher runs in.
func ownProject(ctx context.Context, client *Client, selfID string) (string, error) {
	if selfID == "" {
		return "", ErrNotInComposeProject
	}
	self, err := client.Inspect(ctx, selfID)
	if err != nil {
		return "", fmt.Errorf("inspect Watcher's own container: %w", err)
	}
	if project := self.ComposeProject(); project != "" {
		return project, nil
	}
	return "", ErrNotInComposeProject
}

func (d *Docker) Name() string { return "docker" }

// Stream starts the supervisor and returns the channel of lines. Every line
// carries its container's identity as its source label (RT-DOCK-6).
func (d *Docker) Stream(ctx context.Context) (<-chan source.Line, error) {
	out := make(chan source.Line, lineBuffer)
	go func() {
		defer close(out)
		d.supervise(ctx, out)
	}()
	return out, nil
}

// lifecycle is an attach or detach request produced by the event stream.
type lifecycle struct {
	id     string
	attach bool
}

// attachment is a running follow goroutine, tagged with the generation that
// started it so a late completion cannot delete a newer attach for the same
// container.
type attachment struct {
	cancel context.CancelFunc
	gen    uint64
}

// completion reports that a follow goroutine has stopped.
type completion struct {
	id  string
	gen uint64
}

// supervise owns the containerID -> attachment map: it attaches on
// start/restart, detaches on die/stop, and periodically reconciles against the
// container list so a missed event cannot leave a stream attached or a new
// container unwatched (RT-DOCK-5, RT-DOCK-9).
func (d *Docker) supervise(ctx context.Context, out chan<- source.Line) {
	events := make(chan lifecycle, 64)
	finished := make(chan completion, 64)
	go d.watchEvents(ctx, events)

	attached := make(map[string]attachment)
	var generation uint64
	defer func() {
		for _, a := range attached {
			a.cancel()
		}
	}()

	attach := func(id string) {
		if id == "" {
			return
		}
		if _, ok := attached[id]; ok {
			return
		}
		if self, reason := d.isSelf(id); self {
			d.log.Warn("refusing to watch Watcher's own container", "container", id, "reason", reason)
			return
		}
		c, err := d.client.Inspect(ctx, id)
		if err != nil {
			d.log.Warn("cannot inspect container", "container", id, "error", err)
			return
		}
		if !c.Running || !d.selector.Matches(c) {
			return
		}
		child, cancel := context.WithCancel(ctx)
		generation++
		attached[id] = attachment{cancel: cancel, gen: generation}
		go d.follow(child, c, generation, out, finished)
	}
	detach := func(id string) {
		if a, ok := attached[id]; ok {
			a.cancel()
			delete(attached, id)
		}
	}

	d.reconcile(ctx, attach, detach, attached)

	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case update := <-events:
			if update.attach {
				attach(update.id)
			} else {
				detach(update.id)
			}
		case done := <-finished:
			if a, ok := attached[done.id]; ok && a.gen == done.gen {
				delete(attached, done.id)
			}
		case <-ticker.C:
			d.reconcile(ctx, attach, detach, attached)
		}
	}
}

// reconcile lists the containers in scope, attaching any that are running and
// detaching any that have gone away.
func (d *Docker) reconcile(ctx context.Context, attach func(string), detach func(string), attached map[string]attachment) {
	containers, err := d.client.List(ctx)
	if err != nil {
		d.log.Warn("cannot list containers", "error", err)
		return
	}
	live := make(map[string]bool)
	for _, c := range containers {
		if !c.Running || !d.selector.Matches(c) {
			continue
		}
		if self, _ := d.isSelf(c.ID); self {
			continue
		}
		live[c.ID] = true
		attach(c.ID)
	}
	for id := range attached {
		if !live[id] {
			detach(id)
		}
	}
}

// isSelf reports whether id is Watcher's own container, extending the guard to
// container identity (RT-DOCK-7).
func (d *Docker) isSelf(id string) (bool, string) {
	if id == "" || d.selfID == "" {
		return false, ""
	}
	if guard.SameContainer(d.selfID, id) {
		return true, "container is Watcher's own container"
	}
	return false, ""
}

// follow streams one container's logs, reconnecting with backoff on a transient
// error and stopping when the container ends or the context is cancelled.
func (d *Docker) follow(ctx context.Context, c Container, gen uint64, out chan<- source.Line, finished chan<- completion) {
	defer func() {
		select {
		case finished <- completion{id: c.ID, gen: gen}:
		case <-ctx.Done():
		}
	}()

	label := c.DisplayName()
	backoff := streamBackoffInitial
	for {
		if ctx.Err() != nil {
			return
		}
		body, err := d.client.Logs(ctx, c.ID, d.since)
		if err != nil {
			d.log.Warn("cannot open container logs", "container", label, "error", err)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, streamBackoffMax)
			continue
		}
		backoff = streamBackoffInitial
		err = readStream(ctx, body, c.Tty, func(line string) bool {
			return sendLine(ctx, out, source.Line{Raw: line, Source: label, ArrivedAt: time.Now()})
		})
		body.Close()
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			return
		}
		d.log.Warn("container log stream dropped; reconnecting", "container", label, "error", err)
		if !sleepCtx(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, streamBackoffMax)
	}
}

// watchEvents keeps the lifecycle event stream open, reconnecting with backoff
// when the daemon closes it.
func (d *Docker) watchEvents(ctx context.Context, out chan<- lifecycle) {
	backoff := eventBackoffInitial
	for {
		if ctx.Err() != nil {
			return
		}
		body, err := d.client.Events(ctx)
		if err != nil {
			d.log.Warn("cannot open docker events", "error", err)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, eventBackoffMax)
			continue
		}
		backoff = eventBackoffInitial
		err = decodeEvents(ctx, body, out)
		body.Close()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			d.log.Warn("docker event stream ended", "error", err)
		}
		if !sleepCtx(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, eventBackoffMax)
	}
}

type dockerEvent struct {
	Type   string `json:"Type"`
	Action string `json:"Action"`
	Actor  struct {
		ID string `json:"ID"`
	} `json:"Actor"`
}

// decodeEvents turns the event stream into attach and detach requests.
func decodeEvents(ctx context.Context, r io.Reader, out chan<- lifecycle) error {
	decoder := json.NewDecoder(r)
	for {
		var ev dockerEvent
		if err := decoder.Decode(&ev); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if ev.Type != "container" || ev.Actor.ID == "" {
			continue
		}

		var attach bool
		switch ev.Action {
		case "start", "restart", "unpause":
			attach = true
		case "die", "stop", "kill", "destroy", "pause":
			attach = false
		default:
			continue
		}

		select {
		case out <- lifecycle{id: ev.Actor.ID, attach: attach}:
		case <-ctx.Done():
			return nil
		}
	}
}

func loggerOrDiscard(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return l
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func sendLine(ctx context.Context, out chan<- source.Line, line source.Line) bool {
	select {
	case out <- line:
		return true
	case <-ctx.Done():
		return false
	}
}
