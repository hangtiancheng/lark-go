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

package plan_execute_replan

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/cloudwego/eino/adk"
	plan_execute "github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/ai/models"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/config"
)

// replanResponse is the structured output format for the replanner.
type replanResponse struct {
	Done      bool     `json:"done"`
	Remaining []string `json:"remaining"`
	Summary   string   `json:"summary"`
}

// NewRePlanAgent creates the replanning agent that adjusts the execution plan
// based on the results of completed steps. It uses structured output instead
// of tool calling for better compatibility with different LLM providers.
func NewRePlanAgent(ctx context.Context, cfg *config.Config) (adk.Agent, error) {
	chatModel, err := models.NewThinkChatModel(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return &customReplanner{
		chatModel: chatModel,
	}, nil
}

type customReplanner struct {
	chatModel model.ToolCallingChatModel
}

func (r *customReplanner) Name(_ context.Context) string {
	return "replanner"
}

func (r *customReplanner) Description(_ context.Context) string {
	return "a replanner agent that uses structured output"
}

func (r *customReplanner) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer func() {
			panicErr := recover()
			if panicErr != nil {
				e := fmt.Errorf("panic in replanner: %v\n%s", panicErr, debug.Stack())
				generator.Send(&adk.AgentEvent{Err: e})
			}
			generator.Close()
		}()

		// Get session values
		executedStep, ok := adk.GetSessionValue(ctx, plan_execute.ExecutedStepSessionKey)
		if !ok {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("executed step not found")})
			return
		}
		executedStepStr := executedStep.(string)

		plan, ok := adk.GetSessionValue(ctx, plan_execute.PlanSessionKey)
		if !ok {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("plan not found")})
			return
		}
		planObj := plan.(plan_execute.Plan)

		var executedSteps []plan_execute.ExecutedStep
		executedStepsVal, ok := adk.GetSessionValue(ctx, plan_execute.ExecutedStepsSessionKey)
		if ok {
			executedSteps = executedStepsVal.([]plan_execute.ExecutedStep)
		}

		// Add current step to executed steps
		executedSteps = append(executedSteps, plan_execute.ExecutedStep{
			Step:   planObj.FirstStep(),
			Result: executedStepStr,
		})
		adk.AddSessionValue(ctx, plan_execute.ExecutedStepsSessionKey, executedSteps)

		userInput, ok := adk.GetSessionValue(ctx, plan_execute.UserInputSessionKey)
		if !ok {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("user input not found")})
			return
		}
		userInputMsgs := userInput.([]adk.Message)

		// Build prompt for structured output
		var userInputStr strings.Builder
		for _, msg := range userInputMsgs {
			userInputStr.WriteString(msg.Content)
			userInputStr.WriteString("\n")
		}

		planBytes, _ := planObj.MarshalJSON()

		// Present completed steps and their results as two separate sections,
		// mirroring the Next.js pipeline (index.ts): steps are enumerated,
		// results are listed in the same order, one per line.
		var completedStepsStr strings.Builder
		var resultsStr strings.Builder
		for i, step := range executedSteps {
			fmt.Fprintf(&completedStepsStr, "%d. %s\n", i+1, step.Step)
			resultsStr.WriteString(step.Result)
			resultsStr.WriteString("\n")
		}

		promptText := fmt.Sprintf(`You are a replanning agent reviewing execution progress toward an objective. Analyze the completed steps and their outcomes to decide whether the objective is fully achieved or further action is required.

Task:
%s

Original Plan:
%s

Completed Steps:
%s

Results So Far:
%s

Based on the progress above, respond with ONLY a JSON object matching this schema:
{
  "done": <boolean>,
  "remaining": ["<step>", ...],
  "summary": "<final report when done, otherwise empty string>"
}

Set "done" to true and provide a comprehensive summary only when the objective is fully achieved. Otherwise, set "done" to false and list only the remaining steps. Do not include any text, explanations, or markdown formatting outside the JSON object.`,
			userInputStr.String(), string(planBytes), completedStepsStr.String(), resultsStr.String())

		// Call the model
		msgs := []*schema.Message{schema.UserMessage(promptText)}
		resp, err := r.chatModel.Generate(ctx, msgs)
		if err != nil {
			generator.Send(&adk.AgentEvent{Err: err})
			return
		}

		// Parse the structured output. extractJSONObject tolerates ```json fences
		// and leading/trailing reasoning that some models (e.g. Qwen3.7) emit
		// even when asked to output JSON only.
		clean, extractErr := extractJSONObject(resp.Content)
		if extractErr != nil {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("extract replan JSON: %w (raw output: %q)", extractErr, resp.Content)})
			return
		}
		var replanResp replanResponse
		if err := json.Unmarshal([]byte(clean), &replanResp); err != nil {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("failed to parse replan response: %w", err)})
			return
		}

		if replanResp.Done {
			// Task complete - send response and break loop
			output := schema.AssistantMessage(replanResp.Summary, nil)
			generator.Send(adk.EventFromMessage(output, nil, schema.Assistant, ""))
			generator.Send(&adk.AgentEvent{Action: adk.NewBreakLoopAction(r.Name(ctx))})
		} else {
			// Need replanning - create new plan
			newPlan := &customPlan{Steps: replanResp.Remaining}
			adk.AddSessionValue(ctx, plan_execute.PlanSessionKey, newPlan)

			planJSON, _ := newPlan.MarshalJSON()
			output := schema.AssistantMessage(string(planJSON), nil)
			generator.Send(adk.EventFromMessage(output, nil, schema.Assistant, ""))
		}
	}()

	return iterator
}
