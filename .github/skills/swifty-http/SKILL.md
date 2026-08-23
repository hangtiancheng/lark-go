---
name: swifty-http
description: >
  Lightweight, Koa-inspired HTTP framework for Go, module
  github.com/hangtiancheng/swifty.go/swifty_http, built entirely on the standard
  library with zero external dependencies. Exports Application (New, Default,
  Listen, Shutdown, Use, Router, ServeHTTP, SetFuncMap, LoadHTMLGlob, Static,
  Get/Post/Put/Delete/Patch/Head/Options/All), Context (JSON, String, HTML, Data,
  Redirect, Throw, SetStatus, BindJSON, Query, Param, PostForm, FormFile, Get, Set,
  SSE, Upgrade, plus fields Request/Writer/Path/Method/Status/Body/Type/State/Params),
  Router (Use, Router, Get/Post/Put/Delete/Patch/Head/Options/All, Static), SSEWriter
  (Event, Data, JSON, ID, Retry, Comment, Heartbeat, Stream, Closed, Flush, Done),
  WSConn (Listen, OnMessage, OnClose, OnError, OnPing, OnPong, ReadMessage, ReadJSON,
  WriteMessage, WriteJSON, Send, WriteText, WriteBinary, Ping, Close,
  CloseWithMessage, Closed, Heartbeat, SetReadDeadline, SetWriteDeadline, NetConn),
  UpgradeOptions (ReadBufferSize, WriteBufferSize, MaxMessageSize, CheckOrigin,
  Subprotocols), Logger, Recovery, H, Middleware, TextMessage, BinaryMessage,
  CloseMessage, PingMessage, PongMessage, ErrWSClosed, ErrWSInvalidFrame. Use when
  working on swifty_http code: HTTP routing, trie-based path matching, Koa-style
  middleware chains (func(ctx *Context, next func())), the deferred response pattern
  (ctx.Status / ctx.Body / ctx.Type), the 404-to-200 status promotion, Server-Sent
  Events streaming, zero-dependency RFC 6455 WebSocket servers, router groups with
  prefixes, or koa-static-style file serving. Trigger tokens: swifty_http,
  swifty.New(), swifty.Default(), app.Use(, app.Router(, app.Listen(, app.Shutdown(,
  ctx.SSE(), ctx.Upgrade(, ctx.Throw(, ctx.SetStatus(, ctx.BindJSON(, swifty.H{,
  WSConn, SSEWriter, UpgradeOptions, ErrWSClosed. Do NOT use for plain net/http
  servers without a swifty_http import; gin, echo, fiber, chi, gorilla/mux;
  gorilla/websocket; Node.js Koa; reverse proxies (nginx, envoy, traefik); or gRPC
  services (use the swifty-rpc skill).
---

# swifty_http

A lightweight, Koa-inspired HTTP framework for Go with trie-based routing, onion-model
middleware (`func(ctx *Context, next func())`), deferred response rendering with
immediate 404-to-200 status promotion in the body setters, Server-Sent Events support,
a hand-rolled RFC 6455 WebSocket implementation (event-driven and synchronous APIs,
including fragmented-message reassembly), prefix-normalized router groups, and
koa-static-style file serving without directory listings. The framework has no
external module dependencies at all: everything is built on the Go standard library.

Module path: `github.com/hangtiancheng/swifty.go/swifty_http`

Source root: `swifty_http/` (flat layout, all `.go` files in the module root)

Go directive: `go 1.26.0`

## When to load adjacent skills

- Load `swifty-cache` alongside this skill when the task touches
  `swifty_cache.DashboardHandler()` or `swifty_cache.StartDashboard()`. Those helpers
  are `swifty_http` middleware and consume `ctx.Upgrade`, `UpgradeOptions`, and
  `WSConn` directly. This is the only sibling module that imports `swifty_http`.
- Load `swifty-orm` when handlers query MongoDB and the question is about the query
  builder rather than the HTTP surface.
- Load `swifty-rpc` when the service also exposes a TCP RPC surface. `swifty_rpc` does
  not import `swifty_http`; the two are independent transports.

## Architecture overview

```
Application (top level, implements http.Handler)
  |-- root    *Router           root router, prefix ""
  |-- router  *router           per-method trie: method -> *node, plus handlers map
  |-- routers []*Router         every Router ever created, in creation order
  |-- htmlTemplates *template.Template
  |-- funcMap       template.FuncMap
  |-- server  *http.Server      constructed in New(), so Shutdown never races Listen

Router (route group)
  |-- prefix      string        normalized, e.g. "/v1/v2"
  |-- middlewares []Middleware  appended by Use()
  |-- parent      *Router       set for child groups; never read after construction
  |-- app         *Application  shared back-pointer; registration goes to app.router

router (internal, one per Application)
  |-- roots    map[string]*node        HTTP method -> trie root
  |-- handlers map[string]Middleware   "METHOD-/canonical/pattern" -> handler

node (trie, internal)
  |-- pattern  string   non-empty only on registered leaves
  |-- part     string   one path segment: literal, ":name", or "*name"
  |-- children []*node
  |-- isWild   bool     true for ":" and "*" parts

Context (per request, created by newContext)
  |-- Request, Writer            net/http plumbing
  |-- Path, Method               copied from Request at construction
  |-- Status, Body, Type         deferred response
  |-- State, Params              middleware bag, route params
  |-- headers map[string]string  buffered response headers (unexported)
  |-- app, flushed, statusSet    unexported wiring and flags
  |-- SSE()     -> *SSEWriter    takes over the response
  |-- Upgrade() -> *WSConn       hijacks the connection

Request lifecycle:
  ServeHTTP -> walk app.routers in creation order
            -> for each Router whose prefix matches via matchRouterPath,
               append its middleware slice (collected at request time,
               so Use() after Router creation still takes effect)
            -> newContext(w, req); ctx.app = app
            -> app.router.handle(ctx, middlewares)
                 -> getRoute resolves the trie; on a miss synthesize a notFound
                    handler (Status=404, statusSet=true, plain-text body)
                 -> on a hit set ctx.Params, look up handlers[METHOD-pattern]
                 -> compose(middlewares, handler)(ctx)
                    compose panics "next() called multiple times" on double next()
                 -> ctx.respond()   skipped when ctx.flushed == true
                                    (set by ctx.SSE(), ctx.Upgrade(),
                                     and the Static handler)

WebSocket lifecycle:
  ctx.Upgrade(opts) -> CheckOrigin (403), method must be GET (405),
                       Connection: upgrade (400), Upgrade: websocket (400),
                       Sec-WebSocket-Version: 13 (400 + advisory header),
                       Sec-WebSocket-Key required (400)
                    -> negotiate subprotocol (optional)
                    -> hijack via http.NewResponseController(ctx.Writer).Hijack()
                    -> ctx.flushed = true; ctx.Status = 101 (statusSet)
                    -> write the 101 handshake directly to the net.Conn
                    -> return *WSConn
  WSConn.Listen()      blocking event loop -> OnMessage/OnClose/OnError/OnPing/OnPong
  WSConn.ReadMessage() manual per-message read (alternative to Listen)
  WSConn.Close()       close frame + net.Conn close; Closed() channel closes once
```

Key invariants:

- Handlers and middleware do not write to `ctx.Writer` directly. They set
  `ctx.Status`, `ctx.Body`, `ctx.Type`, and buffer headers with `ctx.Set`. The HTTP
  write happens once in `ctx.respond()` after the chain returns. This enables the
  onion model: code after `next()` sees and may mutate the response. Violating it by
  writing to `ctx.Writer` yourself commits the status line early, and `respond()` then
  produces a duplicate `WriteHeader` warning from `net/http`.
- Body setters (`JSON`, `String`, `Data`, `HTML`) promote the default 404 to 200
  immediately (`promoteStatus`), so middleware after `next()` observes the final
  status, matching Koa's `ctx.body =` setter semantics. `TestPromotedStatusVisibleToMiddlewareAfterNext`
  pins this down.
- Calling `next()` more than once in the same middleware panics with
  `"swifty_http: next() called multiple times"`; under `Default()` the Recovery
  middleware converts this into a 500 response.
- `ctx.flushed = true` tells `respond()` to skip rendering. `SSE()`, `Upgrade()`,
  and the Static handler set it because they own the response bytes. Anything you
  assign to `ctx.Body` afterwards is silently discarded.
- `Application` and `Context` must be produced by `New`/`Default` and by the framework
  respectively. Their zero values are not usable; see Zero-value behaviour below.

## Core types

### Application

```go
type Application struct {
    // all fields unexported
}

func New() *Application
func Default() *Application   // New() + Use(Logger(), Recovery())
```

| Method                                                       | Notes                                                                                    |
| ------------------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| `Listen(addr string) error`                                  | Sets `server.Addr = addr`, calls `ListenAndServe`; returns `http.ErrServerClosed` after a successful `Shutdown` |
| `Shutdown(ctx context.Context) error`                        | Delegates to `http.Server.Shutdown`; returns `nil` when the internal server is nil        |
| `SetFuncMap(funcMap template.FuncMap)`                       | Stores the func map for the next `LoadHTMLGlob`; does not re-parse anything                |
| `LoadHTMLGlob(pattern string)`                               | `template.Must(template.New("").Funcs(app.funcMap).ParseGlob(pattern))`; panics on parse errors |
| `ServeHTTP(w http.ResponseWriter, req *http.Request)`         | `http.Handler` implementation; collects middleware, builds the Context, dispatches         |
| `Use(middlewares ...Middleware)`                             | Delegates to the root Router                                                              |
| `Router(prefix string) *Router`                              | Creates a child of the root Router                                                        |
| `Get(pattern string, handler Middleware)`                    | Delegates to the root Router                                                              |
| `Post(pattern string, handler Middleware)`                   | Delegates to the root Router                                                              |
| `Put(pattern string, handler Middleware)`                    | Delegates to the root Router                                                              |
| `Delete(pattern string, handler Middleware)`                 | Delegates to the root Router                                                              |
| `Patch(pattern string, handler Middleware)`                  | Delegates to the root Router                                                              |
| `Head(pattern string, handler Middleware)`                   | Delegates to the root Router                                                              |
| `Options(pattern string, handler Middleware)`                | Delegates to the root Router                                                              |
| `All(pattern string, handler Middleware)`                    | Registers GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS in that order                       |
| `Static(relativePath string, root string)`                   | Delegates to the root Router                                                              |

Behavioural notes:

- `New()` constructs the internal `http.Server` immediately (not in `Listen`), so a
  concurrent `Shutdown` never races the server field assignment or silently no-ops
  before `Listen` runs. The idiom `go app.Listen(addr)` plus `app.Shutdown(ctx)` from
  the main goroutine is safe.
- `New()` seeds `app.routers` with the root Router, so the root's middleware is always
  first in the collected chain.
- `Listen` returns whatever `ListenAndServe` returns, including bind errors;
  `TestListenReturnsListenError` asserts `New().Listen("bad address")` errors.
- Call `SetFuncMap` before `LoadHTMLGlob`, otherwise the func map is not applied to
  the already-parsed templates.
- The internal `http.Server` is not exposed. `ReadTimeout`, `WriteTimeout`,
  `MaxHeaderBytes`, and TLS cannot be configured through this API. To customize them,
  build your own `http.Server{Handler: app}` and call `ListenAndServe` yourself; note
  that `app.Shutdown` then shuts down the unused internal server, not yours.

### Context

Per-request state carrying the request, response writer, deferred response fields,
and route params.

```go
type H map[string]any             // convenience alias for inline JSON literals
type Middleware func(ctx *Context, next func())

type Context struct {
    Request *http.Request
    Writer  http.ResponseWriter

    // request info (Koa-style fields)
    Path   string
    Method string

    // deferred response (Koa-style)
    Status int
    Body   any
    Type   string

    // middleware data sharing
    State  map[string]any
    Params map[string]string

    // unexported: headers, app, flushed, statusSet
}
```

Field-level semantics:

- `Request` and `Writer` are the raw `net/http` values. `Path` and `Method` are copied
  from `req.URL.Path` and `req.Method` at construction; mutating them later does not
  re-route the request (routing has already happened).
- `Status` defaults to `http.StatusNotFound`. Body setters call `promoteStatus`,
  which upgrades 404 to 200 unless the status was explicitly recorded (internal
  `statusSet` flag, set by `SetStatus`, `Throw`, `Redirect`, the notFound handler,
  `SSE`, `Upgrade`, the Static miss path, and `Recovery`). `respond()` applies the same
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
  renderer by type switch. `nil` means no body, and only the status line is written.
- `State` is initialized to an empty map by `newContext`. It is a plain map with no
  locking. Synchronize access yourself if you fan out goroutines from a handler.
- `Params` is nil until `router.handle` assigns it, and stays nil on a 404. It is
  populated before the chain starts; treat it as read-only. `Param` reads a nil map
  safely and returns `""`.

Status and error control:

```go
func (ctx *Context) SetStatus(status int)
func (ctx *Context) Throw(status int, msg string)
```

`SetStatus` records an explicitly chosen status, like Koa's `ctx.status` setter; a
status set this way is never overridden by the automatic 404-to-200 promotion.

`Throw` sets the status (marking it explicit) and assigns
`ctx.Body = H{"message": msg, "data": nil}`. The `data` key is included so error
responses share the unified `{message, data}` envelope used on success paths. `Throw`
does not stop execution: it only stages the response. Return from your middleware or
handler immediately after calling it, and do not call `next()`.

Request accessors:

| Method                                                                | Description                                                     |
| --------------------------------------------------------------------- | --------------------------------------------------------------- |
| `Query(key string) string`                                            | `ctx.Request.URL.Query().Get(key)`                              |
| `Param(key string) string`                                            | Route path parameter (`:name` or `*name`); `""` when absent      |
| `PostForm(key string) string`                                         | `ctx.Request.FormValue(key)` (urlencoded or multipart)          |
| `Get(header string) string`                                           | `ctx.Request.Header.Get(header)`                                |
| `BindJSON(out any) error`                                             | Decode the JSON request body; closes `Request.Body` via `defer` |
| `FormFile(key string) (multipart.File, *multipart.FileHeader, error)` | `ctx.Request.FormFile(key)`                                     |

`BindJSON` is destructive: it consumes and closes the request body. Call it at most
once per request. There is no raw-body accessor and no size limit; read
`ctx.Request.Body` yourself (wrapped in `http.MaxBytesReader` if you need a cap)
before any `BindJSON` call if you need the bytes. The returned error is the raw
`encoding/json` error, not wrapped.

Response setters (deferred; written by `respond()` at the end of the chain):

| Method                                 | Description                                                                                                                                                                                                                     |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `JSON(obj any)`                        | `Type = "application/json"`, `Body = obj`, promote status                                                                                                                                                                       |
| `String(format string, values ...any)` | `Type = "text/plain"`, `Body = fmt.Sprintf(format, values...)`, promote status. Passing a literal with `%` and no values yields a `%!` verb artifact; use `String("%s", s)` for untrusted strings                                |
| `Data(data []byte)`                    | `Body = data`, promote status. Does not touch `Type`, so with an empty `Type` no Content-Type header is set and `net/http` sniffs one from the bytes                                                                             |
| `HTML(name string, data any)`          | `Type = "text/html"`, `Body = htmlPayload{name, data}`, promote status; requires `LoadHTMLGlob`                                                                                                                                  |
| `Redirect(url string)`                 | Buffers the `Location` header; keeps an already-set 3xx status (300, 301, 302, 303, 305, 307, 308), otherwise forces 302; marks the status explicit; if `Body` is nil, sets `Type = "text/plain"` and body `"Redirecting to <url>"` |
| `Set(header string, value string)`     | Buffers a response header under its canonical key (`http.CanonicalHeaderKey`); flushed by `respond()`, by `SSE()`, and by the Static handler                                                                                     |

Note that `Redirect` inspects `ctx.Status`, not `statusSet`. Assigning
`ctx.Status = http.StatusMovedPermanently` directly before `Redirect` is therefore
enough to keep 301, unlike the promotion path which requires `SetStatus`.

Streaming and protocol upgrade:

```go
func (ctx *Context) SSE() *SSEWriter
func (ctx *Context) Upgrade(opts *UpgradeOptions) (*WSConn, error)
```

Both take over the response and set `ctx.flushed = true`. See the dedicated sections
below.

`respond()` behaviour, in order:

1. Return immediately when `ctx.flushed` is true.
2. Fallback 404-to-200 promotion when `Body != nil`, `statusSet == false`, and
   `Status == 404`.
3. For empty statuses (204, 205, 304): clear `Body` and delete buffered
   `Content-Type`, `Content-Length`, and `Transfer-Encoding` headers.
4. Flush buffered headers to `ctx.Writer.Header()` with `Set`.
5. If `Body == nil`, write the status header only and return.
6. Otherwise dispatch on the runtime type of `Body`:
   - `htmlPayload` -> `respondHTML`: a nil `ctx.app` or a missing template set yields
     500 with `{"message":"HTML templates are not loaded"}`; otherwise set
     `text/html`, write the status, execute the named template, and log execution
     errors (the status line has already been sent, so the response is truncated).
   - `[]byte` -> `respondBytes`: `setContentType(ctx.Type)`, write status, write bytes.
   - `string` -> `respondString`: default `Type` to `text/plain`, write status, write.
   - `io.Reader` -> `respondReader`: `setContentType(ctx.Type)`, write status,
     `io.Copy`.
   - anything else -> `respondJSON`: `json.Marshal` first; on marshal failure log,
     force `application/json`, and respond 500 with
     `{"message":"Internal Server Error"}` (the status line is not yet committed, so
     the downgrade is clean); on success default `Type` to `application/json`, write
     the status, write the bytes.

The type switch checks `[]byte` and `string` before `io.Reader`, so a `[]byte` is
never treated as a reader. The switch matches exact types, so a named byte-slice type
falls through to `respondJSON` instead of `respondBytes`: a plain `type Raw []byte`
is then marshalled to a base64 JSON string, while `json.RawMessage` is emitted
verbatim because it implements `json.Marshaler`. Convert to `[]byte` explicitly with
`ctx.Data([]byte(raw))` when you want the bytes written as-is.

### Router

Route grouping with a normalized path prefix and its own middleware stack.

```go
type Router struct {
    // all fields unexported
}

func (app *Application) Router(prefix string) *Router
func (r *Router) Router(prefix string) *Router
```

| Method                                       | Notes                                                                     |
| -------------------------------------------- | ------------------------------------------------------------------------- |
| `Router(prefix string) *Router`              | Child group; prefix is `r.prefix + normalizePrefix(prefix)`; registers the child in `app.routers` |
| `Use(middlewares ...Middleware)`             | Appends to this group's middleware slice                                   |
| `Get(pattern string, handler Middleware)`    | Registers `r.prefix + pattern` for GET                                     |
| `Post(pattern string, handler Middleware)`   | Registers for POST                                                         |
| `Put(pattern string, handler Middleware)`    | Registers for PUT                                                          |
| `Delete(pattern string, handler Middleware)` | Registers for DELETE                                                       |
| `Patch(pattern string, handler Middleware)`  | Registers for PATCH                                                        |
| `Head(pattern string, handler Middleware)`   | Registers for HEAD                                                         |
| `Options(pattern string, handler Middleware)`| Registers for OPTIONS                                                      |
| `All(pattern string, handler Middleware)`    | Registers GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS in that order        |
| `Static(relativePath string, root string)`   | Mounts a GET route at `path.Join(relativePath, "/*filepath")` over `http.Dir(root)` |

Grouping rules:

- `normalizePrefix` forces a leading `/`, strips trailing `/` characters, and maps
  `"/"` to `""`. `app.Router("v1")` and `app.Router("/v1/")` both produce prefix
  `/v1`, keeping trie registration and middleware matching consistent.
  `TestRouterPrefixNormalized` covers the missing-leading-slash case;
  `TestNestedRouter` covers `/v1` -> `/v1/v2` -> `/v1/v2/v3`.
- Middleware is collected at request time: `ServeHTTP` walks all routers whose
  prefix matches the request path and concatenates their middleware in Router
  creation order. Calling `Use` on a parent after creating children still applies
  to matching requests. Execution order therefore follows creation order, not
  nesting depth. In the common case where you create parents before children this
  matches intuition (`TestMiddlewareOrder` asserts
  `root-before, api-before, handler, api-after, root-after`), but two sibling groups
  whose prefixes both match a path run in creation order regardless of nesting.
- Prefix matching is path-segment aware (`matchRouterPath`): prefix `/v1` matches
  `/v1` and `/v1/...` but never `/v10`. Empty prefix and `/` match everything.
  `TestRouterMiddlewareUsesPathBoundary` and `TestMatchRouterPath` cover this.
- `Router.parent` is recorded but never read after construction; middleware
  resolution goes through `app.routers` and prefix matching instead of walking the
  parent chain.
- Each route registration logs `Route <METHOD> - <pattern>` via the standard `log`
  package, with the method right-padded to four characters. There is no way to
  suppress this output other than reconfiguring the default logger.

Static file serving:

`Static(relativePath, root)` mounts a GET route at
`<r.prefix><relativePath>/*filepath` backed by
`http.StripPrefix(path.Join(r.prefix, relativePath), http.FileServer(http.Dir(root)))`.
The handler:

1. Reads `ctx.Param("filepath")` and probes it with `staticFileExists`, which opens
   the target and closes the handle immediately. Missing files yield `Status = 404`
   (marked explicit) with an empty body, and the handler returns without flushing, so
   `respond()` renders the bare 404. `TestStaticProbeClosesFileHandles` asserts the
   probe leaks no descriptors.
2. Serves directories only when they contain an `index.html`; bare directory listings
   are never exposed (koa-static behaviour, `TestStaticDirectoryListingDisabled`).
3. Flushes headers buffered via `ctx.Set` (for example CORS headers set by upstream
   middleware) onto the underlying writer before delegating, because
   `http.FileServer` writes the response itself.
4. Sets `ctx.flushed = true` and serves through a `statusRecorder` wrapper that
   mirrors the status the file server writes back into `ctx.Status`, so Logger
   reports the real code. `statusRecorder` records on the first `WriteHeader`, or 200
   on the first `Write` without an explicit `WriteHeader`.

## The middleware onion and next() semantics

```go
type Middleware func(ctx *Context, next func())

func compose(middlewares []Middleware, final Middleware) func(ctx *Context)
```

`compose` builds a koa-compose-style dispatcher over a captured `index`:

- `dispatch(i)` panics with `"swifty_http: next() called multiple times"` when
  `i <= index`, which is exactly the case where a middleware calls `next()` twice.
- When `i >= len(middlewares)` the `final` handler runs with a no-op `next`, and only
  if `final != nil`.
- Otherwise `middlewares[i](ctx, func() { dispatch(i + 1) })` runs.

Consequences:

- Code before `next()` runs on the way in; code after `next()` runs on the way out,
  in reverse order. This is what makes response-mutating middleware possible.
- Not calling `next()` short-circuits the chain. That is the idiomatic way to reject a
  request: stage a response with `Throw` and return.
- Calling `next()` twice panics. `TestNextCalledTwiceBecomes500` confirms that under
  `Default()` the panic becomes a 500; without `Recovery` it propagates to
  `net/http`, which logs it and closes the connection.
- The handler always receives a non-nil `next` that does nothing, so a handler may
  call `next()` harmlessly (once).
- `compose` is called per request in `router.handle`, so the closure state is not
  shared across requests.

## Routing and the trie

The internal `router` maintains one prefix trie per HTTP method (`roots`) plus a
`method + "-" + pattern -> Middleware` handler map (`handlers`). Lookup is
O(segments) with backtracking across sibling children.

Path parameter syntax:

- `:name` matches a single path segment; read it with `ctx.Param("name")`.
- `*name` matches the remainder of the path; the captured value joins the remaining
  segments with `/`. A bare `*` (no name) matches but captures nothing, because
  `getRoute` only records the parameter when `len(part) > 1`.

Registration (`addRoute`):

1. `parsePattern` splits on `/`, drops empty segments, and stops after the first
   segment starting with `*`. `TestParsePattern` pins the results:
   `"/"` -> `[]`, `"/p/:name"` -> `["p", ":name"]`, `"/p/*"` -> `["p", "*"]`,
   `"/p/*name/*"` -> `["p", "*name"]`, `"//p//:name//"` -> `["p", ":name"]`.
2. The pattern is rebuilt as `"/" + strings.Join(parts, "/")`, so `/users`,
   `/users/`, and `//users` share one canonical pattern and one handler key. The last
   registration wins and no earlier handler becomes silently unreachable behind an
   overwritten trie leaf.
3. `roots[method]` is created on demand and `insert` walks or creates one child per
   segment, marking `isWild` for `:` and `*` parts. The leaf stores the canonical
   pattern.
4. `handlers[method+"-"+pattern] = handler`, overwriting any previous handler for the
   same method and canonical pattern without warning.

Matching (`getRoute` and `node.search`):

- `matchChildren(part)` returns exact-part children first, then wild children, so
  literal segments win over `:name` and `*name` at the same trie level.
  `TestStaticRouteTakesPriorityOverWildcard` asserts `/files/exact` beats
  `/files/*filepath`.
- `search` recurses and backtracks: if a child subtree yields no registered leaf, the
  next candidate child is tried. A node with an empty `pattern` is not a match, so
  intermediate nodes never terminate a search.
- A node whose `part` starts with `*` terminates the search immediately and consumes
  the remaining segments.
- Parameter extraction re-parses the matched leaf's pattern and pairs `:name`
  segments positionally with the request segments, then stops at the first `*name`
  and joins the tail. `TestGetRoute` covers `/hello/swifty` -> `name=swifty` and
  `/assets/css/test.css` -> `filepath=css/test.css`.
- Matching is per HTTP method. A wrong method returns no match, exactly like a wrong
  path.

Dispatch (`router.handle`):

- On a hit: set `ctx.Params`, look up the handler by `method + "-" + n.pattern`, and
  run `compose(middlewares, handler)(ctx)`.
- On a miss: run `compose(middlewares, notFound)(ctx)` where `notFound` sets
  `Status = 404`, `statusSet = true`, and `Body = "404 NOT FOUND: <path>\n"`. The full
  middleware chain still runs, so Logger and Recovery see 404s.
- `ctx.respond()` runs unconditionally afterwards (and returns early when flushed).
- A wildcard route does not match its own prefix. `Static("/assets", dir)` registers
  `/assets/*filepath`, whose trie leaf sits one level below the `assets` node, so
  `/assets` and `/assets/` both 404 while `/assets/index.html` resolves.

There is no 405 handling and no `Allow` header: a path registered only for GET answers
POST with the 404 body.

## SSEWriter

Server-Sent Events support, obtained via `ctx.SSE()`.

```go
type SSEWriter struct {
    // all fields unexported: ctx, flusher, mu
}

func (ctx *Context) SSE() *SSEWriter
```

`SSE()` side effects, in order:

1. Type-asserts `ctx.Writer` to `http.Flusher`, ignoring failure. When the assertion
   fails the flusher is nil and `Flush` becomes a no-op; writes still succeed but may
   buffer. `httptest.ResponseRecorder` does implement `http.Flusher`, so the SSE tests
   exercise the flushing path.
2. Sets `ctx.flushed = true` and records `ctx.Status = 200` as explicit, so Logger
   reports 200 for SSE requests.
3. Sets `Content-Type: text/event-stream`, `Cache-Control: no-cache`, and
   `Connection: keep-alive` directly on `ctx.Writer.Header()`.
4. Flushes headers previously buffered via `ctx.Set` on top of those three. A
   `ctx.Set("Content-Type", ...)` therefore overrides `text/event-stream`.
5. Writes the 200 status line and returns the `SSEWriter`.

Call `ctx.SSE()` after any `ctx.Set` calls you want flushed, call it at most once per
request, and do not mix it with the deferred response setters afterwards.
`TestSSEWithCustomHeaders` covers the buffered-header flush.

| Method                                     | Wire output and behaviour                                                                                                        |
| ------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------- |
| `Event(event string, data string)`         | `event: <event>\n`, then one `data:` line per input line, then a blank line                                                       |
| `Data(data string)`                        | One `data:` line per input line, then a blank line                                                                               |
| `JSON(event string, obj any)`              | `json.Marshal`, then optional `event:` line (omitted when `event == ""`), then a single `data: <json>\n\n`; marshal errors return silently without writing |
| `ID(id string)`                            | `id: <id>\n` only, a field line rather than a complete event                                                                      |
| `Retry(ms int)`                            | `retry: <ms>\n\n`                                                                                                                |
| `Comment(text string)`                     | One `: <line>\n` per input line, then a blank line                                                                               |
| `Heartbeat(interval time.Duration) func()` | Spawns a goroutine writing `: keepalive\n\n` every interval; returns an idempotent stop function                                  |
| `Stream(ch <-chan string)`                 | Forwards channel values via `Data` until `ch` closes or the request context is done                                              |
| `Closed() <-chan struct{}`                 | Returns `ctx.Request.Context().Done()` for disconnect detection                                                                  |
| `Flush()`                                  | Invokes the underlying `http.Flusher` if present                                                                                 |
| `Done()`                                   | `Data("[DONE]")`, producing the OpenAI-compatible `data: [DONE]\n\n` sentinel                                                     |

`TestSSEWriter`, `TestSSEMultilineData`, `TestSSEJSON`, `TestSSEComment`,
`TestSSEStream`, and `TestSSEClosed` pin the exact byte sequences above.

Concurrency and lifetime contracts:

- Every write method takes the internal `sync.Mutex`, so concurrent producers (for
  example your handler plus the heartbeat goroutine) are safe.
  `TestSSEHeartbeatConcurrency` exercises this under `-race`.
- `Flush()` does not take the mutex. It is safe against the flusher itself but does
  not order against an in-progress `Data`; prefer the write methods, which already
  flush.
- The stop function returned by `Heartbeat` is idempotent (`sync.Once`) and blocks
  until the heartbeat goroutine has exited. The goroutine also exits on its own when
  the request context is canceled. Skipping the stop call leaks the goroutine only
  until the request context is canceled, which for a normal `net/http` server happens
  when the handler returns.
- `ID` writes a standalone field line; SSE clients dispatch an event only after a
  `data:` field followed by a blank line. Call `ID` immediately before the `Event`,
  `Data`, or `JSON` call it should be bundled with.
- `Retry` ends with a blank line, which terminates any partially written event
  block; emit it between complete events.
- `Stream` returns when the channel closes or the client disconnects. It does not
  drain or cancel the upstream producer; tie the producer to `ctx.Request.Context()`
  to avoid leaks.
- `Event` does not reject an empty event name and will write `event: \n`; use `Data`
  or `JSON("", obj)` for unnamed events.
- The writer holds a `*Context`. Do not use an `SSEWriter` after the handler returns:
  `net/http` invalidates the `ResponseWriter` at that point.

## WebSocket

A hand-rolled RFC 6455 server-side implementation with both an event-driven
(Node.js `ws`-style) API and a synchronous read/write API. No external module is
involved.

Constants (untyped integer constants matching the RFC opcodes):

```go
const (
    TextMessage   = 1
    BinaryMessage = 2
    CloseMessage  = 8
    PingMessage   = 9
    PongMessage   = 10
)
```

Sentinel errors:

```go
var (
    ErrWSClosed       = errors.New("websocket: connection closed")
    ErrWSInvalidFrame = errors.New("websocket: invalid frame")
)
```

Both are returned bare, never wrapped, so compare with `==` or `errors.Is`
interchangeably. `Upgrade` does not return either sentinel: it returns freshly
constructed `errors.New` values with no wrapping, so the only way to classify an
upgrade failure programmatically is the staged `ctx.Status`.

### UpgradeOptions

```go
type UpgradeOptions struct {
    ReadBufferSize  int
    WriteBufferSize int
    // MaxMessageSize caps a single frame and the reassembled fragmented
    // message total on the read side. Zero means the default (65536 bytes).
    MaxMessageSize int
    CheckOrigin    func(r *http.Request) bool
    Subprotocols   []string
}
```

| Field             | Default when zero or when `opts == nil`                       | Effect                                                                     |
| ----------------- | ------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `ReadBufferSize`  | 4096                                                          | Minimum size of the `bufio.Reader` used for frame reads                    |
| `WriteBufferSize` | ignored entirely                                              | Declared but never read; writes go unbuffered straight to the `net.Conn`   |
| `MaxMessageSize`  | 65536 (`maxFrameSize`)                                        | Read-side cap on a single frame and on the reassembled fragmented total    |
| `CheckOrigin`     | nil, meaning every origin is accepted                         | Return false to reject with 403                                            |
| `Subprotocols`    | nil, meaning no `Sec-WebSocket-Protocol` response header      | Server preference order for negotiation                                    |

Passing `nil` options is valid and is what the tests do. Note the security
implication of the `CheckOrigin` default: unlike gorilla/websocket, which enforces a
same-origin policy by default, this implementation accepts any `Origin` unless you
supply a `CheckOrigin` function. Always set one for browser-facing endpoints.

### Upgrade

```go
func (ctx *Context) Upgrade(opts *UpgradeOptions) (*WSConn, error)
```

Steps, in order:

1. `CheckOrigin` (when `opts != nil` and the func is non-nil): failure throws 403
   `"origin not allowed"` and returns an error.
2. Method must be GET: otherwise throws 405.
3. `Connection` must contain the `upgrade` token: otherwise 400. `headerContains`
   splits comma lists and compares case-insensitively after trimming, so
   `Connection: keep-alive, Upgrade` passes (`TestHeaderContains`).
4. `Upgrade` must contain `websocket`: otherwise 400.
5. `Sec-WebSocket-Version` must contain `13`: otherwise 400 plus an advisory
   `Sec-WebSocket-Version: 13` response header buffered via `ctx.Set`
   (gorilla/websocket behaviour, asserted by `TestWebSocketUpgradeRequiresVersion13`).
6. `Sec-WebSocket-Key` must be present: otherwise 400.
7. Optional subprotocol negotiation: `negotiateSubprotocol` scans the server list in
   order and returns the first entry the client also offered, comparing trimmed
   values exactly (case-sensitive). No match yields no response header.
8. Hijack via `http.NewResponseController(ctx.Writer).Hijack()`; failure (for example
   under `httptest.ResponseRecorder`) throws 500 and returns an error.
9. Sets `ctx.flushed = true` and records `ctx.Status = 101` as explicit, so Logger
   reports 101 for real upgrades only.
10. Clears the connection deadline with `conn.SetDeadline(time.Time{})`. Any
    `ReadTimeout`/`WriteTimeout` the server applied no longer bounds this connection;
    use `SetReadDeadline`/`SetWriteDeadline` if you need one.
11. Reuses the hijacked `bufio.Reader` when it already holds buffered bytes or when
    its size is at least `ReadBufferSize`; otherwise allocates
    `bufio.NewReaderSize(conn, readBufSize)`. Reusing it when bytes are buffered is
    mandatory: those bytes are already-read client frames.
12. Computes `Sec-WebSocket-Accept` as
    `base64(sha1(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))`
    (`TestComputeAcceptKey`) and writes the `101 Switching Protocols` response, plus
    `Sec-WebSocket-Protocol` when negotiated, directly to the connection. A write
    failure closes the connection and returns an error without staging any HTTP
    response, since the connection is already hijacked.

On every validation failure `Upgrade` returns a non-nil error and leaves a deferred
error response on the context (via `Throw`), which `respond()` renders normally.
Just `return` from the handler after an `Upgrade` error; do not write anything else.

### WSConn

```go
type WSConn struct {
    // all fields unexported: conn, br, writeMu, readMu, closed, once,
    // maxMessageSize, onMessage, onClose, onError, onPing, onPong
}
```

Event-driven API:

```go
func (ws *WSConn) OnMessage(fn func(messageType int, data []byte))
func (ws *WSConn) OnClose(fn func(code int, text string))
func (ws *WSConn) OnError(fn func(err error))
func (ws *WSConn) OnPing(fn func(data []byte))
func (ws *WSConn) OnPong(fn func(data []byte))
func (ws *WSConn) Listen()
```

`Listen` runs a blocking read loop over reassembled messages:

- Ping frames invoke `OnPing` (if registered) and always receive an automatic pong.
- Pong frames invoke `OnPong` (if registered).
- Close frames are parsed into `(code, text)`, answered with a close frame, close the
  `Closed()` channel exactly once, invoke `OnClose`, and return. `Listen` does not
  close the underlying `net.Conn`; call `Close` (typically deferred) to release it.
- Text and binary frames invoke `OnMessage` with the reassembled payload.
- Any read error goes to `handleError`, which invokes `OnError` unless the connection
  was already closed (suppressing the trailing "use of closed network connection"),
  closes the `Closed()` channel, and returns.

Register handlers before calling `Listen`. The `On*` setters are plain field
assignments with no synchronization, so registering them from another goroutine while
`Listen` is running is a data race.

Synchronous API:

```go
func (ws *WSConn) ReadMessage() (messageType int, data []byte, err error)
func (ws *WSConn) ReadJSON(v any) error
func (ws *WSConn) WriteMessage(messageType int, data []byte) error
func (ws *WSConn) WriteJSON(v any) error
func (ws *WSConn) Send(text string) error        // WriteMessage(TextMessage, []byte(text))
func (ws *WSConn) WriteText(text string) error   // WriteMessage(TextMessage, []byte(text))
func (ws *WSConn) WriteBinary(data []byte) error // WriteMessage(BinaryMessage, data)
func (ws *WSConn) Ping() error
func (ws *WSConn) Close() error
func (ws *WSConn) CloseWithMessage(code int, text string) error
```

- `ReadMessage` holds `readMu` for the whole call, transparently answers pings with
  pongs, skips pongs, and returns only `TextMessage` or `BinaryMessage` payloads. On a
  close frame it replies with a close frame, closes the `Closed()` channel, invokes
  `OnClose` if registered, and returns `ErrWSClosed`. On a read error it calls
  `handleError` (so `OnError` may fire and `Closed()` closes) and returns the raw
  error.
- `ReadJSON` calls `ReadMessage` and `json.Unmarshal`s the payload, ignoring the
  message type. A binary frame containing JSON is accepted.
- `Send` and `WriteText` are identical; both exist for API familiarity.
- `Ping` writes a ping frame with a nil payload under `writeMu`.
- `Close` closes the `Closed()` channel once, writes a close frame with code 1000 and
  no reason, then closes the `net.Conn` and returns its error. `CloseWithMessage`
  does the same with the supplied code and text.

Utility methods:

```go
func (ws *WSConn) Closed() <-chan struct{}
func (ws *WSConn) SetReadDeadline(t time.Time) error
func (ws *WSConn) SetWriteDeadline(t time.Time) error
func (ws *WSConn) Heartbeat(interval time.Duration) func()  // sends Ping frames
func (ws *WSConn) NetConn() net.Conn
```

`NetConn` exposes the hijacked connection for `LocalAddr`, `RemoteAddr`, or raw
access. Reading from or writing to it directly corrupts the frame stream.

### Frame handling and protocol conformance

Reading path (`readFrame` then `readMessage`):

- Fragmented messages are reassembled: a non-FIN text/binary frame starts a buffer,
  continuation frames (opcode 0) append, and the FIN continuation returns the whole
  message. `TestWebSocketFragmentedMessage` sends `"hello "`, `"fragmented "`,
  `"world"` and expects `"hello fragmented world"`.
- A continuation without a preceding data frame, a new data frame while a fragmented
  message is in progress, or a reassembled message exceeding the limit all return
  `ErrWSInvalidFrame`.
- Control frames arriving between fragments are returned to the caller immediately, as
  RFC 6455 5.4 permits. The reassembly buffer is a local variable of `readMessage`, so
  it is discarded when that happens: subsequent continuation frames then hit the
  "continuation without a preceding data frame" branch and fail with
  `ErrWSInvalidFrame`. Fragmented messages interleaved with control frames are
  therefore not supported despite the code comment implying otherwise.
- The message size limit bounds both a single frame (checked against the declared
  length before allocation) and the reassembled total. It defaults to 65536 bytes
  (`maxFrameSize`) and is raised per connection via `UpgradeOptions.MaxMessageSize`.
  The limit applies to reads only; `writeFrame` emits frames of any size, using the
  2-byte, 4-byte, or 10-byte header form as needed.
- Client frames must be masked (RFC 6455 5.1); unmasked frames are rejected with
  `ErrWSInvalidFrame` (`TestWebSocketRejectsUnmaskedClientFrame`). Server frames are
  written unmasked, which is correct for the server role.
- RSV1-3 bits must be zero (no extensions are negotiated, so no compression);
  control frames (close/ping/pong) must have FIN set and a payload of at most 125
  bytes; unknown opcodes are rejected.
- Close payloads are parsed by `parseClosePayload`: fewer than 2 bytes yields
  `(1000, "")`; otherwise a big-endian code plus the remaining bytes as text, replaced
  by `""` when the text is not valid UTF-8 (`TestParseClosePayload`).
- Text frame payloads are not UTF-8 validated. RFC 6455 requires closing with 1007 on
  invalid UTF-8 in a text message; this implementation delivers the raw bytes.

### Concurrency and lifetime contracts

- Writes (`WriteMessage`, `WriteJSON`, `Send`, `WriteText`, `WriteBinary`, `Ping`,
  `Close`, `CloseWithMessage`, and the internal `writeCloseFrame`) are serialized by
  `writeMu`, so concurrent writers are safe.
- `ReadMessage` and `ReadJSON` are serialized by `readMu`. `Listen` does not acquire
  `readMu`, so mixing `Listen` with `ReadMessage` on the same connection is a data
  race on the `bufio.Reader`. Pick one read pattern per connection.
- The automatic pong that `Listen` sends uses `writeFrame` without taking `writeMu`,
  so a concurrent application write can interleave with that pong and corrupt the
  frame stream. `ReadMessage` has the same shape. Avoid writing from another goroutine
  while a read loop is answering pings, or answer pings yourself via `OnPing` and a
  `WriteMessage(PongMessage, ...)` call that does take the lock.
- `Close` and `CloseWithMessage` close the `Closed()` channel exactly once
  (`sync.Once`). Calling `Close` twice does not panic, but the second call returns the
  `net.Conn` double-close error.
- `Closed()` also closes when the peer sends a close frame or when a read error is
  observed, so it is a general "this connection is finished" signal, not only a
  local-close signal. `TestWebSocketClosedChannel` asserts it stays open until the
  client disconnects.
- `Heartbeat` starts one goroutine and returns an idempotent stop function
  (`sync.Once`) that blocks until the goroutine exits. The goroutine exits on ping
  failure, when `Closed()` closes, or when the stop function is called. Skipping the
  stop call leaks the goroutine until the connection closes.
- Nothing closes the hijacked `net.Conn` for you. `net/http` will not do it after the
  handler returns because the connection was hijacked. Always `defer ws.Close()`;
  omitting it leaks a file descriptor per connection for the lifetime of the process.

## Built-in middleware

### Logger

```go
func Logger() Middleware
```

Records `time.Now()` before `next()`, then logs
`[<status>] <RequestURI> in <duration>` through the standard `log` package. Because
body setters promote the status immediately, and because `SSE()` records 200,
`Upgrade()` records 101, and the Static handler mirrors the file server's real status
via `statusRecorder`, the logged status matches what the client received. Output is
plain text with no request id, method, or size; replace this middleware entirely if
you need structured logging. Note that it reads `ctx.Request.RequestURI`, which is
empty on a client-side `http.Request` but populated for server requests and by
`httptest.NewRequest`.

### Recovery

```go
func Recovery() Middleware
```

Wraps `next()` in `defer`/`recover`. Behaviour on panic:

- `http.ErrAbortHandler` is re-raised so `net/http` can abort the request silently
  (required for `httputil.ReverseProxy` client-disconnect handling).
- Any other panic value is formatted with `%v`, logged together with a traceback
  built from `runtime.Callers(3, ...)` and `runtime.CallersFrames`, and converted into
  a deferred 500 response: `Status = 500` (explicit), `Type` cleared, the buffered
  header map replaced with a fresh empty map, and
  `Body = H{"message": "Internal Server Error"}`. Clearing `Type` and the headers
  prevents a pre-panic `ctx.Type = "text/html"` or stray `ctx.Set` calls from
  polluting the error response. `TestDefaultRecovery` asserts the 500 and body.

The 500 body is fixed. To deliver a custom error envelope, replace `Recovery` with
your own middleware; layering another middleware on top cannot change the body it
assigns, only the headers and status after the fact.

Recovery only protects panics raised inside `next()` on the request goroutine. A
panic in a goroutine you spawn from a handler crashes the process; recover there
yourself. It also cannot repair a response that has already been written, so a panic
after `ctx.SSE()` or `ctx.Upgrade()` produces a staged 500 that `respond()` discards
because `ctx.flushed` is true.

## Internal implementation details affecting correctness

### Status promotion and statusSet

`promoteStatus` runs inside `JSON`, `String`, `Data`, and `HTML`: if the status was
never explicitly recorded and is still the default 404, it becomes 200 immediately.
`respond()` repeats the check as a fallback for direct `ctx.Body` assignments. The
`statusSet` flag is private; the only public ways to set it are `SetStatus`, `Throw`,
and `Redirect`, plus framework paths (notFound, Static miss, `SSE`, `Upgrade`,
`Recovery`).

### Header buffering

`ctx.Set` canonicalizes keys with `http.CanonicalHeaderKey` and stores them in a
`map[string]string`. Consequences: repeated `Set` on the same header overwrites (no
multi-value support, so multiple `Set-Cookie` values are impossible through this API,
use `ctx.Writer.Header().Add` directly), there is no header removal API, and iteration
order is random (harmless, since each key is written with `Set`). Buffered headers are
flushed by `respond()`, by `SSE()`, and by the Static handler before delegation.

### Content-Type precedence

`setContentType` returns early on an empty value and otherwise applies the inferred
`ctx.Type` only when the response writer does not already carry a Content-Type. A user
`ctx.Set("content-type", ...)` in any case therefore always wins over the type implied
by `String`/`JSON`/`HTML` (`TestUserContentTypeHeaderWins`). `respondJSON`'s
marshal-failure path bypasses `setContentType` and forces `application/json`.

### Empty statuses

For 204, 205, and 304, `respond()` drops the body and deletes buffered
`Content-Type`, `Content-Length`, and `Transfer-Encoding` headers, matching Koa's
`statuses.empty` handling and avoiding `net/http` "status code does not allow body"
warnings (`TestEmptyStatusStripsBody`). Note that the deletion only covers headers
buffered through `ctx.Set`; headers written straight to `ctx.Writer.Header()` survive.

### matchRouterPath

Returns true when the router prefix is empty or `/`, or when the request path starts
with the prefix and the next byte is end-of-string or `/`. This is what prevents `/v1`
middleware from firing on `/v10/...`.

### Lock scope

| Lock                 | Covers                                                                                       |
| -------------------- | -------------------------------------------------------------------------------------------- |
| `SSEWriter.mu`       | All SSE write methods and the heartbeat write. Not held by `Flush()`.                        |
| `WSConn.writeMu`     | `writeFrame` calls from the public write methods, `Close`, `CloseWithMessage`, and `writeCloseFrame`. Not held by the automatic pong inside `Listen` and `ReadMessage`. |
| `WSConn.readMu`      | `ReadMessage` and therefore `ReadJSON`. Not held by `Listen`.                                |
| `WSConn.once`        | Closing the `Closed()` channel exactly once.                                                 |

There are no locks anywhere else. `Application.routers`, `router.roots`,
`router.handlers`, `Router.middlewares`, `Context.State`, and the `WSConn.on*` handler
fields are all unsynchronized.

### Goroutine lifecycle

| Goroutine                  | Started by                | Stopped by                                                                 | Leaks if                                              |
| -------------------------- | ------------------------- | -------------------------------------------------------------------------- | ----------------------------------------------------- |
| SSE heartbeat              | `SSEWriter.Heartbeat`     | The returned stop func, or request context cancellation                    | Never permanently; bounded by the request context     |
| WebSocket heartbeat        | `WSConn.Heartbeat`        | The returned stop func, `Closed()` closing, or a ping write failure        | The connection is never closed                        |
| Your own producers         | Your handler              | You                                                                        | `SSEWriter.Stream` returns without canceling them      |

The framework starts no other goroutines. `net/http` owns the per-connection
goroutine, except for hijacked WebSocket connections, whose goroutine remains the
handler goroutine until the handler returns.

### Graceful shutdown behaviour

`Shutdown` calls `http.Server.Shutdown`, which stops accepting new connections and
waits for in-flight handlers, honouring the supplied context deadline. Two caveats
follow from the standard library semantics, not from this framework:

- Hijacked connections (every `Upgrade`ed WebSocket) are not tracked by
  `http.Server.Shutdown` and are not waited on or closed. Track them yourself and call
  `CloseWithMessage(1001, "going away")` on shutdown.
- A long-lived SSE handler is an in-flight request, so `Shutdown` blocks on it until
  the context deadline elapses. Watch `sse.Closed()` and also select on your own
  application shutdown signal if you need a faster exit.

### Zero-value behaviour

- A zero `Application` (`&Application{}` or `var app Application`) is unusable:
  `Listen` dereferences a nil `*http.Server` and panics, `Use`/`Get`/`Router`
  dereference a nil root Router and panic, and `ServeHTTP` panics on the nil internal
  router. Always use `New()` or `Default()`.
- A zero `Context` is unusable: `Set` assigns into a nil map and panics, `State` is
  nil, and `Status` is 0 rather than 404. Contexts come from the framework only.
- A zero `Router` has no `app`, so any registration panics. Routers come from
  `Application.Router` or `Router.Router`.
- A zero `UpgradeOptions` value, and a nil `*UpgradeOptions`, are both valid; see the
  defaults table above.
- `SSEWriter` and `WSConn` zero values are unusable; obtain them from `ctx.SSE()` and
  `ctx.Upgrade()`.
- `H(nil)` marshals to `null`. `ctx.JSON(nil)` sets `Body = nil` typed as `any`, which
  `respond()` treats as no body at all, so it writes only the status line rather than
  the JSON literal `null`.

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
        ctx.State["user"] = "resolved-user"
        next()
    })

    api.Get("/users/:id", func(ctx *swifty.Context, next func()) {
        user, err := findUser(ctx.Param("id"))
        if err != nil {
            ctx.Throw(http.StatusNotFound, "user not found")
            return
        }
        ctx.JSON(swifty.H{"message": "ok", "data": user}) // status auto-promotes to 200
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
    if err := app.Shutdown(shutdownCtx); err != nil {
        log.Printf("shutdown: %v", err)
    }
}
```

### Error handling with Throw, and an explicit 404 with a body

```go
app.Get("/orders/:id", func(ctx *swifty.Context, next func()) {
    order, err := loadOrder(ctx.Param("id"))
    switch {
    case errors.Is(err, errNotFound):
        // Throw marks the status explicit, so the 404 survives the body assignment.
        ctx.Throw(http.StatusNotFound, "order not found")
        return
    case err != nil:
        ctx.Throw(http.StatusInternalServerError, "could not load order")
        return
    }
    ctx.JSON(swifty.H{"message": "ok", "data": order})
})

// A custom 404 envelope needs SetStatus, because a direct ctx.Status assignment
// would be promoted to 200 by the body setter.
app.Get("/legacy/:id", func(ctx *swifty.Context, next func()) {
    ctx.SetStatus(http.StatusNotFound)
    ctx.JSON(swifty.H{"message": "gone", "data": nil})
})
```

### Response-mutating middleware (onion model)

```go
app.Use(func(ctx *swifty.Context, next func()) {
    next() // run the rest of the chain first
    // ctx.Status is already promoted here, so this sees the real code.
    if ctx.Status >= 500 {
        ctx.Set("X-Retry-After", "5")
    }
})
```

### HTML templates

```go
app := swifty.New()
app.SetFuncMap(template.FuncMap{"upper": strings.ToUpper}) // must precede LoadHTMLGlob
app.LoadHTMLGlob("templates/*.tmpl")                       // panics on parse errors

app.Get("/", func(ctx *swifty.Context, next func()) {
    ctx.HTML("index.tmpl", swifty.H{"Name": "swifty"})
})
```

### Static files

```go
app := swifty.New()
app.Static("/assets", "./public")          // serves /assets/*filepath
app.Router("/docs").Static("/files", "./docs-files") // serves /docs/files/*filepath
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
            return // client disconnected; the producer sees the canceled request context
        case chunk, ok := <-upstream:
            if !ok {
                return
            }
            sse.JSON("delta", swifty.H{"content": chunk})
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
            return // ErrWSClosed on a clean close, a read error otherwise
        }
        if err := ws.WriteMessage(msgType, data); err != nil {
            return
        }
    }
})
```

### WebSocket with the event-driven API, origin check, and subprotocols

```go
app.Get("/ws", func(ctx *swifty.Context, next func()) {
    ws, err := ctx.Upgrade(&swifty.UpgradeOptions{
        ReadBufferSize: 8192,
        MaxMessageSize: 1 << 20, // raise the 64 KiB read cap
        CheckOrigin: func(r *http.Request) bool {
            return r.Header.Get("Origin") == "https://example.com"
        },
        Subprotocols: []string{"chat"},
    })
    if err != nil {
        return
    }
    defer ws.Close()

    stop := ws.Heartbeat(30 * time.Second)
    defer stop()

    ws.OnMessage(func(messageType int, data []byte) {
        if messageType == swifty.TextMessage {
            _ = ws.Send("echo: " + string(data))
        }
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

### WebSocket with JSON messages and a graceful close

```go
app.Get("/ws", func(ctx *swifty.Context, next func()) {
    ws, err := ctx.Upgrade(nil)
    if err != nil {
        return
    }
    defer ws.Close()

    var req RequestMessage
    if err := ws.ReadJSON(&req); err != nil {
        if errors.Is(err, swifty.ErrWSClosed) {
            return
        }
        _ = ws.CloseWithMessage(1003, "invalid payload")
        return
    }
    _ = ws.WriteJSON(ResponseMessage{Status: "ok", Data: req.Data})
})
```

## Testing patterns

The suite runs entirely on `net/http/httptest` and needs no fixtures beyond temp
directories.

Plain handler and middleware assertions go through `Application.ServeHTTP` with a
recorder, so no port is bound:

```go
app := swifty.New()
app.Get("/ping", func(ctx *swifty.Context, next func()) { ctx.String("pong") })

rec := httptest.NewRecorder()
app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
// rec.Code, rec.Body.String(), rec.Header().Get(...)
```

Response-rendering unit tests skip routing entirely by building a Context with the
unexported constructor and calling `respond()` directly (in-package tests only):

```go
rec := httptest.NewRecorder()
ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
ctx.SetStatus(http.StatusNotFound)
ctx.JSON(H{"message": "no such user"})
ctx.respond()
```

SSE assertions match exact byte sequences against `rec.Body.String()`, since
`httptest.ResponseRecorder` implements `http.Flusher` and records everything.

WebSocket tests need a real socket because `Upgrade` hijacks the connection, which
`httptest.ResponseRecorder` cannot do. `websocket_test.go` starts
`httptest.NewServer(app)`, dials with `net.Dial`, writes the handshake request by
hand, and reads the response with `http.ReadResponse`. Its reusable helpers are worth
copying when extending the suite:

- `dialWS(t, serverURL, path) *testWSClient` performs the handshake and asserts 101,
  returning the `net.Conn` and its `bufio.Reader`.
- `writeClientFrame(conn, opcode, payload)` writes a masked FIN frame.
- `writeClientFrameRaw(conn, opcode, fin, mask, payload)` gives explicit FIN and mask
  control, which is how the fragmentation and unmasked-rejection tests are built.
- `readServerFrame(br) (int, []byte, error)` parses one unmasked server frame.
- `trackedFS` / `trackedFile` in `swifty_test.go` count opens and closes to prove the
  static probe does not leak descriptors.

Set read deadlines on the client connection (`conn.SetReadDeadline`) in any test that
expects the server not to reply, otherwise a rejected-frame test hangs.

## Pitfalls / known limitations

1. Explicit 404 with a body requires `SetStatus` or `Throw`. Assigning
   `ctx.Status = http.StatusNotFound` directly and then calling a body setter yields
   200, because the promotion logic cannot distinguish an explicit 404 field
   assignment from the default. Use `ctx.SetStatus(http.StatusNotFound)` before the
   body setter.

2. Calling `next()` twice in one middleware panics with
   `"swifty_http: next() called multiple times"`. `compose` enforces the koa-compose
   single-call rule; under `Default()` the panic becomes a 500, without Recovery it
   propagates to `net/http`. Never call `next()` more than once.

3. `Throw` does not abort the chain. It only stages `Status` and `Body`; execution
   continues. Always `return` immediately after it and skip `next()`, or a downstream
   handler will overwrite the error response.

4. `BindJSON` closes the request body. A second call, or any later body read, yields
   an empty or errored read, and there is no built-in buffering. There is also no
   size limit: wrap `ctx.Request.Body` in `http.MaxBytesReader` before decoding if the
   endpoint is public.

5. Route registration is not goroutine-safe. `addRoute` writes `router.roots` and
   `router.handlers`, and `Router` appends to `app.routers`, all without locks, while
   `ServeHTTP` reads them. Register every route and middleware before `Listen`;
   registering from a handler or background goroutine can cause a concurrent map
   read/write panic.

6. Duplicate registrations overwrite silently. The same method plus canonical pattern
   (slashes collapsed, so `/users` equals `/users/` equals `//users`) replaces the
   previous handler without warning or error.

7. `ctx.Set` cannot express multi-value headers. It buffers into a
   `map[string]string`, so only the last value per canonical key survives and there is
   no removal API. For multiple `Set-Cookie` headers write to
   `ctx.Writer.Header().Add` directly, and note that Recovery discards buffered
   headers on panic but not ones written directly.

8. `SetFuncMap` must precede `LoadHTMLGlob`. The func map is captured at parse time,
   so calling it afterwards has no effect on already-parsed templates.
   `LoadHTMLGlob` panics on parse errors (`template.Must`), and a template execution
   error after the status line is committed only logs, leaving a truncated response.

9. Mixing SSE or WebSocket with the deferred response setters is unsupported. After
   `ctx.SSE()` or a successful `ctx.Upgrade`, `ctx.flushed` is true and `respond()` is
   a no-op, so later `ctx.JSON`/`ctx.String`/`ctx.Set` calls are silently ignored.
   `ctx.SSE()` also writes headers and a status line immediately, so calling it twice
   duplicates them.

10. `SSEWriter` degrades silently on a non-flushing writer. If `ctx.Writer` does not
    implement `http.Flusher`, `Flush` is a no-op and events may buffer until the
    handler returns. Test SSE against `httptest.NewRecorder` (which does flush) or a
    real server, not a hand-rolled writer shim.

11. `SSEWriter.Stream` does not cancel the producer. It returns on channel close or
    client disconnect but never drains the channel, so an unbuffered producer blocks
    forever. Tie your producer to `ctx.Request.Context()`.

12. `SSEWriter` and `Context` must not outlive the handler. Both hold the
    `http.ResponseWriter`, which `net/http` invalidates when the handler returns.
    Spawning a goroutine that writes SSE events after the handler returns produces
    undefined behaviour.

13. WebSocket origin checking is opt-in. With `opts == nil` or `CheckOrigin == nil`,
    every `Origin` is accepted, unlike gorilla/websocket's same-origin default. Set
    `CheckOrigin` on every browser-facing endpoint or you have a cross-site
    WebSocket hijacking hole.

14. WebSocket reads are capped at 65536 bytes by default. Single frames and
    reassembled fragmented messages beyond `MaxMessageSize` return
    `ErrWSInvalidFrame` and kill the read loop. Raise it via
    `UpgradeOptions.MaxMessageSize`. The write side is not capped, so a peer running
    the same implementation with a smaller limit will reject oversized frames you
    send.

15. `WSConn.Listen` and `ReadMessage` must not run concurrently. `Listen` does not
    take `readMu`, so the two race on the `bufio.Reader`. Pick one read pattern per
    connection.

16. The automatic pong reply is written without `writeMu`. Both `Listen` and
    `ReadMessage` call `writeFrame(PongMessage, ...)` directly, so a concurrent
    application write can interleave bytes and corrupt the frame stream. Either keep
    all writes on the read-loop goroutine, or handle pings yourself in `OnPing` with
    a locking `WriteMessage(PongMessage, payload)` call.

17. Fragmented messages interleaved with control frames fail. `readMessage` keeps the
    reassembly buffer in a local variable and returns control frames to the caller
    immediately, discarding the buffer. The next continuation frame then returns
    `ErrWSInvalidFrame`. This contradicts the code comment citing RFC 6455 5.4.

18. `UpgradeOptions.WriteBufferSize` is accepted but never read. All WebSocket writes
    go unbuffered to the `net.Conn`.

19. `ctx.Upgrade` requires a hijackable writer. Under `httptest.ResponseRecorder` the
    hijack fails and `Upgrade` stages a 500 and returns an error; use
    `httptest.NewServer` for WebSocket tests.

20. `Upgrade` clears the connection deadline. Any server-level `ReadTimeout` or
    `WriteTimeout` stops applying after the upgrade, so an idle or stalled peer holds
    the connection forever unless you set `SetReadDeadline`/`SetWriteDeadline` or run
    a `Heartbeat`.

21. Hijacked connections are invisible to `Shutdown`. `http.Server.Shutdown` neither
    waits for nor closes WebSocket connections. Track them yourself and close them on
    shutdown, otherwise the process exits with clients still attached.

22. `Upgrade` errors cannot be classified with `errors.Is`. Every failure path returns
    a fresh `errors.New` value with no wrapping and no sentinel. Read the staged
    `ctx.Status` (403, 405, 400, 500) if you need to distinguish them.

23. Text frames are not UTF-8 validated. RFC 6455 mandates closing with 1007 on
    invalid UTF-8 text messages; this implementation delivers the raw bytes.

24. No 405 support and no `Allow` header. A path registered only for GET answers POST
    with the notFound handler's 404 body.

25. A wildcard route does not match its own prefix. `Static("/assets", dir)` registers
    `/assets/*filepath`, so `/assets` and `/assets/` return 404 while
    `/assets/index.html` works. Register an extra route if you need the bare prefix to
    resolve.

26. `Recovery`'s 500 body is fixed. It always emits
    `{"message":"Internal Server Error"}` and clears `Type` and buffered headers.
    Fork the middleware for a custom error envelope, and recover inside any goroutine
    you spawn from a handler, since Recovery cannot see it.

27. `Router.Use` after route registration still applies, but sibling ordering is by
    Router creation order, not nesting depth. Two groups whose prefixes both match the
    request path run in the order their `Router()` calls happened, which can surprise
    you if you create a nested group before its parent's sibling.

28. Route registration logs unconditionally. Every `Get`/`Post`/... call prints
    `Route <METHOD> - <pattern>` to the standard logger. There is no flag to disable
    it; reconfigure `log` if the output is unwanted.

29. The internal `http.Server` is not configurable and not exposed. Timeouts, header
    limits, and TLS cannot be set through `Application`. Build your own
    `http.Server{Handler: app}` if you need them, and remember that `app.Shutdown`
    then targets the wrong server.

30. `ctx.JSON(nil)` writes no body. `respond()` treats a nil `Body` as "status line
    only", so it does not emit the JSON literal `null`. Use
    `ctx.Data([]byte("null"))` if you need that byte sequence.

## File map

| File                | Purpose                                                                                                                                                        |
| ------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `swifty.go`         | `Application`, `New`/`Default`, `Listen`/`Shutdown`, `SetFuncMap`/`LoadHTMLGlob`, the delegating registration methods, `ServeHTTP`, and `compose` with the double-next guard |
| `context.go`        | `Context`, `H`, `Middleware`, `newContext`, request accessors (`Query`, `Param`, `PostForm`, `Get`, `BindJSON`, `FormFile`), `Throw`, `SetStatus`, header buffering via `Set` |
| `response.go`       | Body setters (`JSON`, `String`, `Data`, `HTML`, `Redirect`), `promoteStatus`, `htmlPayload`, `emptyStatus`, `respond` and the per-type renderers, `setContentType` |
| `group.go`          | `Router` with `normalizePrefix`, `Use`, method registration and `addRoute` logging, `All`, `Static` with the existence probe, no directory listing, and `statusRecorder`, plus `matchRouterPath` |
| `router.go`         | Internal `router`: `parsePattern`, pattern canonicalization in `addRoute`, `getRoute`, `getRoutes`, `handle`, and the synthesized 404 handler                    |
| `trie.go`           | Trie `node`: `insert`, `search` with backtracking, `travel`, `String`, and literal-before-wildcard child matching                                                |
| `sse.go`            | `SSEWriter`, `Context.SSE`, event/data/comment framing, `Heartbeat`, `Stream`, `Closed`, `Flush`, `Done`                                                          |
| `websocket.go`      | `Context.Upgrade`, `UpgradeOptions`, `WSConn`, opcode constants, `ErrWSClosed`/`ErrWSInvalidFrame`, frame I/O with fragmentation reassembly and RFC validation, handshake helpers |
| `logger.go`         | `Logger` middleware                                                                                                                                            |
| `recovery.go`       | `Recovery` middleware with `http.ErrAbortHandler` passthrough and the `trace` traceback builder                                                                  |
| `main.go`           | Runnable demo, not part of the library: `//go:build ignore` plus `package main`, so it is excluded from `go build ./...`, `go vet`, and `go doc`. Run it with `go run main.go`; it serves `/`, `/panic`, and `/events` on `:9999` |
| `context_test.go`   | Request accessor, `BindJSON`, `State`, and `Throw` tests                                                                                                        |
| `response_test.go`  | Renderer, status promotion, `SetStatus`, empty-status, `Redirect`, Content-Type precedence, and onion-model tests                                                |
| `router_test.go`    | `parsePattern`, trie route resolution, `getRoutes`, literal-over-wildcard priority, 404, and `node.String` tests                                                 |
| `sse_test.go`       | SSE wire format, multiline data, JSON events, comments, `Stream`, `Closed`, and heartbeat concurrency tests                                                      |
| `swifty_test.go`    | Router nesting and normalization, middleware order, prefix boundary, Recovery, all HTTP methods, static files (listing disabled, descriptor accounting), templates, `Listen` error, and double-next 500 tests |
| `websocket_test.go` | Accept-key, header parsing, subprotocol negotiation, close-payload parsing, handshake and echo, JSON, heartbeat, `Closed()` channel, fragmentation, unmasked-frame rejection, and version-13 validation tests, plus the reusable client helpers |

## Dependencies

None. `swifty_http/go.mod` declares
`module github.com/hangtiancheng/swifty.go/swifty_http` with `go 1.26.0` and contains
no `require` block at all, only `replace` directives pointing the sibling modules
(`swifty_cache`, `swifty_orm`, `swifty_rpc`) at their local directories for when a
consumer inside this workspace pulls them in. The `swifty_http` replace directive is
commented out.

The significance is that every feature is implemented against the standard library:

- WebSocket support is hand-rolled RFC 6455 framing over a hijacked `net.Conn`, not
  gorilla/websocket or nhooyr/websocket. That means no permessage-deflate, no
  client-side dialer, no automatic UTF-8 validation, and no `SetPongHandler`-style
  API, but also no supply-chain surface and no version skew.
- SSE is direct `fmt.Fprintf` into the `http.ResponseWriter` plus `http.Flusher`.
- Routing is a hand-written trie, not `gorilla/mux` or `httprouter`.
- Templating is `html/template`; JSON is `encoding/json`; logging is `log`.

Standard library packages used across the non-test files: `bufio`, `context`,
`crypto/sha1`, `encoding/base64`, `encoding/binary`, `encoding/json`, `errors`, `fmt`,
`html/template`, `io`, `log`, `mime/multipart`, `net`, `net/http`, `path`, `runtime`,
`strings`, `sync`, `time`, `unicode/utf8`.

Note the use of `strings.SplitSeq` (Go 1.24+) in `sse.go` and `websocket.go` and of
`http.NewResponseController` (Go 1.20+) in `websocket.go`; the `go 1.26.0` directive is
well above both floors.

## Cross-references

- `swifty-cache` is a genuine integration point and the only sibling that imports
  this module. `swifty_cache.DashboardHandler()` returns a
  `func(ctx *swifty_http.Context, next func())`, which is assignable to `Middleware`,
  and it calls `ctx.Upgrade(&swifty_http.UpgradeOptions{...})` then drives the
  resulting `*swifty_http.WSConn` with `WriteJSON`, `ReadJSON`, `Heartbeat`,
  `Closed`, and `Close`. `swifty_cache.StartDashboard(addr)` builds its own
  `swifty_http.New()` application and registers the handler at `/dashboard/ws`. Mount
  the handler on your own app instead when you want a single port:
  `app.Get("/dashboard/ws", cache.DashboardHandler())`. Beyond the dashboard,
  handlers commonly wrap data fetches in a cache `Group` before responding with
  `ctx.JSON`.
- `swifty-orm` is a MongoDB ORM with no dependency on this module. Handlers typically
  call ORM queries to fetch or mutate data and then respond via `ctx.JSON`. Consult
  that skill for query-builder questions.
- `swifty-rpc` is a TCP RPC framework with no dependency on this module. For services
  exposing both HTTP and RPC surfaces, use `swifty_http` for HTTP and `swifty_rpc` for
  RPC. Do not answer gRPC-style questions under this skill.

## Behavioural contracts cheat sheet

Non-obvious contracts enforced by the source. Use this table when generating or
reviewing `swifty_http` code.

| Area                     | Contract                                                                                                                                                                                                                     |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Status promotion         | Body setters promote 404 to 200 immediately (`promoteStatus`); middleware after `next()` sees the final status. `respond()` repeats the check for direct `ctx.Body` assignments.                                              |
| Explicit status          | `SetStatus`, `Throw`, and `Redirect` mark the status explicit; direct `ctx.Status` field assignment does not, so an explicit 404 with a body requires `SetStatus`/`Throw`.                                                    |
| Throw shape              | `Throw` sets `Body = H{"message": msg, "data": nil}` and does not abort the chain; return immediately after it.                                                                                                               |
| Redirect status          | `Redirect` inspects `ctx.Status` (not `statusSet`), keeps 300/301/302/303/305/307/308, otherwise forces 302, and adds a `"Redirecting to <url>"` text body when `Body` is nil.                                                 |
| Empty statuses           | 204/205/304 strip the body and buffered Content-Type/Content-Length/Transfer-Encoding in `respond()`.                                                                                                                         |
| Content-Type             | A Content-Type buffered via `ctx.Set` always beats the inferred `ctx.Type`; `ctx.Data` with an empty `Type` leaves sniffing to `net/http`.                                                                                    |
| Header keys              | `ctx.Set` canonicalizes keys; single value per header, no removal API, no multi-value (`Set-Cookie`) support.                                                                                                                 |
| Body types               | `respond()` switches `htmlPayload`, `[]byte`, `string`, `io.Reader`, then JSON; nil `Body` writes only the status line.                                                                                                        |
| respond skip             | `respond()` returns early when `ctx.flushed`; `SSE()`, `Upgrade()`, and Static rely on this.                                                                                                                                  |
| JSON failure             | `respondJSON` marshals before committing the status line, so marshal failure downgrades cleanly to a 500 JSON body.                                                                                                           |
| next() guard             | `compose` panics on a repeated `next()`; Recovery turns it into a 500. Not calling `next()` short-circuits.                                                                                                                   |
| Recovery                 | Re-raises `http.ErrAbortHandler`; otherwise logs `%v` plus a traceback and resets Status/Type/headers/Body to a fixed 500 JSON. Does not cover spawned goroutines.                                                            |
| Logger accuracy          | Logger reads `ctx.Status` after the chain; SSE records 200, Upgrade records 101, Static mirrors the real status via `statusRecorder`.                                                                                         |
| Router prefix            | `normalizePrefix` forces a leading `/` and strips trailing `/`; `/v1` matches `/v1` and `/v1/...`, never `/v10`.                                                                                                              |
| Middleware collection    | Collected at request time from all matching routers in Router creation order; `Use` after child creation still applies.                                                                                                       |
| Pattern canonicalization | `addRoute` rebuilds the pattern from parsed parts; `/users`, `/users/`, and `//users` share one handler slot (last wins).                                                                                                     |
| Wildcard                 | `*name` must be last; later segments are silently dropped, a bare `*` captures nothing, and the wildcard route does not match its own bare prefix. Literal children beat wildcards, and `search` backtracks.                  |
| Static                   | Probes existence with the handle closed immediately, 404 on miss, no directory listing without `index.html`, flushes `ctx.Set` headers before delegating.                                                                     |
| Server lifecycle         | `http.Server` is built in `New()`; `go app.Listen` plus `Shutdown` is race-free; `Listen` returns `http.ErrServerClosed` after shutdown; hijacked WebSocket connections are not waited on.                                     |
| Templates                | `SetFuncMap` before `LoadHTMLGlob`; `LoadHTMLGlob` panics on parse errors; execution errors after the status line only log.                                                                                                   |
| Zero values              | `Application`, `Context`, `Router`, `SSEWriter`, and `WSConn` zero values panic or misbehave; only `UpgradeOptions` has a usable zero value (and a usable nil pointer).                                                        |
| Registration safety      | Route and middleware registration is unsynchronized; do it all before `Listen`.                                                                                                                                              |
| SSE heartbeat            | Stop functions (SSE and WebSocket) are idempotent and block until the goroutine exits.                                                                                                                                       |
| SSE fields               | `ID` is a bare field line (pair it with `Data`/`Event`/`JSON`); `Retry` terminates the current event block; `JSON` drops marshal errors silently.                                                                             |
| WS handshake             | GET only (405), Connection/Upgrade tokens (400), `Sec-WebSocket-Version: 13` (400 plus advisory header), key required (400), origin check (403, opt-in), hijack required (500); errors are unwrapped and sentinel-free.       |
| WS frames                | Fragmentation reassembled up to `MaxMessageSize` bytes total (default 65536); unmasked client frames, non-zero RSV bits, oversized or fragmented control frames, and unknown opcodes all yield `ErrWSInvalidFrame`.           |
| WS reads                 | `ReadMessage` auto-answers pings and returns `ErrWSClosed` on close; `Listen` does not take `readMu`, so never mix the two; control frames between fragments break reassembly.                                                |
| WS close                 | `Closed()` closes exactly once and also fires on peer close or read error; read errors after a local `Close` do not trigger `OnError`; nothing closes the `net.Conn` for you.                                                 |
| WS writes                | Serialized by `writeMu`, except the automatic pong inside the read loops; `WriteBufferSize` is unused; there is no write-side size cap.                                                                                       |
