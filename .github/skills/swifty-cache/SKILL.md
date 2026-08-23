---
name: swifty-cache
description: >
  Distributed groupcache-style cache for Go (module
  github.com/hangtiancheng/swifty.go/swifty_cache): read-through Group with a
  byte-budgeted two-level LRU store, singleflight deduplication, consistent
  hashing, etcd service discovery, gRPC transport, and a WebSocket dashboard.
  Use when working with Group, NewGroup, GetGroup, GetAllGroups, ListGroups,
  DestroyGroup, DestroyAllGroups, Getter, GetterFunc, ByteView, Cache,
  CacheOptions, DefaultCacheOptions, Store, StoreOptions, NewStoreOptions,
  NewStore, Entry, Value, SingleFlightGroup, ConsistentHashMap,
  NewConsistentHash, ConHashConfig, DefaultConHashConfig, ConHashOption,
  WithConsistentHashConfig, PeerPicker, Peer, ClientPicker, NewClientPicker,
  Client, NewClient, Server, NewServer, ServerOptions, DefaultServerOptions,
  Register, RegisterConfig, DefaultRegisterConfig, DashboardHandler,
  StartDashboard, WithExpiration, WithPeers, WithCacheOptions,
  WithServiceName, WithEtcdEndpoints, WithDialTimeout, ErrKeyRequired,
  ErrValueRequired, ErrGroupClosed, HashBKRD, MaskOfNextPowOf2, Now, Create,
  ValidPeerAddr, the pb package (SwiftyCacheClient, SwiftyCacheServer,
  RegisterSwiftyCacheServer, Request, ResponseForGet, ResponseForDelete),
  cache topology, peer sync, or byte eviction. Do NOT use for groupcache,
  bigcache, ristretto, Redis/Memcached client code, or plain in-process caches
  that need no clustering.
---

# swifty_cache

A distributed, groupcache-aligned caching framework with gRPC transport,
etcd-based service discovery, consistent hashing over a stable (fixed-replica)
ring, single-flight request coalescing, a bucketed two-level LRU store with a
real byte budget, and an optional real-time WebSocket dashboard. Unlike
groupcache, which is strictly read-through, swifty_cache also propagates `Set`
and `Delete` to the owning peer with best-effort, eventually consistent
semantics.

Module path: `github.com/hangtiancheng/swifty.go/swifty_cache`

Source root: `swifty_cache/`

Go directive in `swifty_cache/go.mod`: `go 1.26.0`.

All types live in the flat `swifty_cache` package. `pb/` is the only
sub-package; it holds generated protobuf/gRPC code.

## When to load adjacent skills

- Load `swifty-http` when touching `dashboard.go`, mounting
  `DashboardHandler()` on an existing application, or changing the WebSocket
  snapshot protocol. This is the only real integration point: `dashboard.go`
  imports `github.com/hangtiancheng/swifty.go/swifty_http` and uses
  `swifty_http.New`, `Application.Get`, `Application.Listen`,
  `swifty_http.Context.Upgrade`, `swifty_http.UpgradeOptions`, and
  `swifty_http.WSConn`.
- Load `swifty-orm` when the `Getter` you write for a `Group` loads cache
  misses out of MongoDB through the ORM engine.
- Load `swifty-rpc` only when composing cache groups into a swifty_rpc
  service. swifty_cache's own transport is plain gRPC, not swifty_rpc.

## Architecture overview

```
                      process-global registry
                      groups map[string]*Group  (groupsMu sync.RWMutex)
                      NewGroup / GetGroup / GetAllGroups / ListGroups
                      DestroyGroup / DestroyAllGroups
                                  |
Group (cache namespace; duplicate name panics)
  |-- name        string
  |-- getter      Getter                (GetterFunc adapts a plain func)
  |-- expiration  time.Duration         (WithExpiration; 0 = no TTL)
  |-- closed      atomic.Int32          (Close is CAS-idempotent)
  |-- stats       groupStats            (all atomic.Int64 counters)
  |-- loader      *SingleFlightGroup    (sync.Map + WaitGroup, panic-recovering)
  |-- peersMu     sync.RWMutex          (guards peers)
  |-- peers       PeerPicker            (WithPeers or RegisterPeers, once)
  `-- mainCache   *Cache
        |-- opts        CacheOptions
        |-- mu          sync.RWMutex     (guards store pointer + Clear)
        |-- hits/misses atomic.Int64
        `-- store       Store            (lazily built on first write)
              `-- *lruStore (returned by NewStore)
                    |-- locks  []sync.Mutex      (one per bucket)
                    |-- caches [][2]*cache       ([bucket][L1,L2])
                    |     |-- [0] L1: CapPerBucket entries, write entry point
                    |     `-- [1] L2: Level2Cap entries, read promotion target
                    |-- maxBucketBytes = MaxBytes / (mask+1)
                    |-- cleanupTick *time.Ticker + closeCh + closeOnce
                    `-- cleanupLoop goroutine (exits on Close)

PeerPicker (interface) -> *ClientPicker
  |-- selfAddr  string             (":port" normalized to "<localIPv4>:port")
  |-- svcName   string             (WithServiceName; default "swifty_cache")
  |-- mu        sync.RWMutex       (guards consHash + clients)
  |-- consHash  *ConsistentHashMap (self is permanently in the ring)
  |-- clients   map[string]*Client (one gRPC Peer per remote address)
  |-- etcdCli   *client_v3.Client  (endpoints from DefaultRegisterConfig)
  `-- ctx/cancel                    (watchServiceChanges goroutine lifetime)
        `-- watch /services/<svcName>/ with snapshot-revision continuation,
            1s backoff, and full resync (fetchAllServices) on channel break

ConsistentHashMap
  |-- config     *ConHashConfig  (DefaultReplicas, HashFunc)
  |-- keys       []int           (sorted virtual-node hashes)
  |-- hashMap    map[int]string  (virtual hash -> node)
  |-- nodeHashes map[string][]int(node -> its virtual hashes)
  `-- nodeCounts map[string]*int64 + totalRequests atomic.Int64

Peer (interface) -> *Client
  `-- grpc.ClientConn (insecure, WaitForReady) + pb.SwiftyCacheClient

Server (one per node)
  |-- pb.UnimplementedSwiftyCacheServer (embedded)
  |-- grpcServer   *grpc.Server         (MaxRecvMsgSize = opts.MaxMsgSize)
  |-- healthServer *health.Server       (SERVING under svcName)
  |-- etcdCli      *client_v3.Client    (opts.EtcdEndpoints)
  |-- stopCh       chan error + stopOnce sync.Once
  `-- Get / Set / Delete RPCs -> GetGroup(req.Group) with the peer-request
      context marker injected, so peer traffic is never re-forwarded

Dashboard (optional, WebSocket)
  |-- StartDashboard(addr)   -> swifty_http app, GET /dashboard/ws (sync.Once)
  |-- DashboardHandler()     -> mountable handler for an existing app
  `-- per connection: 2s snapshot push + 30s heartbeat + command reader
```

Read path (`Group.Get`):

1. Return `ErrGroupClosed` if closed, `ErrKeyRequired` if the key is empty.
2. Increment `gets`. Call `mainCache.Get`; on hit, increment `local_hits` and
   return the view.
3. Increment `local_misses` and call the unexported `load`.
4. `load` runs `SingleFlightGroup.Do(key, fn)`. Inside the callback (leader
   only):
   - Re-check `mainCache.Get`. Singleflight only dedups overlapping calls, so
     two serial callers can both miss before the first populates. On hit,
     return the cached view without touching the getter.
   - Increment `loads_deduped` and call `loadData`:
     - If a `PeerPicker` is registered and the request did NOT originate from
       a peer (the `peerRequestContextKey` value is absent), call
       `PickPeer(key)`.
       - `ok && !isSelf && peer != nil`: call `peer.Get(group, key)`. On
         success increment `peer_hits` and return the cloned bytes; on failure
         increment `peer_misses`, log, and fall through.
       - Owner is self (`isSelf == true`) or the ring has no client: fall
         through to the local `Getter`.
     - Call `getter.Get(ctx, key)`. On error return
       `fmt.Errorf("failed to get data: %w", err)`. On success increment
       `loader_hits` and wrap the bytes in a `ByteView` over a clone.
   - Store the resulting `ByteView` in `mainCache`, using
     `AddWithExpiration(key, view, time.Now().Add(expiration))` when
     `expiration > 0`, otherwise `Add`. Population happens inside the callback,
     so only the leader writes.
5. `load` adds the elapsed nanoseconds to `loadDuration` and increments
   `loads` unconditionally. On error it increments `loader_errors`. The result
   is type-asserted with an `ok` check; a non-`ByteView` result becomes
   `fmt.Errorf("unexpected load result type %T", ...)`, never a panic.

Write path (`Group.Set` / `Group.Delete`):

1. Reject when closed (`ErrGroupClosed`), when the key is empty
   (`ErrKeyRequired`), or, for `Set` only, when `len(value) == 0`
   (`ErrValueRequired`).
2. Update the local cache synchronously. `Set` clones the value into a
   `ByteView`; `Delete` calls `mainCache.Delete` and ignores its bool.
3. If the request did not originate from a peer and a `PeerPicker` is
   registered, spawn `go g.syncToPeers(peers, op, key, value)`. That goroutine
   calls `PickPeer(key)`, returns immediately when `!ok || isSelf || peer ==
   nil`, and otherwise forwards the operation on
   `context.WithTimeout(withPeerRequest(context.Background()), 3*time.Second)`.
   Failures are logged only; both methods have already returned `nil`.

## Consistency model

swifty_cache is best-effort and eventually consistent.

- `Set` and `Delete` write locally, then fire-and-forget one RPC to the single
  owning peer with a 3-second timeout (`peerSyncTimeout`), no retry, no
  acknowledgement, no rollback. The caller never sees a sync failure.
- Non-owning peers never learn of a change; their cached copies converge only
  through TTL expiry. To bound staleness in a cluster, always configure
  `WithExpiration`.
- There is no read repair. When the owner is unreachable the caller falls back
  to its local `Getter` and caches the value locally.
- Values fetched from a remote peer are cached unconditionally in the local
  `mainCache`. There is no probabilistic hot-key cache as in groupcache, so
  non-owner replicas are common and TTL is the only convergence mechanism.
- Topology changes repoint the ring; during divergence a key may briefly hash
  to a different owner. Peer-originated requests are always answered locally
  (`isPeerRequest(ctx)` short-circuits both `loadData` and `syncToPeers`),
  which makes forwarding loops impossible.
- Do not use swifty_cache for state that requires strong consistency.

## Core types

### Value

```go
type Value interface {
    Len() int
}
```

Every value stored in a `Store` must implement `Value`; `Len` is the byte size
used for the budget accounting. `ByteView` satisfies it.

### Entry

```go
type Entry struct {
    Key      string
    Size     int    // value.Len()
    ExpireAt int64  // nanosecond deadline on the coarse clock; maxExpireAt = math.MaxInt64 means never
    Level    int    // 0 = L1, 1 = L2
}
```

Returned by `Cache.Entries` and `Group.Entries`, and passed to `Store.Walk`
callbacks. `dashboard.go` converts it directly to its JSON `entrySnapshot`
(`key`, `size`, `expire_at`, `level`), so the field order and types are load
bearing for the wire format.

### ByteView

```go
type ByteView struct {
    b []byte // unexported
}

func (b ByteView) Len() int          // implements Value
func (b ByteView) ByteSlice() []byte // defensive copy on every call
func (b ByteView) String() string    // []byte -> string conversion (copies)
```

Immutable by construction: `b` is unexported and `ByteSlice` returns
`cloneBytes(b.b)`, so mutating the result never affects the cached value
(pinned by `TestGetterFuncAndByteView`). There is no exported constructor.
External packages receive `ByteView` values from `Group.Get` and
`Cache.Get`; only in-package code (and in-package tests) can build one with
`ByteView{b: ...}`. The zero value is valid and has `Len() == 0`,
`String() == ""`, and `ByteSlice()` returning an empty non-nil slice.

Every path that produces a `ByteView` from caller-supplied bytes clones first:
`Group.Set`, `Group.loadData`, and `Group.getFromPeer` all use
`ByteView{b: cloneBytes(...)}`.

### Getter and GetterFunc

```go
type Getter interface {
    Get(ctx context.Context, key string) ([]byte, error)
}

type GetterFunc func(ctx context.Context, key string) ([]byte, error)

func (f GetterFunc) Get(ctx context.Context, key string) ([]byte, error)
```

`GetterFunc` adapts a plain function to `Getter`. A panic inside the getter is
recovered by the singleflight layer and surfaces as an error from `Group.Get`
(pinned by `TestGroupGetterPanicReturnsError`). A getter that returns
zero-length bytes with a nil error is cached as an empty `ByteView`; that
asymmetry with `Group.Set`, which rejects empty values, is intentional in the
code but easy to trip over.

### Group

A cache namespace. Groups live in a process-global
`map[string]*Group` guarded by `groupsMu sync.RWMutex`.

Sentinel errors:

```go
var ErrKeyRequired   = errors.New("key is required")
var ErrValueRequired = errors.New("value is required")
var ErrGroupClosed   = errors.New("cache group is closed")
```

All three are returned bare (never wrapped), so `errors.Is` and `==` both
work. Loader failures are wrapped: `failed to get data: %w` around the
getter's error, and `failed to get from peer: %w` around the peer error before
the fallback.

Options:

```go
type GroupOption func(*Group)

func WithExpiration(d time.Duration) GroupOption     // 0 = no TTL
func WithPeers(peers PeerPicker) GroupOption         // sets g.peers directly
func WithCacheOptions(opts CacheOptions) GroupOption // replaces g.mainCache
```

`WithPeers` assigns `g.peers` without taking `peersMu` (it runs before the
group is published), and it counts as the one allowed registration:
`RegisterPeers` panics afterwards.

`WithCacheOptions` calls `NewCache(opts)` and replaces the whole `mainCache`,
discarding the `MaxBytes = cacheBytes` value `NewGroup` had wired in. Set
`opts.MaxBytes` yourself.

Constructor and registry:

```go
func NewGroup(name string, cacheBytes int64, getter Getter, opts ...GroupOption) *Group
func GetGroup(name string) *Group
func GetAllGroups() map[string]*Group
func ListGroups() []string
func DestroyGroup(name string) bool
func DestroyAllGroups()
```

`NewGroup` behaviour:

- Panics with `"nil Getter"` when `getter == nil`.
- Starts from `DefaultCacheOptions()` and overwrites `MaxBytes` with
  `cacheBytes`, then applies the options in order.
- Panics with `"duplicate registration of group " + name` when the name is
  already registered (groupcache semantics, pinned by
  `TestNewGroupPanicsOnDuplicate`).
- Logs the creation, then calls `StartDashboard(addr)` when
  `g.mainCache.opts.DashboardAddr != ""`.
- Accepts an empty name; it becomes the registry key.

`GetGroup` returns `nil` for unknown names. `GetAllGroups` returns a copied
map (via `maps.Copy`) so mutating it is safe, but the `*Group` values are
shared. `ListGroups` returns names in map-iteration order, so it is not
sorted. `DestroyGroup` removes the entry under the write lock, releases the
lock, then calls `Close`; it returns `false` when the name was not
registered. `DestroyAllGroups` snapshots and replaces the map, then closes
each group outside the lock (both are pinned against deadlock by
`TestGroupRegistryOperationsDoNotDeadlock`).

Methods:

| Method           | Signature                                               | Notes                                                                       |
| ---------------- | ------------------------------------------------------- | --------------------------------------------------------------------------- |
| Get              | `(ctx context.Context, key string) (ByteView, error)`   | Read-through; peer failure is logged and falls back, never surfaced         |
| Set              | `(ctx context.Context, key string, value []byte) error` | Local write, then async peer sync; rejects empty value with ErrValueRequired |
| Delete           | `(ctx context.Context, key string) error`               | Local delete, then async peer sync; returns nil even when the key was absent |
| Clear            | `()`                                                    | No-op when closed; clears the local cache only, never propagates            |
| Close            | `() error`                                              | CAS-idempotent; closes mainCache and unregisters; always returns nil        |
| RegisterPeers    | `(peers PeerPicker)`                                    | At most once; panics `"RegisterPeers called more than once"` on the second   |
| Stats            | `() map[string]any`                                     | See key list below                                                          |
| DashboardEnabled | `() bool`                                               | True when mainCache is non-nil and its DashboardAddr is non-empty           |
| Entries          | `() []Entry`                                            | Live entries from the local cache; nil when closed or mainCache is nil      |

`Group.Stats` keys:

```
name              string
closed            bool
expiration        time.Duration   - the raw value, not a string
gets              int64  - Get calls that passed validation
loads             int64  - SingleFlightGroup.Do invocations (leaders and followers)
loads_deduped     int64  - callbacks that actually ran loadData
local_hits        int64
local_misses      int64
peer_hits         int64
peer_misses       int64
loader_hits       int64
loader_errors     int64
server_requests   int64  - incremented only by Server.Get
hit_rate          float64  - present only when local_hits + local_misses > 0
avg_load_time_ms  float64  - present only when loads > 0
cache_<key>       any      - every Cache.Stats key, prefixed with "cache_"
```

`Group.Clear` resets the cache's hit/miss counters (through `Cache.Clear`) but
leaves every group counter intact, so `gets` and `local_hits` keep
accumulating across a clear.

Concurrency: `Group` is safe for concurrent use. `peers` is guarded by
`peersMu` (write in `RegisterPeers`, read in `getPeers`), `closed` is an
`atomic.Int32`, every stat is an `atomic.Int64`, and `mainCache` does its own
locking. `mainCache` and `getter` are never reassigned after construction.

### Cache

A thin wrapper over a `Store` adding lazy initialization, hit/miss counters,
and an idempotent close.

```go
type CacheOptions struct {
    MaxBytes      int64                         // byte budget; default 8 MiB
    BucketCount   uint16                        // default 16; rounded up to a power of 2
    CapPerBucket  uint16                        // L1 entries per bucket; default 512
    Level2Cap     uint16                        // L2 entries per bucket; default 256
    CleanupTime   time.Duration                 // expiry sweep interval; default 1m
    OnEvicted     func(key string, value Value) // default nil
    DashboardAddr string                        // default ""; non-empty makes NewGroup start the dashboard
}

func DefaultCacheOptions() CacheOptions
func NewCache(opts CacheOptions) *Cache
```

`DefaultCacheOptions` returns `MaxBytes: 8*1024*1024`, `BucketCount: 16`,
`CapPerBucket: 512`, `Level2Cap: 256`, `CleanupTime: time.Minute`,
`OnEvicted: nil`, and an empty `DashboardAddr`.

`DashboardAddr` is the only `CacheOptions` field the `Cache` itself reads
outside store construction; the rest are copied into `StoreOptions` when the
store is created (`CleanupTime` maps to `StoreOptions.CleanupInterval`).

`NewCache` allocates no store. `ensureInitialized` builds it on the first
`Add` or `AddWithExpiration` under the write lock, and refuses when the cache
is already closed. `Get`, `Delete`, `Clear`, `Len`, `Entries`, and the
size-dependent part of `Stats` all treat an uninitialized cache as empty.

| Method            | Signature                                                            | Notes                                                                       |
| ----------------- | -------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| Add               | `(key string, value ByteView)`                                       | Logs and returns on a closed cache; initializes the store; logs Set errors   |
| AddWithExpiration | `(key string, value ByteView, expirationTime time.Time)`             | Converts to a duration with `time.Until`; drops the entry when already past  |
| Get               | `(ctx context.Context, key string) (ByteView, bool)`                 | `ctx` is accepted but unused; counts a miss when uninitialized               |
| Delete            | `(key string) bool`                                                  | False when closed or uninitialized                                          |
| Clear             | `()`                                                                 | Takes the write lock; resets hits and misses; no-op when closed/uninitialized |
| Len               | `() int`                                                             | Deduplicated entry count across both LRU levels                             |
| Close             | `()`                                                                 | CAS-idempotent; closes the store, nils it, resets `initialized` to 0         |
| DashboardEnabled  | `() bool`                                                            | `opts.DashboardAddr != ""`                                                  |
| Entries           | `() []Entry`                                                         | Collects every live entry via `Store.Walk`; nil when closed/uninitialized    |
| Stats             | `() map[string]any`                                                  | See below                                                                   |

`Cache.Stats` always emits `initialized` (bool), `closed` (bool), `hits`
(int64), and `misses` (int64). Once initialized it adds `size` (int),
`hit_rate` (float64, `0.0` when there were no requests), and — only when the
store is a `*lruStore`, which is the default — `bytes` (int64, current live
key+value bytes) and `evictions` (int64, cumulative capacity and byte-budget
evictions). `TestStatsAlignmentCounters` asserts that `cache_bytes` and
`cache_evictions` reach `Group.Stats`.

Concurrency: `Cache` is safe for concurrent use, and safe to use concurrently
with `Close`. Every accessor checks `closed`/`initialized`, then takes the
mutex and nil-checks `store` before dereferencing, so a racing `Close` degrades
to a miss rather than a nil dereference. `TestCacheConcurrentUseWithClose`
hammers 20 rounds of four writers against `Close` to pin this. `Add`,
`AddWithExpiration`, `Get`, `Delete`, `Len`, and `Entries` hold the read lock
(the store shards internally); `Clear`, `ensureInitialized`, and `Close` hold
the write lock.

### Store interface, StoreOptions, and lruStore

```go
type Store interface {
    Get(key string) (Value, bool)
    Set(key string, value Value) error
    SetWithExpiration(key string, value Value, expiration time.Duration) error
    Delete(key string) bool
    Clear()
    Len() int
    Walk(fn func(Entry) bool)
    Close()
}

type StoreOptions struct {
    MaxBytes        int64  // <= 0 disables byte-budget eviction entirely
    BucketCount     uint16
    CapPerBucket    uint16
    Level2Cap       uint16
    CleanupInterval time.Duration
    OnEvicted       func(key string, value Value)
}

func NewStoreOptions() StoreOptions // 8 MiB, 16, 512, 256, 1m, nil
func NewStore(opts StoreOptions) *lruStore
```

`NewStore` fills zero values with the same defaults `NewStoreOptions` uses
(`BucketCount` 16, `CapPerBucket` 512, `Level2Cap` 256, `CleanupInterval` 1m
when `<= 0`). It does not default `MaxBytes`: a zero or negative `MaxBytes`
turns the byte budget off and leaves only per-level entry-count eviction.

`NewStore` returns the unexported concrete type `*lruStore`. Callers outside
the package can hold the value with `:=` and use two extra methods, but cannot
name the type; declare variables as `Store` instead.

```go
func (s *lruStore) Bytes() int64     // live key+value bytes across all buckets
func (s *lruStore) Evictions() int64 // cumulative capacity + byte-budget evictions
```

Both walk every bucket taking each bucket lock in turn, so they are snapshots
that are not atomic across buckets.

## Two-level LRU store and byte budgeting

Bucketing:

- `mask = MaskOfNextPowOf2(BucketCount)`; the store allocates `mask+1` buckets.
  `BucketCount` is therefore effectively rounded up to the next power of two
  (`3` becomes 4 buckets, `16` stays 16).
- A key lands in bucket `HashBKRD(key) & mask`. `HashBKRD` can return a
  negative `int32`, but `mask` is a small non-negative `int32`, so the AND is
  always a valid index.
- Each bucket has its own `sync.Mutex`. All store operations except `Bytes`,
  `Evictions`, `Clear`, `Len`, and `Walk` touch exactly one bucket, so
  throughput scales with `BucketCount`.

Two levels per bucket:

- `caches[i][0]` is L1 with `CapPerBucket` slots; it is the only write entry
  point.
- `caches[i][1]` is L2 with `Level2Cap` slots; it is populated only by read
  promotion.
- Each level is a fixed-size array plus an intrusive doubly linked list
  (`Create(cap)` allocates `cap` node slots and a `cap+1` link table), so no
  per-entry allocation happens after construction.

Write (`Set` / `SetWithExpiration`):

1. `expireAt = Now() + expiration.Nanoseconds()` when `expiration > 0`,
   otherwise `maxExpireAt` (`math.MaxInt64`).
2. `caches[idx][0].put(...)` inserts or updates in L1, adjusting `bytes` by
   `len(key) + value.Len()`. When L1 is full it recycles the LRU tail slot;
   if that slot still held a live entry it decrements `bytes`, increments
   `nevict`, and fires `OnEvicted`.
3. `caches[idx][1].drop(key)` removes any stale copy previously promoted to
   L2. L1 is the single write authority; without this the old value could
   resurface after the L1 slot is recycled. Pinned by
   `TestLRUStoreSetInvalidatesL2Copy`.
4. When `maxBucketBytes > 0`, loop while `L1.bytes + L2.bytes >
   maxBucketBytes`, calling `evictFromBucket` — which tries `L1.evictOldest`
   first and then `L2.evictOldest` — and break when neither can evict. Each
   eviction increments `nevict` and fires `OnEvicted`. Pinned by
   `TestLRUStoreMaxBytesEviction`.

`Set` always returns `nil` in the current implementation; the `error` in the
`Store` signature exists for alternative implementations.

Byte budget arithmetic: `maxBucketBytes = MaxBytes / (mask+1)`, clamped up to
`1` when the division rounds to zero. The budget is therefore per bucket, not
global.

Read (`Get`):

1. `caches[idx][0].del(key)` logically retires the L1 entry (sets
   `expireAt = 0`, subtracts its bytes, moves the slot to the free end) and
   returns the node and its former `expireAt`.
2. On an L1 hit the value reference is taken and `n1.v` is set to `nil`. If
   `Now() >= expireAt`, both copies are dropped, `OnEvicted` fires exactly
   once, and the read reports a miss (pinned by
   `TestLRUStoreExpiredReadFiresOnEvicted`). Otherwise the entry is
   `put` into L2 with the same `expireAt` and returned.
3. On an L1 miss, L2 is peeked. An expired L2 entry is dropped with one
   `OnEvicted` call and reported as a miss; a live one is moved to L2's MRU
   end via `adjust` and returned.

Consequences worth remembering:

- A single `Get` moves the entry from L1 to L2. A second `Get` is served from
  L2. Promotion into a full L2 evicts L2's LRU tail (capacity eviction with
  `OnEvicted`).
- Promotion adds bytes to L2 without running the byte-budget loop, so a bucket
  can transiently exceed `maxBucketBytes` between writes.
- A retired L1 slot keeps its key in the level's internal `hashMap` until an
  overflowing `put` recycles it, so effective L1 capacity dips under
  read-heavy churn.

`Delete` retires the key in both levels under the bucket lock and fires
`OnEvicted` once, preferring L1's value, then nils both value references. It
returns true when either level held the key.

`Clear`, `Len`, and `Walk` each iterate both levels of every bucket, taking one
bucket lock at a time, and deduplicate keys with a `map[string]struct{}`
(O(N) per bucket). `Walk` visits L2 first (`Level: 1`), then the L1 entries not
already seen (`Level: 0`); returning `false` from the callback stops only the
current level's traversal, so a `false` return does not stop the whole walk.

Expiry sweep: `NewStore` starts `cleanupLoop`, which on every `CleanupInterval`
tick collects keys whose `expireAt <= Now()` from both levels of every bucket
and deletes them. `Close` runs under `closeOnce`: it stops the ticker and
closes `closeCh`, which ends the goroutine. `Close` is safe to call repeatedly.

Clock: `Now() int64` reads an atomically stored coarse clock. A goroutine
started in `init()` writes `time.Now().UnixNano()`, then adds 100 ms nine times
with 100 ms sleeps, then sleeps once more — a full resynchronization with the
real clock roughly once per second and 100 ms granularity in between. This
goroutine has no stop mechanism and runs for the life of the process.

Exported helpers (in-package plumbing that happens to be exported):

```go
func HashBKRD(s string) (hash int32)      // BKDR hash, hash*131 + byte; may be negative
func MaskOfNextPowOf2(cap uint16) uint16  // next power of two minus one; 1 -> 0, 2 -> 1, 3 -> 3
func Now() int64                          // coarse atomic nanosecond clock
func Create(cap uint16) *cache            // raw single-level LRU factory; returns an unexported type
```

`Create` is exported but returns the unexported `*cache`, so it is only usable
from inside the package (and in-package tests such as `TestLRUHelpers`). Do not
build APIs on it.

### SingleFlightGroup

```go
type SingleFlightGroup struct {
    m sync.Map // unexported
}

func (g *SingleFlightGroup) Do(key string, fn func() (any, error)) (any, error)
```

The zero value is ready to use; `var g SingleFlightGroup` works and is how
both `Group` and the tests use it.

Mechanics: the caller allocates a `call`, does `wg.Add(1)`, and races on
`m.LoadOrStore(key, c)`. A follower (`loaded == true`) waits on the existing
call's `WaitGroup` and returns the leader's `(val, err)` verbatim — the same
`any` value, not a copy, so `fn` must not return a mutable value that callers
will write to. The leader defers `m.Delete(key)` and `c.wg.Done()`, so the key
is released as soon as `fn` returns and serial callers re-execute `fn`. That is
exactly why `Group.load` re-checks the cache inside the callback.

A panic inside `fn` is recovered and converted to
`fmt.Errorf("singleflight: panic during call: %v", r)` with a nil value; every
waiting caller receives that error and the key is released so later calls
succeed (`TestSingleFlightPanicIsRecovered`).

There is no `Forget` and no `DoChan`. `Do` does not take a context, so a slow
leader blocks all followers for the full duration of `fn` regardless of caller
cancellation.

## Distributed layer

### PeerPicker and Peer

```go
type PeerPicker interface {
    PickPeer(key string) (peer Peer, ok bool, self bool)
    Close() error
}

type Peer interface {
    Get(group string, key string) ([]byte, error)
    Set(ctx context.Context, group string, key string, value []byte) error
    Delete(group string, key string) (bool, error)
    Close() error
}
```

`PickPeer` contract as implemented and consumed:

- `(nil, false, false)` — the ring is empty, the key is empty, or the owner has
  no client. `Group` falls through to the local getter and skips peer sync.
- `(nil, true, true)` — the local node owns the key. `Group` loads locally with
  no network hop (`TestGroupSelfPickLoadsLocally`) and skips peer sync.
- `(peer, true, false)` — a remote owner. `Group` calls `peer.Get` on reads and
  forwards `Set`/`Delete` from the sync goroutine.

Note the asymmetry in `Peer`: only `Set` takes a context. `Get` and `Delete`
build their own.

`*Client` is the only `Peer` implementation in the package; the compile-time
assertion `var _ Peer = (*Client)(nil)` lives in `client.go`.

### ClientPicker

```go
type PickerOption func(*ClientPicker)

func WithServiceName(name string) PickerOption // default "swifty_cache" (defaultSvcName)

func NewClientPicker(addr string, opts ...PickerOption) (*ClientPicker, error)
func (p *ClientPicker) PickPeer(key string) (Peer, bool, bool)
func (p *ClientPicker) PrintPeers()
func (p *ClientPicker) Close() error
```

Construction order in `NewClientPicker`:

1. When `addr` starts with `:`, resolve the first non-loopback IPv4 interface
   with `getLocalIP()` and rewrite `addr` to `<ip><addr>`. This mirrors what
   `registerWithConfig` does before writing to etcd, so the node recognizes
   its own registration. A resolution failure returns
   `failed to resolve self address: %v`.
2. Build the picker with `svcName = "swifty_cache"`, an empty client map, and
   `NewConsistentHash()` (which uses `DefaultConHashConfig`, i.e. 50 replicas
   and CRC32-IEEE). There is no option to configure the picker's ring.
3. Apply the `PickerOption`s.
4. Create an etcd client from `DefaultRegisterConfig.Endpoints` and
   `DefaultRegisterConfig.DialTimeout`. There is no per-picker etcd endpoint
   option: mutate `DefaultRegisterConfig` before constructing the picker.
5. Add `addr` to the ring permanently, so keys owned by the local node resolve
   to `(nil, true, true)`.
6. Run `startServiceDiscovery`: one synchronous `fetchAllServices` (a 3-second
   prefix `Get` on `/services/<svcName>/`) to seed the peer set, then
   `go watchServiceChanges()`. A seeding failure cancels the context, closes
   the etcd client, and returns the error.

Discovery loop (`watchServiceChanges`):

- Each iteration calls `fetchAllServices` to reconcile the peer set and obtain
  the snapshot revision, then `watchOnce(rev)`.
- `watchOnce` creates a watcher on the prefix with
  `client_v3.WithRev(rev+1)` so no event is missed between snapshot and watch.
  It returns `true` (terminating the loop) when `p.ctx` is done, and `false`
  when the channel closes, the response is canceled, or `resp.Err()` is
  non-nil.
- After a `false` return the loop waits one second (or exits on ctx done) and
  starts over with a fresh full resync. This is what makes the picker survive
  etcd restarts and compaction.

Event handling:

- `addrFromEventKey(key)` strips `/services/<svcName>/` from the etcd key.
  Addresses always come from the key, never the value, because DELETE events
  carry an empty value (pinned by `TestAddrFromEventKey`).
- `EventTypePut` for an unknown address calls `set(addr)`: `NewClient`, then
  `consHash.Add(addr)` and `clients[addr] = client`. A `NewClient` failure is
  logged and the address is not added.
- `EventTypeDelete` for a known address closes the client and calls
  `remove(addr)` (`consHash.Remove` plus map delete).
- Events whose address is empty or equals `selfAddr` are skipped; self is
  already permanently in the ring.
- `fetchAllServices` additionally removes and closes clients for addresses that
  vanished from etcd since the last snapshot.

Concurrency and locking: `mu` guards `clients` and the ring membership
operations. `PickPeer` takes the read lock; `handleWatchEvents` and
`fetchAllServices` take the write lock for the whole batch, including
`NewClient` and `client.Close()` calls. `grpc.NewClient` does not block on
connectivity, so the stall is short, but heavy topology churn does serialize
against `PickPeer`.

`Close` cancels the discovery context, then under the write lock closes every
client and the etcd client, aggregating failures into a single
`errors while closing: %v` error. It does not wait for the discovery goroutine
to observe the cancellation.

`PrintPeers` logs the discovered addresses under the read lock; it does not
include `selfAddr`, which lives in the ring but not in `clients`.

### Client

```go
func NewClient(addr string, svcName string, etcdCli *client_v3.Client) (*Client, error)
func (c *Client) Get(group, key string) ([]byte, error)
func (c *Client) Set(ctx context.Context, group, key string, value []byte) error
func (c *Client) Delete(group, key string) (bool, error)
func (c *Client) Close() error
```

- `NewClient` calls `grpc.NewClient(addr, grpc.WithTransportCredentials(
  insecure.NewCredentials()), grpc.WithDefaultCallOptions(
  grpc.WaitForReady(true)))`. It does not dial eagerly and does not verify that
  `addr` is reachable, so it essentially only fails on a malformed target
  (`failed to create grpc client: %v`).
- `svcName` and `etcdCli` are stored on the struct and never read again.
  `NewClient` neither uses nor closes `etcdCli`; passing `nil` is safe and is
  what test code does.
- `Get` and `Delete` each build
  `context.WithTimeout(context.Background(), 3*time.Second)` and discard any
  caller context. Only `Set` honours the passed `ctx` (which `syncToPeers`
  gives a 3-second deadline, so writes are bounded too).
- Errors are wrapped with `%v`, not `%w`:
  `failed to get value from swifty_cache: %v`,
  `failed to set value to swifty_cache: %v`,
  `failed to delete value from swifty_cache: %v`. gRPC status codes are
  therefore not recoverable with `status.FromError` on the returned error.
- `Get` returns `resp.GetValue()`, so a nil response value becomes a nil slice
  rather than an error. `Delete` returns `resp.GetValue()` (bool).
- `Close` closes only the gRPC connection, and returns nil when `conn` is nil
  (which is how `TestClientMethods` closes a hand-built `&Client{grpcCli: ...}`).
- There is no TLS, no retry, no circuit breaker, and no per-call
  `WaitForReady` opt-out. `WaitForReady(true)` means calls to a down peer block
  until the 3-second deadline rather than failing fast.

### Server

```go
type ServerOptions struct {
    EtcdEndpoints []string
    DialTimeout   time.Duration
    MaxMsgSize    int // becomes grpc.MaxRecvMsgSize only
}

var DefaultServerOptions = &ServerOptions{
    EtcdEndpoints: []string{"localhost:2379"},
    DialTimeout:   5 * time.Second,
    MaxMsgSize:    4 << 20,
}

type ServerOption func(*ServerOptions)

func WithEtcdEndpoints(endpoints []string) ServerOption
func WithDialTimeout(timeout time.Duration) ServerOption

func NewServer(addr, svcName string, opts ...ServerOption) (*Server, error)
func (s *Server) Start() error
func (s *Server) Stop()
func (s *Server) Get(ctx context.Context, req *pb.Request) (*pb.ResponseForGet, error)
func (s *Server) Set(ctx context.Context, req *pb.Request) (*pb.ResponseForGet, error)
func (s *Server) Delete(ctx context.Context, req *pb.Request) (*pb.ResponseForDelete, error)
```

`Server` embeds `pb.UnimplementedSwiftyCacheServer` by value, so it satisfies
`pb.SwiftyCacheServer` and stays forward compatible.

`NewServer`:

- Copies `*DefaultServerOptions` by value, then applies the options. Mutating
  the global `DefaultServerOptions` before the call therefore changes the
  baseline for every later server.
- Creates an etcd client from the resolved endpoints and dial timeout. A
  failure returns `failed to create etcd client: %v` and no server.
- Builds `grpc.NewServer(grpc.MaxRecvMsgSize(options.MaxMsgSize))`. The send
  limit stays at the gRPC default, which also caps response sizes.
- Registers itself with `pb.RegisterSwiftyCacheServer`, registers a
  `health.Server`, and sets the serving status for `svcName` to `SERVING`.
- There is no TLS support: `ServerOptions` has no credential or certificate
  fields and no `ServerOption` configures transport security. Transport is
  plaintext on both ends.

`Start` listens on `addr` (`failed to listen: %v` on failure), spawns a
goroutine that calls the internal `registerWithConfig(svcName, addr, stopCh,
&RegisterConfig{Endpoints: opts.EtcdEndpoints, DialTimeout: opts.DialTimeout})`
so `WithEtcdEndpoints` is honoured for registration, and then blocks in
`grpcServer.Serve(lis)`. Registration failures are logged, not returned:
`Start` keeps serving a node that never joined the ring.

`Stop` runs under `sync.Once`: flip health to `NOT_SERVING` (so load balancers
drain first), close `stopCh` (which makes the registration goroutine revoke the
lease), `GracefulStop` the gRPC server, and close the server's etcd client.
`Serve` then returns nil, so `Start` unblocks with a nil error.

RPC handlers:

- All three look the group up with `GetGroup(req.Group)` and return
  `fmt.Errorf("group %s not found", req.Group)` when it is missing. The group
  must exist in the receiving process's registry.
- `Get` increments the group's `server_requests` counter, then calls
  `group.Get(withPeerRequest(ctx), req.Key)` so a local miss is served by the
  local getter and never re-forwarded (pinned by
  `TestPeerOriginatedGetDoesNotForward`). On success it returns
  `&pb.ResponseForGet{Value: view.ByteSlice()}`, a fresh copy of the bytes.
- `Set` marks the context (`if !isPeerRequest(ctx)`) and calls `group.Set`, so
  the receiver does not re-broadcast. It echoes `req.Value` back in a
  `ResponseForGet`.
- `Delete` calls `group.Delete(withPeerRequest(ctx), req.Key)` and returns
  `&pb.ResponseForDelete{Value: err == nil}, err` — both a response and the
  error, which gRPC will collapse to an error status when `err != nil`.
- The handlers use no `Server` state beyond the group registry, so a zero-value
  `&Server{}` answers RPCs. `group_test.go` and `regression_test.go` both rely
  on that to test handlers without a listener.

## Consistent hashing

```go
type ConHashConfig struct {
    DefaultReplicas int                      // virtual nodes per physical node
    HashFunc        func(data []byte) uint32

    // Deprecated: the auto-rebalancer was removed to keep the key-to-node
    // mapping stable (groupcache semantics). These fields are ignored.
    MinReplicas          int
    MaxReplicas          int
    LoadBalanceThreshold float64
}

var DefaultConHashConfig = &ConHashConfig{
    DefaultReplicas:      50,
    MinReplicas:          10,
    MaxReplicas:          200,
    HashFunc:             crc32.ChecksumIEEE,
    LoadBalanceThreshold: 0.25,
}

type ConHashOption func(*ConsistentHashMap)

func WithConsistentHashConfig(config *ConHashConfig) ConHashOption // nil is ignored

func NewConsistentHash(opts ...ConHashOption) *ConsistentHashMap
func (m *ConsistentHashMap) Add(nodes ...string) error
func (m *ConsistentHashMap) Remove(node string) error
func (m *ConsistentHashMap) Get(key string) string
func (m *ConsistentHashMap) GetStats() map[string]float64
```

`MinReplicas`, `MaxReplicas`, and `LoadBalanceThreshold` are still present on
the struct and still populated in `DefaultConHashConfig`, but no code reads
them. They exist purely for source compatibility.

Behaviour:

- `NewConsistentHash` defaults `config` to the shared `DefaultConHashConfig`
  pointer. Mutating that global changes every ring created afterwards — and,
  because the pointer is shared, every ring created before as well.
- Virtual nodes are keyed by
  `HashFunc(fmt.Appendf(nil, "%s-%d", node, i))` for `i` in
  `[0, DefaultReplicas)`. Replica counts are fixed; there is no background
  rebalancer, so `ConsistentHashMap` starts no goroutine and needs no `Close`.
- `Add` returns `errors.New("no nodes provided")` when called with zero
  arguments, skips empty node names, is a no-op for a node already in
  `nodeHashes`, skips virtual-node hash collisions with existing ring entries
  rather than stealing ownership, and re-sorts `keys` once at the end.
  `TestMapConcurrentAccess` pins the idempotent re-add.
- `Remove` returns `errors.New("invalid node")` for an empty name and
  `fmt.Errorf("node %s not found", node)` for an unknown one. It deletes
  exactly the hashes recorded in `nodeHashes[node]`, so collisions skipped at
  add time cannot leave dangling ring entries, and it filters `keys` in place
  so the slice stays sorted.
- `Get` returns `""` for an empty key or an empty ring. Otherwise it binary
  searches `keys` for the first hash `>= HashFunc(key)`, wrapping to index 0,
  and returns the owning node. It holds only the read lock and bumps the
  per-node counter and `totalRequests` atomically, so the hot path is not
  serialized.
- `GetStats` returns `map[node]share` where share is the node's cumulative
  request count divided by `totalRequests`. It returns an empty map before the
  first `Get`. Counters are never reset, and `Remove` deletes a node's counter
  without decrementing `totalRequests`, so after removals the shares no longer
  sum to 1.

Configuration hazards: a custom `ConHashConfig` with a nil `HashFunc` panics on
the first `Get`, and one with `DefaultReplicas <= 0` registers nodes in
`nodeHashes` and `nodeCounts` without placing any virtual node on the ring, so
`Get` keeps returning `""`. Neither case is validated.

Concurrency: `mu` guards `config`, `keys`, `hashMap`, `nodeHashes`, and
`nodeCounts`. `Add`/`Remove` take the write lock; `Get`/`GetStats` take the
read lock. `TestMapConcurrentAccess` runs 1200 `Get` calls against 20
add/remove cycles.

## etcd service registration

```go
type RegisterConfig struct {
    Endpoints   []string
    DialTimeout time.Duration
}

var DefaultRegisterConfig = &RegisterConfig{
    Endpoints:   []string{"localhost:2379"},
    DialTimeout: 5 * time.Second,
}

func Register(svcName, addr string, stopCh <-chan error) error
```

`Register` delegates to the internal `registerWithConfig` with
`DefaultRegisterConfig`. `Server.Start` calls `registerWithConfig` directly
with the server's own endpoints, so calling `Register` yourself is only needed
for custom setups.

Behaviour:

- Returns `errors.New("empty address")` for an empty `addr`.
- Creates its own etcd client, separate from `Server.etcdCli` and
  `ClientPicker.etcdCli`.
- When `addr[0] == ':'`, rewrites it to `<firstNonLoopbackIPv4>:<port>` using
  `getLocalIP()`. `NewClientPicker` applies the same rewrite, so handing the
  same `":port"` string to both sides stays consistent.
- `registerLease` grants a 10-second lease (5-second context), writes
  `/services/<svcName>/<addr>` with the address as the value under that lease,
  and starts `KeepAlive` on `context.Background()`.
- The registration goroutine (owner of the etcd client, which it closes on
  exit) selects on `stopCh` and the keepalive channel. On `stopCh` close it
  revokes the lease with a 3-second timeout and returns. When the keepalive
  channel closes it calls `reRegister`, which retries `registerLease` with
  exponential backoff starting at 1 second and doubling while below 30
  seconds, until it succeeds or `stopCh` closes. Nodes therefore rejoin
  automatically after an etcd hiccup or lease expiry.
- `stopCh` is `<-chan error`; only closing it (or sending any value) triggers
  the shutdown path. The `Server` uses an unbuffered `chan error` it only ever
  closes.
- The exported `Register` always reads the global `DefaultRegisterConfig`. Only
  the server's internal path honours `WithEtcdEndpoints`.

## Dashboard

```go
func DashboardHandler() func(ctx *swifty_http.Context, next func())
func StartDashboard(addr string)
```

`StartDashboard` is guarded by a package-level `sync.Once`. The first call
creates a `swifty_http` application, registers `GET /dashboard/ws` with
`DashboardHandler()`, and serves it in a goroutine. Later calls, including ones
with a different address, do nothing and log nothing. `NewGroup` calls it
automatically when `CacheOptions.DashboardAddr` is non-empty. There is no
shutdown function: the listener runs until the process exits.

`DashboardHandler` upgrades the request with
`ctx.Upgrade(&swifty_http.UpgradeOptions{ReadBufferSize: 4096,
WriteBufferSize: 4096})`, logs and returns on failure, and otherwise hands the
`*swifty_http.WSConn` to the internal `serveDashboardConn`. It never calls
`next()`, so it terminates the middleware chain. Mount it on an existing
application to share a port instead of opening a second listener.

Per connection, `serveDashboardConn` starts a 30-second heartbeat
(`ws.Heartbeat`) and two goroutines:

- A writer that sends one snapshot immediately, then every 2 seconds, and
  exits on a write error or `ws.Closed()`. On exit it stops the heartbeat and
  closes the connection.
- A reader that loops `ws.ReadJSON(&cmd)` and exits on the first error.

Wire format:

```json
{"type":"snapshot","groups":[
  {"name":"scores","stats":{...},"entries":[
    {"key":"player:42","size":16,"expire_at":1234567890,"level":0}
  ]}
]}
```

`buildSnapshot` walks `GetAllGroups()` and includes only groups whose
`DashboardEnabled()` is true. Each entry carries the group's full `Stats()` map
and its `Entries()` converted one-to-one from `Entry` to the JSON snapshot
struct.

Commands from the client:

```json
{"action":"delete","group":"scores","key":"player:42"}
```

`handleCommand` resolves the group with `GetGroup`, ignores it when missing or
dashboard-disabled, and for `action == "delete"` calls
`g.Delete(context.Background(), cmd.Key)` — which also triggers the normal
async peer sync. Unknown actions are logged and ignored. There is no
authentication and no origin check.

## Protobuf surface (pb/)

Definition (`pb/swifty.proto`, `package pb`, `option go_package = "./"`):

```protobuf
message Request           { string group = 1; string key = 2; bytes value = 3; }
message ResponseForGet    { bytes value = 1; }
message ResponseForDelete { bool  value = 1; }

service SwiftyCache {
  rpc Get   (Request) returns (ResponseForGet);
  rpc Set   (Request) returns (ResponseForGet);
  rpc Delete(Request) returns (ResponseForDelete);
}
```

Because `go_package` is `"./"`, the generated files declare `package __`. Always
import the directory with an explicit alias, exactly as the package itself
does:

```go
pb "github.com/hangtiancheng/swifty.go/swifty_cache/pb"
```

Exported surface:

```go
// messages
type Request struct { Group string; Key string; Value []byte /* + protoimpl fields */ }
func (x *Request) GetGroup() string
func (x *Request) GetKey() string
func (x *Request) GetValue() []byte

type ResponseForGet struct { Value []byte }
func (x *ResponseForGet) GetValue() []byte

type ResponseForDelete struct { Value bool }
func (x *ResponseForDelete) GetValue() bool

// each message also has Reset(), String(), ProtoMessage(),
// ProtoReflect() protoreflect.Message, and the deprecated
// Descriptor() ([]byte, []int)

// service
type SwiftyCacheClient interface {
    Get(ctx context.Context, in *Request, opts ...grpc.CallOption) (*ResponseForGet, error)
    Set(ctx context.Context, in *Request, opts ...grpc.CallOption) (*ResponseForGet, error)
    Delete(ctx context.Context, in *Request, opts ...grpc.CallOption) (*ResponseForDelete, error)
}
func NewSwiftyCacheClient(cc grpc.ClientConnInterface) SwiftyCacheClient

type SwiftyCacheServer interface {
    Get(context.Context, *Request) (*ResponseForGet, error)
    Set(context.Context, *Request) (*ResponseForGet, error)
    Delete(context.Context, *Request) (*ResponseForDelete, error)
    // unexported mustEmbedUnimplementedSwiftyCacheServer()
}
type UnimplementedSwiftyCacheServer struct{} // embed by value, not pointer
type UnsafeSwiftyCacheServer interface{}     // opts out of forward compatibility
func RegisterSwiftyCacheServer(s grpc.ServiceRegistrar, srv SwiftyCacheServer)

// descriptors and constants
var File_pb_swifty_proto protoreflect.FileDescriptor
var SwiftyCache_ServiceDesc grpc.ServiceDesc // ServiceName "pb.SwiftyCache", 3 unary methods, no streams
const SwiftyCache_Get_FullMethodName    = "/pb.SwiftyCache/Get"
const SwiftyCache_Set_FullMethodName    = "/pb.SwiftyCache/Set"
const SwiftyCache_Delete_FullMethodName = "/pb.SwiftyCache/Delete"
```

`SwiftyCacheClient` is an interface, which is what makes `client_test.go`'s
`fakeSwiftyCacheClient` possible: inject it into `&Client{grpcCli: fake}` and
test `Client` without a server.

Conventions when extending:

- All three RPCs share `Request`; `value` is only meaningful for `Set`. `Set`
  reuses `ResponseForGet`. Prefer dedicated messages for new RPCs over
  overloading these.
- The protocol carries no metadata fields (trace id, tenant, version). Add
  those through gRPC metadata and interceptors, not the message body.
- Regenerate with `protoc-gen-go` v1.36.x and `protoc-gen-go-grpc` v1.5.x to
  match the committed output.

## Internals that affect correctness

Lock scope:

- `groupsMu` covers only the registry map. `DestroyGroup` and
  `DestroyAllGroups` release it before calling `Group.Close`, which reacquires
  it, so no re-entrant deadlock is possible.
- `Group.peersMu` covers only the `peers` field.
- `Cache.mu` covers the `store` pointer, not the store's contents; the store
  shards internally, which is why `Add`/`Get` only need the read lock.
- `lruStore.locks[i]` covers both levels of bucket `i` plus that bucket's
  `bytes` and `nevict` counters. Nothing holds two bucket locks at once, so
  bucket ordering is irrelevant.
- `ClientPicker.mu` covers `clients` and ring mutations initiated by the
  picker. `ConsistentHashMap` has its own `mu`, so `PickPeer` takes two read
  locks in a fixed order (picker, then ring).

Goroutine lifecycle:

| Goroutine                       | Started by                    | Stopped by                                | Leaks when                                            |
| ------------------------------- | ----------------------------- | ----------------------------------------- | ----------------------------------------------------- |
| coarse clock updater            | package `init()` in `lru.go`  | never                                     | always alive; unavoidable, one per process            |
| `lruStore.cleanupLoop`          | `NewStore`                    | `Store.Close` (`closeOnce` + `closeCh`)   | you build a `Cache`/store and never `Close` it        |
| `Group.syncToPeers`             | each non-peer `Set`/`Delete`  | returns after the RPC or its 3s timeout   | never joined; `Group.Close` does not wait for it       |
| `ClientPicker.watchServiceChanges` | `NewClientPicker`          | `ClientPicker.Close` (context cancel)     | you drop the picker without `Close`                    |
| registration keepalive          | `registerWithConfig`          | `stopCh` close (revokes the lease)        | `stopCh` never closes; the etcd client is never closed |
| `Server.Start` registration     | `Server.Start`                | `Server.Stop` closes `stopCh`             | `Stop` is never called                                 |
| dashboard listener              | first `StartDashboard`        | never                                     | always, once started                                  |
| dashboard writer + reader       | each WebSocket connection     | write/read error or `ws.Closed()`         | the peer holds the socket open forever                 |

Shutdown ordering that actually releases everything for one node:
`Group.Close()` (stops the store sweep), then `ClientPicker.Close()` (stops
discovery, closes peer connections and its etcd client), then `Server.Stop()`
(drains health, revokes the lease, closes the server's etcd client). The
dashboard listener and the clock goroutine survive regardless.

Buffer reuse and copying:

- Values are cloned on every boundary crossing: into the cache (`Group.Set`,
  `loadData`, `getFromPeer`), and out of it (`ByteView.ByteSlice`,
  `Server.Get`). Callers can safely mutate any `[]byte` they hand in or get
  back.
- The LRU levels reuse fixed node arrays. Retiring an entry sets `v = nil` so
  the value becomes collectable even while the slot stays allocated.

Timeout interaction:

- Peer read: `Client.Get` hard-codes 3 seconds and ignores the caller context,
  so a caller's cancellation does not shorten it.
- Peer write: `syncToPeers` builds a 3-second context; `Client.Set` honours it.
- Singleflight has no timeout, so followers wait for the leader's full latency:
  worst case 3 seconds of peer RPC plus the getter's own latency.
- `Cache.AddWithExpiration` computes the remaining duration with the real clock
  (`time.Until`) and the store converts it against the coarse clock
  (`Now() + duration`). The two can differ by up to ~100 ms.

Zero values and omitted options:

- `SingleFlightGroup` zero value is usable.
- `&Server{}` answers RPCs (registry-only handlers) but cannot `Start` or
  `Stop` meaningfully — `Stop` would panic closing a nil `stopCh` channel.
- `Cache{}` built via a struct literal (not `NewCache`) has an all-zero
  `CacheOptions`, so the lazily created store falls back to `NewStore`'s
  defaults for bucket counts and cleanup, and to no byte budget
  (`MaxBytes == 0`).
- `NewGroup` with `cacheBytes <= 0` disables byte eviction; only per-level
  entry counts bound memory.
- A `Group` without peers is a pure local read-through cache: `getPeers()`
  returns nil, so `loadData` goes straight to the getter and `Set`/`Delete`
  never spawn a sync goroutine.

## Typical usage

### Single node, in process

```go
package main

import (
    "context"
    "fmt"
    "time"

    cache "github.com/hangtiancheng/swifty.go/swifty_cache"
)

func main() {
    ctx := context.Background()

    g := cache.NewGroup("scores", 64<<20, cache.GetterFunc(
        func(ctx context.Context, key string) ([]byte, error) {
            // Load from a database, remote API, and so on.
            return []byte("score-of-" + key), nil
        },
    ), cache.WithExpiration(5*time.Minute))
    defer g.Close()

    view, err := g.Get(ctx, "player:42")
    if err != nil {
        panic(err)
    }
    fmt.Println(view.String()) // score-of-player:42
}
```

### Tuning the store

```go
opts := cache.DefaultCacheOptions()
opts.MaxBytes = 256 << 20 // WithCacheOptions discards NewGroup's cacheBytes
opts.BucketCount = 64     // rounded up to a power of two
opts.CapPerBucket = 4096
opts.Level2Cap = 2048
opts.CleanupTime = 30 * time.Second
opts.OnEvicted = func(key string, value cache.Value) {
    log.Printf("evicted %s (%d bytes)", key, value.Len())
}

g := cache.NewGroup("inventory", 256<<20, loader, cache.WithCacheOptions(opts))
defer g.Close()
```

### Distributed, with etcd

Start and stop in this order:

1. `NewServer`, then `srv.Start()` in a goroutine.
2. `NewClientPicker` with the same `addr` string passed to `NewServer` (both
   sides normalize `":port"` identically).
3. `NewGroup`, then `g.RegisterPeers(picker)`.
4. Shut down with `g.Close()`, then `picker.Close()`, then `srv.Stop()`.

```go
package main

import (
    "context"
    "log"
    "time"

    cache "github.com/hangtiancheng/swifty.go/swifty_cache"
)

func main() {
    ctx := context.Background()

    srv, err := cache.NewServer(
        ":9001", "swifty_cache",
        cache.WithEtcdEndpoints([]string{"127.0.0.1:2379"}),
        cache.WithDialTimeout(5*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }
    go func() {
        if err := srv.Start(); err != nil {
            log.Printf("server stopped: %v", err)
        }
    }()

    // NewClientPicker and the exported Register both read this global.
    cache.DefaultRegisterConfig.Endpoints = []string{"127.0.0.1:2379"}

    picker, err := cache.NewClientPicker(":9001", cache.WithServiceName("swifty_cache"))
    if err != nil {
        srv.Stop()
        log.Fatal(err)
    }

    g := cache.NewGroup("scores", 64<<20, cache.GetterFunc(
        func(ctx context.Context, key string) ([]byte, error) {
            return []byte("loaded-" + key), nil
        },
    ), cache.WithExpiration(time.Minute))
    g.RegisterPeers(picker)

    view, err := g.Get(ctx, "player:42")
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("value=%s stats=%+v", view.String(), g.Stats())

    // Ordered shutdown.
    _ = g.Close()
    _ = picker.Close()
    srv.Stop()
}
```

### Error handling

```go
view, err := g.Get(ctx, key)
switch {
case errors.Is(err, cache.ErrGroupClosed):
    // The group was closed; do not retry against this handle.
    return nil, err
case errors.Is(err, cache.ErrKeyRequired):
    return nil, fmt.Errorf("bad request: %w", err)
case err != nil:
    // Everything else is a loader failure, wrapped as
    // "failed to get data: <getter error>". A peer failure never reaches
    // here: it is logged and falls back to the local getter.
    return nil, err
}
return view.ByteSlice(), nil
```

```go
// Set rejects empty values; Delete never reports "not found".
if err := g.Set(ctx, key, payload); errors.Is(err, cache.ErrValueRequired) {
    // Use Delete to represent absence instead of an empty value.
    return g.Delete(ctx, key)
}
```

Peer sync failures are invisible to the caller by design. To detect them, watch
the `[SwiftyCache] failed to sync ...` log lines or the `peer_misses` counter
in `Group.Stats`.

### Graceful shutdown driven by a signal

```go
func run(srv *cache.Server, picker *cache.ClientPicker, g *cache.Group) {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    // Close the group first so the store's cleanup goroutine exits.
    if err := g.Close(); err != nil {
        log.Printf("group close: %v", err)
    }
    // Then discovery: cancels the etcd watch and closes every peer client.
    if err := picker.Close(); err != nil {
        log.Printf("picker close: %v", err)
    }
    // Finally the server: NOT_SERVING, lease revoke, GracefulStop.
    srv.Stop()
}
```

### Dashboard on its own port

```go
opts := cache.DefaultCacheOptions()
opts.MaxBytes = 32 << 20 // WithCacheOptions replaces the cache, so set this here
opts.DashboardAddr = "127.0.0.1:8080"

g := cache.NewGroup("inventory", 32<<20, cache.GetterFunc(
    func(ctx context.Context, key string) ([]byte, error) {
        return []byte("item-" + key), nil
    },
), cache.WithCacheOptions(opts), cache.WithExpiration(10*time.Minute))
defer g.Close()

// NewGroup already called StartDashboard; ws://127.0.0.1:8080/dashboard/ws is live.
log.Printf("dashboard enabled: %v", g.DashboardEnabled())
```

### Dashboard mounted on an existing application

```go
import (
    cache "github.com/hangtiancheng/swifty.go/swifty_cache"
    "github.com/hangtiancheng/swifty.go/swifty_http"
)

app := swifty_http.New()
app.Get("/admin/cache/ws", cache.DashboardHandler())
// The group still needs DashboardEnabled() == true to appear in snapshots,
// so set CacheOptions.DashboardAddr even when you mount the handler yourself.
log.Fatal(app.Listen(":8080"))
```

## Testing patterns

The suite needs neither etcd nor a live gRPC server. Reuse these harnesses:

- `group_test.go` defines `fakePeerPicker` (`peer Peer`, `ok bool`,
  `isSelf bool`) and `fakePeer` (per-method `getFunc`, `setFunc`,
  `deleteFunc`). They cover self-ownership, remote reads, peer failure
  fallback, write propagation, and the peer-request marker without any
  network.
- `client_test.go` defines `fakeSwiftyCacheClient` implementing
  `pb.SwiftyCacheClient`. Inject it with `&Client{grpcCli: fake}` to exercise
  `Client.Get`/`Set`/`Delete` on both happy and error paths. Note that a
  `Client` built this way has a nil `conn`, so `Close` returns nil.
- `lru_test.go` defines `type testValue string` with `Len()`, the minimal
  `Value` implementation for store tests.
- Both `group_test.go` and `regression_test.go` construct `&Server{}` directly
  to test RPC handlers against the group registry, with no listener.
- `regression_test.go` pins the invariants that previous bugs violated: byte
  budget enforcement, L2 invalidation on `Set`, exactly-once `OnEvicted` for an
  expired read, singleflight panic recovery, getter panic surfacing as an
  error, duplicate-registration panic, peer-originated non-forwarding,
  self-pick local load, the in-callback cache double-check,
  `addrFromEventKey` DELETE parsing, `Cache` close races, and stats counter
  alignment. Keep them passing.
- `single_flight_test.go` lives in the external test package
  `swifty_cache_test`, so it can only use exported API. The rest of the suite
  is in-package and reaches into `ByteView{b: ...}`, `group.mainCache`,
  `s.caches[0][0].bytes`, and `group.load`.

Always start a test that touches groups with `DestroyAllGroups()`. `NewGroup`
registers into a process-global map and panics on duplicate names, so a
leftover registration crashes a later test.

TTL tests use durations well above the coarse clock's granularity: set 150 ms
and sleep 400 ms. Anything tighter is flaky.

Run the suite from the module root:

```
cd swifty_cache && go test ./...
```

## Pitfalls and known limitations

1. `NewGroup` panics on a duplicate group name. The registry is process
   global and there is no replace-and-warn fallback. Call `DestroyGroup(name)`
   or `DestroyAllGroups()` before re-creating a group, and start group tests
   with `DestroyAllGroups()`.
2. `WithCacheOptions` replaces the whole `mainCache` and therefore discards the
   `cacheBytes` argument `NewGroup` had written into `CacheOptions.MaxBytes`.
   Set `opts.MaxBytes` explicitly or you silently fall back to whatever the
   options you passed contain (8 MiB from `DefaultCacheOptions`, or no budget
   at all from a bare `CacheOptions{}`).
3. TTLs below roughly 200 ms are unreliable. `Now()` advances in 100 ms steps
   and resynchronizes with the real clock about once per second, and
   `Cache.AddWithExpiration` computes the remaining duration on the real clock
   before the store converts it against the coarse clock. Use TTLs of
   seconds or more.
4. Writes are best effort. `Set`/`Delete` reach only the single owning peer,
   with one attempt, a 3-second timeout, no retry, and no acknowledgement.
   Non-owner replicas converge only through TTL, so always set
   `WithExpiration` in a cluster.
5. `Client.Get` and `Client.Delete` ignore the caller's context and use their
   own 3-second background timeout. Cancelling an inbound request does not
   cancel the peer read it triggered, and `WaitForReady(true)` means a down
   peer blocks for the full 3 seconds instead of failing fast.
6. Transport is plaintext in both directions: the client hard-codes
   `insecure.NewCredentials()` and the server has no TLS option. Do not run
   across untrusted networks without a sidecar or mesh.
7. The byte budget is per bucket (`MaxBytes / bucketCount`, clamped up to 1).
   A skewed key distribution evicts from a hot bucket while cold buckets sit
   far under budget, so effective global capacity can be well below
   `MaxBytes`.
8. Byte eviction runs only on the write path. Promoting an entry from L1 to L2
   on read adds bytes without triggering the eviction loop, so a bucket can
   exceed its budget until the next write to that bucket.
9. `ServerOptions.MaxMsgSize` sets only `grpc.MaxRecvMsgSize`. The send limit
   stays at the gRPC default (4 MiB), which caps `Get` responses regardless of
   how large you set `MaxMsgSize`.
10. The exported `Register` and `NewClientPicker` always read the global
    `DefaultRegisterConfig`; only `Server.Start` honours `WithEtcdEndpoints`.
    When etcd is not on `localhost:2379`, mutate `DefaultRegisterConfig`
    before constructing the picker or it will watch the wrong cluster while
    the server registers with the right one.
11. Each node holds three independent etcd clients: the registration
    goroutine's, `Server.etcdCli`, and `ClientPicker.etcdCli`. `NewClient`'s
    `etcdCli` parameter is dead weight — stored, never read, never closed —
    so passing `nil` is correct.
12. `StartDashboard` is a process-global `sync.Once` with no error path: only
    the first address ever wins, later addresses are silently ignored, and
    there is no way to stop the listener. The WebSocket endpoint has no
    authentication or origin check yet accepts `delete` commands, so bind it
    to loopback or front it with auth middleware.
13. `getLocalIP` returns the first non-loopback IPv4 interface address. On a
    multi-NIC or container host the registered address may be unreachable
    from peers. Prefer passing an explicit `host:port` over `":port"`.
14. L1 slots freed by a read or delete are only logically retired
    (`expireAt = 0`), and the key stays in the level's internal map until an
    overflowing `put` recycles the slot. Effective L1 capacity therefore dips
    under read-heavy churn.
15. `Clear`, `Len`, and the cleanup sweep walk both levels of every bucket
    under the bucket lock with map-based dedup (O(N) per bucket). Avoid
    calling `Clear` or `Len` on a hot path with a large cache — and note that
    `Cache.Stats` calls `Len`, so scraping stats frequently is not free.
16. `ClientPicker.handleWatchEvents` and `fetchAllServices` create and close
    gRPC clients while holding the picker write lock, so heavy topology churn
    briefly stalls `PickPeer`.
17. `SingleFlightGroup` has no `Forget`, no `DoChan`, and no context. A slow
    leader blocks every follower for the leader's full latency, and followers
    receive the leader's exact return value — never mutate it.
18. Values fetched from a remote peer are cached locally with no hot-key
    probability gate (there is no groupcache-style hotCache), so read-heavy
    shared keys get replicated on every node that touches them.
19. `ConHashConfig.MinReplicas`, `MaxReplicas`, and `LoadBalanceThreshold` are
    declared and populated in `DefaultConHashConfig` but completely ignored;
    the auto-rebalancer was removed to keep ownership stable. Setting them
    changes nothing.
20. `DefaultConHashConfig`, `DefaultRegisterConfig`, and
    `DefaultServerOptions` are exported pointers to shared mutable structs.
    `NewConsistentHash` stores the `DefaultConHashConfig` pointer itself, so a
    later mutation retroactively changes existing rings; `NewServer` copies
    `*DefaultServerOptions` by value, so only servers created afterwards see
    the change. Mutate them once during startup, before constructing anything.
21. A custom `ConHashConfig` with a nil `HashFunc` panics on the first `Get`,
    and one with `DefaultReplicas <= 0` places no virtual nodes on the ring so
    `Get` always returns `""`. Neither is validated; always copy
    `DefaultConHashConfig.HashFunc` when writing a custom config.
22. `ConsistentHashMap.GetStats` counters are cumulative and never reset, and
    `Remove` drops a node's counter without decrementing `totalRequests`, so
    after any removal the reported shares no longer sum to 1.
23. `Server.Start` logs but does not return registration failures, so a node
    can serve gRPC happily while absent from etcd and therefore from every
    peer's ring. Watch for `failed to register service` in the logs.
24. `Server.Delete` returns both a `*pb.ResponseForDelete` and the error;
    gRPC discards the message when the error is non-nil, so clients see only
    the status.
25. `Group.Set` rejects empty values with `ErrValueRequired`, but a `Getter`
    that returns zero-length bytes is cached as an empty `ByteView`. Empty
    values are reachable through the read path and not the write path.
26. `Group.Delete` returns `nil` whether or not the key existed; it discards
    `Cache.Delete`'s bool. Use `Cache.Delete` directly if you need to know.
27. `Group.Close` does not wait for in-flight `syncToPeers` goroutines, and
    those goroutines are unbounded — one per `Set`/`Delete`. A burst of writes
    to a slow peer can accumulate goroutines that each live up to 3 seconds.
28. Forgetting `Close` leaks goroutines: a `Cache` that was ever written to
    leaves its `cleanupLoop` and ticker running, and an unclosed
    `ClientPicker` leaves its etcd watch goroutine and every peer connection
    alive.
29. `ValidPeerAddr` is a weak validator — it accepts `localhost:<anything>` or
    any dotted-quad with any non-empty port string, with no port-range check
    and no IPv6 support. Nothing in the package calls it; it is opt-in.
30. `NewStore` returns the unexported `*lruStore`, and `Create` returns the
    unexported `*cache`. External code cannot name either type: declare
    `var s Store = NewStore(opts)` and do not build on `Create`.
31. `ListGroups` and `GetAllGroups` iterate a map, so ordering is
    non-deterministic. Sort before comparing or displaying.
32. The generated `pb` package declares `package __` because
    `option go_package = "./"`. Import it with an explicit alias or the
    resulting code is unreadable.

## File map

| File                          | Purpose                                                                                                                     |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `byte_view.go`                | Immutable `ByteView` value type (`Len`, `ByteSlice`, `String`) and the internal `cloneBytes`                                 |
| `store.go`                    | `Value` and `Store` interfaces, `StoreOptions`, `NewStoreOptions`                                                            |
| `lru.go`                      | Bucketed two-level LRU (`lruStore`, `NewStore`, `Bytes`, `Evictions`), `Entry`, the coarse clock `Now`, `HashBKRD`, `MaskOfNextPowOf2`, `Create`, cleanup loop |
| `cache.go`                    | `Cache` wrapper and `CacheOptions`/`DefaultCacheOptions`: lazy init, lock-guarded store access, hit/miss and byte stats, idempotent close |
| `group.go`                    | `Group`, `Getter`/`GetterFunc`, `GroupOption`s, sentinel errors, read-through with in-callback double-check, async peer sync, peer-request marker, global registry |
| `single_flight.go`            | `SingleFlightGroup.Do` with `sync.Map` fast path and panic recovery                                                          |
| `consistent_hash.go`          | `ConsistentHashMap`, `NewConsistentHash`, `ConHashOption`, `WithConsistentHashConfig`: fixed replicas, idempotent `Add`, collision-safe, atomic stats, no goroutine |
| `config.go`                   | `ConHashConfig` and `DefaultConHashConfig` (rebalancer fields deprecated and ignored)                                        |
| `peers.go`                    | `PeerPicker`/`Peer` interfaces and `ClientPicker`: self-in-ring, key-derived event addresses, resilient etcd watch loop      |
| `client.go`                   | `Client`, the gRPC `Peer` implementation (insecure transport, 3s timeouts on `Get`/`Delete`)                                 |
| `server.go`                   | `Server`, `ServerOptions`/`ServerOption`s, gRPC + health registration, `Start`/`Stop`, RPC handlers with peer-marker injection |
| `register.go`                 | `RegisterConfig`, `DefaultRegisterConfig`, `Register`, etcd lease + keepalive, backoff re-registration, `getLocalIP` rewrite |
| `dashboard.go`                | `StartDashboard`, `DashboardHandler`, snapshot/command WebSocket protocol over `swifty_http`                                 |
| `utils.go`                    | `ValidPeerAddr` (opt-in helper, unused internally)                                                                          |
| `regression_test.go`          | Regression tests pinning refactor invariants: byte budget, L2 invalidation, exactly-once `OnEvicted`, panic recovery, duplicate panic, non-forwarding, stats alignment |
| `group_test.go`               | Group, Cache, Server, and utility tests plus the `fakePeerPicker` / `fakePeer` harnesses                                     |
| `client_test.go`              | `Client` happy-path and error tests via the `fakeSwiftyCacheClient` stub                                                     |
| `lru_test.go`                 | Store and single-level LRU tests, helper assertions, `testValue` `Value` implementation                                      |
| `consistent_hash_test.go`     | Ring add/get/remove/stats and concurrent churn tests                                                                        |
| `single_flight_test.go`       | External-package singleflight tests: value/error propagation and duplicate suppression                                      |
| `pb/swifty.proto`             | Protocol definition (`package pb`, `go_package = "./"`)                                                                     |
| `pb/swifty.pb.go`             | Generated message types `Request`, `ResponseForGet`, `ResponseForDelete` and `File_pb_swifty_proto`                         |
| `pb/swifty_grpc.pb.go`        | Generated gRPC client/server stubs, `SwiftyCache_ServiceDesc`, full-method-name constants                                    |

## Dependencies

Direct requires in `swifty_cache/go.mod` (`go 1.26.0`):

- `github.com/hangtiancheng/swifty.go/swifty_http v0.0.2` — the dashboard's
  HTTP application, routing, and WebSocket upgrade. Resolved through a
  `replace` to `../swifty_http`; sibling `replace` directives also point
  `swifty_orm` and `swifty_rpc` at local paths even though this module does
  not require them.
- `google.golang.org/grpc v1.82.1` — peer transport (`grpc.NewClient`,
  `grpc.NewServer`, `insecure` credentials) plus `health` and
  `health/grpc_health_v1` for the serving-status endpoint.
- `google.golang.org/protobuf v1.36.11` — runtime for the generated messages.
- `go.etcd.io/etcd/client/v3 v3.7.0` — service registration (lease, keepalive,
  revoke) and discovery (prefix get, watch).

Everything else in `go.mod` is marked `// indirect`, including
`go.etcd.io/etcd/api/v3`, `go.etcd.io/etcd/client/pkg/v3`, `go.uber.org/zap`,
`go.opentelemetry.io/otel`, and the `golang.org/x` and `genproto` modules.

Standard library beyond those: `context`, `errors`, `fmt`, `hash/crc32`, `log`,
`maps`, `math`, `net`, `sort`, `strings`, `sync`, `sync/atomic`, `time`.

## Cross-references

- `swifty-http` — `dashboard.go` is the single integration point. It uses
  `swifty_http.New()`, `Application.Get`, `Application.Listen`,
  `Context.Upgrade`, `UpgradeOptions{ReadBufferSize, WriteBufferSize}`, and
  `WSConn` (`WriteJSON`, `ReadJSON`, `Heartbeat`, `Closed`, `Close`). Load that
  skill when changing the dashboard or mounting `DashboardHandler()` on a
  shared application.
- `swifty-orm` — load when the `Getter` behind a `Group` reads cache misses
  from MongoDB through the ORM engine. There is no compile-time dependency.
- `swifty-rpc` — load when exposing cache groups through a swifty_rpc service.
  swifty_cache's own peer transport is plain gRPC and does not use swifty_rpc.
