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
	"errors"
	"fmt"
	"log"
	"maps"
	"sync"
	"sync/atomic"
	"time"
)

var (
	groupsMu sync.RWMutex
	groups   = make(map[string]*Group)
)

var ErrKeyRequired = errors.New("key is required")
var ErrValueRequired = errors.New("value is required")
var ErrGroupClosed = errors.New("cache group is closed")

type peerRequestContextKey struct{}

// Getter loads data for a key.
type Getter interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

// GetterFunc adapts a function to the Getter interface.
type GetterFunc func(ctx context.Context, key string) ([]byte, error)

// Get implements Getter.
func (f GetterFunc) Get(ctx context.Context, key string) ([]byte, error) {
	return f(ctx, key)
}

// Group is a cache namespace with an associated data loader.
type Group struct {
	name       string
	getter     Getter
	mainCache  *Cache
	peersMu    sync.RWMutex
	peers      PeerPicker
	loader     *SingleFlightGroup
	expiration time.Duration
	closed     atomic.Int32
	stats      groupStats
}

type groupStats struct {
	gets           atomic.Int64
	loads          atomic.Int64
	loadsDeduped   atomic.Int64
	localHits      atomic.Int64
	localMisses    atomic.Int64
	peerHits       atomic.Int64
	peerMisses     atomic.Int64
	loaderHits     atomic.Int64
	loaderErrors   atomic.Int64
	serverRequests atomic.Int64
	loadDuration   atomic.Int64
}

// GroupOption configures a Group.
type GroupOption func(*Group)

// WithExpiration sets the default cache entry TTL. A zero value means no TTL.
func WithExpiration(d time.Duration) GroupOption {
	return func(g *Group) {
		g.expiration = d
	}
}

// WithPeers configures distributed peers for the group.
func WithPeers(peers PeerPicker) GroupOption {
	return func(g *Group) {
		g.peers = peers
	}
}

// WithCacheOptions replaces the default cache options.
func WithCacheOptions(opts CacheOptions) GroupOption {
	return func(g *Group) {
		g.mainCache = NewCache(opts)
	}
}

// NewGroup creates and registers a cache group.
func NewGroup(name string, cacheBytes int64, getter Getter, opts ...GroupOption) *Group {
	if getter == nil {
		panic("nil Getter")
	}

	cacheOpts := DefaultCacheOptions()
	cacheOpts.MaxBytes = cacheBytes

	g := &Group{
		name:      name,
		getter:    getter,
		mainCache: NewCache(cacheOpts),
		loader:    &SingleFlightGroup{},
	}

	for _, opt := range opts {
		opt(g)
	}

	groupsMu.Lock()
	defer groupsMu.Unlock()

	if _, exists := groups[name]; exists {
		panic("duplicate registration of group " + name)
	}

	groups[name] = g
	log.Printf("Created cache group [%s] with cacheBytes=%d, expiration=%v", name, cacheBytes, g.expiration)

	if addr := g.mainCache.opts.DashboardAddr; addr != "" {
		StartDashboard(addr)
	}

	return g
}

// GetGroup returns the group registered with name, or nil when it does not exist.
func GetGroup(name string) *Group {
	groupsMu.RLock()
	defer groupsMu.RUnlock()
	return groups[name]
}

// Get returns a value from cache or loads it on miss.
func (g *Group) Get(ctx context.Context, key string) (ByteView, error) {
	if g.closed.Load() == 1 {
		return ByteView{}, ErrGroupClosed
	}
	if key == "" {
		return ByteView{}, ErrKeyRequired
	}

	g.stats.gets.Add(1)

	view, ok := g.mainCache.Get(ctx, key)
	if ok {
		g.stats.localHits.Add(1)
		return view, nil
	}

	g.stats.localMisses.Add(1)
	return g.load(ctx, key)
}

// Set stores a value in the local cache and optionally syncs it to the owning peer.
func (g *Group) Set(ctx context.Context, key string, value []byte) error {
	if g.closed.Load() == 1 {
		return ErrGroupClosed
	}
	if key == "" {
		return ErrKeyRequired
	}
	if len(value) == 0 {
		return ErrValueRequired
	}

	view := ByteView{b: cloneBytes(value)}
	if g.expiration > 0 {
		g.mainCache.AddWithExpiration(key, view, time.Now().Add(g.expiration))
	} else {
		g.mainCache.Add(key, view)
	}

	if !isPeerRequest(ctx) {
		if peers := g.getPeers(); peers != nil {
			go g.syncToPeers(peers, "set", key, value)
		}
	}

	return nil
}

// Delete removes a value from the local cache and optionally syncs the deletion.
func (g *Group) Delete(ctx context.Context, key string) error {
	if g.closed.Load() == 1 {
		return ErrGroupClosed
	}
	if key == "" {
		return ErrKeyRequired
	}

	g.mainCache.Delete(key)

	if !isPeerRequest(ctx) {
		if peers := g.getPeers(); peers != nil {
			go g.syncToPeers(peers, "delete", key, nil)
		}
	}

	return nil
}

const peerSyncTimeout = 3 * time.Second

func (g *Group) syncToPeers(peers PeerPicker, op string, key string, value []byte) {
	peer, ok, isSelf := peers.PickPeer(key)
	if !ok || isSelf || peer == nil {
		return
	}

	syncCtx, cancel := context.WithTimeout(withPeerRequest(context.Background()), peerSyncTimeout)
	defer cancel()

	var err error
	switch op {
	case "set":
		err = peer.Set(syncCtx, g.name, key, value)
	case "delete":
		_, err = peer.Delete(g.name, key)
	default:
		err = fmt.Errorf("unsupported peer sync operation %q", op)
	}

	if err != nil {
		log.Printf("[SwiftyCache] failed to sync %s to peer: %v", op, err)
	}
}

// Clear removes all local cached values.
func (g *Group) Clear() {
	if g.closed.Load() == 1 {
		return
	}
	g.mainCache.Clear()
	log.Printf("[SwiftyCache] cleared cache for group [%s]", g.name)
}

// Close closes the group and removes it from the global registry.
func (g *Group) Close() error {
	if !g.closed.CompareAndSwap(0, 1) {
		return nil
	}

	if g.mainCache != nil {
		g.mainCache.Close()
	}

	groupsMu.Lock()
	delete(groups, g.name)
	groupsMu.Unlock()

	log.Printf("[SwiftyCache] closed cache group [%s]", g.name)
	return nil
}

func (g *Group) load(ctx context.Context, key string) (ByteView, error) {
	startTime := time.Now()
	viewInterface, err := g.loader.Do(key, func() (any, error) {
		// Singleflight only dedups concurrent overlapping calls; two serial
		// callers can both miss the cache, so check again before loading.
		if view, ok := g.mainCache.Get(ctx, key); ok {
			return view, nil
		}

		g.stats.loadsDeduped.Add(1)

		view, err := g.loadData(ctx, key)
		if err != nil {
			return ByteView{}, err
		}

		if g.expiration > 0 {
			g.mainCache.AddWithExpiration(key, view, time.Now().Add(g.expiration))
		} else {
			g.mainCache.Add(key, view)
		}
		return view, nil
	})

	loadDuration := time.Since(startTime).Nanoseconds()
	g.stats.loadDuration.Add(loadDuration)
	g.stats.loads.Add(1)

	if err != nil {
		g.stats.loaderErrors.Add(1)
		return ByteView{}, err
	}

	view, ok := viewInterface.(ByteView)
	if !ok {
		g.stats.loaderErrors.Add(1)
		return ByteView{}, fmt.Errorf("unexpected load result type %T", viewInterface)
	}
	return view, nil
}

func (g *Group) loadData(ctx context.Context, key string) (ByteView, error) {
	// Requests forwarded by a peer must be answered locally, otherwise
	// transiently inconsistent hash rings can bounce a key between nodes.
	if peers := g.getPeers(); peers != nil && !isPeerRequest(ctx) {
		peer, ok, isSelf := peers.PickPeer(key)
		if ok && !isSelf && peer != nil {
			value, err := g.getFromPeer(ctx, peer, key)
			if err == nil {
				g.stats.peerHits.Add(1)
				return value, nil
			}

			g.stats.peerMisses.Add(1)
			log.Printf("[SwiftyCache] failed to get from peer: %v", err)
		}
	}

	bytes, err := g.getter.Get(ctx, key)
	if err != nil {
		return ByteView{}, fmt.Errorf("failed to get data: %w", err)
	}

	g.stats.loaderHits.Add(1)
	return ByteView{b: cloneBytes(bytes)}, nil
}

func (g *Group) getFromPeer(ctx context.Context, peer Peer, key string) (ByteView, error) {
	bytes, err := peer.Get(g.name, key)
	if err != nil {
		return ByteView{}, fmt.Errorf("failed to get from peer: %w", err)
	}
	return ByteView{b: cloneBytes(bytes)}, nil
}

// RegisterPeers registers a PeerPicker. It may only be called once.
func (g *Group) RegisterPeers(peers PeerPicker) {
	g.peersMu.Lock()
	defer g.peersMu.Unlock()
	if g.peers != nil {
		panic("RegisterPeers called more than once")
	}
	g.peers = peers
	log.Printf("[SwiftyCache] registered peers for group [%s]", g.name)
}

func (g *Group) getPeers() PeerPicker {
	g.peersMu.RLock()
	defer g.peersMu.RUnlock()
	return g.peers
}

// Stats returns a snapshot of group and cache statistics.
func (g *Group) Stats() map[string]any {
	stats := map[string]any{
		"name":            g.name,
		"closed":          g.closed.Load() == 1,
		"expiration":      g.expiration,
		"gets":            g.stats.gets.Load(),
		"loads":           g.stats.loads.Load(),
		"loads_deduped":   g.stats.loadsDeduped.Load(),
		"local_hits":      g.stats.localHits.Load(),
		"local_misses":    g.stats.localMisses.Load(),
		"peer_hits":       g.stats.peerHits.Load(),
		"peer_misses":     g.stats.peerMisses.Load(),
		"loader_hits":     g.stats.loaderHits.Load(),
		"loader_errors":   g.stats.loaderErrors.Load(),
		"server_requests": g.stats.serverRequests.Load(),
	}

	totalGets := stats["local_hits"].(int64) + stats["local_misses"].(int64)
	if totalGets > 0 {
		stats["hit_rate"] = float64(stats["local_hits"].(int64)) / float64(totalGets)
	}

	totalLoads := stats["loads"].(int64)
	if totalLoads > 0 {
		stats["avg_load_time_ms"] = float64(g.stats.loadDuration.Load()) / float64(totalLoads) / float64(time.Millisecond)
	}

	if g.mainCache != nil {
		cacheStats := g.mainCache.Stats()
		for k, v := range cacheStats {
			stats["cache_"+k] = v
		}
	}

	return stats
}

// DashboardEnabled reports whether the dashboard is enabled for this group.
func (g *Group) DashboardEnabled() bool {
	return g.mainCache != nil && g.mainCache.DashboardEnabled()
}

// Entries returns all live entries in the group's local cache.
func (g *Group) Entries() []Entry {
	if g.closed.Load() == 1 || g.mainCache == nil {
		return nil
	}
	return g.mainCache.Entries()
}

// GetAllGroups returns a snapshot of all registered groups.
func GetAllGroups() map[string]*Group {
	groupsMu.RLock()
	defer groupsMu.RUnlock()

	result := make(map[string]*Group, len(groups))
	maps.Copy(result, groups)
	return result
}

// ListGroups returns all registered group names.
func ListGroups() []string {
	groupsMu.RLock()
	defer groupsMu.RUnlock()

	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	return names
}

// DestroyGroup closes and removes a named group.
func DestroyGroup(name string) bool {
	groupsMu.Lock()
	g, exists := groups[name]
	if exists {
		delete(groups, name)
	}
	groupsMu.Unlock()

	if !exists {
		return false
	}
	_ = g.Close()
	log.Printf("[SwiftyCache] destroyed cache group [%s]", name)
	return true
}

// DestroyAllGroups closes and removes every registered group.
func DestroyAllGroups() {
	groupsMu.Lock()
	toClose := make([]*Group, 0, len(groups))
	for _, g := range groups {
		toClose = append(toClose, g)
	}
	groups = make(map[string]*Group)
	groupsMu.Unlock()

	for _, g := range toClose {
		_ = g.Close()
		log.Printf("[SwiftyCache] destroyed cache group [%s]", g.name)
	}
}

func withPeerRequest(ctx context.Context) context.Context {
	return context.WithValue(ctx, peerRequestContextKey{}, true)
}

func isPeerRequest(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, ok := ctx.Value(peerRequestContextKey{}).(bool)
	return ok && value
}
