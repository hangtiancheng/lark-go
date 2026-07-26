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
	"fmt"
	"reflect"
	"sync"

	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/codec"
	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/protocol"
	istream "github.com/hangtiancheng/swifty.go/swifty_rpc/internal/stream"
	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/transport"
)

var (
	serverStreamType = reflect.TypeFor[istream.ServerStream]()
	contextType      = reflect.TypeFor[context.Context]()
	errorType        = reflect.TypeFor[error]()
)

type Handler struct {
	codec codec.Codec
}

func NewHandler(s any, opts ...HandleOption) (*Handler, error) {
	h := &Handler{}

	for _, opt := range opts {
		if err := opt(h); err != nil {
			return nil, err
		}
	}

	if h.codec == nil {
		return nil, fmt.Errorf("codec must not be nil")
	}

	return h, nil
}

func (h *Handler) Process(conn *transport.TCPConnection, msg *protocol.Message, server any, streamWg *sync.WaitGroup) {
	// Honour the codec announced by the client; fall back to the server
	// codec for peers that do not set Header.CodecType.
	reqCodec := h.codec
	if ct := msg.Header.CodecType; ct != 0 {
		cc, err := codec.New(codec.Type(ct))
		if err != nil {
			h.writeError(conn, msg.Header.RequestID, err.Error())
			return
		}
		reqCodec = cc
	}

	result, streaming, err := h.invoke(
		context.Background(),
		conn,
		msg.Header.RequestID,
		server,
		msg.Header.ServiceName,
		msg.Header.MethodName,
		msg.Body,
		reqCodec,
		streamWg,
	)

	if streaming {
		return
	}

	if err != nil {
		h.writeError(conn, msg.Header.RequestID, err.Error())
		return
	}

	var body []byte
	if result != nil {
		var marshalErr error
		body, marshalErr = reqCodec.Marshal(result)
		if marshalErr != nil {
			h.writeError(conn, msg.Header.RequestID, marshalErr.Error())
			return
		}
	}

	resp := &protocol.Message{
		Header: &protocol.Header{
			RequestID:   msg.Header.RequestID,
			Compression: codec.CompressionGzip,
		},
		Body: body,
	}

	_ = conn.Write(resp)
}

func (h *Handler) writeError(conn *transport.TCPConnection, requestID uint64, errMsg string) {
	resp := &protocol.Message{
		Header: &protocol.Header{
			RequestID:   requestID,
			Error:       errMsg,
			Compression: codec.CompressionGzip,
		},
	}
	_ = conn.Write(resp)
}

// safeCall invokes a service method and converts panics into errors so a
// misbehaving handler cannot crash the whole server process.
func safeCall(method reflect.Value, args []reflect.Value) (results []reflect.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return method.Call(args), nil
}

func (h *Handler) invoke(ctx context.Context, conn *transport.TCPConnection, requestID uint64, service any, serviceName, methodName string, body []byte, cc codec.Codec, streamWg *sync.WaitGroup) (any, bool, error) {
	if service == nil {
		return nil, false, fmt.Errorf("service not found: %s", serviceName)
	}

	serviceValue := reflect.ValueOf(service)
	method := serviceValue.MethodByName(methodName)
	if !method.IsValid() {
		return nil, false, fmt.Errorf("method not found: %s.%s", serviceName, methodName)
	}

	methodType := method.Type()
	numIn := methodType.NumIn()
	numOut := methodType.NumOut()

	// grpc-go style: (ctx context.Context, req *T) (*R, error)
	if numIn == 2 && numOut == 2 &&
		methodType.In(0).Implements(contextType) &&
		methodType.In(1).Kind() == reflect.Pointer &&
		methodType.Out(0).Kind() == reflect.Pointer &&
		methodType.Out(1).Implements(errorType) {

		req := reflect.New(methodType.In(1).Elem())
		if len(body) > 0 {
			if err := cc.Unmarshal(body, req.Interface()); err != nil {
				return nil, false, err
			}
		}
		results, err := safeCall(method, []reflect.Value{reflect.ValueOf(ctx), req})
		if err != nil {
			return nil, false, err
		}
		if errVal := results[1].Interface(); errVal != nil {
			return nil, false, errVal.(error)
		}
		if results[0].IsNil() {
			return nil, false, nil
		}
		return results[0].Elem().Interface(), false, nil
	}

	// net/rpc style: (req *T, reply *R) error  OR  (req *T, stream ServerStream) error
	if numIn == 2 && numOut == 1 && methodType.Out(0).Implements(errorType) {
		reqType := methodType.In(0)

		if reqType.Kind() != reflect.Pointer {
			return nil, false, fmt.Errorf("unsupported method signature: %s.%s", serviceName, methodName)
		}

		req := reflect.New(reqType.Elem())
		if len(body) > 0 {
			if err := cc.Unmarshal(body, req.Interface()); err != nil {
				return nil, false, err
			}
		}

		secondParam := methodType.In(1)

		if secondParam.Implements(serverStreamType) {
			ss := &serverStream{
				conn:      conn,
				requestID: requestID,
				codec:     cc,
				ctx:       ctx,
			}
			args := []reflect.Value{req, reflect.ValueOf(ss).Convert(secondParam)}
			run := func() {
				results, err := safeCall(method, args)
				if err == nil {
					if errVal := results[0].Interface(); errVal != nil {
						err = errVal.(error)
					}
				}
				if err != nil {
					_ = ss.sendError(err.Error())
				} else {
					_ = ss.end()
				}
			}
			// Run the streaming handler asynchronously so a long-lived
			// stream does not block every other request multiplexed on
			// this connection. streamWg keeps the connection open until
			// all streams complete.
			if streamWg != nil {
				streamWg.Go(func() {
					run()
				})
			} else {
				run()
			}
			return nil, true, nil
		}

		if secondParam.Kind() == reflect.Pointer {
			reply := reflect.New(secondParam.Elem())
			results, err := safeCall(method, []reflect.Value{req, reply})
			if err != nil {
				return nil, false, err
			}
			if errVal := results[0].Interface(); errVal != nil {
				return nil, false, errVal.(error)
			}
			return reply.Elem().Interface(), false, nil
		}
	}

	return nil, false, fmt.Errorf("unsupported method signature: %s.%s", serviceName, methodName)
}
