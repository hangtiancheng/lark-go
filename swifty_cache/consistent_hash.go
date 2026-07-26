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
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// ConsistentHashMap is a concurrent consistent hash ring with virtual nodes.
// Virtual node counts are fixed (DefaultReplicas per node), matching
// groupcache's stable key-to-owner mapping.
type ConsistentHashMap struct {
	mu            sync.RWMutex
	config        *ConHashConfig
	keys          []int
	hashMap       map[int]string
	nodeHashes    map[string][]int
	nodeCounts    map[string]*int64
	totalRequests atomic.Int64
}

// NewConsistentHash creates a consistent hash ring.
func NewConsistentHash(opts ...ConHashOption) *ConsistentHashMap {
	m := &ConsistentHashMap{
		config:     DefaultConHashConfig,
		hashMap:    make(map[int]string),
		nodeHashes: make(map[string][]int),
		nodeCounts: make(map[string]*int64),
	}

	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Option configures a Map.
type ConHashOption func(*ConsistentHashMap)

// WithConfig sets the hash ring config.
func WithConsistentHashConfig(config *ConHashConfig) ConHashOption {
	return func(m *ConsistentHashMap) {
		if config != nil {
			m.config = config
		}
	}
}

// Add adds nodes to the hash ring. Adding an existing node is a no-op.
func (m *ConsistentHashMap) Add(nodes ...string) error {
	if len(nodes) == 0 {
		return errors.New("no nodes provided")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, node := range nodes {
		if node == "" {
			continue
		}
		m.addNodeLocked(node, m.config.DefaultReplicas)
	}
	sort.Ints(m.keys)
	return nil
}

// Remove removes a node from the hash ring.
func (m *ConsistentHashMap) Remove(node string) error {
	if node == "" {
		return errors.New("invalid node")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.removeNodeLocked(node)
}

// Get returns the node responsible for key.
func (m *ConsistentHashMap) Get(key string) string {
	if key == "" {
		return ""
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.keys) == 0 {
		return ""
	}

	hash := int(m.config.HashFunc([]byte(key)))
	idx := sort.Search(len(m.keys), func(i int) bool {
		return m.keys[i] >= hash
	})
	if idx == len(m.keys) {
		idx = 0
	}

	node := m.hashMap[m.keys[idx]]
	if counter, ok := m.nodeCounts[node]; ok {
		atomic.AddInt64(counter, 1)
	}
	m.totalRequests.Add(1)
	return node
}

func (m *ConsistentHashMap) addNodeLocked(node string, replicas int) {
	if _, exists := m.nodeHashes[node]; exists {
		return
	}

	hashes := make([]int, 0, replicas)
	for i := range replicas {
		hash := int(m.config.HashFunc(fmt.Appendf(nil, "%s-%d", node, i)))
		if _, taken := m.hashMap[hash]; taken {
			// Virtual node hash collision with an existing entry; skip it
			// instead of silently stealing ownership.
			continue
		}
		m.keys = append(m.keys, hash)
		m.hashMap[hash] = node
		hashes = append(hashes, hash)
	}
	m.nodeHashes[node] = hashes
	if _, ok := m.nodeCounts[node]; !ok {
		m.nodeCounts[node] = new(int64)
	}
}

func (m *ConsistentHashMap) removeNodeLocked(node string) error {
	hashes, ok := m.nodeHashes[node]
	if !ok {
		return fmt.Errorf("node %s not found", node)
	}

	toRemove := make(map[int]struct{}, len(hashes))
	for _, hash := range hashes {
		delete(m.hashMap, hash)
		toRemove[hash] = struct{}{}
	}

	kept := m.keys[:0]
	for _, k := range m.keys {
		if _, drop := toRemove[k]; !drop {
			kept = append(kept, k)
		}
	}
	m.keys = kept

	delete(m.nodeHashes, node)
	delete(m.nodeCounts, node)
	return nil
}

// GetStats returns the traffic share per node.
func (m *ConsistentHashMap) GetStats() map[string]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]float64)
	total := m.totalRequests.Load()
	if total == 0 {
		return stats
	}
	for node, counter := range m.nodeCounts {
		stats[node] = float64(atomic.LoadInt64(counter)) / float64(total)
	}
	return stats
}
