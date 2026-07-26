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
	"math"
	"sync"
	"sync/atomic"
	"time"
)

const maxExpireAt = math.MaxInt64

type Entry struct {
	Key      string
	Size     int
	ExpireAt int64
	Level    int
}

type lruStore struct {
	locks          []sync.Mutex
	caches         [][2]*cache
	onEvicted      func(key string, value Value)
	cleanupTick    *time.Ticker
	closeCh        chan struct{}
	closeOnce      sync.Once
	maxBucketBytes int64
	mask           int32
}

// NewStore creates a cache store implementation.
func NewStore(opts StoreOptions) *lruStore {
	if opts.BucketCount == 0 {
		opts.BucketCount = 16
	}
	if opts.CapPerBucket == 0 {
		opts.CapPerBucket = 512
	}
	if opts.Level2Cap == 0 {
		opts.Level2Cap = 256
	}
	if opts.CleanupInterval <= 0 {
		opts.CleanupInterval = time.Minute
	}

	mask := MaskOfNextPowOf2(opts.BucketCount)

	var maxBucketBytes int64
	if opts.MaxBytes > 0 {
		maxBucketBytes = opts.MaxBytes / int64(mask+1)
		if maxBucketBytes <= 0 {
			maxBucketBytes = 1
		}
	}

	s := &lruStore{
		locks:          make([]sync.Mutex, mask+1),
		caches:         make([][2]*cache, mask+1),
		onEvicted:      opts.OnEvicted,
		cleanupTick:    time.NewTicker(opts.CleanupInterval),
		closeCh:        make(chan struct{}),
		maxBucketBytes: maxBucketBytes,
		mask:           int32(mask),
	}

	for i := range s.caches {
		s.caches[i][0] = Create(opts.CapPerBucket)
		s.caches[i][1] = Create(opts.Level2Cap)
	}

	go s.cleanupLoop()

	return s
}

func (s *lruStore) Get(key string) (Value, bool) {
	idx := HashBKRD(key) & s.mask
	s.locks[idx].Lock()
	defer s.locks[idx].Unlock()

	currentTime := Now()

	n1, status1, expireAt := s.caches[idx][0].del(key)
	if status1 > 0 {
		v := n1.v
		n1.v = nil
		if currentTime >= expireAt {
			s.caches[idx][1].drop(key)
			if s.onEvicted != nil {
				s.onEvicted(key, v)
			}
			return nil, false
		}

		s.caches[idx][1].put(key, v, expireAt, s.onEvicted)
		return v, true
	}

	if n2, idx2 := s.caches[idx][1].peek(key); n2 != nil && n2.expireAt > 0 {
		if currentTime >= n2.expireAt {
			v := n2.v
			s.caches[idx][1].drop(key)
			if s.onEvicted != nil {
				s.onEvicted(key, v)
			}
			return nil, false
		}

		s.caches[idx][1].adjust(idx2, p, n)
		return n2.v, true
	}

	return nil, false
}

func (s *lruStore) Set(key string, value Value) error {
	return s.SetWithExpiration(key, value, 0)
}

func (s *lruStore) SetWithExpiration(key string, value Value, expiration time.Duration) error {
	var expireAt int64
	if expiration > 0 {
		expireAt = Now() + expiration.Nanoseconds()
	} else {
		expireAt = maxExpireAt
	}

	idx := HashBKRD(key) & s.mask
	s.locks[idx].Lock()
	defer s.locks[idx].Unlock()

	s.caches[idx][0].put(key, value, expireAt, s.onEvicted)
	// L1 is the single write authority: drop any stale copy promoted to L2
	// earlier, otherwise it can resurface after the L1 slot is recycled.
	s.caches[idx][1].drop(key)

	if s.maxBucketBytes > 0 {
		for s.caches[idx][0].bytes+s.caches[idx][1].bytes > s.maxBucketBytes {
			if !s.evictFromBucket(idx) {
				break
			}
		}
	}

	return nil
}

func (s *lruStore) evictFromBucket(idx int32) bool {
	if s.caches[idx][0].evictOldest(s.onEvicted) {
		return true
	}
	return s.caches[idx][1].evictOldest(s.onEvicted)
}

func (s *lruStore) Delete(key string) bool {
	idx := HashBKRD(key) & s.mask
	s.locks[idx].Lock()
	defer s.locks[idx].Unlock()

	return s.delete(key, idx)
}

func (s *lruStore) Clear() {
	for i := range s.caches {
		s.locks[i].Lock()

		keys := make(map[string]struct{})
		s.caches[i][0].walk(func(key string, value Value, expireAt int64) bool {
			keys[key] = struct{}{}
			return true
		})
		s.caches[i][1].walk(func(key string, value Value, expireAt int64) bool {
			keys[key] = struct{}{}
			return true
		})
		for key := range keys {
			s.delete(key, int32(i))
		}

		s.locks[i].Unlock()
	}
}

func (s *lruStore) Len() int {
	count := 0

	for i := range s.caches {
		s.locks[i].Lock()

		seen := make(map[string]struct{})
		s.caches[i][0].walk(func(key string, value Value, expireAt int64) bool {
			seen[key] = struct{}{}
			return true
		})
		s.caches[i][1].walk(func(key string, value Value, expireAt int64) bool {
			seen[key] = struct{}{}
			return true
		})
		count += len(seen)

		s.locks[i].Unlock()
	}

	return count
}

func (s *lruStore) Walk(fn func(Entry) bool) {
	for i := range s.caches {
		s.locks[i].Lock()

		seen := make(map[string]struct{})

		s.caches[i][1].walk(func(key string, value Value, expireAt int64) bool {
			seen[key] = struct{}{}
			return fn(Entry{Key: key, Size: value.Len(), ExpireAt: expireAt, Level: 1})
		})

		s.caches[i][0].walk(func(key string, value Value, expireAt int64) bool {
			if _, ok := seen[key]; ok {
				return true
			}
			return fn(Entry{Key: key, Size: value.Len(), ExpireAt: expireAt, Level: 0})
		})

		s.locks[i].Unlock()
	}
}

// Bytes returns the total live bytes (keys + values) across all buckets.
func (s *lruStore) Bytes() int64 {
	var total int64
	for i := range s.caches {
		s.locks[i].Lock()
		total += s.caches[i][0].bytes + s.caches[i][1].bytes
		s.locks[i].Unlock()
	}
	return total
}

// Evictions returns the cumulative number of capacity/byte-budget evictions.
func (s *lruStore) Evictions() int64 {
	var total int64
	for i := range s.caches {
		s.locks[i].Lock()
		total += s.caches[i][0].nevict + s.caches[i][1].nevict
		s.locks[i].Unlock()
	}
	return total
}

func (s *lruStore) Close() {
	s.closeOnce.Do(func() {
		if s.cleanupTick != nil {
			s.cleanupTick.Stop()
		}
		close(s.closeCh)
	})
}

var clock, p, n = time.Now().UnixNano(), uint16(0), uint16(1)

func Now() int64 { return atomic.LoadInt64(&clock) }

func init() {
	go func() {
		for {
			atomic.StoreInt64(&clock, time.Now().UnixNano())
			for range 9 {
				time.Sleep(100 * time.Millisecond)
				atomic.AddInt64(&clock, int64(100*time.Millisecond))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
}

func HashBKRD(s string) (hash int32) {
	for i := 0; i < len(s); i++ {
		hash = hash*131 + int32(s[i])
	}

	return hash
}

func MaskOfNextPowOf2(cap uint16) uint16 {
	if cap > 0 && cap&(cap-1) == 0 {
		return cap - 1
	}

	cap |= cap >> 1
	cap |= cap >> 2
	cap |= cap >> 4

	return cap | (cap >> 8)
}

type node struct {
	k        string
	v        Value
	expireAt int64
}

type cache struct {
	doubleLink [][2]uint16
	m          []node
	hashMap    map[string]uint16
	bytes      int64
	nevict     int64
	last       uint16
}

func Create(cap uint16) *cache {
	return &cache{
		doubleLink: make([][2]uint16, cap+1),
		m:          make([]node, cap),
		hashMap:    make(map[string]uint16, cap),
		last:       0,
	}
}

func entryBytes(key string, val Value) int64 {
	if val == nil {
		return int64(len(key))
	}
	return int64(len(key)) + int64(val.Len())
}

func (c *cache) put(key string, val Value, expireAt int64, onEvicted func(string, Value)) int {
	if idx, ok := c.hashMap[key]; ok {
		nd := &c.m[idx-1]
		if nd.expireAt > 0 {
			c.bytes += entryBytes(key, val) - entryBytes(nd.k, nd.v)
		} else {
			c.bytes += entryBytes(key, val)
		}
		nd.v, nd.expireAt = val, expireAt
		c.adjust(idx, p, n)
		return 0
	}

	if c.last == uint16(cap(c.m)) {
		tail := &c.m[c.doubleLink[0][p]-1]
		if (*tail).expireAt > 0 {
			c.bytes -= entryBytes((*tail).k, (*tail).v)
			c.nevict++
			if onEvicted != nil {
				onEvicted((*tail).k, (*tail).v)
			}
		}

		delete(c.hashMap, (*tail).k)
		c.bytes += entryBytes(key, val)
		c.hashMap[key], (*tail).k, (*tail).v, (*tail).expireAt = c.doubleLink[0][p], key, val, expireAt
		c.adjust(c.doubleLink[0][p], p, n)

		return 1
	}

	c.last++
	if len(c.hashMap) <= 0 {
		c.doubleLink[0][p] = c.last
	} else {
		c.doubleLink[c.doubleLink[0][n]][p] = c.last
	}

	c.m[c.last-1].k = key
	c.m[c.last-1].v = val
	c.m[c.last-1].expireAt = expireAt
	c.doubleLink[c.last] = [2]uint16{0, c.doubleLink[0][n]}
	c.hashMap[key] = c.last
	c.doubleLink[0][n] = c.last
	c.bytes += entryBytes(key, val)

	return 1
}

func (c *cache) peek(key string) (*node, uint16) {
	if idx, ok := c.hashMap[key]; ok {
		return &c.m[idx-1], idx
	}
	return nil, 0
}

func (c *cache) del(key string) (*node, int, int64) {
	if idx, ok := c.hashMap[key]; ok && c.m[idx-1].expireAt > 0 {
		e := c.m[idx-1].expireAt
		c.bytes -= entryBytes(c.m[idx-1].k, c.m[idx-1].v)
		c.m[idx-1].expireAt = 0
		c.adjust(idx, n, p)
		return &c.m[idx-1], 1, e
	}

	return nil, 0, 0
}

// drop logically deletes key and releases the slot's value reference.
func (c *cache) drop(key string) {
	if nd, st, _ := c.del(key); st > 0 {
		nd.v = nil
	}
}

// evictOldest logically removes the least recently used live entry.
func (c *cache) evictOldest(onEvicted func(string, Value)) bool {
	for idx := c.doubleLink[0][p]; idx != 0; idx = c.doubleLink[idx][p] {
		nd := &c.m[idx-1]
		if nd.expireAt <= 0 {
			continue
		}
		c.bytes -= entryBytes(nd.k, nd.v)
		c.nevict++
		v := nd.v
		nd.expireAt = 0
		nd.v = nil
		c.adjust(idx, n, p)
		if onEvicted != nil {
			onEvicted(nd.k, v)
		}
		return true
	}
	return false
}

func (c *cache) walk(walker func(key string, value Value, expireAt int64) bool) {
	for idx := c.doubleLink[0][n]; idx != 0; idx = c.doubleLink[idx][n] {
		if c.m[idx-1].expireAt > 0 && !walker(c.m[idx-1].k, c.m[idx-1].v, c.m[idx-1].expireAt) {
			return
		}
	}
}

func (c *cache) adjust(idx, f, t uint16) {
	if c.doubleLink[idx][f] != 0 {
		c.doubleLink[c.doubleLink[idx][t]][f] = c.doubleLink[idx][f]
		c.doubleLink[c.doubleLink[idx][f]][t] = c.doubleLink[idx][t]
		c.doubleLink[idx][f] = 0
		c.doubleLink[idx][t] = c.doubleLink[0][t]
		c.doubleLink[c.doubleLink[0][t]][f] = idx
		c.doubleLink[0][t] = idx
	}
}

func (s *lruStore) delete(key string, idx int32) bool {
	n1, s1, _ := s.caches[idx][0].del(key)
	n2, s2, _ := s.caches[idx][1].del(key)
	deleted := s1 > 0 || s2 > 0

	if deleted && s.onEvicted != nil {
		if n1 != nil && n1.v != nil {
			s.onEvicted(key, n1.v)
		} else if n2 != nil && n2.v != nil {
			s.onEvicted(key, n2.v)
		}
	}

	if n1 != nil {
		n1.v = nil
	}
	if n2 != nil {
		n2.v = nil
	}

	return deleted
}

func (s *lruStore) cleanupLoop() {
	for {
		select {
		case <-s.cleanupTick.C:
			currentTime := Now()

			for i := range s.caches {
				s.locks[i].Lock()

				expiredKeys := make(map[string]struct{})

				s.caches[i][0].walk(func(key string, value Value, expireAt int64) bool {
					if currentTime >= expireAt {
						expiredKeys[key] = struct{}{}
					}
					return true
				})

				s.caches[i][1].walk(func(key string, value Value, expireAt int64) bool {
					if currentTime >= expireAt {
						expiredKeys[key] = struct{}{}
					}
					return true
				})

				for key := range expiredKeys {
					s.delete(key, int32(i))
				}

				s.locks[i].Unlock()
			}
		case <-s.closeCh:
			return
		}
	}
}
