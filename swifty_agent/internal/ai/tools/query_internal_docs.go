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

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/ai/retriever"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/config"
)

// QueryInternalDocsInput defines the input for the internal docs search tool.
// Mirrors the Next.js zod schema: required query with the same description.
type QueryInternalDocsInput struct {
	Query string `json:"query" jsonschema:"required" jsonschema_description:"Query string used to retrieve internal documents"`
}

// queryInternalDocResult is the per-document result returned to the LLM.
// Aligned with the Next.js retrieveDocs output shape (id/content/metadata).
type queryInternalDocResult struct {
	ID       string         `json:"id"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata"`
}

// NewQueryInternalDocsTool creates a tool that searches the internal knowledge base
// using RAG (Retrieval-Augmented Generation) to find relevant documents and
// extract processing steps from the company's documentation.
// Construction errors are returned to the caller instead of terminating the process.
func NewQueryInternalDocsTool(cfg *config.Config) (tool.InvokableTool, error) {
	t, err := utils.InferOptionableTool(
		"query_internal_docs",
		"Search internal documentation and knowledge base for relevant information. Performs RAG to find similar documents and extract processing steps. Useful for understanding internal procedures, best practices, or step-by-step guides.",
		func(ctx context.Context, input *QueryInternalDocsInput, opts ...tool.Option) (string, error) {
			// Runtime failures are returned as JSON error payloads (nil error) so the
			// agent can reason about them instead of aborting the ReAct stream.
			errPayload := func(err error) (string, error) {
				b, _ := json.Marshal(map[string]any{
					"success": false,
					"error":   err.Error(),
					"message": "Failed to search internal documentation",
				})
				return string(b), nil
			}

			rr, err := retriever.NewRedisRetriever(ctx, cfg)
			if err != nil {
				return errPayload(fmt.Errorf("create retriever: %w", err))
			}
			resp, err := rr.Retrieve(ctx, input.Query)
			if err != nil {
				return errPayload(fmt.Errorf("retrieve docs: %w", err))
			}

			// Map Eino documents to the {id, content, metadata} shape expected by
			// the Next.js retrieveDocs tool. The stored metadata is a JSON string
			// (see indexer.documentToHashes); parse it into a map for the LLM.
			results := make([]queryInternalDocResult, 0, len(resp))
			for _, d := range resp {
				results = append(results, queryInternalDocResult{
					ID:       d.ID,
					Content:  d.Content,
					Metadata: parseMetadataField(d.MetaData["metadata"]),
				})
			}

			b, err := json.Marshal(results)
			if err != nil {
				return errPayload(fmt.Errorf("marshal docs: %w", err))
			}
			return string(b), nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("infer query_internal_docs tool: %w", err)
	}
	return t, nil
}

// parseMetadataField converts the stored metadata value (a JSON string produced
// by the indexer) into a map. Non-string or unparseable values yield an empty map.
func parseMetadataField(v any) map[string]any {
	s, ok := v.(string)
	if !ok || s == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return map[string]any{}
	}
	return m
}
