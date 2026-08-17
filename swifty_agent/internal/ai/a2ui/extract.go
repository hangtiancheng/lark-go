package a2ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

var messageKeys = []string{"createSurface", "updateComponents", "updateDataModel", "deleteSurface"}

// ValidateMessages structurally validates a parsed A2UI v0.9 message list.
// It approximates the web_core A2uiMessageListSchema semantics (required
// version, exactly one strict message key, non-empty components); the client
// remains the validation authority and drops any message Go lets through.
func ValidateMessages(messages []any) error {
	if len(messages) == 0 {
		return fmt.Errorf("message list is empty")
	}
	for i, item := range messages {
		if err := validateMessage(item); err != nil {
			return fmt.Errorf("message %d: %w", i, err)
		}
	}
	return nil
}

func validateMessage(item any) error {
	obj, ok := item.(map[string]any)
	if !ok {
		return fmt.Errorf("not a JSON object")
	}
	if v, _ := obj["version"].(string); v != "v0.9" {
		return fmt.Errorf(`"version" must be "v0.9"`)
	}
	var found string
	for key := range obj {
		if key == "version" {
			continue
		}
		if !slices.Contains(messageKeys, key) {
			return fmt.Errorf("unrecognized key %q", key)
		}
		if found != "" {
			return fmt.Errorf("contains multiple message keys; exactly one of createSurface / updateComponents / updateDataModel / deleteSurface is allowed")
		}
		found = key
	}
	if found == "" {
		return fmt.Errorf("must contain exactly one of createSurface / updateComponents / updateDataModel / deleteSurface")
	}
	payload, ok := obj[found].(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be a JSON object", found)
	}
	if sid, _ := payload["surfaceId"].(string); sid == "" {
		return fmt.Errorf("%s.surfaceId must be a non-empty string", found)
	}
	switch found {
	case "createSurface":
		if cid, _ := payload["catalogId"].(string); cid == "" {
			return fmt.Errorf("createSurface.catalogId must be a non-empty string")
		}
	case "updateComponents":
		comps, ok := payload["components"].([]any)
		if !ok || len(comps) == 0 {
			return fmt.Errorf("updateComponents.components must be a non-empty array")
		}
		for j, c := range comps {
			comp, ok := c.(map[string]any)
			if !ok {
				return fmt.Errorf("updateComponents.components[%d] is not a JSON object", j)
			}
			if id, _ := comp["id"].(string); id == "" {
				return fmt.Errorf("updateComponents.components[%d].id must be a non-empty string", j)
			}
			if name, _ := comp["component"].(string); name == "" {
				return fmt.Errorf("updateComponents.components[%d].component must be a non-empty string", j)
			}
		}
	}
	return nil
}

// ParseBlock parses and validates the raw inner content of an <a2ui-json>
// block. A single top-level object is tolerated and wrapped into a list.
func ParseBlock(raw string) ([]any, error) {
	var parsed any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	messages, ok := parsed.([]any)
	if !ok {
		messages = []any{parsed}
	}
	if err := ValidateMessages(messages); err != nil {
		return nil, err
	}
	return messages, nil
}

// ExtractResult is the outcome of extracting the A2UI block from a complete
// LLM response. No tags present is not an error — the reply is plain markdown
// (Messages nil, Err nil). Err is set when a block existed but was
// unterminated or invalid.
type ExtractResult struct {
	CleanText string
	Messages  []any
	Err       error
}

// Extract splits a complete LLM response into visible markdown and the
// validated A2UI message list.
func Extract(text string) ExtractResult {
	start := strings.Index(text, OpenTag)
	if start == -1 {
		return ExtractResult{CleanText: strings.TrimSpace(text)}
	}
	end := strings.Index(text[start+len(OpenTag):], CloseTag)
	if end == -1 {
		return ExtractResult{
			CleanText: strings.TrimSpace(text[:start]),
			Err:       fmt.Errorf("output opened %s but never closed it with %s", OpenTag, CloseTag),
		}
	}
	end += start + len(OpenTag)
	cleanText := strings.TrimSpace(text[:start] + text[end+len(CloseTag):])
	messages, err := ParseBlock(text[start+len(OpenTag) : end])
	return ExtractResult{CleanText: cleanText, Messages: messages, Err: err}
}

// partialTagSuffixLength returns the longest k (< len(tag)) such that s ends
// with the first k characters of tag.
func partialTagSuffixLength(s, tag string) int {
	max := min(len(s), len(tag)-1)
	for k := max; k > 0; k-- {
		if strings.HasSuffix(s, tag[:k]) {
			return k
		}
	}
	return 0
}

// StreamFilter is a stateful stream filter: it forwards text immediately
// (holding back only tails that could be the start of an opening tag split
// across chunks) and buffers tagged block contents silently until the closing
// tag arrives.
type StreamFilter struct {
	buffer  string
	inBlock bool
}

func NewStreamFilter() *StreamFilter {
	return &StreamFilter{}
}

// Push feeds one stream chunk and returns pass-through text safe to forward
// immediately plus the raw inner contents of any completed blocks.
func (f *StreamFilter) Push(chunk string) (text string, blocks []string) {
	f.buffer += chunk
	for {
		if f.inBlock {
			closeIdx := strings.Index(f.buffer, CloseTag)
			if closeIdx == -1 {
				return text, blocks
			}
			blocks = append(blocks, f.buffer[:closeIdx])
			f.buffer = f.buffer[closeIdx+len(CloseTag):]
			f.inBlock = false
		} else {
			openIdx := strings.Index(f.buffer, OpenTag)
			if openIdx == -1 {
				hold := partialTagSuffixLength(f.buffer, OpenTag)
				text += f.buffer[:len(f.buffer)-hold]
				f.buffer = f.buffer[len(f.buffer)-hold:]
				return text, blocks
			}
			text += f.buffer[:openIdx]
			f.buffer = f.buffer[openIdx+len(OpenTag):]
			f.inBlock = true
		}
	}
}

// Flush ends the stream: an unterminated block is surfaced back with its
// opening tag restored rather than silently dropped, so callers can treat it
// as an invalid block instead of leaking raw JSON into the visible text.
func (f *StreamFilter) Flush() string {
	rest := f.buffer
	if f.inBlock {
		rest = OpenTag + rest
	}
	f.buffer = ""
	f.inBlock = false
	return rest
}
