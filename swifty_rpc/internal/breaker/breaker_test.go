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

package breaker

import (
	"testing"
	"time"
)

func TestCircuitBreakerClosedAndOpen(t *testing.T) {
	cb := NewCircuitBreaker(4, 0.5, 20*time.Millisecond)
	for range 2 {
		cb.RecordSuccess()
	}
	for range 2 {
		cb.RecordFailure()
	}
	if cb.State() != Open {
		t.Fatalf("state = %v, want Open", cb.State())
	}
	if cb.Allow() {
		t.Fatal("open breaker should reject before timeout")
	}
	time.Sleep(25 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("open breaker should allow one half-open probe after timeout")
	}
	if cb.Allow() {
		t.Fatal("half-open breaker should allow only one probe")
	}
	cb.RecordSuccess()
	if cb.State() != Closed {
		t.Fatalf("state = %v, want Closed", cb.State())
	}
}

func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(1, 1.0, time.Millisecond)
	cb.RecordFailure()
	if cb.State() != Open {
		t.Fatalf("state = %v, want Open", cb.State())
	}
	time.Sleep(2 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("expected half-open probe")
	}
	cb.RecordFailure()
	if cb.State() != Open {
		t.Fatalf("state = %v, want Open after failed probe", cb.State())
	}
}

func TestCircuitBreakerWindowReset(t *testing.T) {
	cb := NewCircuitBreaker(2, 1.0, time.Second)
	cb.RecordFailure()
	cb.RecordSuccess()
	if cb.State() != Closed {
		t.Fatalf("state = %v, want Closed", cb.State())
	}
	cb.RecordFailure()
	if cb.State() != Closed {
		t.Fatalf("state = %v, want Closed before full window", cb.State())
	}
	cb.RecordFailure()
	if cb.State() != Open {
		t.Fatalf("state = %v, want Open", cb.State())
	}
}
