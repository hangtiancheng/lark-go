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

package swifty_cache_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hangtiancheng/swifty.go/swifty_cache"
)

func TestDoReturnsValueAndError(t *testing.T) {
	var g swifty_cache.SingleFlightGroup
	value, err := g.Do("key", func() (any, error) {
		return "value", nil
	})
	if err != nil || value != "value" {
		t.Fatalf("Do returned %v, %v", value, err)
	}

	wantErr := errors.New("load failed")
	value, err = g.Do("err", func() (any, error) {
		return nil, wantErr
	})
	if value != nil || !errors.Is(err, wantErr) {
		t.Fatalf("Do error result = %v, %v", value, err)
	}
}

func TestDoSuppressesDuplicateCalls(t *testing.T) {
	var (
		g     swifty_cache.SingleFlightGroup
		calls int
		mu    sync.Mutex
	)
	ready := make(chan struct{})
	release := make(chan struct{})

	const callers = 16
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			value, err := g.Do("key", func() (any, error) {
				mu.Lock()
				calls++
				if calls == 1 {
					close(ready)
				}
				mu.Unlock()
				<-release
				return "value", nil
			})
			if err != nil {
				errs <- err
				return
			}
			if value != "value" {
				errs <- errors.New("unexpected value")
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
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}

	if _, err := g.Do("key", func() (any, error) {
		calls++
		return "next", nil
	}); err != nil {
		t.Fatalf("sequential Do returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("sequential calls = %d, want 2", calls)
	}
}
