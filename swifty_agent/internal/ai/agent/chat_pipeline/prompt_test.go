package chat_pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/ai/a2ui"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/config"
)

// The A2UI prompt section is full of literal JSON braces; it must be injected
// as an FString variable VALUE so the template parser never sees them.
func TestChatTemplateInjectsA2uiSection(t *testing.T) {
	ctx := context.Background()
	tpl, err := newChatTemplate(ctx, &config.Config{})
	if err != nil {
		t.Fatalf("newChatTemplate: %v", err)
	}
	vars, err := newInputToChatLambda(ctx, &UserMessage{
		ID:      "t",
		Query:   "hello",
		History: []*schema.Message{},
	})
	if err != nil {
		t.Fatalf("newInputToChatLambda: %v", err)
	}
	vars["documents"] = "doc-body"

	msgs, err := tpl.Format(ctx, vars)
	if err != nil {
		t.Fatalf("template format failed (FString brace leak?): %v", err)
	}
	if len(msgs) == 0 || msgs[0].Role != schema.System {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	system := msgs[0].Content
	for _, want := range []string{
		a2ui.OpenTag,
		`{"version":"v0.9"`,
		"## Interactive UI (A2UI v0.9)",
		"doc-body",
	} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	if strings.Contains(system, "{a2ui_section}") {
		t.Error("a2ui_section placeholder was not substituted")
	}
}
