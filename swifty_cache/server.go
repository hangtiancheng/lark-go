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

package swifty_cache

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"log"

	pb "github.com/hangtiancheng/swifty.go/swifty_cache/pb"
	client_v3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	grpc_health "google.golang.org/grpc/health/grpc_health_v1"
)

// Server exposes cache groups over gRPC.
type Server struct {
	pb.UnimplementedSwiftyCacheServer
	addr         string
	svcName      string
	grpcServer   *grpc.Server
	healthServer *health.Server
	etcdCli      *client_v3.Client
	stopCh       chan error
	stopOnce     sync.Once
	opts         *ServerOptions
}

// ServerOptions configures a cache server.
type ServerOptions struct {
	EtcdEndpoints []string
	DialTimeout   time.Duration
	MaxMsgSize    int
}

// DefaultServerOptions contains default server settings.
var DefaultServerOptions = &ServerOptions{
	EtcdEndpoints: []string{"localhost:2379"},
	DialTimeout:   5 * time.Second,
	MaxMsgSize:    4 << 20,
}

// ServerOption mutates server options.
type ServerOption func(*ServerOptions)

// WithEtcdEndpoints sets the etcd endpoints used for registration.
func WithEtcdEndpoints(endpoints []string) ServerOption {
	return func(o *ServerOptions) {
		o.EtcdEndpoints = endpoints
	}
}

// WithDialTimeout sets the etcd dial timeout.
func WithDialTimeout(timeout time.Duration) ServerOption {
	return func(o *ServerOptions) {
		o.DialTimeout = timeout
	}
}

// NewServer creates a gRPC cache server.
func NewServer(addr, svcName string, opts ...ServerOption) (*Server, error) {
	options := *DefaultServerOptions
	for _, opt := range opts {
		opt(&options)
	}

	etcdCli, err := client_v3.New(client_v3.Config{
		Endpoints:   options.EtcdEndpoints,
		DialTimeout: options.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %v", err)
	}

	serverOpts := []grpc.ServerOption{grpc.MaxRecvMsgSize(options.MaxMsgSize)}

	srv := &Server{
		addr:       addr,
		svcName:    svcName,
		grpcServer: grpc.NewServer(serverOpts...),
		etcdCli:    etcdCli,
		stopCh:     make(chan error),
		opts:       &options,
	}

	pb.RegisterSwiftyCacheServer(srv.grpcServer, srv)

	healthServer := health.NewServer()
	grpc_health.RegisterHealthServer(srv.grpcServer, healthServer)
	healthServer.SetServingStatus(svcName, grpc_health.HealthCheckResponse_SERVING)
	srv.healthServer = healthServer

	return srv, nil
}

// Start registers the server and starts serving gRPC requests.
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	go func() {
		regCfg := &RegisterConfig{
			Endpoints:   s.opts.EtcdEndpoints,
			DialTimeout: s.opts.DialTimeout,
		}
		if err := registerWithConfig(s.svcName, s.addr, s.stopCh, regCfg); err != nil {
			log.Printf("failed to register service: %v", err)
		}
	}()

	log.Printf("Server starting at %s", s.addr)
	return s.grpcServer.Serve(lis)
}

// Stop gracefully stops the server and closes external resources.
func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		if s.healthServer != nil {
			s.healthServer.SetServingStatus(s.svcName, grpc_health.HealthCheckResponse_NOT_SERVING)
		}
		close(s.stopCh)
		s.grpcServer.GracefulStop()
		if s.etcdCli != nil {
			_ = s.etcdCli.Close()
		}
	})
}

// Get implements the gRPC Get method.
func (s *Server) Get(ctx context.Context, req *pb.Request) (*pb.ResponseForGet, error) {
	group := GetGroup(req.Group)
	if group == nil {
		return nil, fmt.Errorf("group %s not found", req.Group)
	}
	group.stats.serverRequests.Add(1)

	view, err := group.Get(withPeerRequest(ctx), req.Key)
	if err != nil {
		return nil, err
	}
	return &pb.ResponseForGet{Value: view.ByteSlice()}, nil
}

// Set implements the gRPC Set method.
func (s *Server) Set(ctx context.Context, req *pb.Request) (*pb.ResponseForGet, error) {
	group := GetGroup(req.Group)
	if group == nil {
		return nil, fmt.Errorf("group %s not found", req.Group)
	}

	if !isPeerRequest(ctx) {
		ctx = withPeerRequest(ctx)
	}
	if err := group.Set(ctx, req.Key, req.Value); err != nil {
		return nil, err
	}
	return &pb.ResponseForGet{Value: req.Value}, nil
}

// Delete implements the gRPC Delete method.
func (s *Server) Delete(ctx context.Context, req *pb.Request) (*pb.ResponseForDelete, error) {
	group := GetGroup(req.Group)
	if group == nil {
		return nil, fmt.Errorf("group %s not found", req.Group)
	}

	err := group.Delete(withPeerRequest(ctx), req.Key)
	return &pb.ResponseForDelete{Value: err == nil}, err
}
