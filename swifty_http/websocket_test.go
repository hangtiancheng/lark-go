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
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestComputeAcceptKey(t *testing.T) {
	// RFC 6455 Section 4.2.2 example
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	expected := base64.StdEncoding.EncodeToString(h.Sum(nil))
	got := computeAcceptKey(key)
	if got != expected {
		t.Errorf("computeAcceptKey(%q) = %q, want %q", key, got, expected)
	}
}

func TestHeaderContains(t *testing.T) {
	h := http.Header{}
	h.Set("Connection", "keep-alive, Upgrade")
	h.Set("Upgrade", "websocket")

	if !headerContains(h, "Connection", "upgrade") {
		t.Error("expected Connection to contain upgrade")
	}
	if !headerContains(h, "Upgrade", "websocket") {
		t.Error("expected Upgrade to contain websocket")
	}
	if headerContains(h, "Connection", "close") {
		t.Error("Connection should not contain close")
	}
}

func TestNegotiateSubprotocol(t *testing.T) {
	got := negotiateSubprotocol("chat, superchat", []string{"superchat", "chat"})
	if got != "superchat" {
		t.Errorf("negotiateSubprotocol = %q, want superchat", got)
	}

	got = negotiateSubprotocol("foo", []string{"bar"})
	if got != "" {
		t.Errorf("negotiateSubprotocol = %q, want empty", got)
	}
}

func TestParseClosePayload(t *testing.T) {
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, 1000)
	code, text := parseClosePayload(payload)
	if code != 1000 || text != "" {
		t.Errorf("parseClosePayload = (%d, %q), want (1000, \"\")", code, text)
	}

	payload = append(payload, []byte("bye")...)
	code, text = parseClosePayload(payload)
	if code != 1000 || text != "bye" {
		t.Errorf("parseClosePayload = (%d, %q), want (1000, \"bye\")", code, text)
	}
}

func TestWebSocketUpgradeAndEcho(t *testing.T) {
	app := New()
	app.Get("/ws", func(ctx *Context, next func()) {
		ws, err := ctx.Upgrade(nil)
		if err != nil {
			return
		}
		defer ws.Close()

		for {
			msgType, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if err := ws.WriteMessage(msgType, data); err != nil {
				return
			}
		}
	})

	server := httptest.NewServer(app)
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	key := base64.StdEncoding.EncodeToString([]byte("test-key-12345678"))

	req := "GET /ws HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Connection: Upgrade\r\n" +
		"Upgrade: websocket\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"\r\n"

	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}

	expectedAccept := computeAcceptKey(key)
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != expectedAccept {
		t.Fatalf("accept key mismatch: got %q, want %q", got, expectedAccept)
	}

	testMsg := []byte("hello websocket")
	if err := writeClientFrame(conn, TextMessage, testMsg); err != nil {
		t.Fatal(err)
	}

	opcode, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatal(err)
	}
	if opcode != TextMessage {
		t.Fatalf("expected text message, got opcode %d", opcode)
	}
	if string(payload) != "hello websocket" {
		t.Fatalf("echo mismatch: got %q", string(payload))
	}
}

func TestWebSocketJSON(t *testing.T) {
	app := New()

	type Message struct {
		Text string `json:"text"`
	}

	app.Get("/ws", func(ctx *Context, next func()) {
		ws, err := ctx.Upgrade(nil)
		if err != nil {
			return
		}
		defer ws.Close()

		var msg Message
		if err := ws.ReadJSON(&msg); err != nil {
			return
		}
		msg.Text = "echo: " + msg.Text
		_ = ws.WriteJSON(msg)
	})

	server := httptest.NewServer(app)
	defer server.Close()

	ws := dialWS(t, server.URL, "/ws")
	defer ws.Close()

	if err := writeClientFrame(ws.conn, TextMessage, []byte(`{"text":"hi"}`)); err != nil {
		t.Fatal(err)
	}

	_, payload, err := readServerFrame(ws.br)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"echo: hi"`) {
		t.Fatalf("unexpected response: %s", string(payload))
	}
}

func TestWebSocketHeartbeat(t *testing.T) {
	app := New()

	app.Get("/ws", func(ctx *Context, next func()) {
		ws, err := ctx.Upgrade(nil)
		if err != nil {
			return
		}
		defer ws.Close()

		stop := ws.Heartbeat(50 * time.Millisecond)
		defer stop()

		time.Sleep(200 * time.Millisecond)
	})

	server := httptest.NewServer(app)
	defer server.Close()

	ws := dialWS(t, server.URL, "/ws")
	defer ws.conn.Close()

	_ = ws.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

	pingCount := 0
	for range 5 {
		opcode, _, err := readServerFrame(ws.br)
		if err != nil {
			break
		}
		if opcode == PingMessage {
			pingCount++
			_ = writeClientFrame(ws.conn, PongMessage, nil)
		}
	}

	if pingCount == 0 {
		t.Error("expected at least one ping from heartbeat")
	}
}

func TestWebSocketClosedChannel(t *testing.T) {
	app := New()

	var serverWS *WSConn
	var wg sync.WaitGroup
	wg.Add(1)

	app.Get("/ws", func(ctx *Context, next func()) {
		ws, err := ctx.Upgrade(nil)
		if err != nil {
			return
		}
		serverWS = ws
		wg.Done()

		<-ws.Closed()
	})

	server := httptest.NewServer(app)
	defer server.Close()

	ws := dialWS(t, server.URL, "/ws")
	wg.Wait()

	select {
	case <-serverWS.Closed():
		t.Fatal("should not be closed yet")
	default:
	}

	ws.conn.Close()
	time.Sleep(50 * time.Millisecond)
}

// --- test helpers ---

type testWSClient struct {
	conn net.Conn
	br   *bufio.Reader
}

func dialWS(t *testing.T, serverURL string, path string) *testWSClient {
	t.Helper()
	addr := strings.TrimPrefix(serverURL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}

	key := base64.StdEncoding.EncodeToString([]byte("test-key-12345678"))
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Connection: Upgrade\r\n" +
		"Upgrade: websocket\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"\r\n"

	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}

	return &testWSClient{conn: conn, br: br}
}

func (c *testWSClient) Close() {
	_ = writeClientFrame(c.conn, CloseMessage, formatClosePayload(closeNormal, ""))
	c.conn.Close()
}

func writeClientFrame(conn net.Conn, opcode int, payload []byte) error {
	length := len(payload)
	var header []byte

	switch {
	case length <= 125:
		header = []byte{byte(0x80 | opcode), byte(0x80 | length)}
	case length <= 65535:
		header = make([]byte, 4)
		header[0] = byte(0x80 | opcode)
		header[1] = byte(0x80 | 126)
		binary.BigEndian.PutUint16(header[2:], uint16(length))
	default:
		header = make([]byte, 10)
		header[0] = byte(0x80 | opcode)
		header[1] = byte(0x80 | 127)
		binary.BigEndian.PutUint64(header[2:], uint64(length))
	}

	maskKey := [4]byte{0x12, 0x34, 0x56, 0x78}
	header = append(header, maskKey[:]...)

	masked := make([]byte, length)
	for i := range payload {
		masked[i] = payload[i] ^ maskKey[i%4]
	}

	if _, err := conn.Write(header); err != nil {
		return err
	}
	if _, err := conn.Write(masked); err != nil {
		return err
	}
	return nil
}

func readServerFrame(br *bufio.Reader) (int, []byte, error) {
	h := make([]byte, 2)
	if _, err := io.ReadFull(br, h); err != nil {
		return 0, nil, err
	}

	opcode := int(h[0] & 0x0F)
	length := uint64(h[1] & 0x7F)

	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(br, ext); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(br, ext); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		return 0, nil, err
	}

	return opcode, payload, nil
}

// writeClientFrameRaw writes a single frame with explicit FIN and mask control.
func writeClientFrameRaw(conn net.Conn, opcode int, fin bool, mask bool, payload []byte) error {
	b0 := byte(opcode)
	if fin {
		b0 |= 0x80
	}
	b1 := byte(len(payload))
	if mask {
		b1 |= 0x80
	}
	frame := []byte{b0, b1}
	if mask {
		maskKey := [4]byte{0x12, 0x34, 0x56, 0x78}
		frame = append(frame, maskKey[:]...)
		for i, p := range payload {
			frame = append(frame, p^maskKey[i%4])
		}
	} else {
		frame = append(frame, payload...)
	}
	_, err := conn.Write(frame)
	return err
}

func TestWebSocketFragmentedMessage(t *testing.T) {
	app := New()
	app.Get("/ws", func(ctx *Context, next func()) {
		ws, err := ctx.Upgrade(nil)
		if err != nil {
			return
		}
		defer ws.Close()
		msgType, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = ws.WriteMessage(msgType, data)
	})

	server := httptest.NewServer(app)
	defer server.Close()

	ws := dialWS(t, server.URL, "/ws")
	defer ws.Close()

	// text frame without FIN, continuation without FIN, final continuation
	if err := writeClientFrameRaw(ws.conn, TextMessage, false, true, []byte("hello ")); err != nil {
		t.Fatal(err)
	}
	if err := writeClientFrameRaw(ws.conn, 0, false, true, []byte("fragmented ")); err != nil {
		t.Fatal(err)
	}
	if err := writeClientFrameRaw(ws.conn, 0, true, true, []byte("world")); err != nil {
		t.Fatal(err)
	}

	opcode, payload, err := readServerFrame(ws.br)
	if err != nil {
		t.Fatal(err)
	}
	if opcode != TextMessage {
		t.Fatalf("opcode = %d, want text", opcode)
	}
	if string(payload) != "hello fragmented world" {
		t.Fatalf("reassembled message = %q", string(payload))
	}
}

func TestWebSocketRejectsUnmaskedClientFrame(t *testing.T) {
	app := New()
	app.Get("/ws", func(ctx *Context, next func()) {
		ws, err := ctx.Upgrade(nil)
		if err != nil {
			return
		}
		defer ws.Close()
		msgType, data, err := ws.ReadMessage()
		if err != nil {
			return // expected: unmasked frame must be rejected
		}
		_ = ws.WriteMessage(msgType, data)
	})

	server := httptest.NewServer(app)
	defer server.Close()

	ws := dialWS(t, server.URL, "/ws")
	defer ws.conn.Close()

	if err := writeClientFrameRaw(ws.conn, TextMessage, true, false, []byte("naked")); err != nil {
		t.Fatal(err)
	}

	_ = ws.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	opcode, payload, err := readServerFrame(ws.br)
	if err == nil && opcode == TextMessage && string(payload) == "naked" {
		t.Fatal("server echoed an unmasked frame; RFC 6455 requires rejecting it")
	}
}

func TestWebSocketUpgradeRequiresVersion13(t *testing.T) {
	app := New()
	app.Get("/ws", func(ctx *Context, next func()) {
		if _, err := ctx.Upgrade(nil); err != nil {
			return
		}
	})

	server := httptest.NewServer(app)
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	key := base64.StdEncoding.EncodeToString([]byte("test-key-12345678"))
	req := "GET /ws HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Connection: Upgrade\r\n" +
		"Upgrade: websocket\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for missing Sec-WebSocket-Version", resp.StatusCode)
	}
	if resp.Header.Get("Sec-Websocket-Version") != "13" {
		t.Fatalf("missing Sec-WebSocket-Version: 13 advisory header")
	}
}

// ensure sha1 import is used
var _ = sha1.New
