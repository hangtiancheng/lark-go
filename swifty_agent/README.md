# Swifty Agent

_Author: Swifty_

A Go-based AI-powered intelligent operations assistant, migrated from the Next.js project (`swifty-cli/apps/swifty-agent`). Provides RAG-enhanced conversations, knowledge base indexing, and automated alert analysis capabilities, built with the [swifty_http](../swifty_http) framework and [Eino](https://www.cloudwego.io/docs/eino/) AI orchestration.

## Features

- **/api/chat** — Synchronous RAG conversation (ReAct agent + tool calling)
- **/api/chat_stream** — SSE streaming conversation
- **/api/upload** — Upload documents to the knowledge base (Markdown split by headings + vector indexing)
- **/api/ai_ops** — Plan-Execute-Replan alert analysis

## Tech Stack

- Go 1.25+ / [swifty_http](../swifty_http) HTTP framework
- [Eino](https://github.com/cloudwego/eino) AI pipeline orchestration
- Redis Stack (RediSearch vector search)
- OpenAI / Anthropic / Ollama multi-provider support

## Getting Started

### 1. Prerequisites

```bash
# Start infrastructure (Redis/Prometheus/Grafana)
make docker-up

# Install hot-reload tool (optional)
go install github.com/air-verse/air@latest
```

### 2. Configuration

Edit [config.json](./config.json) and provide at minimum:

- `think_chat_model.api_key` / `quick_chat_model.api_key` — LLM API key
- `embedding_model.api_key` — OpenAI API key

See [.env.example](./.env.example) for a configuration field reference.

### 3. Run

```bash
# Hot-reload development
make dev          # or air

# Standard run
make run          # or go run .

# Build
make build        # outputs bin/swifty-agent
```

The server listens on `:6872`.

### 4. Index Knowledge Base (Optional)

Place Markdown documents in `./data/docs/` and batch-index them:

```bash
go run ./cmd/knowledge
```

## Configuration Reference

| config.json Field                   | Description                                                                                  | Default                     |
| ----------------------------------- | -------------------------------------------------------------------------------------------- | --------------------------- |
| `server_addr`                       | Listen address                                                                               | `:6872`                     |
| `model_provider`                    | LLM provider: `openai` / `anthropic`                                                         | `openai`                    |
| `think_chat_model`                  | Planning/replan model (supports `thinking` field to enable Anthropic extended thinking)      | —                           |
| `quick_chat_model`                  | Chat/tool execution model                                                                    | —                           |
| `embedding_model.provider`          | Embedding backend: `openai` / `ollama`                                                       | `openai`                    |
| `embedding_model.dimensions`        | Vector dimensions (index must be rebuilt after switching providers; auto-detected by server) | `2048`                      |
| `redis`                             | Redis Stack connection                                                                       | `localhost:6379`            |
| `mcp_url`                           | MCP log tool SSE endpoint (gracefully degrades when unavailable)                             | `http://localhost:3000/sse` |
| `prometheus_url`                    | Prometheus URL (leave empty to disable the alert query tool)                                 | `http://127.0.0.1:9090`     |
| `log_topic_region` / `log_topic_id` | Log topic injected into the system prompt (takes effect only when both fields are non-empty) | Empty                       |
| `file_dir`                          | Knowledge base document directory                                                            | `./data/docs`               |

## API

| Method | Path               | Description                                                            |
| ------ | ------------------ | ---------------------------------------------------------------------- |
| POST   | `/api/chat`        | `{"id","question"}` → `{"message","data":{"answer"}}`                  |
| POST   | `/api/chat_stream` | SSE: `connected` / `message` / `done` / `error`                        |
| POST   | `/api/upload`      | multipart `file` → `{"message","data":{"fileName,filePath,fileSize"}}` |
| POST   | `/api/ai_ops`      | → `{"message","data":{"result","detail"}}`                             |

## CLI Tools

```bash
go run ./cmd/chat        # Interactive testing of the chat pipeline
go run ./cmd/knowledge   # Batch-index ./data/docs
go run ./cmd/recall      # Test vector retrieval
go run ./cmd/ai_ops      # Run alert analysis standalone
go run ./cmd/llm_tool    # Test tool binding
```

## Docker

```bash
make docker-build
docker run -p 6872:6872 -v $(pwd)/config.json:/app/config.json swifty-agent
```

## Project Structure

```
swifty_agent/
├── main.go                      # Entry point
├── config.json                  # Configuration
├── internal/
│   ├── app/                     # HTTP handlers (chat/upload/ai_ops)
│   ├── config/                  # Configuration loading
│   ├── consts/                  # Constants (Redis keys/fields)
│   ├── ai/
│   │   ├── agent/               # Pipelines (chat/knowledge_index/plan_execute_replan)
│   │   ├── tools/               # Tools (mysql/prometheus/mcp/docs/time)
│   │   ├── models/              # LLM model factory (incl. Anthropic signature patching)
│   │   ├── embedder/            # Embedding (openai/ollama)
│   │   ├── indexer/             # Redis vector indexer
│   │   ├── retriever/           # Redis vector retriever
│   │   └── loader/              # Document loader
│   └── utility/                 # redis/mem/log_callback
├── cmd/                         # CLI tools
├── fe/                          # Frontend (standalone)
├── docker-compose.yml           # Redis/Prometheus/Grafana
├── Dockerfile                   # Container build
├── Makefile                     # Build entry point
└── .air.toml                    # Hot-reload configuration
```

## Migration Notes

This project was migrated from Next.js. See [review.md](./review.md) for alignment details and [checklist.md](./checklist.md) for the alignment task tracker.
