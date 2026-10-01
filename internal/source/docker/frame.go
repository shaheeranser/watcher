package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strings"
)

// frameHeaderLength is the size of the Docker multiplexed-stream header: one
// stream byte, three padding bytes, and a big-endian payload length.
const frameHeaderLength = 8

// stream identifies which of a container's output streams a frame carries.
const (
	streamStdin  = 0
	streamStdout = 1
	streamStderr = 2
)

// readStream turns a container log stream into lines. A non-TTY container's
// stream is multiplexed and is demultiplexed here; a TTY container's is raw.
// emit returns false to stop early. A clean end of stream (the container
// stopped) returns nil; a transport error is returned so the caller can
// reconnect.
func readStream(ctx context.Context, r io.Reader, tty bool, emit func(string) bool) error {
	if tty {
		return readRaw(ctx, r, emit)
	}
	return readMultiplexed(ctx, r, emit)
}

// readMultiplexed reassembles partial lines per stream, so a frame boundary in
// the middle of a line never splits it and stdout and stderr never merge.
func readMultiplexed(ctx context.Context, r io.Reader, emit func(string) bool) error {
	var header [frameHeaderLength]byte
	pending := make(map[byte][]byte)

	for {
		if ctx.Err() != nil {
			return nil
		}
		if _, err := io.ReadFull(r, header[:]); err != nil {
			if isStreamEnd(err) {
				return flushPending(pending, emit)
			}
			return err
		}

		size := binary.BigEndian.Uint32(header[4:frameHeaderLength])
		if size == 0 {
			continue
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(r, payload); err != nil {
			if isStreamEnd(err) {
				return flushPending(pending, emit)
			}
			return err
		}

		if header[0] != streamStdout && header[0] != streamStderr {
			continue
		}
		buf := append(pending[header[0]], payload...)
		if !splitCompleteLines(&buf, emit) {
			return nil
		}
		pending[header[0]] = buf
	}
}

// readRaw passes a TTY container's stream through as a single line stream.
func readRaw(ctx context.Context, r io.Reader, emit func(string) bool) error {
	var pending []byte
	buf := make([]byte, 32*1024)
	for {
		if ctx.Err() != nil {
			return nil
		}
		n, err := r.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			if !splitCompleteLines(&pending, emit) {
				return nil
			}
		}
		if err != nil {
			if isStreamEnd(err) {
				if line, ok := takePending(&pending); ok {
					emit(line)
				}
				return nil
			}
			return err
		}
	}
}

// splitCompleteLines emits every newline-terminated line in buf and keeps the
// unterminated remainder.
func splitCompleteLines(buf *[]byte, emit func(string) bool) bool {
	for {
		i := bytes.IndexByte(*buf, '\n')
		if i < 0 {
			return true
		}
		line := strings.TrimSuffix(string((*buf)[:i]), "\r")
		*buf = (*buf)[i+1:]
		if !emit(line) {
			return false
		}
	}
}

// flushPending emits each stream's unterminated remainder at end of stream.
func flushPending(pending map[byte][]byte, emit func(string) bool) error {
	for _, stream := range []byte{streamStdout, streamStderr} {
		if line, ok := takePending2(&pending, stream); ok {
			if !emit(line) {
				return nil
			}
		}
	}
	return nil
}

func takePending(buf *[]byte) (string, bool) {
	if len(*buf) == 0 {
		return "", false
	}
	line := strings.TrimSuffix(string(*buf), "\r")
	*buf = nil
	return line, true
}

func takePending2(pending *map[byte][]byte, stream byte) (string, bool) {
	buf := (*pending)[stream]
	if len(buf) == 0 {
		return "", false
	}
	line := strings.TrimSuffix(string(buf), "\r")
	delete(*pending, stream)
	return line, true
}

// isStreamEnd reports whether err is an ordinary end of a log stream rather
// than a transport failure.
func isStreamEnd(err error) bool {
	return err == io.EOF || err == io.ErrUnexpectedEOF
}
