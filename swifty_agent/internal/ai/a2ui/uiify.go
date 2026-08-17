package a2ui

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/utility/logger"
)

// UiifyReport runs the post "UI-ify" pass: one no-tools call that optionally
// renders a finished AI Ops report as an A2UI surface. Returns nil when the
// report has nothing structured to visualize, the block stays invalid after
// one corrective retry, or the call fails — the report itself is never at
// risk. Mirrors uiifyReport in the Next.js plan-execute-replan pipeline.
func UiifyReport(ctx context.Context, cm model.BaseChatModel, report string) []any {
	system := "You render A2UI surfaces for an OnCall assistant.\n" + PromptSection
	question := "Below is an alert operations analysis report. If it presents structured data worth visualizing " +
		"(alert lists, metric series, tabular results), reply with ONLY one A2UI block wrapped between " +
		OpenTag + " and " + CloseTag + ".\n" +
		"Rules:\n" +
		"- The report is the ONLY source: visualize facts it states, copied verbatim — NEVER invent data.\n" +
		"- Do not visualize intermediate execution chatter (e.g. current-time lookups) and never repeat the same data twice.\n" +
		"- Never render empty tables or placeholder rows like \"(none)\" or \"—\".\n" +
		"- Titles must be short noun phrases, not sentences; omit a Table caption when a heading already labels it.\n" +
		"- If the report has nothing structured to render (e.g. zero active alerts, prose-only conclusions), reply with the single word NONE.\n\n" +
		"Report:\n" + report
	resp, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(question),
	})
	if err != nil {
		logger.L().Error("a2ui: ai_ops uiify request failed", "error", err)
		return nil
	}
	extracted := Extract(resp.Content)
	if extracted.Messages != nil {
		return extracted.Messages
	}
	if extracted.Err == nil {
		// No block at all (e.g. the model replied NONE): nothing to render.
		return nil
	}
	return CorrectBlock(ctx, cm, nil, question, resp.Content, extracted.Err.Error())
}
