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

// Package embedder provides factory functions for creating text embedding models.
// The embeddings are used to vectorize documents for storage and retrieval in Redis.
//
// Two providers are supported via the OpenAI-compatible /v1/embeddings protocol
// (both use the eino-ext libs/acl/openai client under the hood):
//   - "openai" (default): OpenAI (text-embedding-v4)
//   - "ollama": local Ollama instance (e.g. nomic-embed-text, 768d)
//
// This mirrors the Next.js lib/ai/embedder.ts provider switch.
package embedder

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/libs/acl/openai"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/config"
)

// Embedder wraps the OpenAI-compatible embedding client. It implements
// embedding.Embedder so it can be used by Eino indexers and retrievers.
type Embedder struct {
	cli *openai.EmbeddingClient
}

// New creates a text embedding model based on cfg.EmbeddingModel.Provider.
func New(ctx context.Context, cfg *config.Config) (embedding.Embedder, error) {
	provider := cfg.EmbeddingModel.Provider
	if provider == "" {
		provider = "openai"
	}

	var baseURL, apiKey, model string

	switch provider {
	case "ollama":
		// Ollama exposes an OpenAI-compatible /v1/embeddings endpoint (v0.1.24+).
		// No API key is required, but the client demands a non-empty string.
		baseURL = strings.TrimRight(cfg.EmbeddingModel.OllamaBaseURL, "/") + "/v1"
		apiKey = "ollama"
		model = cfg.EmbeddingModel.OllamaModel
	default: // openai
		baseURL = cfg.EmbeddingModel.BaseURL
		apiKey = cfg.EmbeddingModel.APIKey
		model = cfg.EmbeddingModel.Model
	}

	encFmt := openai.EmbeddingEncodingFormatFloat
	ecfg := &openai.EmbeddingConfig{
		BaseURL:        baseURL,
		APIKey:         apiKey,
		HTTPClient:     &http.Client{Timeout: 60 * time.Second},
		Model:          model,
		EncodingFormat: &encFmt,
	}

	cli, err := openai.NewEmbeddingClient(ctx, ecfg)
	if err != nil {
		return nil, err
	}
	return &Embedder{cli: cli}, nil
}

// EmbedStrings returns the embeddings for the given texts.
func (e *Embedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	return e.cli.EmbedStrings(ctx, texts, opts...)
}

// ProbeDimension returns the actual vector dimension produced by the configured
// embedding provider. It creates a temporary embedder, embeds a short probe
// string, and measures the output length. This is authoritative over any static
// config value — the model's real output determines the RediSearch index DIM
// (mirrors the Next.js ensureIndex probe in lib/redis/client.ts).
func ProbeDimension(ctx context.Context, cfg *config.Config) (int, error) {
	eb, err := New(ctx, cfg)
	if err != nil {
		return 0, fmt.Errorf("create probe embedder: %w", err)
	}
	vecs, err := eb.EmbedStrings(ctx, []string{"dimension probe"})
	if err != nil {
		return 0, fmt.Errorf("probe embedding: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return 0, fmt.Errorf("probe embedding returned empty vector")
	}
	return len(vecs[0]), nil
}
