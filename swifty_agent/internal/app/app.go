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

// Package app provides the HTTP application layer for the Swifty Chatbot.
// It wires up routes, middleware, and request handlers using the swifty_http framework.
package app

import (
	"net/http"

	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/config"
	"github.com/hangtiancheng/swifty.go/swifty_http"
)

// App encapsulates the HTTP application and its dependencies.
type App struct {
	engine *swifty_http.Application
	cfg    *config.Config
}

// New creates a new App with all routes and middleware configured.
func New(cfg *config.Config) *App {
	engine := swifty_http.Default()
	engine.Use(corsMiddleware)

	a := &App{engine: engine, cfg: cfg}

	api := engine.Router("/api")
	api.Post("/chat", a.handleChat)
	api.Post("/chat_stream", a.handleChatStream)
	api.Post("/upload", a.handleFileUpload)
	api.Post("/ai_ops", a.handleAIOps)
	api.Post("/log", a.handleSentryLog)
	api.Get("/metrics", a.handleMetrics)

	return a
}

// Engine returns the underlying swifty_http Application for starting the server.
func (a *App) Engine() *swifty_http.Application {
	return a.engine
}

// corsMiddleware adds CORS headers to all responses and handles preflight requests.
// Methods/headers are restricted to GET+POST+OPTIONS / Content-Type to match the
// Next.js route handlers (lib/api/* CORS_HEADERS).
func corsMiddleware(ctx *swifty_http.Context, next func()) {
	ctx.Set("Access-Control-Allow-Origin", "*")
	ctx.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	ctx.Set("Access-Control-Allow-Headers", "Content-Type")
	if ctx.Method == http.MethodOptions {
		ctx.Status = http.StatusNoContent
		return
	}
	next()
}
