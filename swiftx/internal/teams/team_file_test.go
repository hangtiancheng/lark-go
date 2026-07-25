package teams

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTeamFileRoundTrip(t *testing.T) {
	useTempHome(t)

	tm := NewTeamManager()
	team := tm.CreateTeamFull("Refactor Auth", ModeInProcess, "lead", "refactor the auth module")
	team.AddMember("alice", nil, nil, "anthropic")
	team.SetMemberMeta("alice", "worker", "claude-sonnet-4-6", "/tmp/wt/alice")

	// Use a fresh manager to simulate a teammate process or the next session.
	fresh := NewTeamManager()
	got := fresh.GetTeam("Refactor Auth")
	if got == nil {
		t.Fatal("expected team to be reconstructed from disk, got nil")
	}
	if got.LeadAgentID != "lead" {
		t.Errorf("LeadAgentID = %q, want lead", got.LeadAgentID)
	}
	if got.Description != "refactor the auth module" {
		t.Errorf("Description = %q, want 'refactor the auth module'", got.Description)
	}
	m, ok := got.Members["alice"]
	if !ok {
		t.Fatalf("member alice was not restored, current members: %v", got.Members)
	}
	if m.AgentType != "worker" || m.Model != "claude-sonnet-4-6" || m.WorktreePath != "/tmp/wt/alice" {
		t.Errorf("member metadata mismatch: %+v", m)
	}
}

func TestTeamFilePathIsSanitized(t *testing.T) {
	useTempHome(t)

	tm := NewTeamManager()
	tm.CreateTeamFull("Refactor Auth!", ModeTmux, "lead", "")

	want := filepath.Join(teamsBaseDir(), "refactor-auth-", "config.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected config at %s, stat failed: %v", want, err)
	}
}

func TestDeleteTeamRemovesDir(t *testing.T) {
	useTempHome(t)

	tm := NewTeamManager()
	tm.CreateTeamFull("gone", ModeInProcess, "lead", "")
	if _, err := os.Stat(teamDir("gone")); err != nil {
		t.Fatalf("directory should exist after team creation: %v", err)
	}

	tm.DeleteTeam("gone")
	if _, err := os.Stat(teamDir("gone")); !os.IsNotExist(err) {
		t.Errorf("directory should be removed after team deletion, err = %v", err)
	}
	if fresh := NewTeamManager().GetTeam("gone"); fresh != nil {
		t.Errorf("a deleted team should not be recoverable from disk")
	}
}

func TestGetTeamMissingReturnsNil(t *testing.T) {
	useTempHome(t)
	if got := NewTeamManager().GetTeam("never-existed"); got != nil {
		t.Errorf("non-existent team should return nil, got %+v", got)
	}
}
