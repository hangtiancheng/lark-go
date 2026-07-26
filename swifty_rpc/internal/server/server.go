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
	"log"
	"net"
	"sync"
	"time"

	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/codec"
	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/limiter"
	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/protocol"
	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/transport"
)

type Server struct {
	services map[string]any
	limiter  *limiter.TokenBucket
	listener net.Listener
	handler  *Handler

	conns        map[*transport.TCPConnection]struct{}
	closing      chan struct{}
	mu           sync.Mutex
	wg           sync.WaitGroup
	serveWg      sync.WaitGroup
	shutdownOnce sync.Once
}

func NewServer(opts ...ServerOption) (*Server, error) {
	h, err := NewHandler(nil, WithHandlerCodec(codec.JSON))
	if err != nil {
		return nil, err
	}

	s := &Server{
		services: make(map[string]any),
		limiter:  limiter.NewTokenBucket(10000),
		handler:  h,
		conns:    make(map[*transport.TCPConnection]struct{}),
		closing:  make(chan struct{}),
	}

	for _, opt := range opts {
		if err := opt(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Server) Register(name string, service any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.services[name] = service
}

func (s *Server) Handle(conn *transport.TCPConnection) {
	var streamWg sync.WaitGroup
	defer conn.Close()
	defer streamWg.Wait()

	for {
		msg, err := conn.Read()
		if err != nil {
			return
		}

		if !s.limiter.Allow() {
			resp := &protocol.Message{
				Header: &protocol.Header{
					RequestID:   msg.Header.RequestID,
					Error:       "rate limit exceeded",
					Compression: codec.CompressionGzip,
				},
			}
			_ = conn.Write(resp)
			continue
		}
		s.mu.Lock()
		service := s.services[msg.Header.ServiceName]
		s.mu.Unlock()
		s.handler.Process(conn, msg, service, &streamWg)
	}
}

func (s *Server) Serve(lis net.Listener) error {
	s.mu.Lock()
	s.listener = lis
	s.mu.Unlock()

	// A shutdown that ran before the listener was registered could not
	// close it; honour that shutdown here instead of blocking in Accept.
	select {
	case <-s.closing:
		_ = lis.Close()
		return nil
	default:
	}

	s.serveWg.Add(1)
	defer s.serveWg.Done()

	for {
		conn, err := lis.Accept()
		if err != nil {
			select {
			case <-s.closing:
				return nil
			default:
				continue
			}
		}

		tcpConn := transport.NewTCPConnection(conn)

		s.mu.Lock()
		s.conns[tcpConn] = struct{}{}
		s.mu.Unlock()

		s.wg.Go(func() {
			s.Handle(tcpConn)
			s.mu.Lock()
			delete(s.conns, tcpConn)
			s.mu.Unlock()
		})
	}
}

func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

func (s *Server) GracefulStop() {
	s.beginShutdown()

	// Wait for the accept loop to exit so no new connection can be
	// registered after the interrupt sweep below.
	s.serveWg.Wait()

	// Interrupt connections blocked in Read. Handlers that are mid-request
	// (including active streams) finish their work before the connection
	// goroutine exits; idle connections exit immediately.
	s.mu.Lock()
	for conn := range s.conns {
		_ = conn.SetReadDeadline(time.Now())
	}
	s.mu.Unlock()

	s.wg.Wait()
	s.limiter.Stop()

	log.Println("server graceful stop complete")
}

func (s *Server) Stop() {
	s.beginShutdown()

	s.serveWg.Wait()

	s.mu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.limiter.Stop()

	log.Println("server stop complete")
}

func (s *Server) beginShutdown() {
	s.shutdownOnce.Do(func() {
		close(s.closing)

		s.mu.Lock()
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.mu.Unlock()
	})
}
