package term

import (
	"context"
	"io"
	"testing"

	"github.com/coder/websocket"
)

// frame is a single websocket message: its type plus payload.
type frame struct {
	typ  websocket.MessageType
	data []byte
}

// fakeConn is an in-memory wsConn: it replays queued inbound frames in order
// (returning io.EOF once drained) and captures outbound frames for assertions.
type fakeConn struct {
	in  []frame
	idx int
	out []frame
}

func (f *fakeConn) Read(context.Context) (websocket.MessageType, []byte, error) {
	if f.idx >= len(f.in) {
		return 0, nil, io.EOF
	}
	fr := f.in[f.idx]
	f.idx++
	return fr.typ, fr.data, nil
}

func (f *fakeConn) Write(_ context.Context, typ websocket.MessageType, p []byte) error {
	f.out = append(f.out, frame{typ: typ, data: append([]byte(nil), p...)})
	return nil
}

// A BINARY inbound frame is the user's keystrokes: its bytes must surface via
// Read as the exec stdin stream.
func TestSession_BinaryFrameIsStdin(t *testing.T) {
	conn := &fakeConn{in: []frame{{typ: websocket.MessageBinary, data: []byte("ls -la\n")}}}
	s := newSession(context.Background(), conn)

	buf := make([]byte, 64)
	n, err := s.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := string(buf[:n]); got != "ls -la\n" {
		t.Errorf("Read = %q, want %q", got, "ls -la\n")
	}
}

// A TEXT inbound frame carrying {"resize":…} must be routed to Next() as a
// terminal size and must NOT leak into the stdin stream Read returns.
func TestSession_TextResizeGoesToNextNotRead(t *testing.T) {
	conn := &fakeConn{in: []frame{
		{typ: websocket.MessageText, data: []byte(`{"resize":{"cols":120,"rows":40}}`)},
		{typ: websocket.MessageBinary, data: []byte("x")},
	}}
	s := newSession(context.Background(), conn)

	// The resize frame is demuxed away; Read returns only the binary stdin.
	buf := make([]byte, 64)
	n, err := s.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := string(buf[:n]); got != "x" {
		t.Errorf("Read leaked non-stdin: = %q, want %q", got, "x")
	}

	sz := s.Next()
	if sz == nil {
		t.Fatal("Next() = nil, want a terminal size")
	}
	if sz.Width != 120 || sz.Height != 40 {
		t.Errorf("Next() = %dx%d, want 120x40", sz.Width, sz.Height)
	}
}

// When resizes arrive faster than Next() drains them, the latest size must win:
// the capacity-1 channel drops the stale size rather than the new one (and never
// blocks the Read pump).
func TestSession_LatestResizeWins(t *testing.T) {
	conn := &fakeConn{in: []frame{
		{typ: websocket.MessageText, data: []byte(`{"resize":{"cols":80,"rows":24}}`)},
		{typ: websocket.MessageText, data: []byte(`{"resize":{"cols":200,"rows":50}}`)},
		{typ: websocket.MessageBinary, data: []byte("x")},
	}}
	s := newSession(context.Background(), conn)

	// Drive both resizes through the demux without an intervening Next().
	buf := make([]byte, 64)
	if _, err := s.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}

	sz := s.Next()
	if sz == nil {
		t.Fatal("Next() = nil, want the latest terminal size")
	}
	if sz.Width != 200 || sz.Height != 50 {
		t.Errorf("Next() = %dx%d, want latest 200x50 (stale 80x24 must be dropped)", sz.Width, sz.Height)
	}
}

// Write is the exec stdout/terminal-output path: its bytes must be emitted to
// the peer as a single BINARY frame.
func TestSession_WriteEmitsBinaryFrame(t *testing.T) {
	conn := &fakeConn{}
	s := newSession(context.Background(), conn)

	n, err := s.Write([]byte("output"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len("output") {
		t.Errorf("Write n = %d, want %d", n, len("output"))
	}
	if len(conn.out) != 1 {
		t.Fatalf("emitted %d frames, want 1", len(conn.out))
	}
	if conn.out[0].typ != websocket.MessageBinary {
		t.Errorf("frame type = %v, want MessageBinary", conn.out[0].typ)
	}
	if got := string(conn.out[0].data); got != "output" {
		t.Errorf("frame data = %q, want %q", got, "output")
	}
}
