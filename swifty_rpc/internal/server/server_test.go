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

package server

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/codec"
	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/protocol"
	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/transport"
)

type testArgs struct {
	A int
	B int
}

type testReply struct {
	Result int
}

type testService struct{}

func (s *testService) Add(args *testArgs, reply *testReply) error {
	reply.Result = args.A + args.B
	return nil
}

func (s *testService) Fail(args *testArgs, reply *testReply) error {
	return errors.New("service failed")
}

func (s *testService) Bad(args *testArgs) {}

type grpcStyleService struct{}

func (s *grpcStyleService) Add(ctx context.Context, args *testArgs) (*testReply, error) {
	return &testReply{Result: args.A + args.B}, nil
}

func (s *grpcStyleService) Fail(ctx context.Context, args *testArgs) (*testReply, error) {
	return nil, errors.New("service failed")
}

func (s *grpcStyleService) NilReply(ctx context.Context, args *testArgs) (*testReply, error) {
	return nil, nil
}

func TestHandlerInvokeGRPCStyle(t *testing.T) {
	h, err := NewHandler(nil, WithHandlerCodec(codec.JSON))
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}
	cc, _ := codec.New(codec.JSON)
	body, err := cc.Marshal(&testArgs{A: 3, B: 4})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	result, _, err := h.invoke(t.Context(), nil, 1, &grpcStyleService{}, "Test", "Add", body, cc, nil)
	if err != nil {
		t.Fatalf("invoke returned error: %v", err)
	}
	if result.(testReply).Result != 7 {
		t.Fatalf("result = %+v", result)
	}
	if _, _, err := h.invoke(t.Context(), nil, 1, &grpcStyleService{}, "Test", "Fail", body, cc, nil); err == nil || !strings.Contains(err.Error(), "service failed") {
		t.Fatalf("expected service error, got %v", err)
	}
	result, _, err = h.invoke(t.Context(), nil, 1, &grpcStyleService{}, "Test", "NilReply", body, cc, nil)
	if err != nil {
		t.Fatalf("NilReply returned error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result, got %v", result)
	}
}

func TestHandlerInvoke(t *testing.T) {
	h, err := NewHandler(nil, WithHandlerCodec(codec.JSON))
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}
	cc, _ := codec.New(codec.JSON)
	body, err := cc.Marshal(&testArgs{A: 2, B: 5})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	result, _, err := h.invoke(t.Context(), nil, 1, &testService{}, "Test", "Add", body, cc, nil)
	if err != nil {
		t.Fatalf("invoke returned error: %v", err)
	}
	if result.(testReply).Result != 7 {
		t.Fatalf("result = %+v", result)
	}
	if _, _, err := h.invoke(t.Context(), nil, 1, nil, "Missing", "Add", body, cc, nil); err == nil || !strings.Contains(err.Error(), "service not found") {
		t.Fatalf("expected missing service error, got %v", err)
	}
	if _, _, err := h.invoke(t.Context(), nil, 1, &testService{}, "Test", "Missing", body, cc, nil); err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("expected missing method error, got %v", err)
	}
	if _, _, err := h.invoke(t.Context(), nil, 1, &testService{}, "Test", "Fail", body, cc, nil); err == nil || !strings.Contains(err.Error(), "service failed") {
		t.Fatalf("expected service error, got %v", err)
	}
	if _, _, err := h.invoke(t.Context(), nil, 1, &testService{}, "Test", "Bad", body, cc, nil); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported signature error, got %v", err)
	}
	if _, _, err := h.invoke(t.Context(), nil, 1, &testService{}, "Test", "Add", []byte("{bad"), cc, nil); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestHandlerProcessWritesResponses(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := transport.NewTCPConnection(clientConn)
	serverConnWrapper := transport.NewTCPConnection(serverConn)
	h, err := NewHandler(nil, WithHandlerCodec(codec.JSON))
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}
	cc, _ := codec.New(codec.JSON)
	body, err := cc.Marshal(&testArgs{A: 3, B: 4})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	go h.Process(serverConnWrapper, &protocol.Message{
		Header: &protocol.Header{RequestID: 1, ServiceName: "Test", MethodName: "Add"},
		Body:   body,
	}, &testService{}, nil)
	resp, err := client.Read()
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	var reply testReply
	if err := cc.Unmarshal(resp.Body, &reply); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if reply.Result != 7 || resp.Header.Error != "" {
		t.Fatalf("response = %+v reply=%+v", resp.Header, reply)
	}

	go h.Process(serverConnWrapper, &protocol.Message{
		Header: &protocol.Header{RequestID: 2, ServiceName: "Test", MethodName: "Fail"},
		Body:   body,
	}, &testService{}, nil)
	resp, err = client.Read()
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if !strings.Contains(resp.Header.Error, "service failed") {
		t.Fatalf("error response = %+v", resp.Header)
	}
}

func TestServerHandleAndStop(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := transport.NewTCPConnection(clientConn)
	serverSide := transport.NewTCPConnection(serverConn)
	s, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	s.Register("Test", &testService{})
	go s.Handle(serverSide)
	cc, _ := codec.New(codec.JSON)
	body, err := cc.Marshal(&testArgs{A: 8, B: 9})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if err := client.Write(&protocol.Message{
		Header: &protocol.Header{RequestID: 3, ServiceName: "Test", MethodName: "Add"},
		Body:   body,
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	resp, err := client.Read()
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	var reply testReply
	if err := cc.Unmarshal(resp.Body, &reply); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if reply.Result != 17 {
		t.Fatalf("reply = %+v", reply)
	}
	_ = client.Close()
	s.Stop()
}

func TestServerServeAndStop(t *testing.T) {
	s, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(lis) }()
	time.Sleep(10 * time.Millisecond)
	s.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after Stop")
	}
}

func TestServerOptionsAndRegisterConcurrent(t *testing.T) {
	s, err := NewServer(WithServerCodec(codec.JSON))
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			s.Register("Test", &testService{})
		})
	}
	wg.Wait()
	if s.services["Test"] == nil {
		t.Fatal("service was not registered")
	}
	if _, err := NewServer(WithServerCodec(codec.Type(99))); err == nil {
		t.Fatal("expected invalid codec error")
	}
	if _, err := NewHandler(nil); err == nil {
		t.Fatal("expected missing codec error")
	}
	if _, err := NewHandler(nil, WithHandlerCodec(codec.Type(99))); err == nil {
		t.Fatal("expected invalid handler codec error")
	}
}
