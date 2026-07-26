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

package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/protocol"
)

type testCodec struct{}

func (testCodec) Marshal(v any) ([]byte, error) {
	return []byte("unused"), nil
}

func (testCodec) Unmarshal(data []byte, v any) error {
	out := v.(*string)
	*out = "decoded:" + string(data)
	return nil
}

func TestFutureCompletion(t *testing.T) {
	f := NewFuture()
	var callbacks int32
	f.OnComplete(func(err error) {
		if err != nil {
			t.Fatalf("callback error = %v", err)
		}
		atomic.AddInt32(&callbacks, 1)
	})
	f.Done([]byte(`{"Name":"ok","Size":1}`), nil)
	f.Done(nil, errors.New("ignored"))
	if !f.IsDone() {
		t.Fatal("future should be done")
	}
	if atomic.LoadInt32(&callbacks) != 1 {
		t.Fatalf("callbacks = %d, want 1", callbacks)
	}
	data, err := f.Wait()
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if string(data) == "" {
		t.Fatal("expected result data")
	}
	var out struct {
		Name string
		Size int
	}
	if err := f.GetResult(&out); err != nil {
		t.Fatalf("GetResult returned error: %v", err)
	}
	if out.Name != "ok" || out.Size != 1 {
		t.Fatalf("decoded result = %+v", out)
	}
}

func TestFutureUsesConfiguredCodec(t *testing.T) {
	f := NewFutureWithCodec(testCodec{})
	f.Done([]byte("raw"), nil)

	var out string
	if err := f.GetResult(&out); err != nil {
		t.Fatalf("GetResult returned error: %v", err)
	}
	if out != "decoded:raw" {
		t.Fatalf("decoded result = %q", out)
	}
}

func TestFutureTimeoutsAndErrors(t *testing.T) {
	f := NewFuture()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := f.WaitWithContext(ctx); err == nil {
		t.Fatal("expected WaitWithContext timeout")
	}
	if _, err := f.WaitWithTimeout(time.Millisecond); err == nil {
		t.Fatal("expected WaitWithTimeout timeout")
	}
	if err := f.GetResultWithContext(ctx, &struct{}{}); err == nil {
		t.Fatal("expected GetResultWithContext timeout")
	}
	want := errors.New("failed")
	f.Done(nil, want)
	if _, err := f.Wait(); !errors.Is(err, want) {
		t.Fatalf("Wait error = %v, want %v", err, want)
	}
	if err := f.GetResult(&struct{}{}); !errors.Is(err, want) {
		t.Fatalf("GetResult error = %v, want %v", err, want)
	}
}

func TestTCPConnectionAndPacketBuffer(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := NewTCPConnection(clientConn)
	server := NewTCPConnection(serverConn)
	want := &protocol.Message{
		Header: &protocol.Header{RequestID: 9, ServiceName: "S", MethodName: "M"},
		Body:   []byte("body"),
	}
	errCh := make(chan error, 1)
	go func() { errCh <- client.Write(want) }()
	got, err := server.Read()
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got.Header.RequestID != want.Header.RequestID || string(got.Body) != "body" {
		t.Fatalf("message = %+v body=%q", got.Header, got.Body)
	}

	encoded, err := protocol.Encode(want)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	var pb PacketBuffer
	pb.Write(encoded[:5])
	if pkt := pb.Read(); pkt != nil {
		t.Fatal("partial packet should not be ready")
	}
	pb.Write(encoded[5:])
	if pkt := pb.Read(); len(pkt) != len(encoded) {
		t.Fatalf("packet len = %d, want %d", len(pkt), len(encoded))
	}
}

func TestTCPClientSendAsync(t *testing.T) {
	lis, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer lis.Close()
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		tc := NewTCPConnection(conn)
		defer tc.Close()
		msg, err := tc.Read()
		if err != nil {
			return
		}
		_ = tc.Write(&protocol.Message{
			Header: &protocol.Header{RequestID: msg.Header.RequestID},
			Body:   []byte(`{"Result":12}`),
		})
	}()
	client, err := newTCPClient(lis.Addr().String())
	if err != nil {
		t.Fatalf("newTCPClient returned error: %v", err)
	}
	defer client.Close()
	future, err := client.SendAsync(&protocol.Message{Header: &protocol.Header{}, Body: []byte("request")})
	if err != nil {
		t.Fatalf("SendAsync returned error: %v", err)
	}
	data, err := future.WaitWithTimeout(time.Second)
	if err != nil {
		t.Fatalf("WaitWithTimeout returned error: %v", err)
	}
	if string(data) != `{"Result":12}` {
		t.Fatalf("data = %q", data)
	}
}

func TestTCPClientCloseAndPool(t *testing.T) {
	if _, err := newTCPClient("127.0.0.1:1"); err == nil {
		t.Fatal("expected dial error")
	}
	lis, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer lis.Close()
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	pool := NewConnectionPool(lis.Addr().String(), 0, 1)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pool.Acquire(cancelled); err == nil {
		t.Fatal("expected canceled context error")
	}
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if _, err := pool.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire existing returned error: %v", err)
	}
	_ = conn.Close()
	if _, err := pool.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire after closed connection returned error: %v", err)
	}
	pool.Close()
	pool.Close()
	if _, err := pool.Acquire(context.Background()); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("Acquire after Close = %v, want ErrPoolClosed", err)
	}
}

func TestTCPClientCloseUnblocksPending(t *testing.T) {
	lis, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer lis.Close()
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		// Never respond; just hold the connection open.
		buf := make([]byte, 1024)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	client, err := newTCPClient(lis.Addr().String())
	if err != nil {
		t.Fatalf("newTCPClient returned error: %v", err)
	}
	future, err := client.SendAsync(&protocol.Message{Header: &protocol.Header{}, Body: []byte("request")})
	if err != nil {
		t.Fatalf("SendAsync returned error: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := future.Wait()
		done <- err
	}()
	_ = client.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected connection closed error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock pending future")
	}
}

func TestClientStreamTerminalNonBlocking(t *testing.T) {
	s := NewClientStreamConn(context.Background(), testCodec{})
	// Fill the data buffer completely.
	for range 64 {
		s.Push([]byte("frame"))
	}
	// End must not block even though the buffer is full.
	terminated := make(chan struct{})
	go func() {
		s.End()
		close(terminated)
	}()
	select {
	case <-terminated:
	case <-time.After(time.Second):
		t.Fatal("End blocked on a full stream buffer")
	}
	// All buffered frames must still be delivered before EOF.
	for i := range 64 {
		var out string
		if err := s.Recv(&out); err != nil {
			t.Fatalf("Recv[%d] returned error: %v", i, err)
		}
	}
	var out string
	if err := s.Recv(&out); err != io.EOF {
		t.Fatalf("Recv after end = %v, want io.EOF", err)
	}
}
