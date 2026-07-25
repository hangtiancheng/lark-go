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

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/hangtiancheng/swifty.go/swiftx/internal/agent"
	"github.com/hangtiancheng/swifty.go/swiftx/internal/config"
	"github.com/hangtiancheng/swifty.go/swiftx/internal/conversation"
	"github.com/hangtiancheng/swifty.go/swiftx/internal/llm"
	"github.com/hangtiancheng/swifty.go/swiftx/internal/teams"
	"github.com/hangtiancheng/swifty.go/swiftx/internal/tools"
)

// teammateArgs holds the values parsed off the CLI when this process is
// launched as a teammate worker (i.e. spawned by tmux/iTerm from
// teams.BuildTeammateCLI).
type teammateArgs struct {
	teamName   string
	memberName string
}

// parseTeammateFlags returns (args, true) when os.Args carries the
// --teammate flag, signalling teammate-worker mode. Any other value
// returns ok=false and the caller should boot the normal TUI.
//
// Format produced by teams.BuildTeammateCLI:
//
//	swiftx --teammate --team-name <t> --agent-name <n>
//
// The parsing is intentionally minimal: only the three flags this
// worker needs are recognised, and they must come as separate tokens.
func parseTeammateFlags(args []string) (teammateArgs, bool) {
	var out teammateArgs
	if len(args) == 0 || args[0] != "--teammate" {
		return out, false
	}
	i := 1
	for i < len(args) {
		switch args[i] {
		case "--team-name":
			if i+1 < len(args) {
				out.teamName = args[i+1]
				i += 2
				continue
			}
		case "--agent-name":
			if i+1 < len(args) {
				out.memberName = args[i+1]
				i += 2
				continue
			}
		}
		i++
	}
	return out, true
}

// runTeammate boots this process as a worker on an existing team. It
// loads the same config a TUI run would, builds a tools registry, then
// drops into teams.RunInProcessTeammate. The initial task is read from
// the mailbox — the lead writes it there before calling tmux/iTerm
// spawn (see teams.SpawnTeammate).
func runTeammate(args teammateArgs) error {
	if args.teamName == "" || args.memberName == "" {
		return fmt.Errorf("--teammate requires --team-name and --agent-name")
	}

	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("no providers configured")
	}
	provider := cfg.Providers[0]

	client, err := llm.NewClient(&provider, "")
	if err != nil {
		return fmt.Errorf("create LLM client: %w", err)
	}

	registry := tools.NewRegistry()
	for _, t := range builtinTeammateTools() {
		registry.Register(t)
	}

	// The teammate process loads the team directly from config.json on disk so it
	// sees the same member list as the lead. The team directory lives under the
	// user's home directory and is unaffected by worktree directory changes.
	teamMgr := teams.NewTeamManager()
	team := teamMgr.GetTeam(args.teamName)
	if team == nil {
		// Config hasn't been persisted yet (e.g., lead spawns immediately after
		// creating the team); fall back to constructing a local copy. The mailbox
		// directory follows the same convention so delivery still matches.
		team = teams.NewTeam(args.teamName, teams.ModeInProcess)
		teamMgr.CreateTeamWith(team)
	}
	registry.Register(&teams.SendMessageTool{TeamMgr: teamMgr, SenderName: args.memberName})

	member := team.AddMember(args.memberName, client, registry, provider.Protocol)

	// Worker processes get SIGINT/SIGTERM forwarded so closing the
	// pane / Ctrl-C in the tab cleanly cancels the loop and lets
	// deferred cleanup run.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	addendum := teams.BuildTeammateAddendum(args.teamName, args.memberName, nil)

	// No initial prompt argument: the lead wrote the first message to
	// the mailbox before spawn, so the loop's first idle poll picks
	// it up. Passing "" prevents RunInProcessTeammate from injecting
	// a duplicate user message.
	fmt.Fprintf(os.Stderr, "[teammate %s/%s] booted, awaiting tasks\n", args.teamName, args.memberName)
	return teams.RunInProcessTeammate(ctx, team, member, "", addendum, streamEventsToStderr())
}

// builtinTeammateTools is the worker-side tool whitelist. Teammates
// have file/code access but cannot create or delete teams (only the
// lead coordinates team membership).
func builtinTeammateTools() []tools.Tool {
	return []tools.Tool{
		&tools.ReadFileTool{},
		&tools.WriteFileTool{},
		&tools.EditFileTool{},
		&tools.BashTool{},
		&tools.GlobTool{},
		&tools.GrepTool{},
	}
}

// streamEventsToStderr returns a channel that forwards every agent
// event to stderr in a human-readable form. Worker processes have no
// TUI; this gives the tmux/iTerm pane something visible to show.
func streamEventsToStderr() chan<- agent.AgentEvent {
	ch := make(chan agent.AgentEvent, 32)
	go func() {
		for ev := range ch {
			switch e := ev.(type) {
			case agent.StreamText:
				fmt.Fprint(os.Stderr, e.Text)
			case agent.ToolUseEvent:
				fmt.Fprintf(os.Stderr, "\n[tool %s]\n", e.ToolName)
			case agent.ToolResultEvent:
				summary := strings.TrimSpace(e.Output)
				if len(summary) > 200 {
					summary = summary[:200] + "..."
				}
				fmt.Fprintf(os.Stderr, "[result] %s\n", summary)
			case agent.ErrorEvent:
				fmt.Fprintf(os.Stderr, "[error] %s\n", e.Message)
			case agent.LoopComplete:
				fmt.Fprintf(os.Stderr, "[turn done after %d steps]\n", e.TotalTurns)
			}
		}
	}()
	return ch
}

// _ silences the unused-import warning when conversation is referenced
// only indirectly via Member.Conv.
var _ = conversation.NewManager
