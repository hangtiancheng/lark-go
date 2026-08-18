package a2ui

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/utility/logger"
)

// CorrectBlock performs one corrective retry when the LLM produced an invalid
// A2UI block: it replays the conversation with the invalid output and the
// validation error — without tools — asking for ONLY a corrected block.
// Returns nil when the retry is still invalid; callers must degrade honestly,
// never fabricate UI data.
func CorrectBlock(ctx context.Context, cm model.BaseChatModel, history []*schema.Message, question, rawAnswer, validationErr string) []any {
	logger.L().Warn("a2ui: invalid block, retrying once", "error", validationErr)
	msgs := make([]*schema.Message, 0, len(history)+4)
	msgs = append(msgs, schema.SystemMessage(PromptSection))
	msgs = append(msgs, history...)
	msgs = append(msgs,
		schema.UserMessage(question),
		schema.AssistantMessage(rawAnswer, nil),
		schema.UserMessage(fmt.Sprintf(
			"Your A2UI block was invalid: %s. Reply with ONLY the corrected JSON array of A2UI v0.9 messages wrapped between %s and %s — no other text.",
			validationErr, OpenTag, CloseTag)),
	)
	resp, err := cm.Generate(ctx, msgs)
	if err != nil {
		logger.L().Error("a2ui: corrective retry request failed", "error", err)
		return nil
	}
	retried := Extract(resp.Content)
	if retried.Messages != nil {
		return retried.Messages
	}
	reason := "no A2UI block found"
	if retried.Err != nil {
		reason = retried.Err.Error()
	}
	logger.L().Error("a2ui: corrective retry still invalid", "error", reason)
	return nil
}
