---
name: swifty-http
description: >
  Lightweight, Koa-inspired HTTP framework for Go, module
  github.com/hangtiancheng/swifty.go/swifty_http. Exports Application (New, Default,
  Listen, Shutdown), Context (JSON, String, HTML, Data, Redirect, Throw, SetStatus,
  BindJSON, SSE, Upgrade), Router (Use, Get/Post/Put/Delete/Patch/Head/Options/All,
  Static), SSEWriter, WSConn, UpgradeOptions, Logger, Recovery, H, Middleware,
  TextMessage, BinaryMessage, ErrWSClosed, ErrWSInvalidFrame. Use when working on
  swifty_http code: HTTP routing, trie-based path matching, Koa-style middleware
  chains (func(ctx *Context, next func())), the deferred response pattern
  (ctx.Status / ctx.Body / ctx.Type), Server-Sent Events streaming, zero-dependency
  WebSocket servers, or router groups with prefixes. Trigger tokens: swifty_http,
  swifty.New(), swifty.Default(), app.Use(, app.Router(, ctx.SSE(), ctx.Upgrade(,
  ctx.Throw(, swifty.H{, WSConn. Do NOT use for plain net/http servers without a
  swifty_http import; gin, echo, fiber, chi, gorilla/mux; Node.js Koa; reverse
  proxies (nginx, envoy, traefik); or gRPC services (use the swifty-rpc skill).
---

# swifty_http

A lightweight, Koa-inspired HTTP framework for Go with trie-based routing, onion-model
middleware (`func(ctx *Context, next func())`), deferred response rendering with
immediate 404-to-200 status promotion in body setters, Server-Sent Events support, a
zero-dependency RFC 6455 WebSocket implementation (event-driven and synchronous APIs,
including fragmented-message reassembly), prefix-normalized router groups, and
koa-static-style file serving without directory listings.

Module path: `github.com/hangtiancheng/swifty.go/swifty_http`

Source root: `swifty_http/`

## Architecture overview

```
Application (top level, implements http.Handler)
  |-- root *Router          root router, prefix ""
  |-- router *router        per-method trie: method -> *node, plus handlers map
  |-- routers []*Router     every Router ever created, in creation order
  |-- htmlTemplates / funcMap
  |-- server *http.Server   constructed in New(), so Shutdown never races Listen

Request lifecycle:
  ServeHTTP -> walk app.routers in creation order
            -> for each Router whose prefix matches via matchRouterPath,
               append its middleware slice (collected at request time,
               so Use() after Router creation still takes effect)
            -> newContext(w, req); ctx.app = app
            -> router.handle(ctx, middlewares)
                 -> resolve route via trie; synthesize a notFound handler on miss
                    (sets Status=404, statusSet=true, plain-text body)
                 -> compose(middlewares, routeHandler)(ctx)
                    compose panics "next() called multiple times" on double next()
            -> ctx.respond()          skipped when ctx.flushed == true
                                      (set by ctx.SSE(), ctx.Upgrade(),
                                       and the Static handler)

WebSocket lifecycle:
  ctx.Upgrade(opts) -> CheckOrigin (403), method must be GET (405),
                       Connection/Upgrade/Sec-WebSocket-Version:13 (400),
                       Sec-WebSocket-Key required (400)
                    -> negotiate subprotocol (optional)
                    -> hijack via http.NewResponseController(ctx.Writer).Hijack()
                    -> ctx.flushed = true; ctx.Status = 101 (statusSet)
                    -> write the 101 handshake directly to the net.Conn
                    -> return *WSConn
  WSConn.Listen()      blocking event loop -> OnMessage/OnClose/OnError/OnPing/OnPong
  WSConn.ReadMessage() manual per-message read (alternative to Listen)
```

Key invariants:

- Handlers and middleware do not write to `ctx.Writer` directly. They set
  `ctx.Status`, `ctx.Body`, `ctx.Type`, and buffer headers with `ctx.Set`. The HTTP
  write happens once in `ctx.respond()` after the chain returns. This enables the
  onion model: code after `next()` sees and may mutate the response.
- Body setters (`JSON`, `String`, `Data`, `HTML`) promote the default 404 to 200
  immediately (`promoteStatus`), so middleware after `next()` observes the final
  status, matching Koa's `ctx.body =` setter semantics.
- Calling `next()` more than once in the same middleware panics with
  `"swifty_http: next() called multiple times"`; under `Default()` the Recovery
  middleware converts this into a 500 response.
- `ctx.flushed = true` tells `respond()` to skip rendering. `SSE()`, `Upgrade()`,
  and the Static handler set it because they own the response bytes.

## Core types

### Application

```go
func New() *Application
func Default() *Application   // New() + Use(Logger(), Recovery())

func (app *Application) Listen(addr string) error
func (app *Application) Shutdown(ctx context.Context) error
func (app *Application) SetFuncMap(funcMap template.FuncMap)
func (app *Application) LoadHTMLGlob(pattern string)
func (app *Application) ServeHTTP(w http.ResponseWriter, req *http.Request)

// Route registration (delegates to the root Router)
func (app *Application) Use(middlewares ...Middleware)
func (app *Application) Router(prefix string) *Router
func (app *Application) Get(pattern string, handler Middleware)
func (app *Application) Post(pattern string, handler Middleware)
func (app *Application) Put(pattern string, handler Middleware)
func (app *Application) Delete(pattern string, handler Middleware)
func (app *Application) Patch(pattern string, handler Middleware)
func (app *Application) Head(pattern string, handler Middleware)
func (app *Application) Options(pattern string, handler Middleware)
func (app *Application) All(pattern string, handler Middleware)
func (app *Application) Static(relativePath, root string)
```

Behavioral notes:

- `New()` constructs the internal `http.Server` immediately (not in `Listen`), so a
  concurrent `Shutdown` never races the server field assignment or silently no-ops
  before `Listen` runs. The idiom `go app.Listen(addr)` plus `app.Shutdown(ctx)` from
  the main goroutine is safe.
- `Listen` sets `server.Addr` and calls `ListenAndServe`; it returns
  `http.ErrServerClosed` after a successful `Shutdown`. Treat that as normal
  termination.
- `LoadHTMLGlob` uses `template.Must` and panics on parse errors. Call `SetFuncMap`
  before `LoadHTMLGlob`, otherwise the func map is not applied to the parsed
  templates.
- `All` registers the handler for GET, POST, PUT, DELETE, PATCH, HEAD, and OPTIONS.

### Context

Per-request state carrying the request, response writer, deferred response fields,
and route params.

```go
type H map[string]any             // convenience alias for inline JSON literals
type Middleware func(ctx *Context, next func())

type Context struct {
    Request *http.Request
    Writer  http.ResponseWriter
    Path    string                  // request URL path
    Method  string                  // HTTP method
    Status  int                     // deferred status; default http.StatusNotFound
    Body    any             // deferred body; nil means no body
    Type    string                  // Content-Type for the deferred response
    State   map[string]any  // per-request key/value bag for middleware
    Params  map[string]string       // route parameters captured by :name / *name
}
```

Field-level semantics:

- `Status` defaults to `http.StatusNotFound`. Body setters call `promoteStatus`,
  which upgrades 404 to 200 unless the status was explicitly recorded (internal
  `statusSet` flag, set by `SetStatus`, `Throw`, `Redirect`, the notFound handler,
  `SSE`, `Upgrade`, the Static handler, and `Recovery`). `respond()` applies the same
  promotion as a fallback for code that assigns `ctx.Body` directly.
- Assigning `ctx.Status = http.StatusNotFound` directly and then setting a body does
  NOT produce a 404: the promotion logic cannot distinguish an explicit field
  assignment of 404 from the default. Use `ctx.SetStatus(http.StatusNotFound)` or
  `ctx.Throw` when you want a 404 with a body.
- `Type` is set by `JSON` (`application/json`), `String` (`text/plain`), and `HTML`
  (`text/html`). A Content-Type buffered via `ctx.Set("Content-Type", ...)` always
  wins over the inferred `Type` (enforced by `setContentType`).
- `Body` accepts `[]byte`, `string`, `io.Reader`, the internal `htmlPayload`
  produced by `HTML`, or any JSON-serializable value; `respond()` selects the
  renderer by type switch.
- `State` is a plain map with no locking. Synchronize access yourself if you fan out
  goroutines from a handler.
- `Params` is populated by the router before the chain starts; treat it as
  read-only.

Status and error control:

```go
func (ctx *Context) SetStatus(status int)      // records an explicit status; never
                                               // overridden by the 404->200 promotion
func (ctx *Context) Throw(status int, msg string)
```

`Throw` sets the status (marking it explicit) and assigns
`ctx.Body = H{"message": msg, "data": nil}` -- the `data` key is included so error
responses share the unified `{message, data}` envelope used on success paths.

Request accessors:

| Method                                                                | Description                                                     |
| --------------------------------------------------------------------- | --------------------------------------------------------------- |
| `Query(key string) string`                                            | URL query parameter                                             |
| `Param(key string) string`                                            | Route path parameter (`:name` or `*name`)                       |
| `PostForm(key string) string`                                         | Form value (urlencoded or multipart)                            |
| `Get(header string) string`                                           | Request header                                                  |
| `BindJSON(out any) error`                                             | Decode the JSON request body; closes `Request.Body` via `defer` |
| `FormFile(key string) (multipart.File, *multipart.FileHeader, error)` | Uploaded file from a multipart form                             |

`BindJSON` is destructive: it consumes and closes the request body. Call it at most
once per request. There is no raw-body accessor; read `ctx.Request.Body` yourself
before any `BindJSON` call if you need the bytes.

Response setters (deferred; written by `respond()` at the end of the chain):

| Method                                 | Description                                                                                                                                                                                                                      |
| -------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `JSON(obj any)`                        | `Type = "application/json"`, `Body = obj`, promote status                                                                                                                                                                        |
| `String(format string, values ...any)` | `Type = "text/plain"`, `Body = fmt.Sprintf(...)`, promote status                                                                                                                                                                 |
| `Data(data []byte)`                    | `Body = data`, promote status; Content-Type is whatever `Type` holds (may be empty)                                                                                                                                              |
| `HTML(name string, data any)`          | `Type = "text/html"`, `Body = htmlPayload{name, data}`, promote status; requires `LoadHTMLGlob`                                                                                                                                  |
| `Redirect(url string)`                 | Sets the `Location` header; keeps an explicitly chosen 3xx status (300, 301, 302, 303, 305, 307, 308), otherwise defaults to 302; marks the status explicit; if `Body` is nil, sets a `text/plain` body `"Redirecting to <url>"` |
| `Set(header, value string)`            | Buffers a response header under its canonical key (`http.CanonicalHeaderKey`); flushed by `respond()`, `SSE()`, and the Static handler                                                                                           |

`respond()` behavior, in order:

1. Return immediately when `ctx.flushed` is true.
2. Fallback 404-to-200 promotion when `Body != nil`, `statusSet == false`, and
   `Status == 404`.
3. For empty statuses (204, 205, 304): clear `Body` and delete buffered
   `Content-Type`, `Content-Length`, and `Transfer-Encoding` headers.
4. Flush buffered headers to `ctx.Writer.Header()`.
5. If `Body == nil`, write the status header only.
6. Otherwise dispatch on the runtime type of `Body`:
   - `htmlPayload` -> execute the named template; a missing template set yields 500
     with `{"message":"HTML templates are not loaded"}`; execution errors are logged
     (the status line has already been sent).
   - `[]byte` -> written verbatim.
   - `string` -> written as text (`text/plain` default).
   - `io.Reader` -> copied via `io.Copy`.
   - anything else -> `json.Marshal` first; on marshal failure respond 500 with a
     JSON error body (the status line is not yet committed, so the downgrade is
     clean); on success write `application/json` by default.

### Router

Route grouping with a normalized path prefix and its own middleware stack.

```go
func (r *Router) Router(prefix string) *Router
func (r *Router) Use(middlewares ...Middleware)

// Route registration (pattern is prefixed with r.prefix)
func (r *Router) Get(pattern string, handler Middleware)
func (r *Router) Post(pattern string, handler Middleware)
func (r *Router) Put(pattern string, handler Middleware)
func (r *Router) Delete(pattern string, handler Middleware)
func (r *Router) Patch(pattern string, handler Middleware)
func (r *Router) Head(pattern string, handler Middleware)
func (r *Router) Options(pattern string, handler Middleware)
func (r *Router) All(pattern string, handler Middleware)   // GET POST PUT DELETE PATCH HEAD OPTIONS

// Static file serving
func (r *Router) Static(relativePath, root string)
```

Grouping rules:

- `Router(prefix)` normalizes the child prefix (`normalizePrefix`: forces a leading
  `/`, strips trailing `/`, maps `"/"` to `""`), concatenates it onto the parent's
  prefix, and appends the new Router to `app.routers`. `app.Router("v1")` and
  `app.Router("/v1/")` both produce prefix `/v1`, keeping trie registration and
  middleware matching consistent.
- Middleware is collected at request time: `ServeHTTP` walks all routers whose
  prefix matches the request path and concatenates their middleware in Router
  creation order. Calling `Use` on a parent after creating children still applies
  to matching requests. Execution order therefore follows creation order, not
  nesting depth (parents created first run first in practice).
- Prefix matching is path-segment aware (`matchRouterPath`): prefix `/v1` matches
  `/v1` and `/v1/...` but never `/v10`. Empty prefix and `/` match everything.
- Each route registration logs `Route <METHOD> - <pattern>` via the standard `log`
  package.
- `Static(relativePath, root)` mounts a GET route at
  `<r.prefix><relativePath>/*filepath` backed by `http.FileServer(http.Dir(root))`.
  The handler:
  1. Probes the target with `staticFileExists`, which opens and immediately closes
     the file handle. Missing files yield `Status = 404` (marked explicit) with an
     empty body. Directories are served only when they contain an `index.html`;
     bare directory listings are never exposed (koa-static behavior).
  2. Flushes headers buffered via `ctx.Set` (for example CORS headers set by
     upstream middleware) to the underlying writer before delegating.
  3. Sets `ctx.flushed = true` and serves through a `statusRecorder` wrapper that
     mirrors the status the file server writes back into `ctx.Status`, so Logger
     reports the real code.

### SSEWriter

Server-Sent Events support, obtained via `ctx.SSE()`.

```go
func (ctx *Context) SSE() *SSEWriter
```

`SSE()` side effects, in order:

1. Type-asserts `ctx.Writer` to `http.Flusher`; on failure (for example a bare
   `httptest.ResponseRecorder` shim without flushing) the flusher is nil and
   `Flush` becomes a no-op -- writes still succeed but may buffer until the handler
   returns.
2. Sets `ctx.flushed = true` and records `ctx.Status = 200` as explicit, so Logger
   reports 200 for SSE requests.
3. Sets `Content-Type: text/event-stream`, `Cache-Control: no-cache`, and
   `Connection: keep-alive` on the underlying writer.
4. Flushes headers previously buffered via `ctx.Set`.
5. Writes the 200 status line and returns the `SSEWriter`.

Call `ctx.SSE()` after any `ctx.Set` calls you want flushed, and do not mix it with
the deferred response setters afterwards.

```go
func (w *SSEWriter) Event(event string, data string)
func (w *SSEWriter) Data(data string)
func (w *SSEWriter) JSON(event string, obj any)
func (w *SSEWriter) ID(id string)
func (w *SSEWriter) Retry(ms int)
func (w *SSEWriter) Comment(text string)
func (w *SSEWriter) Heartbeat(interval time.Duration) func()
func (w *SSEWriter) Stream(ch <-chan string)
func (w *SSEWriter) Closed() <-chan struct{}
func (w *SSEWriter) Flush()
func (w *SSEWriter) Done()
```

| Method                | Description                                                                                                                     |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `Event(event, data)`  | Writes `event: <event>\n`, then one `data:` line per input line, then a blank line                                              |
| `Data(data)`          | Data block only; multiline input becomes multiple `data:` lines                                                                 |
| `JSON(event, obj)`    | `json.Marshal` then a single `data:` line; the `event:` line is omitted when `event == ""`; marshal errors are silently dropped |
| `ID(id)`              | Writes `id: <id>\n` -- a field line, not a complete event                                                                       |
| `Retry(ms)`           | Writes `retry: <ms>\n\n`                                                                                                        |
| `Comment(text)`       | One `: ` comment line per input line, then a blank line                                                                         |
| `Heartbeat(interval)` | Spawns a goroutine writing `: keepalive\n\n` every interval; returns an idempotent stop function                                |
| `Stream(ch)`          | Forwards channel values via `Data` until `ch` closes or the client disconnects                                                  |
| `Closed()`            | Returns `ctx.Request.Context().Done()` for disconnect detection                                                                 |
| `Flush()`             | Manually invokes the underlying `http.Flusher`                                                                                  |
| `Done()`              | Writes `data: [DONE]\n\n`, the OpenAI-compatible sentinel                                                                       |

Concurrency and lifetime contracts:

- All write methods are guarded by an internal `sync.Mutex`; concurrent producers
  (for example your handler plus the heartbeat goroutine) are safe.
- The stop function returned by `Heartbeat` is idempotent (`sync.Once`) and blocks
  until the heartbeat goroutine has exited. The goroutine also exits on its own when
  the request context is canceled.
- `ID` writes a standalone field line; SSE clients dispatch an event only after a
  `data:` field followed by a blank line. Call `ID` immediately before the `Event`,
  `Data`, or `JSON` call it should be bundled with.
- `Retry` ends with a blank line, which terminates any partially written event
  block; emit it between complete events.
- `Stream` returns when the channel closes or the client disconnects. It does not
  cancel the upstream producer; cancel it yourself to avoid goroutine leaks.
- `Event` does not reject an empty event name; use `Data` or `JSON("", obj)` for
  unnamed events.

### WebSocket

Zero-dependency RFC 6455 server-side WebSocket implementation with both an
event-driven (Node.js ws-style) API and a synchronous read/write API.

Constants and errors:

```go
const (
    TextMessage   = 1
    BinaryMessage = 2
    CloseMessage  = 8
    PingMessage   = 9
    PongMessage   = 10
)

var (
    ErrWSClosed       = errors.New("websocket: connection closed")
    ErrWSInvalidFrame = errors.New("websocket: invalid frame")
)
```

```go
type UpgradeOptions struct {
    ReadBufferSize  int                        // bufio.Reader size; default 4096
    WriteBufferSize int                        // declared but currently unused
    MaxMessageSize  int                        // read-side cap for a single frame and the
                                               // reassembled fragmented total; 0 = 65536
    CheckOrigin     func(r *http.Request) bool // reject with 403 when it returns false
    Subprotocols    []string                   // server-preferred subprotocol list
}

func (ctx *Context) Upgrade(opts *UpgradeOptions) (*WSConn, error)
```

`Upgrade` performs, in order:

1. `CheckOrigin` (if set): failure throws 403 and returns an error.
2. Method must be GET: otherwise throws 405.
3. `Connection` must contain the `upgrade` token and `Upgrade` must contain
   `websocket` (case-insensitive, comma-list aware): otherwise 400.
4. `Sec-WebSocket-Version` must contain `13`: otherwise 400 with an advisory
   `Sec-WebSocket-Version: 13` response header (gorilla/websocket behavior).
5. `Sec-WebSocket-Key` must be present: otherwise 400.
6. Optional subprotocol negotiation: the first server-preferred protocol also
   offered by the client wins.
7. Hijack via `http.NewResponseController(ctx.Writer).Hijack()`; failure (for
   example under `httptest.ResponseRecorder`) throws 500 and returns an error.
8. Sets `ctx.flushed = true` and records `ctx.Status = 101` as explicit (so Logger
   reports 101 for real upgrades only).
9. Clears the connection deadline; reuses the hijacked `bufio.Reader` when it
   already holds buffered bytes or is at least `ReadBufferSize`, otherwise
   allocates a new reader.
10. Writes the `101 Switching Protocols` response (computed `Sec-WebSocket-Accept`,
    optional `Sec-WebSocket-Protocol`) directly to the connection and returns the
    `*WSConn`.

On every validation failure `Upgrade` returns a non-nil error and leaves a deferred
error response on the context (via `Throw`), which `respond()` renders normally --
just `return` from the handler after an `Upgrade` error.

Event-driven API:

```go
func (ws *WSConn) OnMessage(fn func(messageType int, data []byte))
func (ws *WSConn) OnClose(fn func(code int, text string))
func (ws *WSConn) OnError(fn func(err error))
func (ws *WSConn) OnPing(fn func(data []byte))
func (ws *WSConn) OnPong(fn func(data []byte))
func (ws *WSConn) Listen()
```

`Listen` runs a blocking read loop over reassembled messages. Ping frames invoke
`OnPing` and receive an automatic pong. Pong frames invoke `OnPong`. Close frames
trigger a close reply, close the `Closed()` channel, invoke `OnClose`, and return.
Read errors invoke `OnError` (unless the connection was already closed locally --
the trailing "use of closed network connection" error is suppressed), close the
`Closed()` channel, and return. Register handlers before calling `Listen`;
registration is not synchronized.

Synchronous API:

```go
func (ws *WSConn) ReadMessage() (messageType int, data []byte, err error)
func (ws *WSConn) ReadJSON(v any) error
func (ws *WSConn) WriteMessage(messageType int, data []byte) error
func (ws *WSConn) WriteJSON(v any) error
func (ws *WSConn) Send(text string) error        // WriteMessage(TextMessage, ...)
func (ws *WSConn) WriteText(text string) error   // WriteMessage(TextMessage, ...)
func (ws *WSConn) WriteBinary(data []byte) error // WriteMessage(BinaryMessage, ...)
func (ws *WSConn) Ping() error
func (ws *WSConn) Close() error
func (ws *WSConn) CloseWithMessage(code int, text string) error
```

`ReadMessage` transparently answers pings, skips pongs, and on a close frame replies
with a close frame, closes the `Closed()` channel, invokes `OnClose` if registered,
and returns `ErrWSClosed`. It returns only `TextMessage` or `BinaryMessage` payloads
to the caller.

Utility methods:

```go
func (ws *WSConn) Closed() <-chan struct{}
func (ws *WSConn) SetReadDeadline(t time.Time) error
func (ws *WSConn) SetWriteDeadline(t time.Time) error
func (ws *WSConn) Heartbeat(interval time.Duration) func()  // sends Ping frames
func (ws *WSConn) NetConn() net.Conn
```

Frame handling and protocol conformance (reading path):

- Fragmented messages are reassembled: a non-FIN text/binary frame starts a buffer,
  continuation frames (opcode 0) append, and the FIN continuation returns the whole
  message. A continuation without a preceding data frame, a new data frame while a
  fragmented message is in progress, or a reassembled message exceeding the
  message size limit all return `ErrWSInvalidFrame`.
- The message size limit bounds both a single frame and the reassembled message
  total. It defaults to 65536 bytes (`maxFrameSize`) and is raised per connection
  via `UpgradeOptions.MaxMessageSize`. The limit applies to reads only;
  `writeFrame` will emit frames of any size.
- Client frames must be masked (RFC 6455 5.1); unmasked frames are rejected with
  `ErrWSInvalidFrame`.
- RSV1-3 bits must be zero (no extensions are negotiated); control frames
  (close/ping/pong) must have FIN set and a payload of at most 125 bytes; unknown
  opcodes are rejected.
- Close payloads are parsed into `(code, text)`; invalid UTF-8 close text is
  replaced by an empty string; a missing code defaults to 1000.

Concurrency and lifetime contracts:

- Writes (`WriteMessage`, `WriteJSON`, `Send`, `WriteText`, `WriteBinary`, `Ping`,
  and the internal close-frame reply) are serialized by `writeMu`; concurrent
  writers are safe.
- `ReadMessage` / `ReadJSON` are serialized by `readMu`. `Listen` does not acquire
  `readMu`; do not mix `Listen` with `ReadMessage` on the same connection.
- `Close` and `CloseWithMessage` close the `Closed()` channel exactly once
  (`sync.Once`), write a close frame (code 1000 for `Close`), and close the
  underlying `net.Conn`. Calling `Close` twice does not panic but the second call
  returns the net.Conn double-close error.
- `Heartbeat` returns an idempotent stop function (`sync.Once`) that blocks until
  the goroutine exits; the goroutine also exits on ping failure or when the
  connection is closed.
- `WriteBufferSize` in `UpgradeOptions` is declared but not used; writes go
  directly to the `net.Conn` without additional buffering.

## Routing

The internal `router` maintains one prefix trie per HTTP method plus a
`method + "-" + pattern -> Middleware` handler map. Lookup is O(segments).

Path parameter syntax:

- `:name` matches a single path segment; read it with `ctx.Param("name")`.
- `*name` matches the remainder of the path; the captured value joins the remaining
  segments with `/`.

Matching and registration rules:

- Patterns are canonicalized at registration: `parsePattern` collapses consecutive
  slashes and the stored pattern is rebuilt as `"/" + strings.Join(parts, "/")`.
  `/users`, `/users/`, and `//users` therefore share one pattern and one handler
  key -- the last registration wins, and no earlier handler becomes unreachable.
- Registering the same method + canonical pattern twice silently overwrites the
  previous handler. There is no duplicate detection.
- A `*name` wildcard must be the last segment; `parsePattern` stops at the first
  `*` segment, so `/a/*b/c` registers as `/a/*b`.
- Literal segments win over wildcard segments at the same trie level: with both
  `/files/exact` and `/files/*filepath` registered, `/files/exact` hits the literal
  route (`matchChildren` orders exact matches before wild ones).
- Matching is per HTTP method. A miss (wrong path or wrong method) runs a
  synthesized notFound handler through the full middleware chain: `Status = 404`
  (explicit), body `"404 NOT FOUND: <path>\n"`. There is no 405 / `Allow` header
  support.

## Built-in middleware

### Logger

```go
func Logger() Middleware
```

Records `time.Now()` before `next()`, then logs `[<status>] <RequestURI> in
<duration>` through the standard `log` package. Because body setters promote the
status immediately, and because `SSE()` records 200, `Upgrade()` records 101, and
the Static handler mirrors the file server's real status via `statusRecorder`,
the logged status matches what the client received. Output is plain text; replace
this middleware entirely if you need structured logging.

### Recovery

```go
func Recovery() Middleware
```

Wraps `next()` in `defer`/`recover`. Behavior on panic:

- `http.ErrAbortHandler` is re-raised so `net/http` can abort the request silently
  (required for `httputil.ReverseProxy` client-disconnect handling).
- Any other panic value is formatted with `%v`, logged together with a traceback
  built from `runtime.Callers(3, ...)` / `runtime.CallersFrames` (safe against nil
  frames), and converted into a deferred 500 response: `Status = 500` (explicit),
  `Type` cleared, all buffered headers discarded, `Body = H{"message": "Internal
Server Error"}`. Clearing `Type` and headers prevents a pre-panic
  `ctx.Type = "text/html"` or stray `ctx.Set` calls from polluting the error
  response.

The 500 body is fixed. To deliver a custom error envelope, replace `Recovery` with
your own middleware; layering another middleware on top cannot change the body it
assigns.

## Internal implementation details affecting correctness

### compose

```go
func compose(middlewares []Middleware, final Middleware) func(ctx *Context)
```

Builds a koa-compose-style dispatcher. Each `next()` advances an index; calling
`next()` a second time from the same position panics with
`"swifty_http: next() called multiple times"`. When the index passes the middleware
slice, the `final` handler runs with a no-op `next`. Under `Default()` the panic
surfaces as a 500 via Recovery; without Recovery it propagates to `net/http`.

### Status promotion and statusSet

`promoteStatus` runs inside `JSON`, `String`, `Data`, and `HTML`: if the status was
never explicitly recorded and is still the default 404, it becomes 200 immediately.
`respond()` repeats the check as a fallback for direct `ctx.Body` assignments. The
`statusSet` flag is private; the only public ways to set it are `SetStatus`,
`Throw`, and `Redirect` (plus framework paths: notFound, Static miss, `SSE`,
`Upgrade`, `Recovery`).

### Header buffering

`ctx.Set` canonicalizes keys with `http.CanonicalHeaderKey` and stores them in a
`map[string]string`. Consequences: repeated `Set` on the same header overwrites
(no multi-value support, so multiple `Set-Cookie` values are impossible through
this API -- use `ctx.Writer.Header().Add` directly), and there is no header removal
API. Buffered headers are flushed by `respond()`, by `SSE()`, and by the Static
handler before delegation.

### Content-Type precedence

`setContentType` only applies the inferred `ctx.Type` when the response writer does
not already carry a Content-Type. A user `ctx.Set("content-type", ...)` (any case)
therefore always wins over the type implied by `String`/`JSON`/`HTML`.

### Empty statuses

For 204, 205, and 304, `respond()` drops the body and deletes buffered
`Content-Type`, `Content-Length`, and `Transfer-Encoding` headers, matching Koa's
`statuses.empty` handling and avoiding `net/http` "status code does not allow body"
warnings.

### matchRouterPath

Returns true when the router prefix is empty or `/`, or when the request path
starts with the prefix and the next character is end-of-string or `/`. This is what
prevents `/v1` middleware from firing on `/v10/...`.

## Typical usage

### JSON API with router groups and graceful shutdown

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os/signal"
    "syscall"
    "time"

    swifty "github.com/hangtiancheng/swifty.go/swifty_http"
)

func main() {
    app := swifty.Default()

    api := app.Router("/api")
    api.Use(func(ctx *swifty.Context, next func()) {
        if ctx.Get("Authorization") == "" {
            ctx.Throw(http.StatusUnauthorized, "missing token")
            return // do not call next(): short-circuit
        }
        next()
    })

    api.Get("/users/:id", func(ctx *swifty.Context, next func()) {
        user, err := findUser(ctx.Param("id"))
        if err != nil {
            ctx.Throw(http.StatusNotFound, "user not found")
            return
        }
        ctx.JSON(user) // status auto-promotes to 200
    })

    api.Post("/users", func(ctx *swifty.Context, next func()) {
        var in struct {
            Name string `json:"name"`
        }
        if err := ctx.BindJSON(&in); err != nil {
            ctx.Throw(http.StatusBadRequest, "invalid JSON")
            return
        }
        ctx.SetStatus(http.StatusCreated)
        ctx.JSON(swifty.H{"message": "created", "data": in.Name})
    })

    go func() {
        if err := app.Listen(":8080"); err != nil && err != http.ErrServerClosed {
            log.Fatal(err)
        }
    }()

    sigCtx, stopSig := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stopSig()
    <-sigCtx.Done()

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    _ = app.Shutdown(shutdownCtx)
}
```

### SSE streaming with heartbeat and disconnect handling

```go
app.Post("/chat", func(ctx *swifty.Context, next func()) {
    sse := ctx.SSE()
    defer sse.Done()
    stop := sse.Heartbeat(15 * time.Second)
    defer stop()

    upstream := generateResponse(ctx.Request.Context()) // <-chan string
    for {
        select {
        case <-sse.Closed():
            return // client disconnected; cancel your producer via the request context
        case chunk, ok := <-upstream:
            if !ok {
                return
            }
            sse.Data(chunk)
        }
    }
})
```

### WebSocket echo server (synchronous API)

```go
app.Get("/ws", func(ctx *swifty.Context, next func()) {
    ws, err := ctx.Upgrade(nil)
    if err != nil {
        return // Upgrade already staged the error response
    }
    defer ws.Close()

    for {
        msgType, data, err := ws.ReadMessage()
        if err != nil {
            return // ErrWSClosed on clean close, read error otherwise
        }
        if err := ws.WriteMessage(msgType, data); err != nil {
            return
        }
    }
})
```

### WebSocket with the event-driven API and origin check

```go
app.Get("/ws", func(ctx *swifty.Context, next func()) {
    ws, err := ctx.Upgrade(&swifty.UpgradeOptions{
        CheckOrigin:  func(r *http.Request) bool { return r.Header.Get("Origin") == "https://example.com" },
        Subprotocols: []string{"chat"},
    })
    if err != nil {
        return
    }
    defer ws.Close()

    stop := ws.Heartbeat(30 * time.Second)
    defer stop()

    ws.OnMessage(func(messageType int, data []byte) {
        _ = ws.Send("echo: " + string(data))
    })
    ws.OnClose(func(code int, text string) {
        log.Printf("closed: %d %s", code, text)
    })
    ws.OnError(func(err error) {
        log.Printf("error: %v", err)
    })

    ws.Listen() // blocks until close or error
})
```

### WebSocket with JSON messages

```go
app.Get("/ws", func(ctx *swifty.Context, next func()) {
    ws, err := ctx.Upgrade(nil)
    if err != nil {
        return
    }
    defer ws.Close()

    var req RequestMessage
    if err := ws.ReadJSON(&req); err != nil {
        return
    }
    _ = ws.WriteJSON(ResponseMessage{Status: "ok", Data: req.Data})
})
```

## Pitfalls / known limitations

1. Explicit 404 with a body requires `SetStatus` or `Throw`. Assigning
   `ctx.Status = http.StatusNotFound` directly and then calling a body setter yields
   200, because the promotion logic cannot distinguish an explicit 404 field
   assignment from the default. Use `ctx.SetStatus(http.StatusNotFound)` before the
   body setter.

2. Calling `next()` twice in one middleware panics. `compose` enforces the
   koa-compose single-call rule; under `Default()` the panic becomes a 500, without
   Recovery it propagates. Never call `next()` more than once.

3. `BindJSON` closes the request body. A second call, or any later body read,
   yields an empty or errored read. There is no built-in body buffering.

4. Route registration is not goroutine-safe. Register all routes before `Listen`;
   registering from handlers or background goroutines is a data race.

5. Duplicate registrations overwrite silently. The same method + canonical pattern
   (slashes collapsed, so `/users` == `/users/` == `//users`) replaces the previous
   handler without warning.

6. `ctx.Set` cannot express multi-value headers. It buffers into a
   `map[string]string`, so only the last value per canonical key survives. For
   multiple `Set-Cookie` headers, write to `ctx.Writer.Header().Add` directly (and
   note Recovery discards buffered headers on panic, not ones written directly).

7. `SetFuncMap` must precede `LoadHTMLGlob`. The func map is captured at parse
   time; calling it afterwards has no effect on already-parsed templates.
   `LoadHTMLGlob` panics on parse errors (`template.Must`).

8. Mixing SSE or WebSocket with deferred response setters is unsupported. After
   `ctx.SSE()` or a successful `ctx.Upgrade`, `ctx.flushed` is true and `respond()`
   is a no-op; later `ctx.JSON`/`ctx.String` calls are silently ignored.

9. `SSEWriter` on a non-flushing writer degrades silently. If `ctx.Writer` does not
   implement `http.Flusher`, `Flush` is a no-op and events may buffer until the
   handler returns. Test SSE against a real server or a flusher-capable recorder.

10. `SSEWriter.Stream` does not cancel the producer. It returns on channel close or
    client disconnect but never drains the channel; tie your producer to
    `ctx.Request.Context()` to avoid goroutine leaks.

11. WebSocket messages are capped on the read side, 65536 bytes by default. Single
    frames and reassembled fragmented messages beyond the limit return
    `ErrWSInvalidFrame`; raise it with `UpgradeOptions.MaxMessageSize`. The write
    side is not capped, so a peer running the same implementation with a smaller
    limit will reject oversized frames you send.

12. `WSConn.Listen` and `ReadMessage` must not run concurrently. `Listen` does not
    take `readMu`; pick one read pattern per connection.

13. `UpgradeOptions.WriteBufferSize` is accepted but unused. All WebSocket writes go
    unbuffered to the `net.Conn`.

14. `ctx.Upgrade` requires a hijackable writer. Under
    `httptest.ResponseRecorder` the hijack fails and `Upgrade` returns an error
    (staging a 500); use `httptest.NewServer` for WebSocket tests.

15. No 405 support. A path registered only for GET answers POST with the notFound
    handler (404), not 405 with an `Allow` header.

16. Text frames are not UTF-8 validated. RFC 6455 mandates closing with 1007 on
    invalid UTF-8 text messages; this implementation delivers the raw bytes.

17. `Recovery`'s 500 body is fixed. It always emits
    `{"message":"Internal Server Error"}` and clears `Type` and buffered headers;
    fork the middleware for custom error envelopes.

## File map

| File                | Purpose                                                                                                                                                 |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `swifty.go`         | `Application`, `New`/`Default`, `Listen`/`Shutdown`, template loading, `ServeHTTP`, `compose` with double-next guard                                    |
| `context.go`        | `Context`, `H`, `Middleware`, request accessors, `Throw`, `SetStatus`, header buffering (`Set`)                                                         |
| `response.go`       | Body setters (`JSON`, `String`, `Data`, `HTML`, `Redirect`), `promoteStatus`, empty-status handling, `respond` and per-type renderers, `setContentType` |
| `group.go`          | `Router` with `normalizePrefix`, `Use`, method registration, `Static` with existence probe / no directory listing / `statusRecorder`, `matchRouterPath` |
| `router.go`         | Internal `router`: pattern canonicalization in `addRoute`, `getRoute`, `handle`, synthesized 404 handler                                                |
| `trie.go`           | Trie `node`: `insert`, `search`, `travel`, literal-before-wildcard child matching                                                                       |
| `sse.go`            | `SSEWriter` and `Context.SSE`                                                                                                                           |
| `websocket.go`      | `Context.Upgrade`, `UpgradeOptions`, `WSConn`, frame I/O with fragmentation reassembly and RFC validation, constants, errors                            |
| `logger.go`         | `Logger` middleware                                                                                                                                     |
| `recovery.go`       | `Recovery` middleware with `http.ErrAbortHandler` passthrough and safe traceback                                                                        |
| `main.go`           | Demo entry point; `//go:build ignore`, excluded from package builds                                                                                     |
| `context_test.go`   | Request accessor, `BindJSON`, `State`, `Throw` tests                                                                                                    |
| `response_test.go`  | Renderer, promotion, `SetStatus`, empty-status, `Redirect`, Content-Type precedence, onion-model tests                                                  |
| `router_test.go`    | `parsePattern`, trie route resolution, literal-over-wildcard priority, 404 tests                                                                        |
| `sse_test.go`       | SSE wire format, multiline data, JSON events, comments, stream, heartbeat concurrency tests                                                             |
| `swifty_test.go`    | Router nesting/normalization, middleware order, prefix boundary, Recovery, static files (listing disabled, fd close), templates, double-next 500 tests  |
| `websocket_test.go` | Handshake, accept key, echo, JSON, heartbeat, fragmentation, unmasked-frame rejection, version validation tests                                         |

## Dependencies

None beyond the Go standard library. `go.mod` declares
`module github.com/hangtiancheng/swifty.go/swifty_http` with `go 1.26.0` and
`replace` directives pointing the sibling modules (`swifty_cache`, `swifty_orm`,
`swifty_rpc`) at their local directories.

Standard library packages used: `bufio`, `context`, `crypto/sha1`,
`encoding/base64`, `encoding/binary`, `encoding/json`, `errors`, `fmt`,
`html/template`, `io`, `log`, `mime/multipart`, `net`, `net/http`, `path`,
`runtime`, `strings`, `sync`, `time`, `unicode/utf8`.

## Cross-references

- `swifty-cache` -- distributed read-through cache; handlers commonly wrap data
  fetches in a cache `Group` before responding with `ctx.JSON`. The local `replace`
  directive in `go.mod` makes the sibling importable directly.
- `swifty-orm` -- MongoDB ORM; handlers typically call ORM queries to fetch or
  mutate data before responding via `ctx.JSON`.
- `swifty-rpc` -- TCP RPC framework; for services exposing both HTTP and RPC
  surfaces, use `swifty_http` for HTTP and `swifty_rpc` for RPC. Do not document
  gRPC-style questions under this skill.

## Behavioral contracts cheat sheet

Non-obvious contracts enforced by the source. Use this table when generating or
reviewing `swifty_http` code.

| Area                     | Contract                                                                                                                                                                                                                              |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Status promotion         | Body setters promote 404 to 200 immediately (`promoteStatus`); middleware after `next()` sees the final status. `respond()` repeats the check for direct `ctx.Body` assignments.                                                      |
| Explicit status          | `SetStatus`, `Throw`, and `Redirect` mark the status explicit; direct `ctx.Status` field assignment does not, so explicit 404+body requires `SetStatus`/`Throw`.                                                                      |
| Throw shape              | `Throw` sets `Body = H{"message": msg, "data": nil}` -- the unified `{message, data}` envelope.                                                                                                                                       |
| Redirect status          | `Redirect` keeps a previously chosen 3xx (300/301/302/303/305/307/308), otherwise defaults to 302, and adds a `"Redirecting to <url>"` text body when `Body` is nil.                                                                  |
| Empty statuses           | 204/205/304 strip the body and Content-Type/Length/Transfer-Encoding headers in `respond()`.                                                                                                                                          |
| Content-Type             | A Content-Type buffered via `ctx.Set` always beats the inferred `ctx.Type`.                                                                                                                                                           |
| Header keys              | `ctx.Set` canonicalizes keys; single value per header, no removal API, no multi-value (`Set-Cookie`) support.                                                                                                                         |
| respond skip             | `respond()` returns early when `ctx.flushed`; `SSE()`, `Upgrade()`, and Static rely on this.                                                                                                                                          |
| JSON failure             | `respondJSON` marshals before committing the status line; marshal failure downgrades cleanly to 500 JSON.                                                                                                                             |
| next() guard             | `compose` panics on a repeated `next()`; Recovery turns it into a 500.                                                                                                                                                                |
| Recovery                 | Re-raises `http.ErrAbortHandler`; otherwise logs `%v` + traceback and resets Status/Type/headers/Body to a fixed 500 JSON.                                                                                                            |
| Logger accuracy          | Logger reads `ctx.Status` after the chain; SSE records 200, Upgrade records 101, Static mirrors the real status via `statusRecorder`.                                                                                                 |
| Router prefix            | `normalizePrefix` forces a leading `/` and strips trailing `/`; `/v1` matches `/v1` and `/v1/...`, never `/v10`.                                                                                                                      |
| Middleware collection    | Collected at request time from all matching routers in creation order; `Use` after child creation still applies.                                                                                                                      |
| Pattern canonicalization | `addRoute` rebuilds the pattern from parsed parts; `/users`, `/users/`, `//users` share one handler slot (last wins).                                                                                                                 |
| Wildcard                 | `*name` must be last; later segments are silently dropped. Literal children beat wildcards.                                                                                                                                           |
| Static                   | Probes existence (handle closed immediately), 404 on miss, no directory listings without `index.html`, flushes `ctx.Set` headers before delegating.                                                                                   |
| Server lifecycle         | `http.Server` is built in `New()`; `go app.Listen` + `Shutdown` is race-free; `Listen` returns `http.ErrServerClosed` after shutdown.                                                                                                 |
| Templates                | `SetFuncMap` before `LoadHTMLGlob`; `LoadHTMLGlob` panics on parse errors.                                                                                                                                                            |
| SSE heartbeat            | Stop functions (SSE and WS) are idempotent and block until the goroutine exits.                                                                                                                                                       |
| SSE fields               | `ID` is a bare field line (pair it with `Data`/`Event`/`JSON`); `Retry` terminates the current event block.                                                                                                                           |
| WS handshake             | GET only (405), Connection/Upgrade tokens (400), `Sec-WebSocket-Version: 13` (400 + advisory header), key required (400), origin check (403), hijack required (500).                                                                  |
| WS frames                | Fragmentation reassembled up to `MaxMessageSize` bytes total (default 65536, set via `UpgradeOptions`); unmasked client frames, RSV bits, oversized/fragmented control frames, unknown opcodes all rejected with `ErrWSInvalidFrame`. |
| WS reads                 | `ReadMessage` auto-handles ping/pong and returns `ErrWSClosed` on close; `Listen` does not take `readMu` -- never mix the two.                                                                                                        |
| WS close                 | `Closed()` channel closes exactly once; read errors after a local `Close` do not trigger `OnError`.                                                                                                                                   |
| WS writes                | Serialized by `writeMu`; `WriteBufferSize` is unused; no write-side size cap.                                                                                                                                                         |
