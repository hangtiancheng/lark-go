---
name: swifty-rpc
description: >
  TCP-based RPC framework for Go (module path:
  github.com/hangtiancheng/swifty.go/swifty_rpc). Hand-rolled binary protocol,
  not gRPC. Key exported symbols: rpc.Dial, rpc.NewServer, rpc.NewRegistry,
  ClientConn (Invoke, InvokeAsync, NewStream, Close), Future (Wait,
  WaitWithContext, WaitWithTimeout, GetResult, GetResultWithContext, DoneChan,
  IsDone, OnComplete, Done), Server (Register, Serve, GracefulStop, Stop),
  ServerStream (Send, Context), ClientStream (Recv, Context), DialOption,
  ServerOption, CodecType, CodecJSON, CodecProto, Registry (Register, Discover,
  Close), Instance, LoadBalancer, WithTimeout, WithDialCodec, WithRegistry,
  WithLoadBalancer, WithCodec, plus the pkg/api sample services Arith, Arith2,
  Args, Args1, Reply. Use when writing or modifying Go code that imports
  swifty_rpc, calls Invoke/InvokeAsync/NewStream, registers services on
  rpc.NewServer, uses etcd discovery via rpc.NewRegistry, or asks about the
  binary wire protocol (Magic 0x1234 framing, big-endian HeaderLen/BodyLen,
  JSON header, mandatory gzip body), StreamFlag frames (StreamNone, StreamData,
  StreamEnd, StreamError), per-header codec negotiation, RequestID
  multiplexing over one socket, reflection dispatch (grpc-go and net/rpc
  signatures), server-streaming, the three-state circuit breaker,
  token-bucket rate limiting, round-robin/random/weighted-round-robin load
  balancing, connection pooling, or graceful shutdown. Do NOT use for grpc-go
  itself, Connect-Go, Twirp, HTTP/SSE/WebSocket work (use swifty-http), or
  Protobuf schema questions unrelated to swifty_rpc.
---

# swifty_rpc

`github.com/hangtiancheng/swifty.go/swifty_rpc` is a hand-rolled TCP RPC framework. It does not use gRPC, HTTP/2, or any code generator: it defines its own length-delimited binary frame, its own JSON header, its own request multiplexing, and its own reflection-based server dispatch. The public API deliberately mimics grpc-go naming (`Dial`, `ClientConn`, `Invoke`, `NewStream`, `NewServer`, `Serve`, `GracefulStop`) so the call sites read familiarly, but nothing underneath is shared with grpc-go. `google.golang.org/grpc` appears in `go.mod` only as an indirect dependency pulled in by the etcd client; the framework never uses it for transport.

The design philosophy is one-socket-per-address multiplexing plus explicit resilience primitives. A single TCP connection carries every concurrent request to a peer, correlated by a monotonically increasing `RequestID`; a single reader goroutine demultiplexes responses into futures and stream buffers. Resilience (circuit breaking, rate limiting, load balancing, service discovery) is wired only into the registry-backed client path; the static single-address path is deliberately bare.

Public surface: `pkg/rpc` (client, server, options, aliases) and `pkg/api` (sample arithmetic services used by tests and examples). Everything else lives under `internal/` and is not importable from outside the module, yet its behaviour is fully user-visible: wire format, framing, codec negotiation, breaker thresholds, balancer selection, lease TTL semantics, and stream buffering all leak into observable client and server behaviour. This document covers both.

## When to load adjacent skills

- Load `swifty-orm` when the RPC handlers you are writing persist to MongoDB. Service methods registered with `Server.Register` commonly wrap `swifty_orm` queries.
- Load `swifty-cache` when you want to memoize RPC replies or back a handler with a distributed cache. swifty_rpc has no cache of its own.
- Load `swifty-http` when the task involves HTTP, REST, SSE, or WebSocket. swifty_rpc cannot speak HTTP; there is no gateway or transcoding layer.

Do not load any of them for pure protocol, transport, or dispatch questions about swifty_rpc.

## Architecture overview

```
                         PUBLIC (importable)                    INTERNAL (not importable)
  ==================================================  ||  ==========================================

   user code
      |
      | rpc.Dial(target, opts...)                     ||
      v                                               ||
  +---------------------------+                       ||
  | rpc.ClientConn            |                       ||
  |  mode: static | registry  |                       ||
  |  codec, codecType, timeout|                       ||
  +------+-------------+------+                       ||
         |             |                              ||
  static |             | registry (WithRegistry)      ||
         |             +----------------------------->||  +--------------------------------+
         |                                            ||  | internal/client.Client         |
         |                                            ||  |  limiter.TokenBucket(10000)    |
         |                                            ||  |  load_balance.LoadBalancer     |
         |                                            ||  |  sync.Map pools  (addr -> pool)|
         |                                            ||  |  sync.Map breaker(svc|addr ->) |
         |                                            ||  +----+------------+----------+---+
         |                                            ||       |            |          |
         |                                            ||       v            v          v
         |                                            ||  registry.    breaker.    (pipeline order:
         |                                            ||  Registry     CircuitBrk   limiter -> addr
         |                                            ||  (etcd v3)    (10,0.6,5s)  -> breaker -> pool
         |                                            ||       |                    -> marshal -> send)
         |                                            ||       v
         |                                            ||  etcd KV + lease + watch
         |                                            ||
         +--------------------+-----------------------||-----------+
                              v                                    v
                       +-------------------------------------------------+
                       | internal/transport.ConnectionPool  (maxActive=1)|
                       |   Acquire(ctx) -> *TCPClient  (lazy dial)       |
                       +-----------------------+-------------------------+
                                               v
                       +-------------------------------------------------+
                       | internal/transport.TCPClient  (ONE per socket)  |
                       |   seq atomic.Uint64      (RequestID allocator)  |
                       |   pending sync.Map  seq -> *Future              |
                       |   streams sync.Map  seq -> *ClientStreamConn    |
                       |   goroutine: readLoop()  -> demux by StreamFlag |
                       +-----------------------+-------------------------+
                                               v
                       +-------------------------------------------------+
                       | internal/transport.TCPConnection                |
                       |   bufio.Reader(4096) + PacketBuffer (resync)    |
                       |   writeMu-serialized Write, SetLinger(0) Close  |
                       +-----------------------+-------------------------+
                                               v
                       +-------------------------------------------------+
                       | internal/protocol   Encode / Decode             |
                       |   Magic 0x1234 | HeaderLen | BodyLen | H | B    |
                       +-----------------------+-------------------------+
                                               v
                       +-------------------------------------------------+
                       | internal/codec   Codec (JSON=1, PROTO=2)        |
                       |   Compress/Decompress (CompressionGzip=1)       |
                       +-------------------------------------------------+
                                               |
                                          === TCP wire ===
                                               |
                       +-------------------------------------------------+
                       | internal/server.Server                         |
                       |   goroutine: Serve accept loop  (serveWg)       |
                       |   goroutine per conn: Handle    (wg)            |
                       |   limiter.TokenBucket(10000)   shared, global   |
                       |   services map[string]any      mu-guarded       |
                       +-----------------------+-------------------------+
                                               v
                       +-------------------------------------------------+
                       | internal/server.Handler                        |
                       |   Process: per-header codec selection           |
                       |   invoke:  reflect dispatch, 3 signatures       |
                       |   safeCall: recover -> "handler panic: v"       |
                       |   streaming -> streamWg.Go(run)                 |
                       +-----------------------+-------------------------+
                                               v
                       +-------------------------------------------------+
                       | internal/server.serverStream                   |
                       |   Send -> StreamData, end -> StreamEnd,         |
                       |   sendError -> StreamError                      |
                       +-------------------------------------------------+

  internal/stream  (interface-only package: ServerStream, ClientStream)
      breaks the would-be import cycle between internal/server and internal/transport;
      pkg/rpc re-exports both interfaces as type aliases.
```

Unary call flow, end to end:

```
Invoke(ctx, "Math", "Add", args, &reply)
  -> [registry mode] limiter.Allow()                        -> "rate limit exceeded"
  -> [registry mode] reg.Discover(svc) + lb.Select(list)    -> "no instance available" /
                                                               "load balancer returned empty address"
  -> [registry mode] breaker.Allow()                        -> "circuit breaker open"
  -> pool.Acquire(ctx bounded by timeout)                   -> dial error / ErrPoolClosed
  -> codec.Marshal(args)                                    -> marshal error
  -> TCPClient.SendAsyncWithCodec: seq = nextSeq(),
       pending[seq] = Future, protocol.Encode (gzip body),
       TCPConnection.Write
  -> Future.GetResultWithContext(ctx, reply)
       readLoop reads frame, StreamFlag==StreamNone,
       pending.LoadAndDelete(seq), Future.Done(body, err)
       -> [registry mode] Future.OnComplete -> breaker.RecordSuccess/RecordFailure
  -> codec.Unmarshal(body, reply)
```

Ownership and composition:

- A static-mode `ClientConn` owns exactly one `ConnectionPool` for the dialed target and one `codec.Codec` instance.
- A registry-mode `ClientConn` owns one `internal/client.Client`, which owns one `TokenBucket`, one `LoadBalancer`, one `ConnectionPool` per discovered address, and one `CircuitBreaker` per `service|addr` pair.
- Each `ConnectionPool` owns at most one `TCPClient` (`maxActive` is 1 at every call site in the codebase).
- Each `TCPClient` owns one `TCPConnection`, one `readLoop` goroutine, one `pending` map, and one `streams` map.
- A `Server` owns one `Handler`, one `TokenBucket`, a `conns` set, one accept-loop goroutine, one goroutine per accepted connection, and one goroutine per in-flight streaming handler (tracked by a per-connection `streamWg` declared inside `Handle`).
- A `Registry` owns one etcd client, one cancellable context, one lease-keepalive drain goroutine per `Register` call, and one watch goroutine per discovered service.

Two structural boundaries hold strictly:

1. `pkg/rpc` re-exports a minimal set of internal types via Go type aliases (`CodecType`, `Registry`, `Instance`, `LoadBalancer`, `Future`, `ServerStream`, `ClientStream`). Because they are aliases and not wrappers, every exported method on the aliased type is reachable from user code. Downstream code must never import `internal/*` directly; the compiler forbids it.
2. `internal/stream` contains nothing but the two stream interfaces. It exists solely so `internal/server` and `internal/transport` can both refer to the stream contracts without importing each other.

## Core types

### pkg/rpc type aliases

```go
type CodecType    = codec.Type              // byte; JSON = 1, PROTO = 2
type Registry     = registry.Registry
type Instance     = registry.Instance       // struct { Addr string }
type LoadBalancer = load_balance.LoadBalancer
type ServerStream = stream.ServerStream
type ClientStream = stream.ClientStream
type Future       = transport.Future        // async invocation handle
```

Because these are aliases, `rpc.Registry` is literally `registry.Registry`, so its methods are part of the public surface even though `pkg/rpc` declares no wrappers for them. Same for `Future` and the two stream interfaces.

### pkg/rpc exported variables

```go
var (
    CodecJSON  = codec.JSON   // CodecType(1)
    CodecProto = codec.PROTO  // CodecType(2)
)
```

These are `var`, not `const`, and they are typed `codec.Type` (that is, `CodecType`). They are the only codec identifiers a user is expected to pass to `WithDialCodec` and `WithCodec`. There is no exported way to register a third codec from outside the module: `codec.Register` lives in `internal/codec`.

### pkg/rpc exported functions

```go
func NewRegistry(endpoints []string) (*Registry, error)
func NewServer(opts ...ServerOption) *Server
func Dial(target string, opts ...DialOption) (*ClientConn, error)

func WithTimeout(d time.Duration) DialOption
func WithDialCodec(t CodecType) DialOption
func WithRegistry(reg *Registry) DialOption
func WithLoadBalancer(lb LoadBalancer) DialOption

func WithCodec(t CodecType) ServerOption
```

`NewRegistry` constructs an etcd v3 client with `DialTimeout: 5 * time.Second` and a hard-coded key prefix. It returns the etcd client construction error, which for etcd v3 is a configuration error, not a connectivity error: an unreachable etcd cluster still yields a non-nil `*Registry`, and the failure surfaces later from `Discover` or `Register`.

`NewServer` panics rather than returning an error. It builds an internal server, and on any option error panics with `"swifty_rpc: invalid server option: " + err.Error()`. The only option is `WithCodec`, which fails for an unregistered codec type.

`Dial` performs no network I/O. It validates the codec by instantiating it (`codec.New`), then branches:

- If `WithRegistry` was supplied, it builds an `internal/client.Client` with `WithClientTimeout(o.timeout)`, `WithClientCodec(o.codecType)`, and, when non-nil, `WithClientLoadBalancer(o.loadBalancer)`. `target` is ignored entirely in this mode.
- Otherwise it creates `transport.NewConnectionPool(target, 0, 1)` — a lazy, single-connection pool. The TCP connection is established by the first `Acquire`, that is, by the first `Invoke`, `InvokeAsync`, or `NewStream`.

### DialOption defaults and omission behaviour

| Option | Type | Default when omitted | Notes |
| --- | --- | --- | --- |
| `WithTimeout(d)` | `time.Duration` | `5 * time.Second` | Registry mode rejects `d <= 0` at `Dial` with `client timeout must be positive`. Static mode accepts `d <= 0` silently and then fails every call with `context.DeadlineExceeded`. |
| `WithDialCodec(t)` | `CodecType` | `CodecJSON` | Validated at `Dial`; unregistered type yields `codec: type <n> not registered`. |
| `WithRegistry(reg)` | `*Registry` | nil, so static mode | Presence alone switches the whole connection into registry mode. |
| `WithLoadBalancer(lb)` | `LoadBalancer` | `&load_balance.RoundRobin{}` (zero value) inside `internal/client.NewClient` | Silently ignored in static mode. A nil value passed explicitly is skipped by `Dial`, so the default survives. |

`ServerOption` has exactly one member:

| Option | Type | Default when omitted | Notes |
| --- | --- | --- | --- |
| `WithCodec(t)` | `CodecType` | `CodecJSON` | Sets the fallback codec used only when a request header carries `CodecType == 0`. Panics through `NewServer` if `t` is unregistered. |

### type ClientConn

```go
type ClientConn struct {
    // unexported: mode, pool, regClient, codec, codecType, timeout
}
```

The zero value is not usable: `mode` defaults to `modeStatic` and `pool`/`codec` are nil, so any method call nil-panics. Always construct with `Dial`.

| Method | Behaviour |
| --- | --- |
| `func (cc *ClientConn) Invoke(ctx context.Context, service, method string, args, reply any) error` | Blocking unary call. Wraps `ctx` with `cc.timeout` for both send and wait. Static mode: marshals `args`, sends, then `future.GetResultWithContext(ctx, reply)`. Registry mode: delegates to `internal/client.Client.Invoke`, which additionally forces `future.Done(nil, callCtx.Err())` on timeout or cancel so the breaker records the failure exactly once. |
| `func (cc *ClientConn) InvokeAsync(ctx context.Context, service, method string, args any) (*Future, error)` | Non-blocking send. Acquires the connection under a `cc.timeout`-bounded context (cancelled immediately after acquire), sends, and spawns one watchdog goroutine holding a `time.NewTimer(cc.timeout)`; if the timer fires before the future completes, the future is resolved with `context.DeadlineExceeded`. Available in both modes. |
| `func (cc *ClientConn) NewStream(ctx context.Context, service, method string, args any) (ClientStream, error)` | Opens a server-stream. Pool acquisition is bounded by `cc.timeout`; the stream itself is bound to the caller's `ctx`, which is the only way to cancel it. Registry mode returns an `observedStream` wrapper that feeds terminal outcomes into the circuit breaker. |
| `func (cc *ClientConn) Close() error` | Always returns nil. Static mode closes the pool (which closes the `TCPClient`, which reaps its `readLoop`). Registry mode calls `internal/client.Client.Close`, which stops the client's token-bucket ticker goroutine and closes every per-address pool. Does not close the `*Registry`; call `reg.Close()` yourself. Idempotent for the pool path (`ConnectionPool.Close` is guarded by a `closed` flag). |

Concurrency: a `*ClientConn` is safe for concurrent use by multiple goroutines. `ConnectionPool.Acquire` holds a mutex, `TCPClient` uses `sync.Map` plus an atomic sequence counter plus a write mutex, and `Future` is mutex-guarded. Concurrent `Close` during in-flight calls is safe: subsequent `Acquire` returns `ErrPoolClosed`, and futures already pending are resolved with `connection closed`.

### type Server

```go
type Server struct {
    inner *internal_server.Server
}
```

| Method | Behaviour |
| --- | --- |
| `func (s *Server) Register(name string, service any)` | Stores `service` in a mutex-guarded `map[string]any` under `name`. No signature validation at registration; shape errors surface per-call as `unsupported method signature`. Re-registering the same name overwrites. Safe to call concurrently and safe to call while `Serve` is running. |
| `func (s *Server) Serve(lis net.Listener) error` | Records the listener, then, if shutdown already began, closes the listener and returns nil immediately instead of blocking in `Accept`. Otherwise runs the accept loop: each accepted connection is wrapped in a `TCPConnection`, added to the `conns` set, and handled in its own goroutine registered on `wg`. Returns nil when `Accept` fails and the `closing` channel is closed; on any other `Accept` error it `continue`s. In practice `Serve` never returns a non-nil error. |
| `func (s *Server) GracefulStop()` | Runs `beginShutdown` (close `closing`, close listener; guarded by `sync.Once`), waits for the accept loop via `serveWg`, sweeps `SetReadDeadline(time.Now())` over every tracked connection to interrupt idle blocked reads, waits for all connection goroutines and their streams via `wg`, stops the rate limiter, and logs `server graceful stop complete`. In-flight unary requests and active streams complete first; idle pooled client connections do not delay it (regression-tested in `fixes_test.go`). |
| `func (s *Server) Stop()` | Runs `beginShutdown`, waits for the accept loop, then force-closes every tracked connection (`SetLinger(0)` then `Close`, so peers see RST), stops the limiter, and logs `server stop complete`. Does not wait for handlers. Remains effective after a prior `GracefulStop` because only the listener-closing step is inside the `sync.Once`. |

Not exposed through `pkg/rpc`: `internal/server.Server` also has `Addr() string` and `Handle(conn *transport.TCPConnection)`. Neither is reachable from user code, so to learn the bound address you must keep your own reference to the `net.Listener` and call `lis.Addr()`.

### type Future (alias of transport.Future)

```go
func NewFuture() *Future                                  // internal-only constructor
func NewFutureWithCodec(cc codec.Codec) *Future            // internal-only constructor
func (f *Future) Done(res []byte, err error)
func (f *Future) Wait() ([]byte, error)
func (f *Future) WaitWithContext(ctx context.Context) ([]byte, error)
func (f *Future) WaitWithTimeout(timeout time.Duration) ([]byte, error)
func (f *Future) GetResult(reply any) error
func (f *Future) GetResultWithContext(ctx context.Context, reply any) error
func (f *Future) DoneChan() <-chan struct{}
func (f *Future) IsDone() bool
func (f *Future) OnComplete(fn func(error))
```

Users obtain a `*Future` only from `ClientConn.InvokeAsync`; the constructors live in `internal/transport` and are unreachable.

| Method | Contract |
| --- | --- |
| `Done(res, err)` | Producer side, idempotent. The first call latches `res`/`err`, fires the registered `OnComplete` callback (outside the mutex, before `close(f.done)`), then closes the done channel. Every later call is a no-op, which is what makes late server responses after a timeout harmless. |
| `Wait()` | Blocks on the done channel with no bound and returns the raw, already-decompressed body bytes plus the error. If nothing ever resolves the future, this blocks forever. |
| `WaitWithContext(ctx)` | Returns `ctx.Err()` if `ctx` finishes first; otherwise delegates to `Wait`. |
| `WaitWithTimeout(d)` | `WaitWithContext` over `context.WithTimeout(context.Background(), d)`. |
| `GetResult(reply)` | Blocks unbounded, then, if no error was latched, decodes the body into `reply` with the codec captured at send time. If an error was latched it is returned and `reply` is untouched. |
| `GetResultWithContext(ctx, reply)` | Returns `ctx.Err()` on context completion, otherwise `GetResult`. |
| `DoneChan()` | Receive-only completion channel, suitable for `select`. |
| `IsDone()` | Non-blocking completion probe. |
| `OnComplete(fn)` | Registers a single completion callback. If the future is already complete, `fn` is invoked synchronously with the latched error. Only one callback slot exists: a second `OnComplete` before completion overwrites the first. |

The codec is captured at send time by `NewFutureWithCodec`; a nil codec falls back to JSON, and if JSON is somehow unregistered the constructor panics. Both `sendAsyncStatic` and `internal/client.invokeAsync` pass the connection's codec, so replies decode with the same codec the request used.

Concurrency: all `Future` state is mutex-guarded; concurrent `Wait`, `GetResult`, `Done`, and `OnComplete` are safe. Note that a slow `OnComplete` callback delays every waiter, because it runs before `close(f.done)`.

### Stream interfaces

```go
type ServerStream interface {
    Send(msg any) error
    Context() context.Context
}

type ClientStream interface {
    Recv(msg any) error
    Context() context.Context
}
```

`ServerStream` is what a streaming handler receives. `Send` marshals `msg` with the request codec and writes one `StreamData` frame; the write is serialized by the connection's write mutex, so concurrent `Send` from multiple goroutines produces intact frames. `Context()` returns the context the handler was invoked with, which is derived from `context.Background()` and is therefore never cancelled — do not use it to detect client disconnect or cancellation.

`ClientStream` is what `NewStream` returns. `Recv` decodes the next data frame into `msg`, returns `io.EOF` after the last buffered frame once the server sent `StreamEnd`, returns the server's error for `StreamError`, and returns `ctx.Err()` if the stream context finishes. `Context()` returns the stream's derived context. `Recv` is single-consumer by design; the implementation is race-free but multiple concurrent `Recv` callers will interleave frames arbitrarily.

The concrete implementation `*transport.ClientStreamConn` also has a `Cancel()` method, but it is not part of the `ClientStream` interface and the concrete type is not importable, so the only way to stop a stream from the client side is to cancel the `ctx` you passed to `NewStream`.

### Sentinel errors and error propagation

Only two exported sentinel error values exist, both in `internal/transport`:

- `ErrPoolClosed = errors.New("connection pool closed")` — returned by `ConnectionPool.Acquire` after `Close`. Reachable from user code as the error value returned by `Invoke`/`InvokeAsync`/`NewStream` after `ClientConn.Close`, but not comparable by name because the identifier is not exported through `pkg/rpc`. Match on the string `connection pool closed`.
- `ErrStreamNotFound = errors.New("stream not found")` — declared in `internal/transport/stream.go` and referenced nowhere in the codebase. Dead code.

Everything else crosses the wire as a plain string in `Header.Error` and is reconstructed client-side with `errors.New`, so `errors.Is` and `errors.As` against typed server errors never match. Match on substrings. The complete set of framework-generated messages:

| Message | Origin |
| --- | --- |
| `service not found: <service>` | `internal/server/handler.go`, nil service value |
| `method not found: <service>.<method>` | reflection lookup failed |
| `unsupported method signature: <service>.<method>` | none of the three accepted shapes matched |
| `handler panic: <value>` | `safeCall` recovered a panic |
| `rate limit exceeded` | server `Handle` limiter, and client-side `internal/client` limiter |
| `circuit breaker open` | registry-mode client, breaker in Open or probe-exhausted HalfOpen |
| `no instance available` | `Discover` returned an empty list |
| `load balancer returned empty address` | `lb.Select` returned a zero `Instance` |
| `registry not configured` | registry-mode client with a nil `*Registry` |
| `connection closed` | `TCPClient.Close`, and the send-path closed re-check |
| `connection pool closed` | `ErrPoolClosed` |
| `codec: type <n> not registered` | `codec.New` for an unknown type |
| `codec must not be nil` | `NewHandler` without a codec option |
| `client timeout must be positive` | `WithClientTimeout(d <= 0)` |
| `client load balancer must not be nil` | `WithClientLoadBalancer(nil)` |
| `header is nil` | `protocol.Encode` with a nil header |
| `data too short`, `invalid magic number`, `packet too large`, `incomplete packet` | `protocol.Decode` |
| `compressor not found` | `codec.Compress`/`Decompress` for an unregistered compression type |
| `proto codec: not proto.Message` | protobuf codec applied to a non-proto value |

Panics (not errors): `codec.Register` with a nil factory (`codec: factory is nil`) or a duplicate type (`codec: type <n> already registered`); `NewFutureWithCodec` if JSON is unregistered; `rpc.NewServer` on any option error.

### pkg/api

`pkg/api` is a tiny sample-service package, used by the module's own tests and suitable as a copy-paste starting point. It contains no framework logic.

```go
type Args  struct { A int; B int }
type Args1 struct { A int; B int; C int }
type Reply struct { Result int }

type Arith struct{}
func (a *Arith) Add(_ context.Context, args *Args) (*Reply, error)   // Result = A + B
func (a *Arith) Mul(_ context.Context, args *Args) (*Reply, error)   // Result = A * B

type Arith2 struct{}
func (a *Arith2) Add(_ context.Context, args *Args1) (*Reply, error) // Result = A + B + C
func (a *Arith2) Mul(_ context.Context, args *Args1) (*Reply, error) // Result = A * B * C
```

All four methods use the grpc-go-style signature and ignore the context. `Arith` and `Arith2` are stateless empty structs, so a single value may be registered once and shared by every connection. Register with `server.Register("Arith", &api.Arith{})`.

## Deep dive: wire protocol and message framing

`internal/protocol` defines the entire on-wire contract.

```
byte offset:  0        2                6                10           10+HeaderLen
             +--------+----------------+----------------+------------+--------------+
             | Magic  |   HeaderLen    |    BodyLen     |  Header    |     Body     |
             | uint16 |    uint32      |     uint32     |  JSON      |  codec bytes |
             | BE     |    BE          |     BE         |  HeaderLen |   BodyLen    |
             +--------+----------------+----------------+------------+--------------+

const Magic uint16 = 0x1234
```

- Byte order is big-endian for all three fixed fields (`encoding/binary.BigEndian`).
- The fixed prefix is exactly 10 bytes. There is no protocol version field, no flags byte, and no checksum. Compatibility is entirely a function of the JSON header's field set.
- `HeaderLen` and `BodyLen` are the lengths of the encoded header and the post-compression body respectively.
- The header is always JSON, regardless of the body codec. `Encode` and `Decode` each construct a fresh JSON codec via `codec.New(codec.JSON)` for the header.
- The body is compressed if and only if `Header.Compression != CompressionNone`. Every send site in the codebase hard-codes `codec.CompressionGzip`, so in practice the body is always gzipped. An empty body still produces a valid, non-empty gzip stream (~23 bytes), so `BodyLen` is never 0 on the wire for framework-generated frames.

```go
type Message struct {
    Header *Header
    Body   []byte      // pre-compression on the way in, post-decompression on the way out
}

type Header struct {
    RequestID   uint64
    ServiceName string
    MethodName  string
    Error       string
    CodecType   CodecType                 // protocol-local byte type
    Compression codec.CompressionType
    StreamFlag  StreamFlag `json:",omitempty"`
}

type CodecType byte
const (
    CodecTypeJSON  CodecType = iota + 1   // 1
    CodecTypeProto                        // 2
)

type StreamFlag byte
const (
    StreamNone  StreamFlag = 0   // unary request or unary response
    StreamData  StreamFlag = 1   // one stream payload frame
    StreamEnd   StreamFlag = 2   // stream terminated successfully
    StreamError StreamFlag = 3   // stream terminated with Header.Error set
)
```

Header notes:

- Only `StreamFlag` carries `json:",omitempty"`, so `StreamNone` is absent from the serialized header; a missing key decodes to 0, which means unary. Every other field is always present in the JSON, including empty strings and zero numbers.
- `protocol.CodecType` and `codec.Type` are distinct Go types that share the numeric encoding. Production code converts explicitly: `CodecType: protocol.CodecType(cc.codecType)`. The named constants `CodecTypeJSON` and `CodecTypeProto` are used only in `message_test.go`; the client paths convert from `codec.Type` instead.
- Responses do not echo `ServiceName`, `MethodName`, or `CodecType`. The client relies on the codec it captured at send time, so it cannot detect a server that replied in a different encoding.

`Encode(msg *Message) ([]byte, error)` rejects a nil header with `header is nil`, compresses the body when requested, marshals the header, then writes the whole frame into one freshly allocated `[]byte` of size `10 + headerLen + bodyLen`.

`Decode(data []byte) (*Message, error)` validates in this order: at least 10 bytes (`data too short`), magic equals `0x1234` (`invalid magic number`), `uint64(10) + headerLen + bodyLen <= math.MaxInt` (`packet too large`), `len(data) >= totalLen` (`incomplete packet`); then unmarshals the header and decompresses the body. On a 64-bit platform `packet too large` is unreachable because both lengths are `uint32`; the check exists for 32-bit builds.

`DecodeHeaderLen(data []byte) uint32` and `DecodeBodyLen(data []byte) uint32` read a big-endian `uint32` and return 0 for inputs shorter than 4 bytes. `PacketBuffer` uses them to peek at framing without full decode.

Stream multiplexing over one connection is purely `RequestID` plus `StreamFlag`:

```
client                                        server
  |-- RequestID=7 StreamNone  Feed.Subscribe --> |   (open stream; same frame shape as unary)
  | <-- RequestID=7 StreamData  {chunk 0} -------|
  | <-- RequestID=7 StreamData  {chunk 1} -------|
  |-- RequestID=8 StreamNone   Math.Add ------->|   (unary interleaved on the same socket)
  | <-- RequestID=8 StreamNone  {reply} --------|
  | <-- RequestID=7 StreamEnd  (empty body) ----|
```

There is no separate stream-open frame type: a stream request is indistinguishable on the wire from a unary request. The server decides which it is by reflecting on the target method's second parameter. Consequently the client must know in advance whether to call `Invoke` or `NewStream`, and a mismatch is not diagnosed (see Pitfalls 6 and 7).

`PacketBuffer` handles partial reads and corruption:

```go
func (pb *PacketBuffer) Write(data []byte)
func (pb *PacketBuffer) Read() []byte    // nil when no complete frame is buffered
```

`Read` first resynchronizes by dropping one leading byte at a time while the first two buffered bytes are not the magic number; then it needs at least 10 bytes, then at least `10 + headerLen + bodyLen`. When a full frame is present it copies it into a fresh slice and advances the buffer by reslicing. Because resync is a loop, a valid frame sitting behind garbage is recovered in the same call. The buffer is mutex-guarded and starts at `cap = BufferSize*2 = 8192`.

## Deep dive: codec selection, JSON, protobuf, compression

`internal/codec` is a registry keyed by a one-byte type.

```go
type Codec interface {
    Marshal(v any) ([]byte, error)
    Unmarshal(data []byte, v any) error
}
type Type byte
type Factory func() Codec

func Register(t Type, f Factory)          // panics on nil factory or duplicate type
func New(t Type) (Codec, error)           // "codec: type <n> not registered"

const JSON  Type = 1
const PROTO Type = 2
```

Registration happens in `init` functions in `json.go` and `protobuf.go`, guarded by a `sync.RWMutex`. `New` returns a fresh codec instance per call; both built-in codecs are stateless empty structs, so the allocation is cheap but non-zero — `protocol.Encode` and `protocol.Decode` each allocate one JSON codec per frame.

JSON codec: thin wrappers over `encoding/json.Marshal` and `Unmarshal`. It inherits all `encoding/json` behaviour, notably that only exported fields round-trip, that `int64` values above 2^53 lose precision when a peer decodes into `any`, and that `Unmarshal` on an empty byte slice fails with `unexpected end of JSON input`.

Protobuf codec: requires `proto.Message` on both sides.

```go
func (p *protoCodec) Marshal(v any) ([]byte, error)    // "proto codec: not proto.Message" if v is not
func (p *protoCodec) Unmarshal(data []byte, v any) error
```

There is no generated stub layer, no `.proto` file in the module, and no descriptor registry usage: protobuf here is only an alternative body encoding for types the caller already has as `proto.Message`. `codec_test.go` exercises it with `emptypb.Empty`.

Codec negotiation is per-request and one-directional:

- Every client send path writes `CodecType: protocol.CodecType(cc.codecType)` into the request header. This is true for static unary (`sendAsyncStatic`), registry unary (`invokeAsync`), static stream (`newStreamStatic`), and registry stream (`InvokeStream`).
- `Handler.Process` reads `msg.Header.CodecType`. If it is non-zero it instantiates that codec and uses it for request decoding, reply encoding, and stream frame encoding. If it is zero it falls back to the handler's configured codec (JSON unless `WithCodec` changed it). An unknown announced type produces an error frame with `codec: type <n> not registered`.
- Therefore a `WithDialCodec(CodecProto)` client interoperates with a default-JSON server, provided every request and reply type implements `proto.Message`. `WithCodec` on the server matters only for peers that omit the field.

Compression:

```go
type CompressionType byte
const (
    CompressionNone CompressionType = iota   // 0
    CompressionGzip                          // 1
)

func RegisterCompressor(t CompressionType, c compressor)
func GetCompressor(t CompressionType) compressor
func Compress(data []byte, t CompressionType) ([]byte, error)
func Decompress(data []byte, t CompressionType) ([]byte, error)
type GzipCompressor struct{}
```

`GzipCompressor` is registered for `CompressionGzip` in an `init`. Nothing is registered for `CompressionNone`, so calling `Compress(data, CompressionNone)` returns `compressor not found`; `Encode`/`Decode` avoid that by skipping compression entirely when the header says `CompressionNone`. `Compress` builds a fresh `gzip.Writer` per call with default compression level and no `sync.Pool`; `Decompress` builds a fresh `gzip.Reader` and `io.ReadAll`s it.

`RegisterCompressor` and `GetCompressor` are exported but their `compressor` interface (`compress`, `decompress` — both unexported methods) is not, so no package outside `internal/codec` can implement a compressor. Combined with `internal/` visibility, gzip is effectively the only compression available, and there is no option anywhere in `pkg/rpc` to turn it off.

## Deep dive: connection pool, transport loops, Future correlation

```go
const BufferSize = 4096

func NewTCPConnection(conn net.Conn) *TCPConnection
func (tc *TCPConnection) Read() (*protocol.Message, error)
func (tc *TCPConnection) Write(msg *protocol.Message) error
func (tc *TCPConnection) Close() error
func (tc *TCPConnection) SetReadDeadline(t time.Time) error
func (tc *TCPConnection) RemoteAddr() string
```

`TCPConnection` wraps a `net.Conn` with a 4096-byte `bufio.Reader` and a `PacketBuffer`. `Read` loops: try `PacketBuffer.Read`, and on nil allocate a fresh 4096-byte scratch slice, read into it, append to the buffer, retry. A decode failure is returned to the caller as an error, which on both client and server tears the connection down. `Write` encodes, takes `writeMu`, and loops until every byte is flushed, so a partial `net.Conn.Write` cannot corrupt the frame stream. `Close` calls `SetLinger(0)` when the underlying conn is a `*net.TCPConn`, which sends RST instead of FIN. `SetReadDeadline` exists specifically so `GracefulStop` can unblock idle readers.

```go
type TCPClient struct { /* conn, addr, writeMu, seq, pending, streams, closed */ }

func (c *TCPClient) SendAsync(msg *protocol.Message) (*Future, error)
func (c *TCPClient) SendAsyncWithCodec(msg *protocol.Message, cc codec.Codec) (*Future, error)
func (c *TCPClient) SendStream(ctx context.Context, msg *protocol.Message, cc codec.Codec) (*ClientStreamConn, error)
func (c *TCPClient) Close() error
```

`newTCPClient(addr)` dials with a hard-coded `net.DialTimeout("tcp", addr, 5*time.Second)` and starts exactly one `readLoop` goroutine. The 5-second dial timeout is independent of `WithTimeout` and cannot be configured.

Sequence allocation: `seq atomic.Uint64`, `nextSeq()` returns `seq.Add(1)`, so the first `RequestID` is 1 and 0 is never produced by this client. Wraparound after 2^64 requests is not handled and is not a practical concern.

Send path invariants, shared by `SendAsyncWithCodec` and `SendStream`:

1. Reject early if `closed == 1` (`connection closed`).
2. Allocate `seq`, stamp it into `msg.Header.RequestID`.
3. Store the `Future` in `pending[seq]` or the `ClientStreamConn` in `streams[seq]`.
4. Re-check `closed`; if a concurrent `shutdown` already swept the maps, `LoadAndDelete` our own entry and fail. This closes the window where a future would otherwise be stored after the sweep and never resolved.
5. Write under `writeMu`. On write error, delete the map entry, call `fail(err)` to tear the whole connection down, and return the error.

`readLoop` is the single demultiplexer:

| `Header.StreamFlag` | Action |
| --- | --- |
| `StreamData` | `streams.Load(seq)`, then `ClientStreamConn.Push(msg.Body)`. Entry retained. |
| `StreamEnd` | `streams.LoadAndDelete(seq)`, then `End()` (terminates with `io.EOF`). |
| `StreamError` | `streams.LoadAndDelete(seq)`, then `Error(errors.New(msg.Header.Error))`. |
| default (`StreamNone`) | `pending.LoadAndDelete(seq)`; if absent, drop the frame and continue. Otherwise `Future.Done(nil, errors.New(Header.Error))` when `Error != ""`, else `Future.Done(msg.Body, nil)`. |

Unknown sequence numbers are silently dropped, which is what makes a late response after a client timeout harmless — and also what makes an error response to a stream request disappear (Pitfall 6).

Teardown is unified in `shutdown(err)`, entered by both `fail(err)` (read or write failure) and `Close()` (with `connection closed`). It is guarded by `closed.CompareAndSwap(0, 1)` so it runs at most once: close the socket, `Range` over `pending` resolving every future with `err` and deleting the entry, then `Range` over `streams` terminating each with `err`. Because the socket is closed first, `readLoop` observes a read error and returns, so `Close` reaps the read goroutine. Callers blocked in unbounded `Wait()` or `GetResult()` are always woken (regression-tested by `TestTCPClientCloseUnblocksPending`).

```go
var ErrPoolClosed = errors.New("connection pool closed")

func NewConnectionPool(addr string, maxIdle, maxActive int) *ConnectionPool
func (p *ConnectionPool) Acquire(ctx context.Context) (*TCPClient, error)
func (p *ConnectionPool) Close()
```

`maxIdle` is accepted and never read — the pool has no idle list at all. `maxActive` bounds the slice of live `TCPClient` values and is 1 at every call site (`NewConnectionPool(addr, 0, 1)` in `pkg/rpc/client.go` and in `internal/client/resolver.go`), so in practice every address has exactly one socket.

`Acquire` holds `p.mu` for its entire body, including `newTCPClient`'s 5-second dial:

1. Return `ErrPoolClosed` if closed.
2. A single non-blocking `select` on `ctx.Done()`. This is the only place the context is consulted; it does not bound the dial that follows.
3. If `len(conns) < maxActive`, dial a new client, append, return it.
4. Otherwise scan round-robin from `p.next`, returning the first client whose `closed` flag is 0 and advancing `p.next`. Dead clients are spliced out of the slice with corrected index bookkeeping (`n--; i--`).
5. If every client was dead, dial a fresh one and return it.

`Close` sets `closed` and calls `Close()` on every pooled client, which reaps each `readLoop`. It is idempotent.

`ClientStreamConn` is the client end of a stream:

```go
func NewClientStreamConn(ctx context.Context, cc codec.Codec) *ClientStreamConn
func (s *ClientStreamConn) Push(body []byte)
func (s *ClientStreamConn) End()
func (s *ClientStreamConn) Error(err error)
func (s *ClientStreamConn) Recv(msg any) error
func (s *ClientStreamConn) Context() context.Context
func (s *ClientStreamConn) Cancel()
```

State: a 64-slot buffered channel of data frames, a derived cancellable context, and an out-of-band terminal channel closed exactly once through `sync.Once`.

- `Push` selects over sending to the data channel, `ctx.Done()`, and `termCh`. When the buffer is full and the stream is neither cancelled nor terminated, `Push` blocks — and since `Push` is called from `readLoop`, that blocks every other future and stream on the same socket (Pitfall 4).
- `End`/`Error` never block, even with a full buffer, because terminal state travels on `termCh` rather than through the data channel. `TestClientStreamTerminalNonBlocking` pins this.
- `Recv` drains before terminating: it first tries a non-blocking receive on the data channel; then blocks on data, `termCh`, or `ctx.Done()`; and when `termCh` fires it makes one more non-blocking attempt on the data channel before returning `termErr`. No frame that arrived before `StreamEnd`/`StreamError` is lost. Clean end yields `io.EOF`; a server error yields that error; a finished context yields `ctx.Err()`.
- `Cancel` cancels only the derived context. It sends nothing to the server: the server-side handler keeps running and keeps writing frames, which the client then drops in `Push`.

There is no flow control beyond the 64-frame buffer and TCP's own window. The server never waits for client acknowledgement and there is no windowing, credit, or `StreamFlag` codepoint for backpressure.

## Deep dive: registration and discovery over etcd

```go
type Instance struct { Addr string }

func NewRegistry(endpoints []string) (*Registry, error)
func (r *Registry) Register(service string, ins Instance, ttl int64) error
func (r *Registry) Discover(service string) ([]Instance, error)
func (r *Registry) Close() error
```

All four are reachable from user code because `rpc.Registry` and `rpc.Instance` are type aliases; `pkg/rpc` wraps only the constructor.

Key layout is hard-coded with no namespace parameter:

```
prefix = "/github.com/hangtiancheng/swifty.go/swifty_rpc/services/"
key    = <prefix><service>/<addr>
value  = <addr>
```

`Register(service, ins, ttl)`:

1. `Grant(ctx, ttl)` — `ttl` is in seconds and is passed straight through to etcd. There is no validation, no default, and no minimum; `ttl = 0` means etcd rejects or immediately expires the lease depending on server version.
2. `Put(key, ins.Addr, WithLease(leaseID))`.
3. `KeepAlive(ctx, leaseID)` and one goroutine that only drains the returned channel and returns when it closes.

The keepalive goroutine performs no recovery: when the channel closes because the lease expired or etcd became unreachable, the goroutine exits and the instance silently disappears from every watcher's view with no re-registration attempt. The lease ID is not retained anywhere, so nothing can renew or re-grant it. Long-lived servers need external re-registration (Pitfall 12).

`Discover(service)`:

1. Fast path under `RLock`: if the service already has a cache entry, copy and return.
2. Otherwise `initService`: under the write lock, re-check, `Get(prefix+service+"/", WithPrefix())`, populate `map[addr]Instance` from the values, and start one `watch(service)` goroutine.
3. Return `copyInstances(service)`.

`copyInstances` iterates the inner map and appends to a fresh slice, so callers get a defensive copy and cannot mutate the cache (`registry_test.go` pins both the copy and the cache immutability). Because it iterates a Go map, the returned order is randomized on every call — which breaks positional weighting (Pitfall 11) and makes round-robin degenerate toward random.

`watch(service)` loops forever until `r.ctx` is done: open `Watch(ctx, prefix+service+"/", WithPrefix())`, apply `EventTypePut` by inserting `Instance{Addr: string(event.Kv.Value)}` and `EventTypeDelete` by trimming the key prefix to recover the address and deleting it, and when the watch channel closes sleep one second and re-open. Cache entries are never evicted for a service once created, and there is no bound on the number of watch goroutines other than the number of distinct discovered services.

`Close()` cancels the registry context (stopping every watch goroutine and every keepalive) and closes the etcd client. Both steps are nil-safe, so a zero-value `&Registry{}` can be closed without panicking. `ClientConn.Close()` does not call it — you must close the registry yourself.

`Discover` takes `r.mu.RLock()` and then calls `copyInstances`, which takes `r.mu.RLock()` again. Recursive read locking on `sync.RWMutex` is documented as unsafe: if a writer (a `watch` goroutine handling a put or delete) blocks between the two acquisitions, the inner `RLock` waits for the writer, the writer waits for the outer reader, and `Discover` deadlocks (Pitfall 13).

## Deep dive: load balancing and endpoint refresh

```go
type LoadBalancer interface {
    Select([]registry.Instance) registry.Instance
}
```

Every implementation returns the zero `Instance{}` for an empty input list, which the client turns into `load balancer returned empty address`.

`RoundRobin`:

```go
type RoundRobin struct { idx atomic.Uint64 }
func NewRR() *RoundRobin
func (r *RoundRobin) Select(list []registry.Instance) registry.Instance
```

Lock-free: `i := r.idx.Add(1)` then `list[(i-1) % len(list)]`, so the first selection is index 0. The zero value is usable, which is why `internal/client.NewClient` defaults to `&load_balance.RoundRobin{}` without calling `NewRR`. The counter is not reset when the instance list changes size, so a changing list shifts the rotation.

`Random`:

```go
type Random struct { r *rand.Rand; m sync.Mutex }
func NewRandom() *Random
```

Must be constructed with `NewRandom`; the zero value nil-panics because `r` is nil. Uses `math/rand` seeded from `time.Now().UnixNano()` behind a mutex — not cryptographically random, and the mutex serializes selection.

`WeightedRR`:

```go
type WeightedRR struct { mu sync.Mutex; weights, currentWeight []int; totalWeight int }
func NewWeightedRR(weights []int) *WeightedRR
```

Nginx-style smooth weighted round-robin. `NewWeightedRR` clamps negative weights to 0 and sums the rest into `totalWeight`. `Select` returns the zero `Instance` when `len(list) != len(weights)` or `totalWeight <= 0`; otherwise it adds each weight into `currentWeight`, picks the maximum index, subtracts `totalWeight` from that entry, and returns it. With weights `{1,2,3}` over six selections the distribution is exactly a:1, b:2, c:3 (`load_balance_test.go`).

Endpoint refresh: there is no resolver goroutine and no cached address on the client. `internal/client.getAddr` calls `reg.Discover(service)` on every single `Invoke`, `InvokeAsync`, and `InvokeStream`, then `lb.Select`. `Discover` is a cheap in-memory read after the first call, and the cache is kept fresh asynchronously by the per-service `watch` goroutine. Consequences: an instance removed from etcd stops receiving traffic as soon as the watch event lands, but the already-established `ConnectionPool` for that address stays in `c.pools` forever — pools are never evicted, so a long-running client accumulates one idle socket per address it ever talked to, until `Close`.

## Deep dive: circuit breaker state machine

```go
type State int
const (
    Closed   State = iota   // 0
    Open                    // 1
    HalfOpen                // 2
)

func NewCircuitBreaker(windowSize int, failureThreshold float64, openTimeout time.Duration) *CircuitBreaker
func (cb *CircuitBreaker) Allow() bool
func (cb *CircuitBreaker) RecordSuccess()
func (cb *CircuitBreaker) RecordFailure()
func (cb *CircuitBreaker) State() State
```

All four methods take the same mutex, so the breaker is safe for concurrent use. `NewCircuitBreaker` starts in `Closed` with `lastStateChange = time.Now()`.

`Allow()`:

| State | Result |
| --- | --- |
| `Closed` | always `true`; no counting happens in `Allow` |
| `Open` | `false` until `time.Since(lastStateChange) > openTimeout` (strictly greater); on the first call past that point, transition to `HalfOpen`, set `halfOpenProbe = true`, and return `true` |
| `HalfOpen` | `false` if a probe is already outstanding; otherwise set `halfOpenProbe = true` and return `true` |

So exactly one probe is admitted per half-open episode, and it is admitted by the same `Allow` call that performs the Open→HalfOpen transition.

`RecordSuccess()`:

- `Closed`: `successCount++`, then evaluate the window. `resetClosedWindowIfReady` returns immediately while `failureCount + successCount < windowSize`; once the window is full it computes `failureCount / total` and transitions to `Open` if that is `>= failureThreshold`, otherwise resets both counters to 0. A success can therefore trip the breaker, because it is the event that completes the window.
- `HalfOpen`: transition to `Closed`, reset counters, clear the probe flag.
- `Open`: ignored.

`RecordFailure()`:

- `Closed`: `failureCount++`; return while `total < windowSize`; otherwise if `failureCount / total >= failureThreshold` transition to `Open`, else reset counters.
- `HalfOpen`: transition straight back to `Open`, which restarts `openTimeout`.
- `Open`: ignored.

The window is a fixed-count tumbling window, not a sliding time window: counters reset only when the window fills or a state transition occurs, so a long-idle breaker still carries stale counts. There is no minimum-request guard beyond `windowSize` and no time decay.

`breaker_test.go` pins the exact transitions: `NewCircuitBreaker(4, 0.5, 20ms)` with 2 successes then 2 failures reaches `Open` (rate 0.5 >= 0.5); `Allow` is false before 20 ms, true once after, false on the immediately following call; a `RecordSuccess` then closes it. `NewCircuitBreaker(1, 1.0, 1ms)` opens on a single failure, admits one probe after the timeout, and a failing probe returns it to `Open`. `NewCircuitBreaker(2, 1.0, 1s)` stays `Closed` through failure+success (rate 0.5 < 1.0, counters reset), stays `Closed` on the next single failure (window not full), and opens on the following failure.

The registry-mode client is the only consumer, with hard-coded parameters in `internal/client/resolver.go`:

```go
breaker.NewCircuitBreaker(10, 0.6, 5*time.Second)   // key: service + "|" + addr
```

Breakers are created lazily through `sync.Map.LoadOrStore` and never removed, so the map grows with the number of distinct `service|addr` pairs ever contacted.

Accounting coverage:

- Unary: pool-acquire failure and send failure call `RecordFailure` directly. Otherwise `Future.OnComplete` records success on a nil error and failure otherwise. `Invoke` additionally forces `future.Done(nil, callCtx.Err())` when the call context expires, and `InvokeAsync`'s watchdog forces `future.Done(nil, context.DeadlineExceeded)` on timeout — both fire `OnComplete`, so timeouts count exactly once. `Future.Done` idempotency guarantees no double counting.
- Streams: `observedStream` wraps the raw stream and records at most once via `sync.Once` — success on `io.EOF`, failure on any other terminal error, and deliberately nothing for `context.Canceled`, because caller-initiated cancellation is not treated as a service failure. Acquire and send failures record directly. A stream that the caller abandons without draining to a terminal frame records nothing.

Static mode has no breaker at all.

## Deep dive: token-bucket rate limiter

```go
type TokenBucket struct { tokens, rate int; mu sync.Mutex; stop chan struct{}; once sync.Once }
func NewTokenBucket(rate int) *TokenBucket
func (tb *TokenBucket) Allow() bool
func (tb *TokenBucket) Stop()
```

`NewTokenBucket` clamps a negative `rate` to 0, sets `tokens = rate` so the bucket starts full, and starts one goroutine holding a `time.NewTicker(time.Second)` that assigns `tokens = rate` on every tick. `Allow` decrements under the mutex when `tokens > 0` and returns whether it got a token. `Stop` closes the stop channel through `sync.Once`, so it is idempotent, and the ticker goroutine returns.

Semantics worth internalizing: this is a fixed-window quota, not a smoothed token bucket. Refill is a full reset once per second, not an incremental drip, so all `rate` permits are available at the top of each second and a burst can exhaust them instantly, leaving the rest of that second fully rejected. A rate of 0 rejects everything.

Both consumers use a hard-coded rate of 10000 with no option to change it:

- `internal/server.NewServer` creates `limiter.NewTokenBucket(10000)`. `Server.Handle` calls `Allow()` after reading each frame and before service lookup; on rejection it writes a response carrying the original `RequestID`, `Error: "rate limit exceeded"`, gzip compression, and `StreamFlag` 0, then continues the read loop. The limit is per-server and shared across all connections, services, and methods. `GracefulStop` and `Stop` both call `limiter.Stop()`, so a server that is never stopped leaks the ticker goroutine.
- `internal/client.NewClient` creates `limiter.NewTokenBucket(10000)` and checks it at the top of `invokeAsync` and `InvokeStream`. `Client.Close` stops it.

## Deep dive: streaming

Only server-streaming exists. The protocol has no client-originated stream frames: `StreamFlag` defines `StreamData`, `StreamEnd`, and `StreamError`, all of which are only ever written by `internal/server/stream.go`, and `ClientStream` exposes only `Recv`. There is consequently no client-streaming, no bidirectional streaming, and no half-close: the single request payload passed to `NewStream` is the entirety of what the client can send.

Server side. A method is recognized as streaming when it has the shape `Method(req *T, stream ServerStream) error` — two inputs, one `error` output, first input a pointer, second input implementing `stream.ServerStream`. `Handler.invoke` then:

1. Allocates `*T` with `reflect.New` and unmarshals the request body into it (skipped when the body is empty).
2. Builds a `serverStream{conn, requestID, codec, ctx}` and converts it to the declared parameter type.
3. Runs the handler through `streamWg.Go(...)` when a `*sync.WaitGroup` was supplied (always, from `Server.Handle`), otherwise synchronously. The dedicated goroutine is why a long-lived stream does not block other requests multiplexed on the connection — `fixes_test.go`'s `TestStreamDoesNotBlockUnary` asserts a unary call completes within 500 ms while a 20-chunk, 50-ms-per-chunk stream runs.
4. Returns `(nil, true, nil)`, and `Handler.Process` sees `streaming == true` and writes no unary response.

When the handler returns, the wrapper writes exactly one terminal frame: `sendError(err.Error())` producing `StreamError` if `safeCall` recovered a panic or the handler returned a non-nil error, otherwise `end()` producing `StreamEnd`. `serverStream.Send` writes one `StreamData` frame per call and returns the encode or write error to the handler; a handler that ignores that error keeps sending into a dead connection.

`streamWg` is declared locally inside `Server.Handle` and waited on by a deferred `streamWg.Wait()` that runs before the deferred `conn.Close()`, so the connection outlives all of its streams and `GracefulStop` waits for them through the connection goroutine.

Client side. `NewStream` marshals the request, acquires the pooled connection under the dial timeout, and calls `TCPClient.SendStream(ctx, msg, codec)`, which registers a `ClientStreamConn` in `streams[seq]`. From then on `readLoop` routes frames by flag. `Recv` returns each payload, then `io.EOF`, then `io.EOF` on every subsequent call because `termCh` stays closed.

Cancellation is client-local. Cancelling the `ctx` passed to `NewStream` makes `Recv` return `ctx.Err()` and makes `Push` drop further frames, but the server-side handler runs to completion and its terminal frame is what finally removes the `streams` entry. `ServerStream.Context()` is derived from `context.Background()` and is never cancelled, so a handler cannot detect the abandonment.

## Internal implementation details that affect correctness

Reflection dispatch (`internal/server/handler.go`). `Handler.invoke` accepts exactly three shapes, tested in this order:

| Order | Signature | Requirements checked by reflection | Reply handling |
| --- | --- | --- | --- |
| 1 | `Method(ctx context.Context, req *T) (*R, error)` | `NumIn()==2 && NumOut()==2`, `In(0)` implements `context.Context`, `In(1)` is a pointer, `Out(0)` is a pointer, `Out(1)` implements `error` | Non-nil `*R` is dereferenced with `results[0].Elem().Interface()`; a nil `*R` yields a nil result and therefore an empty response body |
| 2 | `Method(req *T, stream ServerStream) error` | `NumIn()==2 && NumOut()==1`, `Out(0)` implements `error`, `In(0)` is a pointer, `In(1)` implements `stream.ServerStream` | Framework emits the terminal frame; no unary response |
| 3 | `Method(req *T, reply *R) error` | same as 2 but `In(1)` is a plain pointer | Framework allocates `*R` with `reflect.New` and returns `reply.Elem().Interface()` |

Anything else returns `unsupported method signature: <service>.<method>`. Notably `Method(ctx, req) (int, error)` is rejected for the non-pointer first result rather than panicking (`fixes_test.go` `BadReturn`), and `Method(req *T)` with no return is rejected (`server_test.go` `Bad`). Method lookup uses `reflect.Value.MethodByName`, so it sees only exported methods on the registered value; a pointer receiver requires registering a pointer.

Every reflective call goes through `safeCall`, which recovers panics into `handler panic: <value>`. A panicking handler cannot crash the process, cannot poison the connection, and does not prevent subsequent calls (`fixes_test.go` `TestPanicAndBadSignatureDoNotCrashServer` asserts the server still serves afterwards).

`NewHandler(s any, opts ...HandleOption) (*Handler, error)` ignores its first parameter entirely — the service value is supplied per request by `Process`. It errors with `codec must not be nil` when no `WithHandlerCodec` option was given, which is why `NewServer` always passes `WithHandlerCodec(codec.JSON)`.

Lock scope:

- `internal/server.Server.mu` covers `services`, `listener`, and `conns`. `Handle` takes it once per request just to look up the service, so heavy concurrent traffic contends with `Register`, connection registration, and the shutdown sweeps. No lock is held while a handler runs.
- `internal/transport.TCPClient` uses `writeMu` around `conn.Write`, and `TCPConnection` has its own `writeMu` inside `Write`; the double lock is redundant but harmless. `pending` and `streams` are `sync.Map`, `closed` is atomic, `seq` is atomic.
- `ConnectionPool.mu` is held across `net.DialTimeout`, so cold-start dials serialize all concurrent acquirers on that address (Pitfall 5).
- `PacketBuffer.lock` covers the byte buffer; `Future.mu` covers all future state; `CircuitBreaker.mu` and `TokenBucket.mu` cover their own counters; `Registry.mu` is an `RWMutex` over the service cache.

Goroutine lifecycle, exhaustively:

| Goroutine | Started by | Reaped by | Leaks if |
| --- | --- | --- | --- |
| `TCPClient.readLoop` | `newTCPClient`, i.e. the first `Acquire` per address | `TCPClient.Close` (via `shutdown` closing the socket) or peer close | `ClientConn.Close` is never called |
| `InvokeAsync` watchdog | every `InvokeAsync` call, in both modes | the future completing or the `cc.timeout` timer firing | never leaks longer than `timeout` |
| Server accept loop | `Serve` | `beginShutdown` closing the listener; tracked by `serveWg` | `Serve` is called and neither `Stop` nor `GracefulStop` ever runs |
| Per-connection `Handle` | accept loop, via `wg.Go` | peer close, read error, `SetReadDeadline` sweep, or force `Close`; tracked by `wg` | as above |
| Per-stream handler | `Handler.invoke`, via `streamWg.Go` | the handler returning | a handler that never returns pins the connection and blocks `GracefulStop` forever |
| Server `TokenBucket` ticker | `internal/server.NewServer` | `Stop` or `GracefulStop` | a `Server` is constructed and never stopped |
| Client `TokenBucket` ticker | `internal/client.NewClient` (registry mode only) | `ClientConn.Close` | registry-mode conn never closed |
| Registry keepalive drain | each `Registry.Register` call | the keepalive channel closing, or `Registry.Close` cancelling the context | registry never closed and lease never expires |
| Registry watch | first `Discover` per service | `Registry.Close` cancelling the context | registry never closed |

Retry and timeout interaction. There is no retry anywhere: no request retry, no reconnect-and-resend, no hedging, no backoff other than the fixed one-second re-watch delay in the registry. A failed send fails the call and tears the connection down; the next call transparently dials a new socket through `Acquire`, but the failed call is not reissued. Three independent timeouts stack:

1. `net.DialTimeout` inside `newTCPClient`: hard-coded 5 s, not configurable.
2. `cc.timeout` (`WithTimeout`, default 5 s): bounds pool acquisition for all three entry points, bounds the whole call for `Invoke`, and drives the `InvokeAsync` watchdog.
3. The caller's own `ctx`: `Invoke` derives from it, so the effective unary deadline is `min(ctx deadline, now + timeout)`. For `NewStream` the caller's `ctx` governs the stream lifetime with no timeout applied.

None of these is transmitted to the server. `Handler.Process` invokes handlers with `context.Background()`, so a handler keeps running after the client gives up, and `ServerStream.Context()`/the grpc-style `ctx` parameter are never cancelled.

Buffer reuse. There is essentially none, and the hot path allocates freely: a fresh 4096-byte scratch slice per `bufio` read, a fresh copy of every frame in `PacketBuffer.Read`, a fresh output slice in `protocol.Encode`, a fresh JSON codec in both `Encode` and `Decode`, and a fresh `gzip.Writer`/`gzip.Reader` per body. `PacketBuffer` advances by reslicing (`pb.buf = pb.buf[totalLen:]`), so its backing array is only reclaimed when `append` reallocates. No `sync.Pool` is used anywhere.

Codec constraints. The header codec is fixed to JSON, so header field names and types are part of the wire contract. Body codec is per-request. With the JSON body codec, only exported struct fields cross the wire and an empty body fails to unmarshal (see Pitfall 3). With the protobuf body codec, every request and reply type must implement `proto.Message` on both sides or the call fails with `proto codec: not proto.Message` — and note the failure is asymmetric: a client marshalling error is returned locally from `Invoke`, whereas a server-side unmarshalling error comes back as an error frame.

## Typical usage

### Unary server, grpc-go style signature

```go
package main

import (
    "context"
    "log"
    "net"

    "github.com/hangtiancheng/swifty.go/swifty_rpc/pkg/rpc"
)

type Args struct {
    A int
    B int
}

type Reply struct {
    Result int
}

type MathService struct{}

func (s *MathService) Add(ctx context.Context, args *Args) (*Reply, error) {
    return &Reply{Result: args.A + args.B}, nil
}

func main() {
    server := rpc.NewServer() // panics on invalid options; default codec is CodecJSON
    server.Register("Math", &MathService{})

    lis, err := net.Listen("tcp", "127.0.0.1:8080")
    if err != nil {
        log.Fatal(err)
    }

    // Serve blocks; it returns nil once Stop or GracefulStop closes the listener.
    if err := server.Serve(lis); err != nil {
        log.Fatal(err)
    }
}
```

### Unary client, static mode

```go
conn, err := rpc.Dial("127.0.0.1:8080", rpc.WithTimeout(5*time.Second))
if err != nil {
    log.Fatal(err) // only codec validation or registry-client option errors land here
}
defer conn.Close()

var reply Reply
if err := conn.Invoke(context.Background(), "Math", "Add", &Args{A: 1, B: 2}, &reply); err != nil {
    log.Fatal(err) // the TCP connection is established here, on the first call
}
log.Printf("Result: %d", reply.Result)
```

### Protobuf body codec

```go
// Both request and reply types must implement proto.Message.
conn, err := rpc.Dial("127.0.0.1:8080", rpc.WithDialCodec(rpc.CodecProto))
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

reply := &pb.AddReply{}
err = conn.Invoke(context.Background(), "Math", "Add", &pb.AddRequest{A: 1, B: 2}, reply)
```

The server needs no matching option: it honours the per-request `Header.CodecType`. `rpc.WithCodec(rpc.CodecProto)` on the server only changes the fallback used for peers that omit the field.

### Asynchronous invocation

```go
future, err := conn.InvokeAsync(context.Background(), "Math", "Add", &Args{A: 1, B: 2})
if err != nil {
    log.Fatal(err)
}

// ... do other work ...

select {
case <-future.DoneChan():
    var reply Reply
    if err := future.GetResult(&reply); err != nil {
        log.Fatal(err)
    }
    log.Printf("Result: %d", reply.Result)
case <-time.After(time.Second):
    log.Println("still pending")
}
```

The watchdog goroutine resolves the future with `context.DeadlineExceeded` once the dial timeout elapses, so `GetResult` and `Wait` cannot hang forever on an async call. That guarantee does not extend to a future you obtained and then kept past `ClientConn.Close` — `Close` resolves pending futures with `connection closed`.

### Server-streaming: server side

```go
type SubArgs struct{ Count int }
type Event struct{ Index int }

type FeedService struct{}

// Recognized as streaming because the second parameter implements rpc.ServerStream.
// Runs in its own goroutine; the framework writes StreamEnd on a nil return and
// StreamError carrying err.Error() otherwise.
func (s *FeedService) Subscribe(req *SubArgs, stream rpc.ServerStream) error {
    for i := 0; i < req.Count; i++ {
        if err := stream.Send(&Event{Index: i}); err != nil {
            return err // the client is gone; stop producing
        }
    }
    return nil
}
```

### Server-streaming: client side

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel() // cancelling ctx is the only way to abandon the stream

stream, err := conn.NewStream(ctx, "Feed", "Subscribe", &SubArgs{Count: 10})
if err != nil {
    log.Fatal(err)
}

for {
    var event Event
    if err := stream.Recv(&event); err != nil {
        if errors.Is(err, io.EOF) {
            break // clean StreamEnd
        }
        log.Printf("stream failed: %v", err) // StreamError, ctx.Err(), or transport failure
        break
    }
    process(event)
}
```

Consume promptly: the client buffers only 64 frames, and a full buffer blocks the shared `readLoop`.

### Registry mode: registration and discovery

```go
// Server side: publish this instance into etcd under a 10-second lease.
reg, err := rpc.NewRegistry([]string{"localhost:2379"})
if err != nil {
    log.Fatal(err)
}
defer reg.Close()

lis, err := net.Listen("tcp", "0.0.0.0:8080")
if err != nil {
    log.Fatal(err)
}
if err := reg.Register("Math", rpc.Instance{Addr: "10.0.0.5:8080"}, 10); err != nil {
    log.Fatal(err) // reachable via the Registry type alias; pkg/rpc adds no wrapper
}

server := rpc.NewServer()
server.Register("Math", &MathService{})
go server.Serve(lis)
```

```go
// Client side: discover through the same registry. target is ignored.
reg, err := rpc.NewRegistry([]string{"localhost:2379"})
if err != nil {
    log.Fatal(err)
}
defer reg.Close()

conn, err := rpc.Dial(
    "",
    rpc.WithRegistry(reg),
    rpc.WithTimeout(3*time.Second),
    // Omit WithLoadBalancer to keep the default zero-value RoundRobin.
    // Do not combine WeightedRR with the registry: instance order is randomized.
)
if err != nil {
    log.Fatal(err)
}
defer conn.Close() // stops the client limiter and closes every per-address pool

var reply Reply
if err := conn.Invoke(context.Background(), "Math", "Add", &Args{A: 1, B: 2}, &reply); err != nil {
    log.Fatal(err)
}
```

Registry mode is the only way to get rate limiting, circuit breaking, and load balancing.

### Error handling

```go
var reply Reply
err := conn.Invoke(context.Background(), "Math", "Divide", &Args{A: 1, B: 0}, &reply)
switch {
case err == nil:
    // success
case errors.Is(err, context.DeadlineExceeded):
    // client timeout: WithTimeout elapsed. The handler may still be running server-side.
case strings.Contains(err.Error(), "circuit breaker open"):
    // registry mode only: >=60% failures in the last window of 10; retry after ~5s
case strings.Contains(err.Error(), "rate limit exceeded"):
    // 10000/s exceeded, client-side or server-side; the message is identical
case strings.Contains(err.Error(), "no instance available"):
    // registry returned an empty instance list for the service
case strings.Contains(err.Error(), "method not found"),
    strings.Contains(err.Error(), "unsupported method signature"):
    // programming error: fix the registration or the method shape
case strings.Contains(err.Error(), "handler panic"):
    // the server recovered a panic; the process is still healthy
case strings.Contains(err.Error(), "connection closed"),
    strings.Contains(err.Error(), "connection pool closed"):
    // the socket or the ClientConn was torn down under us
default:
    // an error string returned by the service method itself
}
```

Server-produced errors arrive as fresh `errors.New` values, so only `context.DeadlineExceeded` and `context.Canceled` (produced locally) work with `errors.Is`. Everything else needs substring matching.

### Shutdown

```go
server := rpc.NewServer()
server.Register("Math", &MathService{})

lis, err := net.Listen("tcp", "127.0.0.1:8080")
if err != nil {
    log.Fatal(err)
}

errCh := make(chan error, 1)
go func() { errCh <- server.Serve(lis) }()

sig := make(chan os.Signal, 1)
signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
<-sig

// Drains in-flight unary requests and active streams; idle pooled client
// connections are interrupted with SetReadDeadline and do not delay it.
done := make(chan struct{})
go func() { server.GracefulStop(); close(done) }()

select {
case <-done:
case <-time.After(10 * time.Second):
    server.Stop() // force: RST every connection, do not wait for handlers
}

if err := <-errCh; err != nil {
    log.Fatal(err)
}
```

`GracefulStop` blocks as long as the slowest handler or stream, with no internal deadline; always pair it with your own timeout and a `Stop` fallback. `Stop` after `GracefulStop` is valid and effective.

## Testing patterns

The module's own tests provide three reusable harnesses.

Loopback server on an ephemeral port, from `pkg/rpc/stream_test.go`:

```go
func startTestServer(t *testing.T) (string, *Server) {
    t.Helper()
    server := NewServer()
    server.Register("Stream", &streamService{})
    lis, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        t.Fatalf("Listen: %v", err)
    }
    go server.Serve(lis)
    t.Cleanup(server.Stop)
    return lis.Addr().String(), server
}
```

Because `pkg/rpc` does not expose the server's bound address, capture it from `lis.Addr().String()`. Register `server.Stop` (not `GracefulStop`) with `t.Cleanup` so a hung handler fails the test by timeout rather than by deadlock. `fixes_test.go` uses the same shape without `t.Cleanup` so individual tests can choose `GracefulStop`.

In-process transport with no sockets, from `internal/server/server_test.go` and `internal/transport/transport_test.go`:

```go
clientConn, serverConn := net.Pipe()
client := transport.NewTCPConnection(clientConn)
serverSide := transport.NewTCPConnection(serverConn)
go s.Handle(serverSide)
// then client.Write(&protocol.Message{...}) and client.Read()
```

`net.Pipe` exercises framing, codec, compression, and dispatch with no listener and no timing dependence. Note that `net.Pipe` conns are not `*net.TCPConn`, so `SetLinger(0)` is skipped.

Direct dispatch without any connection, also from `server_test.go`:

```go
h, _ := NewHandler(nil, WithHandlerCodec(codec.JSON))
cc, _ := codec.New(codec.JSON)
body, _ := cc.Marshal(&testArgs{A: 3, B: 4})
result, streaming, err := h.invoke(t.Context(), nil, 1, &grpcStyleService{}, "Test", "Add", body, cc, nil)
```

Passing a nil `*TCPConnection` and a nil `streamWg` is safe for the two non-streaming signatures; a nil `streamWg` makes a streaming handler run synchronously, which is how you can test terminal-frame emission deterministically.

Other patterns worth copying: `internal/codec/codec_test.go` has an `assertPanics(t, fn)` helper for the registry panics and a `failingCompressor` for injecting compression errors; `internal/transport/transport_test.go` has a `testCodec` whose `Unmarshal` prefixes `"decoded:"` so codec plumbing can be asserted without JSON; `internal/breaker/breaker_test.go` uses tiny window sizes and millisecond timeouts to pin transitions; `internal/registry/registry_test.go` builds a `&Registry{services: ...}` literal with no etcd client at all, which is the only way to test discovery offline.

## Pitfalls and known limitations

1. Gzip body compression is mandatory and unconfigurable. Every send site in the codebase hard-codes `Compression: codec.CompressionGzip`, and no option in `pkg/rpc` can change it. For small payloads the gzip framing (roughly 23 bytes minimum plus CPU on both ends) exceeds the payload itself. There is no workaround short of forking; if payload overhead matters, batch multiple logical items into one call.

2. Both rate limits and all breaker parameters are hard-coded. `limiter.NewTokenBucket(10000)` appears in `internal/server.NewServer` and `internal/client.NewClient`; `breaker.NewCircuitBreaker(10, 0.6, 5*time.Second)` appears in `internal/client/resolver.go`. None is exposed as an option. The limiter is also a fixed one-second window with a full reset rather than a smooth drip, so 10000 requests can land in the first millisecond of a second and the remainder of that second rejects everything. Do your own admission control upstream if you need real shaping.

3. A grpc-style handler that returns `(nil, nil)` produces a client-side decode error. `Handler.Process` leaves the response body empty when the result is nil; the client's `Future.GetResult` then calls `json.Unmarshal` on an empty slice, which fails with `unexpected end of JSON input`. Always return a non-nil `*R`, even if it is an empty struct.

4. Every address has exactly one socket and one `readLoop`. `maxActive` is 1 at all call sites, so all unary calls and all streams to one address share one connection and one reader goroutine. A stream consumer that lets its 64-frame buffer fill blocks `readLoop` on the next `Push`, which stalls every other pending future and every other stream on that connection. Terminal frames (`StreamEnd`/`StreamError`) never block because they travel on a separate channel, but data frames do. Consume streams promptly, or cancel the stream context so `Push` starts dropping.

5. `ConnectionPool.Acquire` holds the pool mutex across `net.DialTimeout`, and the context does not bound the dial. The context is consulted in a single non-blocking `select` on entry; after that, a 5-second dial to an unreachable address holds the mutex for the full 5 seconds, and every concurrent acquirer for that address queues behind it — even those whose own context has already expired. In registry mode this is per-address, so a single dead instance can stall only the callers routed to it.

6. An error response to a stream request is silently dropped and the stream hangs. `Handler.writeError` always writes `StreamFlag` 0, and the server rate-limit rejection does the same. The client registered that `RequestID` in `streams`, not `pending`, so `readLoop`'s default branch finds nothing in `pending` and discards the frame. `NewStream` against a nonexistent service or method, against a method whose signature does not match, with a body the server cannot unmarshal, or under a server-side rate limit therefore returns a usable stream whose `Recv` blocks until the context is cancelled. Always give a stream context a deadline or cancellation path, and verify method names in an integration test.

7. Unary and stream calls are indistinguishable on the wire, so a mismatch is not diagnosed. Calling `Invoke` on a streaming method makes the server dispatch it as a stream, send `StreamData`/`StreamEnd` frames the client's `readLoop` drops (no `streams` entry), and never write a unary response — the `Invoke` blocks until the timeout. Calling `NewStream` on a unary method makes the server write a `StreamNone` response that lands in `pending`, is not found, and is dropped; the `Recv` then blocks. Keep the calling convention in sync with the handler shape by hand.

8. Static mode has no resilience features at all: no rate limiting, no circuit breaking, no load balancing, no discovery, no health checking. Those live exclusively in `internal/client.Client`, reachable only via `WithRegistry`. `WithLoadBalancer` in static mode is stored in `dialOptions` and never read — a declared-but-unenforced option.

9. `WithTimeout` behaves inconsistently across modes for non-positive values. Registry mode rejects `d <= 0` at `Dial` with `client timeout must be positive`. Static mode accepts it, then `context.WithTimeout(ctx, 0)` expires immediately and every call fails with `context.DeadlineExceeded` and no explanatory error. Always pass a positive duration.

10. Static-mode `Invoke` leaks its `pending` entry on timeout. Registry-mode `Invoke` forces `future.Done(nil, callCtx.Err())` when the context expires; `invokeStatic` does not. The entry stays in the `TCPClient.pending` map until the late response arrives (`LoadAndDelete` then discards it) or the connection shuts down. Bounded and small, but a static client that times out constantly against a silent server accumulates entries for the connection's lifetime.

11. `WeightedRR` plus the etcd registry misroutes traffic. `Select` matches weights to instances positionally, but `registry.copyInstances` iterates a Go map, so the order is randomized on every `Discover`, and `getAddr` calls `Discover` on every request. The weights therefore attach to arbitrary instances each time. `Select` also returns an empty `Instance` — surfacing as `load balancer returned empty address` — whenever the weight-slice length differs from the current instance count, which is guaranteed to happen whenever the fleet scales. Use `WeightedRR` only with a stable, ordered instance list you control; use `RoundRobin` or `Random` with the registry. Note that randomized ordering also degrades `RoundRobin` toward random selection, though without misrouting.

12. Lease expiry is undetected and never repaired. `Registry.Register` starts a goroutine that only drains the keepalive channel and returns when it closes. The lease ID is not retained, so nothing can re-grant it. After an etcd restart or a partition long enough to expire the TTL, the instance vanishes from discovery permanently. Re-register from your own supervision loop, or re-run `Register` periodically.

13. `Registry.Discover` recursively read-locks and can deadlock. The cache-hit path holds `r.mu.RLock()` and then calls `copyInstances`, which takes `r.mu.RLock()` again. Go's `sync.RWMutex` documents that recursive read locking is unsafe: a `watch` goroutine blocked in `r.mu.Lock()` between the two acquisitions makes the inner `RLock` wait for the writer while the writer waits for the outer reader. The window is small but real, and it widens with instance churn. There is no user-side workaround beyond keeping churn low; treat a hung `Invoke` in registry mode with no timeout progress as a candidate for this.

14. The etcd key prefix is hard-coded to `/github.com/hangtiancheng/swifty.go/swifty_rpc/services/` with no namespace parameter. Two environments sharing one etcd cluster will discover each other's instances. Use separate clusters, or separate etcd users with key-range ACLs.

15. `rpc.NewServer` panics instead of returning an error. The only failure mode is `WithCodec` with an unregistered `CodecType`, but that is enough to crash a process fed configuration-driven codec selection. Validate the codec value against `rpc.CodecJSON`/`rpc.CodecProto` before calling, or wrap the call in a `recover`.

16. `TCPConnection.Close` sets `SetLinger(0)`, so closing sends TCP RST rather than FIN. Peers reading during `Server.Stop` or `ClientConn.Close` see `connection reset by peer` instead of a clean EOF, and any data still in the kernel send buffer is discarded. `GracefulStop` avoids this for in-flight work by draining first, but it still closes with RST at the end.

17. Only server-streaming exists. `StreamFlag` has no codepoint for client-originated frames and `ClientStream` has no `Send`, so client-streaming and bidirectional streaming are impossible at the protocol level, and there is no half-close. Model bidirectional flows as two independent server-streams or as repeated unary calls.

18. Method signature validation happens at call time, not registration time. `Register` stores any value under any name without inspecting it. A typo in a method name, an unexported method, a value receiver registered without a pointer, or an unsupported shape all succeed silently and fail per-call with `method not found` or `unsupported method signature`. Add a smoke-test call per method to your test suite.

19. Deadlines and cancellation are not propagated to the server. `Handler.Process` invokes handlers with `context.Background()`, so the `ctx` parameter of a grpc-style handler and `ServerStream.Context()` are never cancelled. A handler keeps consuming CPU, holding transactions, and writing stream frames after the client has timed out or cancelled. Enforce your own server-side deadlines inside handlers.

20. Unary requests on one connection are processed serially. `Server.Handle` reads a frame, dispatches it synchronously through `Process`, and only then reads the next frame. Only streaming handlers get their own goroutine. One slow unary handler therefore delays every subsequent unary call multiplexed on the same socket — and since the client pool holds exactly one socket per address, that means all of that client's calls. Keep unary handlers fast, or move slow work behind a streaming method.

21. Errors lose their type across the wire. Server errors travel as `Header.Error` strings and are rebuilt with `errors.New`, so `errors.Is` and `errors.As` never match a typed server error and no error code, status, or detail payload exists. Encode structured failures in your reply type instead of returning them as `error`.

22. A single malformed frame kills the connection. `TCPConnection.Read` returns any `protocol.Decode` error to its caller: on the client `readLoop` calls `fail(err)`, tearing down the socket and failing every pending future and stream; on the server `Handle` returns and closes the connection. `PacketBuffer` resynchronizes on a bad magic number, but a frame whose header fails JSON unmarshalling or whose body fails gzip decompression is fatal. Never share the port with another protocol.

23. `Serve` can spin on a persistent accept error. When `Accept` fails and the `closing` channel is not closed, the loop `continue`s with no backoff and no error accounting, so a permanent condition such as file-descriptor exhaustion becomes a tight CPU loop. `Serve` also never returns a non-nil error, so a supervising goroutine cannot distinguish a clean shutdown from this state. Monitor accept-side health externally.

24. The server's bound address is not exposed. `internal/server.Server.Addr()` exists but `pkg/rpc.Server` does not wrap it, so with `net.Listen("tcp", "127.0.0.1:0")` you must keep the `net.Listener` and read `lis.Addr()` yourself.

25. `Future.OnComplete` has a single callback slot. Calling it on a future returned by registry-mode `InvokeAsync` before the call completes overwrites the framework's breaker-recording callback, so that call stops feeding the circuit breaker. Use `DoneChan()` for your own completion notification instead.

26. Streams cannot be cancelled through the `ClientStream` interface. `*transport.ClientStreamConn` has `Cancel()`, but the interface exposes only `Recv` and `Context`, and the concrete type is not importable. Cancel the `ctx` you passed to `NewStream`. Note also that cancelling stops only the client: the server handler runs to completion.

27. Per-address pools and per-`service|addr` breakers are never evicted. `internal/client.Client` keeps both in `sync.Map`s populated by `LoadOrStore` with no removal path. A long-running client against a churning fleet accumulates one idle socket and one breaker per address it has ever contacted, released only by `ClientConn.Close`.

28. Resource cleanup is not automatic anywhere. A `Server` that is constructed and never stopped leaks its limiter ticker goroutine; a registry-mode `ClientConn` that is never closed leaks its limiter plus every pool's `readLoop`; a `Registry` that is never closed leaks one watch goroutine per discovered service plus one drain goroutine per `Register`. `ClientConn.Close` does not close the `*Registry` you passed in. Always defer all three closes.

29. `transport.ErrStreamNotFound` is dead code — declared and never referenced. Do not build error handling around it.

30. `NewConnectionPool`'s `maxIdle` parameter is ignored, and `NewHandler`'s first parameter (`s any`) is ignored. Both are vestigial signatures; the pool has no idle list and the handler receives its service value per request from `Process`.

## File map

All 44 `.go` files in the module.

| File | Purpose |
| --- | --- |
| `pkg/rpc/rpc.go` | Type aliases (`CodecType`, `Registry`, `Instance`, `LoadBalancer`, `Future`), the `CodecJSON`/`CodecProto` variables, and `NewRegistry` |
| `pkg/rpc/client.go` | `DialOption` set (`WithTimeout`, `WithDialCodec`, `WithRegistry`, `WithLoadBalancer`), `dialOptions` defaults, `connMode`, `Dial`, `ClientConn` with `Invoke`/`InvokeAsync`/`NewStream`/`Close`, and the static-mode send paths including the async watchdog |
| `pkg/rpc/server.go` | `ServerOption`, `WithCodec`, `NewServer` (panics on option error), and the `Server` facade over `internal/server.Server` |
| `pkg/rpc/stream.go` | `ServerStream` and `ClientStream` type aliases |
| `pkg/rpc/stream_test.go` | End-to-end streaming tests: basic stream, mid-stream error, unary coexistence, context cancellation; defines the `startTestServer` loopback harness |
| `pkg/rpc/fixes_test.go` | Regression tests for previously shipped bugs: H-1 `GracefulStop` with an idle pooled connection, H-2 a slow stream not blocking unary calls, H-3 handler panic and non-pointer return not crashing the server, G-1 `InvokeAsync` reachable from the public API |
| `pkg/api/arith.go` | Sample services `Arith` and `Arith2` plus `Args`, `Args1`, `Reply`, all using the grpc-go-style signature |
| `pkg/api/arith_test.go` | Direct unit tests of the four sample methods, with no RPC involved |
| `internal/protocol/header.go` | `Header` struct, `CodecType` with `CodecTypeJSON`/`CodecTypeProto`, `StreamFlag` with `StreamNone`/`StreamData`/`StreamEnd`/`StreamError` |
| `internal/protocol/message.go` | `Magic = 0x1234`, `Message`, `Encode`, `Decode`, `DecodeHeaderLen`, `DecodeBodyLen`, compression hand-off and overflow/length checks |
| `internal/protocol/message_test.go` | Pins the wire format: magic at bytes 0-1, round trip with gzip, and the `header is nil`/`too short`/`magic`/`incomplete`/bad-gzip error paths |
| `internal/codec/codec.go` | `Codec` interface, `Type`, `Factory`, the mutex-guarded factory map, `Register` (panics on nil or duplicate), `New` |
| `internal/codec/json.go` | `JSON Type = 1` and the `encoding/json` codec, registered in `init` |
| `internal/codec/protobuf.go` | `PROTO Type = 2` and the `google.golang.org/protobuf` codec requiring `proto.Message`, registered in `init` |
| `internal/codec/compress.go` | `CompressionType` (`CompressionNone`, `CompressionGzip`), the unexported `compressor` interface, `GzipCompressor`, `RegisterCompressor`, `GetCompressor`, `Compress`, `Decompress` |
| `internal/codec/codec_test.go` | JSON and protobuf round trips, registry error and panic paths, gzip round trip, missing and failing compressors; provides the `assertPanics` helper |
| `internal/transport/tcp_connection.go` | `BufferSize = 4096`, `PacketBuffer` with looping magic resync, `TCPConnection` (`Read`, write-mutex `Write`, `SetLinger(0)` `Close`, `SetReadDeadline`, `RemoteAddr`) |
| `internal/transport/tcp_client.go` | `TCPClient`: 5-second hard-coded dial, atomic sequence allocation, `pending`/`streams` maps, `SendAsync`, `SendAsyncWithCodec`, `SendStream`, the `readLoop` demultiplexer, and the CAS-guarded unified `shutdown` |
| `internal/transport/tcp_connection_pool.go` | `ErrPoolClosed`, `ConnectionPool` with `Acquire` (mutex held across the dial, dead-connection eviction) and idempotent `Close`; `maxIdle` ignored |
| `internal/transport/future.go` | `Future`: idempotent `Done`, single-slot `OnComplete`, `Wait`/`WaitWithContext`/`WaitWithTimeout`, `GetResult`/`GetResultWithContext`, `DoneChan`, `IsDone`, `NewFuture`/`NewFutureWithCodec` |
| `internal/transport/stream.go` | `ClientStreamConn`: 64-frame buffer, out-of-band `sync.Once` terminal state, drain-before-terminal `Recv`, `Push`, `End`, `Error`, `Cancel`, `Context`, and the unused `ErrStreamNotFound` |
| `internal/transport/transport_test.go` | Future completion, codec selection and timeout semantics; `net.Pipe` framing plus `PacketBuffer` partial reads; `SendAsync` round trip over a loopback listener; `Close` unblocking a pending future; terminal frames not blocking on a full 64-frame buffer |
| `internal/server/server.go` | `Server` struct, `NewServer` (JSON handler, 10000-rps limiter), `Register`, the per-connection `Handle` loop with the rate-limit response, `Serve` accept loop with `serveWg`/`wg`, `Addr`, `GracefulStop`, `Stop`, `beginShutdown` |
| `internal/server/handler.go` | `Handler`, `NewHandler` (first parameter unused), `Process` with per-header codec negotiation, `writeError`, `safeCall` panic recovery, and the three-signature reflection dispatch including async stream launch |
| `internal/server/options.go` | `HandleOption`/`WithHandlerCodec` and `ServerOption`/`WithServerCodec` |
| `internal/server/stream.go` | `serverStream`: `Send` writing `StreamData`, `end` writing `StreamEnd`, `sendError` writing `StreamError`, and `Context` returning the handler context |
| `internal/server/server_test.go` | Reflection dispatch for both unary signatures and the failure cases, `Process` response and error frames over `net.Pipe`, `Handle` round trip, `Serve`/`Stop` lifecycle, concurrent `Register`, and option validation |
| `internal/client/client.go` | Registry-mode `Client` struct, `NewClient` defaults (JSON codec, zero-value `RoundRobin`, 10000-rps limiter, 5-second timeout), and `Close` stopping the limiter and every pool |
| `internal/client/invoke.go` | `Invoke` (timeout forced into the future so the breaker records it), `InvokeAsync` (timer watchdog), `InvokeStream`, the shared `invokeAsync` pipeline, and `observedStream` breaker accounting |
| `internal/client/option.go` | `ClientOption` set: `WithClientCodec`, `WithClientTimeout` (rejects `<= 0`), `WithClientLoadBalancer` (rejects nil) |
| `internal/client/resolver.go` | `getPool` (lazy per-address pool), `getAddr` (discover plus select, with the three empty-address errors), `getBreaker` (hard-coded `10, 0.6, 5s`, keyed `service|addr`) |
| `internal/client/client_test.go` | Default JSON codec, option validation failures, the missing-registry error, and positive-timeout acceptance |
| `internal/breaker/breaker.go` | `State` (`Closed`, `Open`, `HalfOpen`), `CircuitBreaker`, `NewCircuitBreaker`, `Allow`, `RecordSuccess`, `RecordFailure`, `State`, and the tumbling-window reset helpers |
| `internal/breaker/breaker_test.go` | Pins the thresholds and transitions: window completion opening the breaker, single half-open probe, failing probe reopening, and window reset below threshold |
| `internal/limiter/token_bucket.go` | `TokenBucket`, `NewTokenBucket` (negative rate clamped to 0, ticker goroutine resetting tokens once per second), `Allow`, idempotent `Stop` |
| `internal/limiter/token_bucket_test.go` | Initial burst and one-second refill, zero and negative rates rejecting everything, concurrent `Allow` with double `Stop` |
| `internal/load_balance/balancer.go` | The `LoadBalancer` interface |
| `internal/load_balance/round_robin.go` | `RoundRobin` (usable zero value, atomic counter, first selection index 0) and `NewRR` |
| `internal/load_balance/random.go` | `Random` and `NewRandom` (time-seeded `math/rand` behind a mutex) |
| `internal/load_balance/weighted_rr.go` | `WeightedRR` and `NewWeightedRR`: smooth weighted round-robin with negative-weight clamping and length/total-weight guards |
| `internal/load_balance/load_balance_test.go` | Round-robin ordering, random non-empty selection, exact weighted distribution over one full cycle, mismatched and zero weights, concurrent selection |
| `internal/registry/registry.go` | `Instance`, `Registry`, `NewRegistry` (hard-coded key prefix, 5-second etcd dial timeout), `Register` (lease grant, put, keepalive drain), `Discover`, `initService`, the `watch` loop, `copyInstances`, `Close` |
| `internal/registry/registry_test.go` | Defensive copying of the instance cache, `Discover` returning a copy, and nil-safe `Close` including a zero-value registry |
| `internal/stream/stream.go` | The `ServerStream` and `ClientStream` interfaces; exists only to break the `internal/server` ↔ `internal/transport` import cycle |

## Dependencies

`go.mod` declares `module github.com/hangtiancheng/swifty.go/swifty_rpc` with `go 1.26.0`. The enclosing `go.work` pins the toolchain at `go 1.26.4`. The code uses recent-toolchain APIs: `reflect.TypeFor`, `sync.WaitGroup.Go`, and range-over-int.

Direct requirements, two of them:

- `go.etcd.io/etcd/client/v3 v3.7.0` — the only external dependency in a hot path. Used exclusively by `internal/registry` for `client_v3.New`, `Grant`, `Put` with `WithLease`, `KeepAlive`, `Get` with `WithPrefix`, and `Watch` with `WithPrefix`. Nothing else in the module imports it, so a build without service discovery still links it but never calls it.
- `google.golang.org/protobuf v1.36.11` — used by `internal/codec/protobuf.go` for `proto.Marshal` and `proto.Unmarshal`, and by `internal/codec/codec_test.go` for `emptypb.Empty`. There is no `.proto` file, no generated code, and no descriptor registry usage in the module.

Everything else in `go.mod` is marked `// indirect`. Two of those deserve explicit mention:

- `google.golang.org/grpc v1.82.1` is indirect, pulled in transitively by the etcd v3 client. swifty_rpc does not use gRPC for transport, framing, streaming, status codes, interceptors, or anything else. Do not infer gRPC semantics from its presence in the dependency graph.
- `go.uber.org/zap v1.28.0` is likewise indirect via etcd. The framework itself logs with the standard library `log` package, and only twice: `server graceful stop complete` and `server stop complete`. There is no logger injection point.

Other indirect entries (`github.com/coreos/go-semver`, `github.com/coreos/go-systemd/v22`, `github.com/go-logr/logr`, `github.com/golang/protobuf`, `github.com/grpc-ecosystem/grpc-gateway/v2`, `go.etcd.io/etcd/api/v3`, `go.etcd.io/etcd/client/pkg/v3`, `go.opentelemetry.io/otel` and its metric SDK, `go.uber.org/multierr`, `golang.org/x/net`, `golang.org/x/sys`, `golang.org/x/text`, and the two `google.golang.org/genproto/googleapis` modules) are all etcd or protobuf transitive dependencies.

Standard library usage across the module: `net`, `bufio`, `io`, `compress/gzip`, `bytes`, `encoding/binary`, `encoding/json`, `reflect`, `context`, `sync`, `sync/atomic`, `time`, `math`, `math/rand`, `errors`, `fmt`, `strings`, `log`.

`go.mod` also carries `replace` directives pointing `swifty_cache`, `swifty_http`, and `swifty_orm` at sibling directories, plus a commented-out self-replace. None of the three siblings is actually imported by swifty_rpc; the directives exist so the module resolves inside the workspace if a dependency is added later.

## Cross-references

- `swifty-orm` — the real integration point. RPC handlers are the natural place to run MongoDB queries; register a service whose methods wrap a `swifty_orm` engine. Keep those handlers fast, because unary requests on one connection are serialized (Pitfall 20), and enforce your own query deadlines, because the client's deadline never reaches the handler (Pitfall 19).
- `swifty-cache` — useful in front of an expensive handler or behind a client that repeats identical calls. swifty_rpc has no response cache, no idempotency keys, and no request deduplication, so any memoization is yours to add at the call site or inside the handler.
- `swifty-http` — complementary and non-overlapping. swifty_rpc speaks only its own binary protocol over raw TCP, so an HTTP or WebSocket edge must be a separate `swifty_http` server that calls into swifty_rpc as a client. There is no transcoding, gateway, or reflection service to generate one automatically.
