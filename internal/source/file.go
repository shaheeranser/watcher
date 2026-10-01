package source

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"time"
)

const (
	filePollInterval = 200 * time.Millisecond
	initialBackoff   = 100 * time.Millisecond
	maxBackoff       = 2 * time.Second
)

// File tails a single log file. By default it starts at the current end so a
// restart does not replay history (CORE-SRC-2); it reopens automatically when
// the file is truncated or replaced by a rotation (CORE-SRC-3). The label
// attributes its lines and defaults to the path.
type File struct {
	path         string
	label        string
	fromStart    bool
	pollInterval time.Duration
	bufSize      int
	log          *slog.Logger
}

func NewFile(path, label string, fromStart bool, log *slog.Logger) *File {
	if label == "" {
		label = path
	}
	return &File{
		path:         path,
		label:        label,
		fromStart:    fromStart,
		pollInterval: filePollInterval,
		bufSize:      defaultBuffer,
		log:          loggerOrDiscard(log),
	}
}

func (f *File) Name() string { return f.label }

// Path exposes the watched file so the self-watch guard can identify it.
func (f *File) Path() string { return f.path }

func (f *File) Stream(ctx context.Context) (<-chan Line, error) {
	if f.path == "" {
		return nil, errors.New("file source: empty path")
	}
	out := make(chan Line, f.bufSize)
	go func() {
		defer close(out)
		f.run(ctx, out)
	}()
	return out, nil
}

func (f *File) run(ctx context.Context, out chan<- Line) {
	atEnd := !f.fromStart
	for {
		if ctx.Err() != nil {
			return
		}
		file, ok := f.open(ctx, atEnd)
		if !ok {
			return
		}
		acc := newLineAccumulator(file)
		outcome := f.follow(ctx, file, acc, out)
		file.Close()

		switch outcome {
		case followStopped:
			return
		case followRotated:
			// A replacement file is a fresh stream: read it from the top.
			atEnd = false
		case followReadError:
			// Reopen the same file without replaying what we already read.
			atEnd = true
		}
	}
}

// open waits, with capped backoff, for the file to exist. A missing file is a
// normal state (the app has not created its log yet), not a fatal error.
func (f *File) open(ctx context.Context, atEnd bool) (*os.File, bool) {
	backoff := initialBackoff
	for {
		file, err := os.Open(f.path)
		if err == nil {
			if atEnd {
				if _, err := file.Seek(0, io.SeekEnd); err != nil {
					file.Close()
					return nil, false
				}
			}
			return file, true
		}
		f.log.Warn("waiting for file", "path", f.path, "error", err)
		if !sleepCtx(ctx, backoff) {
			return nil, false
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

type followOutcome int

const (
	followStopped followOutcome = iota
	followRotated
	followReadError
)

func (f *File) follow(ctx context.Context, file *os.File, acc *lineAccumulator, out chan<- Line) followOutcome {
	for {
		if ctx.Err() != nil {
			return followStopped
		}
		setReadDeadline(file, f.pollInterval)
		lines, err := acc.read()
		for _, raw := range lines {
			if !sendLine(ctx, out, Line{Raw: raw, Source: f.label, ArrivedAt: time.Now()}) {
				return followStopped
			}
		}
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrDeadlineExceeded) {
			f.log.Error("read failed", "path", f.path, "error", err)
			return followReadError
		}
		if len(lines) > 0 {
			continue
		}

		if !sleepCtx(ctx, f.pollInterval) {
			return followStopped
		}
		rotated, err := f.rotated(file)
		if err != nil {
			f.log.Warn("file disappeared; reopening", "path", f.path, "error", err)
			return followRotated
		}
		if rotated {
			f.log.Info("file rotated; reading replacement", "path", f.path)
			return followRotated
		}
	}
}

// rotated reports whether the path now refers to a different file than the one
// being read, or the same file shrank beneath the read position.
func (f *File) rotated(file *os.File) (bool, error) {
	pathInfo, err := os.Stat(f.path)
	if err != nil {
		return false, err
	}
	fileInfo, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !os.SameFile(pathInfo, fileInfo) {
		return true, nil
	}
	pos, err := file.Seek(0, io.SeekCurrent)
	if err == nil && pathInfo.Size() < pos {
		return true, nil
	}
	return false, nil
}
