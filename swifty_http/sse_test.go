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

package swifty_http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSSEWriter(t *testing.T) {
	app := New()
	app.Get("/events", func(ctx *Context, next func()) {
		sse := ctx.SSE()
		sse.Retry(3000)
		sse.ID("1")
		sse.Event("message", "hello")
		sse.Data("world")
		sse.Done()
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/events", nil))

	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("cache-control = %q", rec.Header().Get("Cache-Control"))
	}

	body := rec.Body.String()
	if !strings.Contains(body, "retry: 3000") {
		t.Fatalf("missing retry field in: %q", body)
	}
	if !strings.Contains(body, "id: 1") {
		t.Fatalf("missing id field in: %q", body)
	}
	if !strings.Contains(body, "event: message\ndata: hello\n\n") {
		t.Fatalf("missing event+data in: %q", body)
	}
	if !strings.Contains(body, "data: world\n\n") {
		t.Fatalf("missing data field in: %q", body)
	}
	if !strings.Contains(body, "data: [DONE]\n\n") {
		t.Fatalf("missing DONE marker in: %q", body)
	}
}

func TestSSEMultilineData(t *testing.T) {
	app := New()
	app.Get("/multi", func(ctx *Context, next func()) {
		sse := ctx.SSE()
		sse.Data("line1\nline2\nline3")
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/multi", nil))

	body := rec.Body.String()
	expected := "data: line1\ndata: line2\ndata: line3\n\n"
	if !strings.Contains(body, expected) {
		t.Fatalf("multiline data = %q, want %q", body, expected)
	}
}

func TestSSEJSON(t *testing.T) {
	app := New()
	app.Get("/json", func(ctx *Context, next func()) {
		sse := ctx.SSE()
		sse.JSON("update", H{"count": 42})
		sse.JSON("", H{"anonymous": true})
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/json", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "event: update\ndata: {\"count\":42}\n\n") {
		t.Fatalf("JSON event = %q", body)
	}
	if !strings.Contains(body, "data: {\"anonymous\":true}\n\n") {
		t.Fatalf("anonymous JSON = %q", body)
	}
	if strings.Contains(body, "event: \n") {
		t.Fatalf("empty event name should be omitted: %q", body)
	}
}

func TestSSEComment(t *testing.T) {
	app := New()
	app.Get("/comment", func(ctx *Context, next func()) {
		sse := ctx.SSE()
		sse.Comment("single line")
		sse.Comment("multi\nline")
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/comment", nil))

	body := rec.Body.String()
	if !strings.Contains(body, ": single line\n\n") {
		t.Fatalf("single comment = %q", body)
	}
	if !strings.Contains(body, ": multi\n: line\n\n") {
		t.Fatalf("multiline comment = %q", body)
	}
}

func TestSSEStream(t *testing.T) {
	app := New()
	app.Get("/stream", func(ctx *Context, next func()) {
		sse := ctx.SSE()
		ch := make(chan string, 3)
		ch <- "one"
		ch <- "two"
		ch <- "three"
		close(ch)
		sse.Stream(ch)
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "data: one\n\n") {
		t.Fatalf("missing 'one' in: %q", body)
	}
	if !strings.Contains(body, "data: two\n\n") {
		t.Fatalf("missing 'two' in: %q", body)
	}
	if !strings.Contains(body, "data: three\n\n") {
		t.Fatalf("missing 'three' in: %q", body)
	}
}

func TestSSEWithCustomHeaders(t *testing.T) {
	app := New()
	app.Get("/sse", func(ctx *Context, next func()) {
		ctx.Set("X-Custom", "value")
		sse := ctx.SSE()
		sse.Data("test")
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sse", nil))

	if rec.Header().Get("X-Custom") != "value" {
		t.Fatalf("custom header not sent: %q", rec.Header().Get("X-Custom"))
	}
}

func TestSSEHeartbeatConcurrency(t *testing.T) {
	app := New()
	app.Get("/heartbeat", func(ctx *Context, next func()) {
		sse := ctx.SSE()
		stop := sse.Heartbeat(5 * time.Millisecond)
		defer stop()

		var wg sync.WaitGroup
		wg.Go(func() {
			for range 10 {
				sse.Data("msg")
				time.Sleep(3 * time.Millisecond)
			}
		})
		wg.Wait()
	})

	c := t.Context()
	req := httptest.NewRequest(http.MethodGet, "/heartbeat", nil).WithContext(c)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "data: msg") {
		t.Fatalf("missing data messages in: %q", body)
	}
}

func TestSSEClosed(t *testing.T) {
	app := New()
	app.Get("/closed", func(ctx *Context, next func()) {
		sse := ctx.SSE()
		ch := sse.Closed()
		select {
		case <-ch:
			t.Fatal("should not be closed yet")
		default:
		}
		sse.Data("ok")
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/closed", nil))

	if !strings.Contains(rec.Body.String(), "data: ok\n\n") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}
