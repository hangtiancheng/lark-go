// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package swifty_http

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	TextMessage   = 1
	BinaryMessage = 2
	CloseMessage  = 8
	PingMessage   = 9
	PongMessage   = 10
)

const (
	wsGUID       = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	maxFrameSize = 65536
	closeNormal  = 1000
)

var (
	ErrWSClosed       = errors.New("websocket: connection closed")
	ErrWSInvalidFrame = errors.New("websocket: invalid frame")
)

type UpgradeOptions struct {
	ReadBufferSize  int
	WriteBufferSize int
	// MaxMessageSize caps a single frame and the reassembled fragmented
	// message total on the read side. Zero means the default (65536 bytes).
	MaxMessageSize int
	CheckOrigin    func(r *http.Request) bool
	Subprotocols   []string
}

type WSConn struct {
	conn           net.Conn
	br             *bufio.Reader
	writeMu        sync.Mutex
	readMu         sync.Mutex
	closed         chan struct{}
	once           sync.Once
	maxMessageSize int

	onMessage func(messageType int, data []byte)
	onClose   func(code int, text string)
	onError   func(err error)
	onPing    func(data []byte)
	onPong    func(data []byte)
}

func (ws *WSConn) messageLimit() int {
	if ws.maxMessageSize > 0 {
		return ws.maxMessageSize
	}
	return maxFrameSize
}

func (ctx *Context) Upgrade(opts *UpgradeOptions) (*WSConn, error) {
	if opts != nil && opts.CheckOrigin != nil {
		if !opts.CheckOrigin(ctx.Request) {
			ctx.Throw(http.StatusForbidden, "origin not allowed")
			return nil, errors.New("websocket: origin check failed")
		}
	}

	if ctx.Method != http.MethodGet {
		ctx.Throw(http.StatusMethodNotAllowed, "websocket: request method is not GET")
		return nil, errors.New("websocket: request method is not GET")
	}
	if !headerContains(ctx.Request.Header, "Connection", "upgrade") {
		ctx.Throw(http.StatusBadRequest, "missing Connection: upgrade")
		return nil, errors.New("websocket: missing Connection upgrade")
	}
	if !headerContains(ctx.Request.Header, "Upgrade", "websocket") {
		ctx.Throw(http.StatusBadRequest, "missing Upgrade: websocket")
		return nil, errors.New("websocket: missing Upgrade header")
	}
	if !headerContains(ctx.Request.Header, "Sec-WebSocket-Version", "13") {
		ctx.Set("Sec-WebSocket-Version", "13")
		ctx.Throw(http.StatusBadRequest, "unsupported websocket version: 13 required")
		return nil, errors.New("websocket: unsupported version")
	}
	key := ctx.Request.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		ctx.Throw(http.StatusBadRequest, "missing Sec-WebSocket-Key")
		return nil, errors.New("websocket: missing key")
	}

	var subprotocol string
	if opts != nil && len(opts.Subprotocols) > 0 {
		clientProtos := ctx.Request.Header.Get("Sec-WebSocket-Protocol")
		subprotocol = negotiateSubprotocol(clientProtos, opts.Subprotocols)
	}

	conn, brw, err := http.NewResponseController(ctx.Writer).Hijack()
	if err != nil {
		ctx.Throw(http.StatusInternalServerError, "hijack not supported")
		return nil, errors.New("websocket: hijack failed: " + err.Error())
	}
	ctx.flushed = true
	ctx.Status = http.StatusSwitchingProtocols
	ctx.statusSet = true

	_ = conn.SetDeadline(time.Time{})

	var br *bufio.Reader
	readBufSize := 4096
	if opts != nil && opts.ReadBufferSize > 0 {
		readBufSize = opts.ReadBufferSize
	}
	if brw.Reader.Buffered() > 0 {
		br = brw.Reader
	} else if brw.Reader.Size() >= readBufSize {
		br = brw.Reader
	} else {
		br = bufio.NewReaderSize(conn, readBufSize)
	}

	acceptKey := computeAcceptKey(key)
	p := make([]byte, 0, 256)
	p = append(p, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "...)
	p = append(p, acceptKey...)
	p = append(p, "\r\n"...)
	if subprotocol != "" {
		p = append(p, "Sec-WebSocket-Protocol: "...)
		p = append(p, subprotocol...)
		p = append(p, "\r\n"...)
	}
	p = append(p, "\r\n"...)

	if _, err := conn.Write(p); err != nil {
		conn.Close()
		return nil, errors.New("websocket: write handshake failed: " + err.Error())
	}

	ws := &WSConn{
		conn:   conn,
		br:     br,
		closed: make(chan struct{}),
	}
	if opts != nil && opts.MaxMessageSize > 0 {
		ws.maxMessageSize = opts.MaxMessageSize
	}
	return ws, nil
}

// --- event handlers (Node.js ws style) ---

func (ws *WSConn) OnMessage(fn func(messageType int, data []byte)) {
	ws.onMessage = fn
}

func (ws *WSConn) OnClose(fn func(code int, text string)) {
	ws.onClose = fn
}

func (ws *WSConn) OnError(fn func(err error)) {
	ws.onError = fn
}

func (ws *WSConn) OnPing(fn func(data []byte)) {
	ws.onPing = fn
}

func (ws *WSConn) OnPong(fn func(data []byte)) {
	ws.onPong = fn
}

// Listen runs a blocking read loop, dispatching to registered event handlers.
// It handles ping/pong automatically and calls OnMessage/OnClose/OnError.
func (ws *WSConn) Listen() {
	for {
		opcode, payload, err := ws.readMessage()
		if err != nil {
			ws.handleError(err)
			return
		}

		switch opcode {
		case PingMessage:
			if ws.onPing != nil {
				ws.onPing(payload)
			}
			_ = ws.writeFrame(PongMessage, payload)
		case PongMessage:
			if ws.onPong != nil {
				ws.onPong(payload)
			}
		case CloseMessage:
			code, text := parseClosePayload(payload)
			_ = ws.writeCloseFrame(code)
			ws.once.Do(func() { close(ws.closed) })
			if ws.onClose != nil {
				ws.onClose(code, text)
			}
			return
		case TextMessage, BinaryMessage:
			if ws.onMessage != nil {
				ws.onMessage(opcode, payload)
			}
		}
	}
}

// --- read/write API ---

func (ws *WSConn) ReadMessage() (messageType int, data []byte, err error) {
	ws.readMu.Lock()
	defer ws.readMu.Unlock()

	for {
		opcode, payload, err := ws.readMessage()
		if err != nil {
			ws.handleError(err)
			return 0, nil, err
		}

		switch opcode {
		case PingMessage:
			_ = ws.writeFrame(PongMessage, payload)
			continue
		case PongMessage:
			continue
		case CloseMessage:
			code, text := parseClosePayload(payload)
			_ = ws.writeCloseFrame(code)
			ws.once.Do(func() { close(ws.closed) })
			if ws.onClose != nil {
				ws.onClose(code, text)
			}
			return 0, nil, ErrWSClosed
		case TextMessage, BinaryMessage:
			return opcode, payload, nil
		default:
			return 0, nil, ErrWSInvalidFrame
		}
	}
}

func (ws *WSConn) ReadJSON(v any) error {
	_, data, err := ws.ReadMessage()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (ws *WSConn) WriteMessage(messageType int, data []byte) error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	return ws.writeFrame(messageType, data)
}

func (ws *WSConn) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return ws.WriteMessage(TextMessage, data)
}

func (ws *WSConn) Send(text string) error {
	return ws.WriteMessage(TextMessage, []byte(text))
}

func (ws *WSConn) WriteText(text string) error {
	return ws.WriteMessage(TextMessage, []byte(text))
}

func (ws *WSConn) WriteBinary(data []byte) error {
	return ws.WriteMessage(BinaryMessage, data)
}

func (ws *WSConn) Ping() error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	return ws.writeFrame(PingMessage, nil)
}

func (ws *WSConn) Close() error {
	ws.once.Do(func() { close(ws.closed) })
	ws.writeMu.Lock()
	_ = ws.writeFrame(CloseMessage, formatClosePayload(closeNormal, ""))
	ws.writeMu.Unlock()
	return ws.conn.Close()
}

func (ws *WSConn) CloseWithMessage(code int, text string) error {
	ws.once.Do(func() { close(ws.closed) })
	ws.writeMu.Lock()
	_ = ws.writeFrame(CloseMessage, formatClosePayload(code, text))
	ws.writeMu.Unlock()
	return ws.conn.Close()
}

func (ws *WSConn) Closed() <-chan struct{} {
	return ws.closed
}

func (ws *WSConn) SetReadDeadline(t time.Time) error {
	return ws.conn.SetReadDeadline(t)
}

func (ws *WSConn) SetWriteDeadline(t time.Time) error {
	return ws.conn.SetWriteDeadline(t)
}

func (ws *WSConn) Heartbeat(interval time.Duration) func() {
	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := ws.Ping(); err != nil {
					return
				}
			case <-ws.closed:
				return
			case <-stop:
				return
			}
		}
	}()

	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() { close(stop) })
		<-done
	}
}

func (ws *WSConn) NetConn() net.Conn {
	return ws.conn
}

// --- frame I/O (write directly to conn, read via bufio.Reader) ---

const continuationFrame = 0

func (ws *WSConn) readFrame() (opcode int, fin bool, payload []byte, err error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(ws.br, header); err != nil {
		return 0, false, nil, err
	}

	fin = header[0]&0x80 != 0
	if header[0]&0x70 != 0 {
		// RSV1-3 must be zero: no extensions are negotiated (RFC 6455 5.2)
		return 0, false, nil, ErrWSInvalidFrame
	}
	opcode = int(header[0] & 0x0F)
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7F)

	switch opcode {
	case continuationFrame, TextMessage, BinaryMessage:
	case CloseMessage, PingMessage, PongMessage:
		// control frames must not be fragmented and carry at most 125 bytes
		if !fin || length > 125 {
			return 0, false, nil, ErrWSInvalidFrame
		}
	default:
		return 0, false, nil, ErrWSInvalidFrame
	}

	// client-to-server frames must be masked (RFC 6455 5.1)
	if !masked {
		return 0, false, nil, ErrWSInvalidFrame
	}

	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(ws.br, ext); err != nil {
			return 0, false, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(ws.br, ext); err != nil {
			return 0, false, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}

	if length > uint64(ws.messageLimit()) {
		return 0, false, nil, ErrWSInvalidFrame
	}

	var maskKey [4]byte
	if _, err := io.ReadFull(ws.br, maskKey[:]); err != nil {
		return 0, false, nil, err
	}

	payload = make([]byte, length)
	if _, err := io.ReadFull(ws.br, payload); err != nil {
		return 0, false, nil, err
	}

	for i := range payload {
		payload[i] ^= maskKey[i%4]
	}

	return opcode, fin, payload, nil
}

// readMessage returns the next complete message, reassembling fragmented
// data frames. Control frames interleaved between fragments are returned
// immediately, as permitted by RFC 6455 5.4.
func (ws *WSConn) readMessage() (opcode int, payload []byte, err error) {
	var (
		msgType int
		buf     []byte
	)
	for {
		opcode, fin, data, err := ws.readFrame()
		if err != nil {
			return 0, nil, err
		}

		switch opcode {
		case CloseMessage, PingMessage, PongMessage:
			return opcode, data, nil
		case continuationFrame:
			if msgType == 0 {
				// continuation without a preceding data frame
				return 0, nil, ErrWSInvalidFrame
			}
			if len(buf)+len(data) > ws.messageLimit() {
				return 0, nil, ErrWSInvalidFrame
			}
			buf = append(buf, data...)
			if fin {
				return msgType, buf, nil
			}
		default: // TextMessage, BinaryMessage
			if msgType != 0 {
				// new data frame while a fragmented message is in progress
				return 0, nil, ErrWSInvalidFrame
			}
			if fin {
				return opcode, data, nil
			}
			msgType = opcode
			buf = append(buf, data...)
		}
	}
}

func (ws *WSConn) writeFrame(opcode int, payload []byte) error {
	length := len(payload)
	var header []byte

	switch {
	case length <= 125:
		header = []byte{byte(0x80 | opcode), byte(length)}
	case length <= 65535:
		header = make([]byte, 4)
		header[0] = byte(0x80 | opcode)
		header[1] = 126
		binary.BigEndian.PutUint16(header[2:], uint16(length))
	default:
		header = make([]byte, 10)
		header[0] = byte(0x80 | opcode)
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:], uint64(length))
	}

	var buf []byte
	if len(payload) > 0 {
		buf = make([]byte, len(header)+len(payload))
		copy(buf, header)
		copy(buf[len(header):], payload)
	} else {
		buf = header
	}

	_, err := ws.conn.Write(buf)
	return err
}

func (ws *WSConn) writeCloseFrame(code int) error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	return ws.writeFrame(CloseMessage, formatClosePayload(code, ""))
}

func (ws *WSConn) handleError(err error) {
	select {
	case <-ws.closed:
		// already closed locally or by peer; the trailing read error
		// ("use of closed network connection") is not an application error
	default:
		if ws.onError != nil {
			ws.onError(err)
		}
	}
	ws.once.Do(func() { close(ws.closed) })
}

// --- helpers ---

func computeAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func headerContains(h http.Header, key string, value string) bool {
	for _, v := range h[http.CanonicalHeaderKey(key)] {
		for s := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(s), value) {
				return true
			}
		}
	}
	return false
}

func negotiateSubprotocol(clientHeader string, serverProtocols []string) string {
	for _, sp := range serverProtocols {
		for cp := range strings.SplitSeq(clientHeader, ",") {
			if strings.TrimSpace(cp) == sp {
				return sp
			}
		}
	}
	return ""
}

func parseClosePayload(payload []byte) (int, string) {
	if len(payload) < 2 {
		return closeNormal, ""
	}
	code := int(binary.BigEndian.Uint16(payload[:2]))
	text := ""
	if len(payload) > 2 {
		text = string(payload[2:])
		if !utf8.ValidString(text) {
			text = ""
		}
	}
	return code, text
}

func formatClosePayload(code int, text string) []byte {
	payload := make([]byte, 2+len(text))
	binary.BigEndian.PutUint16(payload, uint16(code))
	copy(payload[2:], text)
	return payload
}
