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

package load_balance

import (
	"sync"
	"testing"

	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/registry"
)

func instances() []registry.Instance {
	return []registry.Instance{{Addr: "a"}, {Addr: "b"}, {Addr: "c"}}
}

func TestRoundRobin(t *testing.T) {
	rr := NewRR()
	if got := rr.Select(nil); got.Addr != "" {
		t.Fatalf("empty select = %+v", got)
	}
	list := instances()
	want := []string{"a", "b", "c", "a"}
	for _, addr := range want {
		if got := rr.Select(list).Addr; got != addr {
			t.Fatalf("Select = %q, want %q", got, addr)
		}
	}
}

func TestRandom(t *testing.T) {
	r := NewRandom()
	if got := r.Select(nil); got.Addr != "" {
		t.Fatalf("empty select = %+v", got)
	}
	list := instances()
	for range 20 {
		if got := r.Select(list).Addr; got == "" {
			t.Fatal("random select returned empty instance")
		}
	}
}

func TestWeightedRR(t *testing.T) {
	list := instances()
	w := NewWeightedRR([]int{1, 2, 3})
	got := make(map[string]int)
	for range 6 {
		got[w.Select(list).Addr]++
	}
	if got["a"] != 1 || got["b"] != 2 || got["c"] != 3 {
		t.Fatalf("weighted distribution = %v", got)
	}
	if got := NewWeightedRR([]int{1}).Select(list); got.Addr != "" {
		t.Fatalf("mismatched weight count returned %+v", got)
	}
	if got := NewWeightedRR([]int{0, -1, 0}).Select(list); got.Addr != "" {
		t.Fatalf("zero weights returned %+v", got)
	}
}

func TestConcurrentBalancers(t *testing.T) {
	rr := NewRR()
	random := NewRandom()
	list := instances()
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = rr.Select(list)
		}()
		go func() {
			defer wg.Done()
			_ = random.Select(list)
		}()
	}
	wg.Wait()
}
