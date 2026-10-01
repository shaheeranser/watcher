package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
)

func frame(stream byte, payload string) []byte {
	b := make([]byte, frameHeaderLength)
	b[0] = stream
	binary.BigEndian.PutUint32(b[4:], uint32(len(payload)))
	return append(b, payload...)
}

func collectLines(t *testing.T, data []byte, tty bool) []string {
	t.Helper()
	var lines []string
	if err := readStream(context.Background(), bytes.NewReader(data), tty, func(line string) bool {
		lines = append(lines, line)
		return true
	}); err != nil {
		t.Fatalf("readStream: %v", err)
	}
	return lines
}

func TestMultiplexedCarriesPartialLinesAcrossFrames(t *testing.T) {
	data := bytes.Join([][]byte{
		frame(streamStdout, "hel"),
		frame(streamStdout, "lo\nwor"),
		frame(streamStdout, "ld\n"),
	}, nil)

	got := collectLines(t, data, false)
	want := []string{"hello", "world"}
	if !equalLines(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestMultiplexedKeepsStreamsSeparate(t *testing.T) {
	// A partial stdout line must not absorb a stderr line that arrives between
	// its halves.
	data := bytes.Join([][]byte{
		frame(streamStdout, "out-1"),
		frame(streamStderr, "err-1"),
		frame(streamStdout, "-done\n"),
		frame(streamStderr, "-done\n"),
	}, nil)

	got := collectLines(t, data, false)
	want := []string{"out-1-done", "err-1-done"}
	if !equalLines(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestMultiplexedFlushesTrailingPartialAtEndOfStream(t *testing.T) {
	got := collectLines(t, frame(streamStdout, "no trailing newline"), false)
	want := []string{"no trailing newline"}
	if !equalLines(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestMultiplexedStripsCarriageReturns(t *testing.T) {
	got := collectLines(t, frame(streamStdout, "a\r\nb\r\n"), false)
	want := []string{"a", "b"}
	if !equalLines(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestMultiplexedStopsWhenEmitReturnsFalse(t *testing.T) {
	data := bytes.Join([][]byte{
		frame(streamStdout, "one\n"),
		frame(streamStdout, "two\n"),
	}, nil)

	var lines []string
	err := readStream(context.Background(), bytes.NewReader(data), false, func(line string) bool {
		lines = append(lines, line)
		return false
	})
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if len(lines) != 1 || lines[0] != "one" {
		t.Errorf("lines = %q, want [one]", lines)
	}
}

func TestRawTTYStreamSplitsLines(t *testing.T) {
	got := collectLines(t, []byte("a\nb"), true)
	want := []string{"a", "b"}
	if !equalLines(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
