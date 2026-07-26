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
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/hangtiancheng/swifty.go/swifty_cache/pb"
)

// MaxBytes must bound the total bytes per bucket (groupcache's cacheBytes contract).
func TestLRUStoreMaxBytesEviction(t *testing.T) {
	s := NewStore(StoreOptions{
		MaxBytes:        64, // single bucket -> 64 bytes budget
		BucketCount:     1,
		CapPerBucket:    64,
		Level2Cap:       64,
		CleanupInterval: time.Hour,
	})
	defer s.Close()

	// Each entry is 4 (key) + 12 (value) = 16 bytes; 5 entries exceed 64.
	for i := range 5 {
		key := fmt.Sprintf("k%03d", i)
		if err := s.Set(key, testValue(strings.Repeat("v", 12))); err != nil {
			t.Fatalf("Set returned error: %v", err)
		}
	}

	total := s.caches[0][0].bytes + s.caches[0][1].bytes
	if total > 64 {
		t.Fatalf("bucket bytes = %d, want <= 64", total)
	}
	if _, ok := s.Get("k000"); ok {
		t.Fatal("oldest entry should have been evicted by the byte budget")
	}
	if _, ok := s.Get("k004"); !ok {
		t.Fatal("newest entry should survive")
	}
}

// A Set must invalidate the stale copy previously promoted to L2, otherwise the
// old value can resurface after the L1 slot is recycled.
func TestLRUStoreSetInvalidatesL2Copy(t *testing.T) {
	s := NewStore(StoreOptions{BucketCount: 1, CapPerBucket: 2, Level2Cap: 4, CleanupInterval: time.Hour})
	defer s.Close()

	if err := s.Set("k", testValue("v1")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if _, ok := s.Get("k"); !ok { // promotes v1 to L2
		t.Fatal("Get(k) should hit")
	}
	if err := s.Set("k", testValue("v2")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	// Churn L1 (cap=2) until k's L1 slot is recycled.
	if err := s.Set("x1", testValue("v")); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("x2", testValue("v")); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("x3", testValue("v")); err != nil {
		t.Fatal(err)
	}

	if got, ok := s.Get("k"); ok && got == testValue("v1") {
		t.Fatal("stale L2 value v1 resurfaced after Set(k, v2)")
	}
}

// An expired L1 entry must fire OnEvicted exactly once on read.
func TestLRUStoreExpiredReadFiresOnEvicted(t *testing.T) {
	var mu sync.Mutex
	var evicted []string
	s := NewStore(StoreOptions{
		BucketCount:     1,
		CapPerBucket:    4,
		Level2Cap:       4,
		CleanupInterval: time.Hour,
		OnEvicted: func(key string, value Value) {
			mu.Lock()
			evicted = append(evicted, key)
			mu.Unlock()
		},
	})
	defer s.Close()

	if err := s.SetWithExpiration("gone", testValue("v"), 150*time.Millisecond); err != nil {
		t.Fatalf("SetWithExpiration returned error: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if _, ok := s.Get("gone"); ok {
		t.Fatal("expired key returned a hit")
	}

	mu.Lock()
	defer mu.Unlock()
	count := 0
	for _, k := range evicted {
		if k == "gone" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("OnEvicted fired %d times for expired key, want 1", count)
	}
}

// A panic inside the singleflight fn must surface as an error to all callers.
func TestSingleFlightPanicIsRecovered(t *testing.T) {
	var g SingleFlightGroup

	_, err := g.Do("boom", func() (any, error) {
		panic("kaboom")
	})
	if err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("expected panic error, got %v", err)
	}

	// The key must be released for subsequent calls.
	v, err := g.Do("boom", func() (any, error) { return "ok", nil })
	if err != nil || v != "ok" {
		t.Fatalf("Do after panic = %v, %v", v, err)
	}
}

// A panicking Getter must return an error from Group.Get, not crash callers.
func TestGroupGetterPanicReturnsError(t *testing.T) {
	DestroyAllGroups()
	group := NewGroup("group-panic", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		panic("getter exploded")
	}))
	defer group.Close()

	if _, err := group.Get(context.Background(), "key"); err == nil {
		t.Fatal("expected error from panicking getter")
	}
}

// Duplicate group registration must panic, matching groupcache.
func TestNewGroupPanicsOnDuplicate(t *testing.T) {
	DestroyAllGroups()
	getter := GetterFunc(func(ctx context.Context, key string) ([]byte, error) { return []byte("v"), nil })
	g := NewGroup("group-dup", 1024, getter)
	defer g.Close()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	NewGroup("group-dup", 1024, getter)
}

// Peer-originated reads must be answered locally, never re-forwarded.
func TestPeerOriginatedGetDoesNotForward(t *testing.T) {
	DestroyAllGroups()
	group := NewGroup("group-peer-get", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return []byte("local"), nil
	}))
	defer group.Close()
	group.RegisterPeers(&fakePeerPicker{peer: &fakePeer{getFunc: func(group, key string) ([]byte, error) {
		return nil, errors.New("must not forward a peer request")
	}}, ok: true})

	view, err := group.Get(withPeerRequest(context.Background()), "key")
	if err != nil || view.String() != "local" {
		t.Fatalf("peer-originated Get = %q, %v", view.String(), err)
	}
	if group.Stats()["peer_misses"].(int64) != 0 {
		t.Fatalf("peer request was forwarded: %+v", group.Stats())
	}
}

// PickPeer returning (nil, true, true) for self must load locally.
func TestGroupSelfPickLoadsLocally(t *testing.T) {
	DestroyAllGroups()
	loads := 0
	group := NewGroup("group-self-pick", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		loads++
		return []byte("local"), nil
	}))
	defer group.Close()
	group.RegisterPeers(&fakePeerPicker{peer: nil, ok: true, isSelf: true})

	view, err := group.Get(context.Background(), "key")
	if err != nil || view.String() != "local" || loads != 1 {
		t.Fatalf("self-owned Get = %q, %v, loads=%d", view.String(), err, loads)
	}
}

// Serial duplicate loads must hit the in-callback cache double-check.
func TestLoadDoubleChecksCache(t *testing.T) {
	DestroyAllGroups()
	loads := 0
	group := NewGroup("group-double-check", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		loads++
		return []byte("v"), nil
	}))
	defer group.Close()

	// Preload the cache, then invoke load directly: the callback must see the
	// cached value and skip the getter.
	if _, err := group.Get(context.Background(), "key"); err != nil {
		t.Fatal(err)
	}
	view, err := group.load(context.Background(), "key")
	if err != nil || view.String() != "v" {
		t.Fatalf("load = %q, %v", view.String(), err)
	}
	if loads != 1 {
		t.Fatalf("getter ran %d times, want 1", loads)
	}
}

// addrFromEventKey must extract addresses from etcd keys (DELETE events carry no value).
func TestAddrFromEventKey(t *testing.T) {
	p := &ClientPicker{svcName: "svc"}
	if got := p.addrFromEventKey([]byte("/services/svc/10.0.0.1:9001")); got != "10.0.0.1:9001" {
		t.Fatalf("addrFromEventKey = %q", got)
	}
}

// Concurrent Add/Get/Delete racing with Close must never panic on a nil store.
func TestCacheConcurrentUseWithClose(t *testing.T) {
	for range 20 {
		c := NewCache(CacheOptions{MaxBytes: 1 << 20, CleanupTime: time.Hour})
		var wg sync.WaitGroup
		for w := range 4 {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for j := range 50 {
					key := fmt.Sprintf("w%d-%d", w, j)
					c.Add(key, ByteView{b: []byte("v")})
					c.Get(context.Background(), key)
					c.Delete(key)
				}
			}(w)
		}
		c.Close()
		wg.Wait()
	}
}

// gets / loads_deduped / server_requests / cache bytes+evictions must be reported.
func TestStatsAlignmentCounters(t *testing.T) {
	DestroyAllGroups()
	ctx := context.Background()
	group := NewGroup("group-stats-align", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return []byte("value"), nil
	}))
	defer group.Close()

	if _, err := group.Get(ctx, "key"); err != nil { // miss -> load
		t.Fatal(err)
	}
	if _, err := group.Get(ctx, "key"); err != nil { // hit
		t.Fatal(err)
	}

	stats := group.Stats()
	if stats["gets"].(int64) != 2 {
		t.Fatalf("gets = %v, want 2", stats["gets"])
	}
	if stats["loads"].(int64) != 1 || stats["loads_deduped"].(int64) != 1 {
		t.Fatalf("loads = %v, loads_deduped = %v, want 1/1", stats["loads"], stats["loads_deduped"])
	}
	if stats["server_requests"].(int64) != 0 {
		t.Fatalf("server_requests = %v, want 0", stats["server_requests"])
	}
	if stats["cache_bytes"].(int64) <= 0 {
		t.Fatalf("cache_bytes = %v, want > 0", stats["cache_bytes"])
	}
	if _, ok := stats["cache_evictions"]; !ok {
		t.Fatal("cache_evictions missing from stats")
	}

	srv := &Server{}
	if _, err := srv.Get(ctx, &pb.Request{Group: "group-stats-align", Key: "key"}); err != nil {
		t.Fatalf("server Get returned error: %v", err)
	}
	if got := group.Stats()["server_requests"].(int64); got != 1 {
		t.Fatalf("server_requests = %d, want 1", got)
	}
}
