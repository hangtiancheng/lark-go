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
	"github.com/hangtiancheng/swifty.go/swiftx/internal/teams"
	"testing"
)

func TestParseTeammateFlagsAbsent(t *testing.T) {
	cases := [][]string{
		{},
		{"--help"},
		{"--something", "else"},
	}
	for _, args := range cases {
		if _, ok := parseTeammateFlags(args); ok {
			t.Errorf("parseTeammateFlags(%v) returned ok=true, want false", args)
		}
	}
}

func TestParseTeammateFlagsBasic(t *testing.T) {
	args := []string{"--teammate", "--team-name", "alpha", "--agent-name", "ann"}
	got, ok := parseTeammateFlags(args)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.teamName != "alpha" || got.memberName != "ann" {
		t.Errorf("parsed = %+v", got)
	}
}

func TestParseTeammateFlagsMissingValue(t *testing.T) {
	// Trailing flag without its value should not panic and just leave
	// the field empty so runTeammate can return a friendly error.
	args := []string{"--teammate", "--team-name"}
	got, ok := parseTeammateFlags(args)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.teamName != "" {
		t.Errorf("expected empty teamName, got %q", got.teamName)
	}
}

func TestParseTeammateFlagsIgnoresUnknown(t *testing.T) {
	args := []string{"--teammate", "--noise", "x", "--team-name", "t", "--agent-name", "m"}
	got, ok := parseTeammateFlags(args)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.teamName != "t" || got.memberName != "m" {
		t.Errorf("parsed = %+v", got)
	}
}

// 队友进程自己组装工具集，和进程内队员那份很容易各改各的，所以这里把清单钉死：
// 协作工具必须在，团队管理和子 Agent 必须不在。
func TestBuildTeammateRegistryToolSet(t *testing.T) {
	reg := buildTeammateRegistry(context.Background(), teammateToolOptions{
		WorkDir:    t.TempDir(),
		Protocol:   "anthropic",
		SessionID:  "sess-1",
		TeamMgr:    teams.NewTeamManager(),
		TeamName:   "alpha",
		MemberName: "ann",
	})

	// 干活的工具、通用能力，以及队友之间协作要用的消息和共享任务板
	mustHave := []string{
		"ReadFile", "WriteFile", "EditFile", "Bash", "Glob", "Grep",
		"ToolSearch", "SyntheticOutput", "EnterWorktree", "ExitWorktree",
		"SendMessage", "TaskCreate", "TaskGet", "TaskList", "TaskUpdate",
	}
	for _, name := range mustHave {
		if reg.Get(name) == nil {
			t.Errorf("队友工具集缺少 %s", name)
		}
	}

	// 派人和建团队是 Lead 的职责，队友拿不到
	mustNotHave := []string{"Agent", "TeamCreate", "TeamDelete"}
	for _, name := range mustNotHave {
		if reg.Get(name) != nil {
			t.Errorf("队友工具集不应包含 %s", name)
		}
	}
}
