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

package app

import (
	"net/http"

	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/ai/a2ui"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/ai/agent/plan_execute_replan"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/ai/models"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/utility/logger"
	"github.com/hangtiancheng/swifty.go/swifty_http"
)

// handleAIOps processes AI operations analysis requests.
// It runs the plan-execute-replan agent with a predefined alert analysis prompt
// and returns the final report, detailed execution steps, and an optional A2UI
// surface produced by a post "UI-ify" pass (think model, no tools) — mirrors
// the Next.js /api/ai_ops behavior.
func (a *App) handleAIOps(ctx *swifty_http.Context, next func()) {
	appCtx := ctx.Request.Context()
	resp, detail, err := plan_execute_replan.BuildPlanAgent(appCtx, a.cfg, plan_execute_replan.AIOnOpsQuery)
	if err != nil {
		ctx.Throw(http.StatusInternalServerError, err.Error())
		return
	}
	if resp == "" {
		ctx.Throw(http.StatusInternalServerError, "internal error: empty response")
		return
	}

	data := swifty_http.H{
		"result": resp,
		"detail": detail,
	}
	if cm, cerr := models.NewThinkChatModel(appCtx, a.cfg); cerr != nil {
		logger.L().Error("a2ui: failed to build uiify model", "error", cerr)
	} else if msgs := a2ui.UiifyReport(appCtx, cm, resp); msgs != nil {
		data["a2ui"] = msgs
	}

	ctx.Status = http.StatusOK
	ctx.JSON(swifty_http.H{
		"message": "OK",
		"data":    data,
	})
}
