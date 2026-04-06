package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAgentsAttachingSkill(t *testing.T) {
	agentsDir := t.TempDir()

	// Agent "coder" attaches self-improving-agent + other-skill.
	if err := SetAttachedSkills(agentsDir, "coder", []string{"self-improving-agent", "other-skill"}); err != nil {
		t.Fatalf("SetAttachedSkills coder: %v", err)
	}
	// Agent "researcher" attaches self-improving-agent only.
	if err := SetAttachedSkills(agentsDir, "researcher", []string{"self-improving-agent"}); err != nil {
		t.Fatalf("SetAttachedSkills researcher: %v", err)
	}
	// Agent "writer" attaches nothing — no manifest file at all.
	// (Create the directory so the walk sees it.)
	if err := os.MkdirAll(filepath.Join(agentsDir, "writer"), 0700); err != nil {
		t.Fatalf("mkdir writer: %v", err)
	}

	got, err := AgentsAttachingSkill(agentsDir, "self-improving-agent")
	if err != nil {
		t.Fatalf("AgentsAttachingSkill: %v", err)
	}
	want := []string{"coder", "researcher"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Skill not attached anywhere → empty slice, not nil, for clean JSON marshalling.
	got, err = AgentsAttachingSkill(agentsDir, "unused-skill")
	if err != nil {
		t.Fatalf("AgentsAttachingSkill unused: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("expected empty slice for unused skill, got %v", got)
	}
}
