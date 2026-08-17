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

package chat_pipeline

import (
	"context"
	"time"

	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/ai/a2ui"
)

// newInputToRagLambda extracts the query string from the UserMessage for use
// as the search input to the Redis retriever.
func newInputToRagLambda(ctx context.Context, input *UserMessage, opts ...any) (string, error) {
	return input.Query, nil
}

// newInputToChatLambda transforms the UserMessage into a map of template variables
// for the chat prompt template. It includes the query content, conversation history,
// and the current date.
func newInputToChatLambda(ctx context.Context, input *UserMessage, opts ...any) (map[string]any, error) {
	return map[string]any{
		"content": input.Query,
		"history": input.History,
		"date":    time.Now().Format("1/2/2006, 3:04:05 PM"),
		// Injected as a variable value: the section's literal JSON braces must
		// never reach the FString template parser.
		"a2ui_section": a2ui.PromptSection,
	}, nil
}
