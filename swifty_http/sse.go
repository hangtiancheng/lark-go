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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type SSEWriter struct {
	ctx     *Context
	flusher http.Flusher
	mu      sync.Mutex
}

func (ctx *Context) SSE() *SSEWriter {
	flusher, _ := ctx.Writer.(http.Flusher)
	ctx.flushed = true
	ctx.Status = http.StatusOK
	ctx.statusSet = true
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	ctx.Writer.Header().Set("Cache-Control", "no-cache")
	ctx.Writer.Header().Set("Connection", "keep-alive")
	for k, v := range ctx.headers {
		ctx.Writer.Header().Set(k, v)
	}
	ctx.Writer.WriteHeader(http.StatusOK)
	return &SSEWriter{ctx: ctx, flusher: flusher}
}

func (w *SSEWriter) Event(event string, data string) {
	w.mu.Lock()
	fmt.Fprintf(w.ctx.Writer, "event: %s\n", event)
	w.writeData(data)
	w.flush()
	w.mu.Unlock()
}

func (w *SSEWriter) Data(data string) {
	w.mu.Lock()
	w.writeData(data)
	w.flush()
	w.mu.Unlock()
}

func (w *SSEWriter) JSON(event string, obj any) {
	bytes, err := json.Marshal(obj)
	if err != nil {
		return
	}
	w.mu.Lock()
	if event != "" {
		fmt.Fprintf(w.ctx.Writer, "event: %s\n", event)
	}
	fmt.Fprintf(w.ctx.Writer, "data: %s\n\n", bytes)
	w.flush()
	w.mu.Unlock()
}

func (w *SSEWriter) ID(id string) {
	w.mu.Lock()
	fmt.Fprintf(w.ctx.Writer, "id: %s\n", id)
	w.flush()
	w.mu.Unlock()
}

func (w *SSEWriter) Retry(ms int) {
	w.mu.Lock()
	fmt.Fprintf(w.ctx.Writer, "retry: %d\n\n", ms)
	w.flush()
	w.mu.Unlock()
}

func (w *SSEWriter) Comment(text string) {
	w.mu.Lock()
	for line := range strings.SplitSeq(text, "\n") {
		fmt.Fprintf(w.ctx.Writer, ": %s\n", line)
	}
	fmt.Fprint(w.ctx.Writer, "\n")
	w.flush()
	w.mu.Unlock()
}

func (w *SSEWriter) Heartbeat(interval time.Duration) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.mu.Lock()
				fmt.Fprint(w.ctx.Writer, ": keepalive\n\n")
				w.flush()
				w.mu.Unlock()
			case <-w.ctx.Request.Context().Done():
				return
			case <-stop:
				return
			}
		}
	}()
	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() { close(stop) })
		<-done
	}
}

func (w *SSEWriter) Closed() <-chan struct{} {
	return w.ctx.Request.Context().Done()
}

func (w *SSEWriter) Stream(ch <-chan string) {
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			w.Data(msg)
		case <-w.ctx.Request.Context().Done():
			return
		}
	}
}

func (w *SSEWriter) Flush() {
	w.flush()
}

func (w *SSEWriter) Done() {
	w.Data("[DONE]")
}

func (w *SSEWriter) writeData(data string) {
	lines := strings.SplitSeq(data, "\n")
	for line := range lines {
		fmt.Fprintf(w.ctx.Writer, "data: %s\n", line)
	}
	fmt.Fprint(w.ctx.Writer, "\n")
}

func (w *SSEWriter) flush() {
	if w.flusher != nil {
		w.flusher.Flush()
	}
}
