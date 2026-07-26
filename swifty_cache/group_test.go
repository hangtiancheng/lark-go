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
	"sync"
	"testing"
	"time"

	pb "github.com/hangtiancheng/swifty.go/swifty_cache/pb"
)

func TestGetterFuncAndByteView(t *testing.T) {
	getter := GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return []byte(key), nil
	})
	got, err := getter.Get(context.Background(), "key")
	if err != nil || string(got) != "key" {
		t.Fatalf("GetterFunc returned %q, %v", string(got), err)
	}

	view := ByteView{b: []byte("value")}
	copyOne := view.ByteSlice()
	copyOne[0] = 'X'
	if view.String() != "value" || view.Len() != len("value") {
		t.Fatalf("ByteView was mutated or has wrong length: %q %d", view.String(), view.Len())
	}
	copyTwo := view.ByteSlice()
	copyTwo[0] = 'Y'
	if view.String() != "value" {
		t.Fatalf("ByteSlice did not return a copy: %q", view.String())
	}
}

func TestGroupGetCachesLocalValues(t *testing.T) {
	DestroyAllGroups()
	ctx := context.Background()
	loads := 0
	group := NewGroup("group-cache-local", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		loads++
		return []byte("value-" + key), nil
	}))
	defer group.Close()

	for range 2 {
		view, err := group.Get(ctx, "key")
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}
		if view.String() != "value-key" {
			t.Fatalf("Get returned %q", view.String())
		}
	}
	if loads != 1 {
		t.Fatalf("loader called %d times, want 1", loads)
	}

	stats := group.Stats()
	if stats["local_hits"].(int64) != 1 || stats["local_misses"].(int64) != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestGroupValidationAndClose(t *testing.T) {
	DestroyAllGroups()
	ctx := context.Background()
	group := NewGroup("group-validation", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return []byte("value"), nil
	}))

	if _, err := group.Get(ctx, ""); !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("Get empty key error = %v", err)
	}
	if err := group.Set(ctx, "", []byte("value")); !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("Set empty key error = %v", err)
	}
	if err := group.Set(ctx, "key", nil); !errors.Is(err, ErrValueRequired) {
		t.Fatalf("Set empty value error = %v", err)
	}
	if err := group.Delete(ctx, ""); !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("Delete empty key error = %v", err)
	}
	if err := group.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if _, err := group.Get(ctx, "key"); !errors.Is(err, ErrGroupClosed) {
		t.Fatalf("closed Get error = %v", err)
	}
	if err := group.Set(ctx, "key", []byte("value")); !errors.Is(err, ErrGroupClosed) {
		t.Fatalf("closed Set error = %v", err)
	}
	if err := group.Delete(ctx, "key"); !errors.Is(err, ErrGroupClosed) {
		t.Fatalf("closed Delete error = %v", err)
	}
}

func TestNewGroupPanicsWithNilGetter(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	NewGroup("nil-getter", 1024, nil)
}

func TestGroupSetDeleteClearAndExpiration(t *testing.T) {
	DestroyAllGroups()
	ctx := context.Background()
	group := NewGroup("group-set-delete", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return []byte("loaded"), nil
	}), WithExpiration(20*time.Millisecond))
	defer group.Close()

	if err := group.Set(ctx, "key", []byte("manual")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	view, err := group.Get(ctx, "key")
	if err != nil || view.String() != "manual" {
		t.Fatalf("Get after Set returned %q, %v", view.String(), err)
	}
	if err := group.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	view, err = group.Get(ctx, "key")
	if err != nil || view.String() != "loaded" {
		t.Fatalf("Get after Delete returned %q, %v", view.String(), err)
	}
	group.Clear()
	if group.mainCache.Len() != 0 {
		t.Fatalf("cache length after Clear = %d", group.mainCache.Len())
	}
}

func TestGroupUsesRemotePeerAndClonesBytes(t *testing.T) {
	DestroyAllGroups()
	ctx := context.Background()
	remote := []byte("remote")
	group := NewGroup("group-remote", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return nil, errors.New("local getter should not run")
	}))
	defer group.Close()
	group.RegisterPeers(&fakePeerPicker{peer: &fakePeer{getFunc: func(group, key string) ([]byte, error) {
		return remote, nil
	}}, ok: true})

	view, err := group.Get(ctx, "key")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	remote[0] = 'X'
	if view.String() != "remote" {
		t.Fatalf("remote value was not cloned: %q", view.String())
	}
}

func TestGroupFallsBackWhenPeerFails(t *testing.T) {
	DestroyAllGroups()
	ctx := context.Background()
	localLoads := 0
	group := NewGroup("group-peer-fallback", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		localLoads++
		return []byte("local"), nil
	}))
	defer group.Close()
	group.RegisterPeers(&fakePeerPicker{peer: &fakePeer{getFunc: func(group, key string) ([]byte, error) {
		return nil, errors.New("remote unavailable")
	}}, ok: true})

	view, err := group.Get(ctx, "key")
	if err != nil || view.String() != "local" {
		t.Fatalf("Get returned %q, %v", view.String(), err)
	}
	if localLoads != 1 {
		t.Fatalf("local loads = %d, want 1", localLoads)
	}
	if group.Stats()["peer_misses"].(int64) != 1 {
		t.Fatalf("unexpected stats: %+v", group.Stats())
	}
}

func TestGroupSetAndDeleteSyncToPeer(t *testing.T) {
	DestroyAllGroups()
	ctx := context.Background()
	setCh := make(chan string, 1)
	deleteCh := make(chan string, 1)
	peer := &fakePeer{
		setFunc: func(ctx context.Context, group, key string, value []byte) error {
			setCh <- fmt.Sprintf("%s/%s/%s", group, key, string(value))
			return nil
		},
		deleteFunc: func(group, key string) (bool, error) {
			deleteCh <- fmt.Sprintf("%s/%s", group, key)
			return true, nil
		},
	}
	group := NewGroup("group-sync", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return []byte("value"), nil
	}), WithPeers(&fakePeerPicker{peer: peer, ok: true}))
	defer group.Close()

	if err := group.Set(ctx, "key", []byte("value")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	select {
	case got := <-setCh:
		if got != "group-sync/key/value" {
			t.Fatalf("unexpected set sync %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for set sync")
	}

	if err := group.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	select {
	case got := <-deleteCh:
		if got != "group-sync/key" {
			t.Fatalf("unexpected delete sync %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delete sync")
	}

	if err := group.Set(withPeerRequest(ctx), "peer-key", []byte("value")); err != nil {
		t.Fatalf("peer Set returned error: %v", err)
	}
	select {
	case got := <-setCh:
		t.Fatalf("peer-originated set should not sync, got %q", got)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestRegisterPeersPanicsWhenCalledTwice(t *testing.T) {
	DestroyAllGroups()
	group := NewGroup("group-register-panic", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return []byte("value"), nil
	}))
	defer group.Close()
	group.RegisterPeers(&fakePeerPicker{})

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	group.RegisterPeers(&fakePeerPicker{})
}

func TestGroupSuppressesConcurrentLoads(t *testing.T) {
	DestroyAllGroups()
	ctx := context.Background()
	var (
		loads int
		mu    sync.Mutex
	)
	ready := make(chan struct{})
	release := make(chan struct{})
	group := NewGroup("group-singleflight", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		mu.Lock()
		loads++
		if loads == 1 {
			close(ready)
		}
		mu.Unlock()
		<-release
		return []byte("value"), nil
	}))
	defer group.Close()

	const callers = 16
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			view, err := group.Get(ctx, "key")
			if err != nil {
				errs <- err
				return
			}
			if view.String() != "value" {
				errs <- fmt.Errorf("unexpected value %q", view.String())
			}
		})
	}
	<-ready
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if loads != 1 {
		t.Fatalf("loads = %d, want 1", loads)
	}
}

func TestGroupRegistryOperationsDoNotDeadlock(t *testing.T) {
	DestroyAllGroups()
	group := NewGroup("group-destroy", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return []byte("value"), nil
	}))
	if GetGroup("group-destroy") != group {
		t.Fatal("GetGroup did not return created group")
	}
	if names := ListGroups(); len(names) != 1 || names[0] != "group-destroy" {
		t.Fatalf("ListGroups = %v", names)
	}

	done := make(chan bool, 1)
	go func() { done <- DestroyGroup("group-destroy") }()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("DestroyGroup returned false")
		}
	case <-time.After(time.Second):
		t.Fatal("DestroyGroup deadlocked")
	}
	if DestroyGroup("group-destroy") {
		t.Fatal("DestroyGroup returned true for missing group")
	}

	NewGroup("group-destroy-all-a", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) { return []byte("a"), nil }))
	NewGroup("group-destroy-all-b", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) { return []byte("b"), nil }))
	doneAll := make(chan struct{})
	go func() {
		DestroyAllGroups()
		close(doneAll)
	}()
	select {
	case <-doneAll:
	case <-time.After(time.Second):
		t.Fatal("DestroyAllGroups deadlocked")
	}
}

func TestCacheWrapper(t *testing.T) {
	cache := NewCache(CacheOptions{MaxBytes: 128, CleanupTime: time.Hour})
	ctx := context.Background()
	if _, ok := cache.Get(ctx, "missing"); ok {
		t.Fatal("empty cache returned a hit")
	}
	cache.Add("key", ByteView{b: []byte("value")})
	if view, ok := cache.Get(ctx, "key"); !ok || view.String() != "value" {
		t.Fatalf("cache Get returned %q, %v", view.String(), ok)
	}
	cache.AddWithExpiration("expired", ByteView{b: []byte("value")}, time.Now().Add(-time.Second))
	if _, ok := cache.Get(ctx, "expired"); ok {
		t.Fatal("expired value should not be added")
	}
	stats := cache.Stats()
	if stats["hits"].(int64) != 1 || stats["misses"].(int64) < 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if !cache.Delete("key") {
		t.Fatal("Delete returned false")
	}
	cache.Close()
	cache.Close()
	cache.Add("closed", ByteView{b: []byte("value")})
	if cache.Len() != 0 {
		t.Fatalf("closed cache length = %d", cache.Len())
	}
}

func TestOptionsAndCacheConfig(t *testing.T) {
	DestroyAllGroups()
	cacheOpts := DefaultCacheOptions()
	cacheOpts.MaxBytes = 64
	group := NewGroup("group-options", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return []byte("value"), nil
	}), WithCacheOptions(cacheOpts))
	defer group.Close()
	if group.mainCache.opts.MaxBytes != 64 {
		t.Fatalf("WithCacheOptions was not applied: %+v", group.mainCache.opts)
	}
	if isPeerRequest(nil) {
		t.Fatal("nil context should not be a peer request")
	}
}

func TestServerMethodsAndUtilities(t *testing.T) {
	DestroyAllGroups()
	ctx := context.Background()
	group := NewGroup("server-group", 1024, GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return []byte("value-" + key), nil
	}))
	defer group.Close()
	srv := &Server{}

	getResp, err := srv.Get(ctx, &pb.Request{Group: "server-group", Key: "key"})
	if err != nil || string(getResp.Value) != "value-key" {
		t.Fatalf("server Get returned %v, %v", getResp, err)
	}
	setResp, err := srv.Set(ctx, &pb.Request{Group: "server-group", Key: "key", Value: []byte("manual")})
	if err != nil || string(setResp.Value) != "manual" {
		t.Fatalf("server Set returned %v, %v", setResp, err)
	}
	deleteResp, err := srv.Delete(ctx, &pb.Request{Group: "server-group", Key: "key"})
	if err != nil || !deleteResp.Value {
		t.Fatalf("server Delete returned %v, %v", deleteResp, err)
	}
	if _, err := srv.Get(ctx, &pb.Request{Group: "missing", Key: "key"}); err == nil {
		t.Fatal("expected missing group error")
	}

	if !ValidPeerAddr("localhost:8001") || !ValidPeerAddr("127.0.0.1:8001") || ValidPeerAddr("bad") {
		t.Fatal("ValidPeerAddr returned unexpected results")
	}
}

type fakePeerPicker struct {
	peer   Peer
	ok     bool
	isSelf bool
}

func (p *fakePeerPicker) PickPeer(key string) (Peer, bool, bool) {
	return p.peer, p.ok, p.isSelf
}

func (p *fakePeerPicker) Close() error { return nil }

type fakePeer struct {
	getFunc    func(group, key string) ([]byte, error)
	setFunc    func(ctx context.Context, group, key string, value []byte) error
	deleteFunc func(group, key string) (bool, error)
}

func (p *fakePeer) Get(group, key string) ([]byte, error) {
	if p.getFunc != nil {
		return p.getFunc(group, key)
	}
	return nil, errors.New("get not implemented")
}

func (p *fakePeer) Set(ctx context.Context, group, key string, value []byte) error {
	if p.setFunc != nil {
		return p.setFunc(ctx, group, key, value)
	}
	return nil
}

func (p *fakePeer) Delete(group, key string) (bool, error) {
	if p.deleteFunc != nil {
		return p.deleteFunc(group, key)
	}
	return true, nil
}

func (p *fakePeer) Close() error { return nil }
