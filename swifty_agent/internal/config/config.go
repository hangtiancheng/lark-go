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

// Package config provides configuration loading and types for the swifty_agent application.
// Configuration is loaded from a JSON file and provides settings for AI models,
// vector database connections, and application behavior.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Model provider identifiers selectable via the model_provider config field.
const (
	// ModelProviderOpenAI selects the OpenAI-compatible chat model implementation.
	ModelProviderOpenAI = "openai"
	// ModelProviderAnthropic selects the Anthropic Claude chat model implementation.
	ModelProviderAnthropic = "anthropic"
)

// Config holds all application configuration values.
type Config struct {
	// ServerAddr is the address the HTTP server listens on (e.g., ":8123").
	ServerAddr string `json:"server_addr"`

	// ModelProvider selects the chat model implementation: "openai" (default) or "anthropic".
	ModelProvider string `json:"model_provider"`

	// ThinkChatModel configures the LLM used for deep reasoning tasks (planning, replanning).
	ThinkChatModel ChatModelConfig `json:"think_chat_model"`

	// QuickChatModel configures the LLM used for fast chat responses and tool execution.
	QuickChatModel ChatModelConfig `json:"quick_chat_model"`

	// EmbeddingModel configures the embedding model used for vectorization.
	EmbeddingModel EmbeddingConfig `json:"embedding_model"`

	// FileDir is the directory path for storing uploaded knowledge base files.
	FileDir string `json:"file_dir"`

	// MCP_URL is the Server-Sent Events endpoint for the MCP (Model Context Protocol) tool server.
	MCP_URL string `json:"mcp_url"`

	// PrometheusURL is the base URL of the Prometheus server used by the
	// query_prometheus_alerts tool (e.g. "http://127.0.0.1:9090"). Empty disables queries.
	PrometheusURL string `json:"prometheus_url"`

	// LogTopicRegion / LogTopicID configure the log topic context injected into
	// the chat system prompt. Both must be set to include the line; empty omits
	// it (mirrors Next.js LOG_TOPIC_REGION / LOG_TOPIC_ID env vars).
	LogTopicRegion string `json:"log_topic_region"`
	LogTopicID     string `json:"log_topic_id"`

	// Redis configures the connection to the Redis Stack (RediSearch) vector store.
	Redis RedisConfig `json:"redis"`
}

// ChatModelConfig holds LLM connection settings for OpenAI-compatible and Anthropic API endpoints.
type ChatModelConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`

	// MaxTokens caps the response length. Required by the Anthropic provider
	// (defaults to 4096 when unset); ignored by the OpenAI provider.
	MaxTokens int `json:"max_tokens"`

	// Thinking enables Anthropic extended thinking (only applies when
	// model_provider=anthropic). When enabled, budgetTokens = MaxTokens-1,
	// mirroring the Next.js ANTHROPIC_THINKING config.
	Thinking bool `json:"thinking"`
}

// EmbeddingConfig holds embedding model settings. The vector dimension is
// probed from the live provider at startup (see embedder.ProbeDimension),
// so no dimension configuration is needed.
type EmbeddingConfig struct {
	// Provider selects the embedding backend: "openai" (default) or "ollama".
	Provider string `json:"provider"`

	// OpenAI fields (OpenAI-compatible endpoint).
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`

	// Ollama fields (OpenAI-compatible /v1/embeddings endpoint).
	OllamaBaseURL string `json:"ollama_base_url"`
	OllamaModel   string `json:"ollama_model"`
}

// RedisConfig holds Redis Stack connection settings.
type RedisConfig struct {
	Addr     string `json:"addr"`     // Redis address, default "localhost:6379".
	Password string `json:"password"` // Redis password, default "".
	DB       int    `json:"db"`       // Redis DB number, default 0.
}

// Load reads and parses the configuration file at the given path.
// Missing fields are filled with sensible defaults.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	applyDefaults(cfg)
	return cfg, nil
}

// applyDefaults fills in zero-valued fields with sensible defaults.
func applyDefaults(cfg *Config) {
	if cfg.ServerAddr == "" {
		cfg.ServerAddr = ":8123"
	}
	if cfg.ModelProvider == "" {
		cfg.ModelProvider = ModelProviderOpenAI
	}
	if cfg.ThinkChatModel.MaxTokens == 0 {
		cfg.ThinkChatModel.MaxTokens = 4096
	}
	if cfg.QuickChatModel.MaxTokens == 0 {
		cfg.QuickChatModel.MaxTokens = 4096
	}
	if cfg.FileDir == "" {
		cfg.FileDir = "./data/docs"
	}
	if cfg.Redis.Addr == "" {
		cfg.Redis.Addr = "localhost:6379"
	}
	if cfg.PrometheusURL == "" {
		cfg.PrometheusURL = "http://127.0.0.1:9090"
	}
	if cfg.EmbeddingModel.Provider == "" {
		cfg.EmbeddingModel.Provider = "openai"
	}
	if cfg.EmbeddingModel.OllamaBaseURL == "" {
		cfg.EmbeddingModel.OllamaBaseURL = "http://localhost:11434"
	}
	if cfg.EmbeddingModel.OllamaModel == "" {
		cfg.EmbeddingModel.OllamaModel = "nomic-embed-text"
	}
}
