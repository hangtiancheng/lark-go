package a2ui

import (
	"regexp"
	"strings"
	"testing"
)

// The prompt few-shot examples must themselves pass ParseBlock — mirrors the
// Next.js scripts/a2ui-smoke.ts check.
func TestPromptExamplesAreValid(t *testing.T) {
	re := regexp.MustCompile(`(?s)---BEGIN [A-Z_]+---\n` + regexp.QuoteMeta(OpenTag) + `\n(.*?)\n` + regexp.QuoteMeta(CloseTag) + `\n---END [A-Z_]+---`)
	matches := re.FindAllStringSubmatch(PromptSection, -1)
	if len(matches) != 3 {
		t.Fatalf("expected 3 examples in PromptSection, got %d", len(matches))
	}
	for i, match := range matches {
		msgs, err := ParseBlock(match[1])
		if err != nil {
			t.Errorf("example %d invalid: %v", i, err)
		}
		if len(msgs) != 3 {
			t.Errorf("example %d: expected 3 messages, got %d", i, len(msgs))
		}
	}
}

func TestValidateMessagesRejections(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty list", `[]`},
		{"missing version", `[{"createSurface":{"surfaceId":"x","catalogId":"y"}}]`},
		{"wrong version", `[{"version":"v1.0","createSurface":{"surfaceId":"x","catalogId":"y"}}]`},
		{"no message key", `[{"version":"v0.9"}]`},
		{"unknown key", `[{"version":"v0.9","foo":{"surfaceId":"x"}}]`},
		{"two message keys", `[{"version":"v0.9","createSurface":{"surfaceId":"x","catalogId":"y"},"deleteSurface":{"surfaceId":"x"}}]`},
		{"missing catalogId", `[{"version":"v0.9","createSurface":{"surfaceId":"x"}}]`},
		{"missing surfaceId", `[{"version":"v0.9","deleteSurface":{}}]`},
		{"empty components", `[{"version":"v0.9","updateComponents":{"surfaceId":"x","components":[]}}]`},
		{"component missing id", `[{"version":"v0.9","updateComponents":{"surfaceId":"x","components":[{"component":"Text"}]}}]`},
		{"component missing name", `[{"version":"v0.9","updateComponents":{"surfaceId":"x","components":[{"id":"root"}]}}]`},
		{"not json", `oops`},
	}
	for _, tc := range cases {
		if _, err := ParseBlock(tc.raw); err == nil {
			t.Errorf("%s: expected error, got none", tc.name)
		}
	}
}

func TestParseBlockAcceptsValid(t *testing.T) {
	valid := `[
		{"version":"v0.9","createSurface":{"surfaceId":"s1","catalogId":"` + CatalogID + `"}},
		{"version":"v0.9","updateComponents":{"surfaceId":"s1","components":[{"id":"root","component":"Text","text":"hi"}]}},
		{"version":"v0.9","updateDataModel":{"surfaceId":"s1","path":"/","value":{"a":1}}},
		{"version":"v0.9","deleteSurface":{"surfaceId":"s1"}}
	]`
	msgs, err := ParseBlock(valid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	// A single object is wrapped into a list.
	msgs, err = ParseBlock(`{"version":"v0.9","deleteSurface":{"surfaceId":"s1"}}`)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("single object: msgs=%d err=%v", len(msgs), err)
	}
}

func TestExtract(t *testing.T) {
	block := `[{"version":"v0.9","deleteSurface":{"surfaceId":"s1"}}]`

	res := Extract("plain markdown only")
	if res.CleanText != "plain markdown only" || res.Messages != nil || res.Err != nil {
		t.Fatalf("no-tag extract wrong: %+v", res)
	}

	res = Extract("Summary before.\n" + OpenTag + block + CloseTag + "\nafter")
	if res.CleanText != "Summary before.\n\nafter" {
		t.Errorf("cleanText = %q", res.CleanText)
	}
	if res.Err != nil || len(res.Messages) != 1 {
		t.Errorf("messages=%d err=%v", len(res.Messages), res.Err)
	}

	res = Extract("Summary.\n" + OpenTag + `[{"version":`)
	if res.CleanText != "Summary." || res.Err == nil || res.Messages != nil {
		t.Errorf("unterminated extract wrong: %+v", res)
	}

	res = Extract(OpenTag + "not json" + CloseTag)
	if res.Err == nil || res.Messages != nil {
		t.Errorf("invalid block extract should error: %+v", res)
	}
}

// The filter must produce identical results regardless of chunk boundaries —
// mirrors the smoke test's whole / 7-char / 1-char / mid-tag splits.
func TestStreamFilterChunkInvariance(t *testing.T) {
	block := `[{"version":"v0.9","deleteSurface":{"surfaceId":"s1"}}]`
	input := "Intro text. " + OpenTag + block + CloseTag + " tail text."

	splitRun := func(size int) (string, []string) {
		f := NewStreamFilter()
		var text string
		var blocks []string
		for i := 0; i < len(input); i += size {
			end := min(i+size, len(input))
			tx, bs := f.Push(input[i:end])
			text += tx
			blocks = append(blocks, bs...)
		}
		text += f.Flush()
		return text, blocks
	}

	wantText := "Intro text.  tail text."
	for _, size := range []int{len(input), 7, 1, 5} {
		text, blocks := splitRun(size)
		if text != wantText {
			t.Errorf("size %d: text = %q, want %q", size, text, wantText)
		}
		if len(blocks) != 1 || blocks[0] != block {
			t.Errorf("size %d: blocks = %#v", size, blocks)
		}
	}
}

func TestStreamFilterHoldsPartialTag(t *testing.T) {
	f := NewStreamFilter()
	text, blocks := f.Push("hello <a2ui-")
	if text != "hello " || len(blocks) != 0 {
		t.Fatalf("partial tag not held: text=%q blocks=%v", text, blocks)
	}
	// A false alarm: the tail turns out to be plain text.
	text, _ = f.Push("x rest")
	if text != "<a2ui-x rest" {
		t.Fatalf("false-alarm tail lost: %q", text)
	}
	if rest := f.Flush(); rest != "" {
		t.Fatalf("flush = %q", rest)
	}
}

func TestStreamFilterFlushRestoresUnterminatedBlock(t *testing.T) {
	f := NewStreamFilter()
	text, blocks := f.Push("Summary. " + OpenTag + `[{"version":"v0.9"`)
	if text != "Summary. " || len(blocks) != 0 {
		t.Fatalf("unexpected push output: text=%q blocks=%v", text, blocks)
	}
	rest := f.Flush()
	if !strings.HasPrefix(rest, OpenTag) || !strings.Contains(rest, `"v0.9"`) {
		t.Fatalf("flush should restore open tag: %q", rest)
	}
}
