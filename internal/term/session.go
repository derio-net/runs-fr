// Package term bridges a browser terminal (an xterm.js websocket) to a pod's
// pods/exec stream. The session type adapts inbound/outbound websocket frames
// to the io.Reader / io.Writer / remotecommand.TerminalSizeQueue surface that
// client-go's executor drives.
package term

import (
	"context"
	"encoding/json"
	"io"

	"github.com/coder/websocket"
	"k8s.io/client-go/tools/remotecommand"
)

// wsConn is the subset of *websocket.Conn the session needs. Narrowing it to an
// interface keeps the demux logic unit-testable without a live socket.
type wsConn interface {
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Write(ctx context.Context, typ websocket.MessageType, p []byte) error
}

// resizeEnvelope is the inbound control message xterm.js sends on a resize:
// {"resize":{"cols":C,"rows":R}}. A TEXT frame that does not carry a resize
// (Resize == nil) is simply ignored.
type resizeEnvelope struct {
	Resize *struct {
		Cols uint16 `json:"cols"`
		Rows uint16 `json:"rows"`
	} `json:"resize"`
}

// session demuxes a single attach connection. It implements io.Reader (exec
// stdin), io.Writer (exec stdout/terminal output), and
// remotecommand.TerminalSizeQueue (resize events).
//
// Inbound frames are demuxed BY WEBSOCKET MESSAGE TYPE — never by sniffing the
// payload: a MessageBinary frame is stdin and surfaces via Read; a MessageText
// frame is a control message (resize) routed to the size channel drained by
// Next. This mirrors xterm's addon-attach, which sends keystrokes as binary and
// control JSON as text.
type session struct {
	ctx   context.Context
	conn  wsConn
	sizes chan remotecommand.TerminalSize
	// leftover holds stdin bytes from a binary frame larger than the caller's
	// Read buffer, to be returned on the next Read.
	leftover []byte
}

// newSession builds a session bound to ctx (the exec/attach lifetime) over conn.
func newSession(ctx context.Context, conn wsConn) *session {
	return &session{
		ctx:  ctx,
		conn: conn,
		// Buffered so a resize never blocks the Read pump; only the latest
		// pending size matters, so a full channel drops the stale one.
		sizes: make(chan remotecommand.TerminalSize, 1),
	}
}

// Read returns the next chunk of exec stdin. It reads inbound frames until a
// binary (stdin) frame arrives, routing any intervening text (control) frames
// to the size channel so they never leak into the stdin stream.
func (s *session) Read(p []byte) (int, error) {
	if len(s.leftover) > 0 {
		n := copy(p, s.leftover)
		s.leftover = s.leftover[n:]
		return n, nil
	}
	for {
		typ, data, err := s.conn.Read(s.ctx)
		if err != nil {
			return 0, err
		}
		switch typ {
		case websocket.MessageBinary:
			n := copy(p, data)
			if n < len(data) {
				s.leftover = data[n:]
			}
			return n, nil
		case websocket.MessageText:
			s.handleControl(data)
			// Not stdin — keep reading for the next stdin frame.
		}
	}
}

// handleControl parses a text control frame and, if it is a resize, pushes the
// new size onto the (capacity-1) channel, discarding any stale pending size so
// the latest wins and the push never blocks.
func (s *session) handleControl(data []byte) {
	var env resizeEnvelope
	if err := json.Unmarshal(data, &env); err != nil || env.Resize == nil {
		return
	}
	sz := remotecommand.TerminalSize{Width: env.Resize.Cols, Height: env.Resize.Rows}
	select {
	case s.sizes <- sz:
	default:
		select {
		case <-s.sizes:
		default:
		}
		select {
		case s.sizes <- sz:
		default:
		}
	}
}

// Write emits exec stdout (terminal output) to the peer as a binary frame.
func (s *session) Write(p []byte) (int, error) {
	if err := s.conn.Write(s.ctx, websocket.MessageBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Next returns the next terminal size, blocking until a resize arrives or the
// session context is cancelled (returning nil, which stops remotecommand's
// monitor goroutine).
func (s *session) Next() *remotecommand.TerminalSize {
	select {
	case sz := <-s.sizes:
		return &sz
	case <-s.ctx.Done():
		return nil
	}
}

// Compile-time checks that session satisfies the executor's stream contracts.
var (
	_ io.Reader                       = (*session)(nil)
	_ io.Writer                       = (*session)(nil)
	_ remotecommand.TerminalSizeQueue = (*session)(nil)
)
