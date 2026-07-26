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
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
)

func trace(message string) string {
	var pcs [32]uintptr
	n := runtime.Callers(3, pcs[:])

	var str strings.Builder
	str.WriteString(message)
	str.WriteString("\nTraceback:")
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		fmt.Fprintf(&str, "\n\t%s:%d", frame.File, frame.Line)
		if !more {
			break
		}
	}
	return str.String()
}

func Recovery() Middleware {
	return func(ctx *Context, next func()) {
		defer func() {
			if err := recover(); err != nil {
				if err == http.ErrAbortHandler {
					// net/http uses this sentinel to abort a request without
					// logging; converting it into a 500 would break e.g.
					// httputil.ReverseProxy client-disconnect handling.
					panic(err)
				}
				message := fmt.Sprintf("%v", err)
				log.Printf("%s\n\n", trace(message))
				ctx.Status = http.StatusInternalServerError
				ctx.statusSet = true
				ctx.Type = ""
				ctx.headers = make(map[string]string)
				ctx.Body = H{"message": "Internal Server Error"}
			}
		}()
		next()
	}
}
