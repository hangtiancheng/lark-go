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
	"mime/multipart"
	"net/http"
)

type H map[string]any

type Middleware func(ctx *Context, next func())

type Context struct {
	Request *http.Request
	Writer  http.ResponseWriter

	// request info (Koa-style fields)
	Path   string
	Method string

	// deferred response (Koa-style)
	Status int
	Body   any
	Type   string

	// middleware data sharing
	State  map[string]any
	Params map[string]string

	// response headers (deferred)
	headers map[string]string

	// internal
	app       *Application
	flushed   bool
	statusSet bool
}

func newContext(w http.ResponseWriter, req *http.Request) *Context {
	return &Context{
		Request: req,
		Writer:  w,
		Path:    req.URL.Path,
		Method:  req.Method,
		Status:  http.StatusNotFound,
		State:   make(map[string]any),
		headers: make(map[string]string),
	}
}

func (ctx *Context) Throw(status int, msg string) {
	ctx.Status = status
	ctx.statusSet = true
	// Include "data": nil so error responses match the unified {message, data}
	// shape returned by Next.js and by the success-path ctx.JSON calls.
	ctx.Body = H{"message": msg, "data": nil}
}

// SetStatus records an explicitly chosen status, like Koa's ctx.status setter.
// Unlike assigning the Status field directly, a status set here is never
// overridden by the automatic 404->200 promotion when a body is present.
func (ctx *Context) SetStatus(status int) {
	ctx.Status = status
	ctx.statusSet = true
}

func (ctx *Context) Get(header string) string {
	return ctx.Request.Header.Get(header)
}

func (ctx *Context) Set(header string, value string) {
	ctx.headers[http.CanonicalHeaderKey(header)] = value
}

func (ctx *Context) Query(key string) string {
	return ctx.Request.URL.Query().Get(key)
}

func (ctx *Context) Param(key string) string {
	value := ctx.Params[key]
	return value
}

func (ctx *Context) PostForm(key string) string {
	return ctx.Request.FormValue(key)
}

func (ctx *Context) BindJSON(out any) error {
	defer ctx.Request.Body.Close()
	return json.NewDecoder(ctx.Request.Body).Decode(out)
}

func (ctx *Context) FormFile(key string) (multipart.File, *multipart.FileHeader, error) {
	return ctx.Request.FormFile(key)
}
