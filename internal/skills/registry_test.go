package skills

import "testing"

func TestRequiredSecrets_OpenClaw(t *testing.T) {
	s := &Skill{
		Metadata: map[string]any{
			"openclaw": map[string]any{
				"requires": map[string]any{
					"env": []any{"GEMINI_API_KEY", "ANOTHER_KEY"},
				},
			},
		},
	}
	secrets := s.RequiredSecrets()
	if len(secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(secrets))
	}
	if secrets[0].Key != "GEMINI_API_KEY" {
		t.Errorf("expected key GEMINI_API_KEY, got %s", secrets[0].Key)
	}
	if secrets[0].Label != "GEMINI_API_KEY" {
		t.Errorf("expected label GEMINI_API_KEY, got %s", secrets[0].Label)
	}
	if !secrets[0].Required {
		t.Error("expected required=true")
	}
}

func TestRequiredSecrets_Clawdbot(t *testing.T) {
	s := &Skill{
		Metadata: map[string]any{
			"clawdbot": map[string]any{
				"requires": map[string]any{
					"env": []any{"KIE_API_KEY"},
				},
			},
		},
	}
	secrets := s.RequiredSecrets()
	if len(secrets) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(secrets))
	}
	if secrets[0].Key != "KIE_API_KEY" {
		t.Errorf("expected key KIE_API_KEY, got %s", secrets[0].Key)
	}
}

func TestRequiredSecrets_BothSourcesMerged(t *testing.T) {
	s := &Skill{
		Metadata: map[string]any{
			"openclaw": map[string]any{
				"requires": map[string]any{
					"env": []any{"SHARED_KEY", "OPENCLAW_ONLY"},
				},
			},
			"clawdbot": map[string]any{
				"requires": map[string]any{
					"env": []any{"SHARED_KEY", "CLAWDBOT_ONLY"},
				},
			},
		},
	}
	secrets := s.RequiredSecrets()
	if len(secrets) != 3 {
		t.Fatalf("expected 3 secrets (deduplicated), got %d", len(secrets))
	}
	keys := map[string]bool{}
	for _, s := range secrets {
		keys[s.Key] = true
	}
	for _, expected := range []string{"SHARED_KEY", "OPENCLAW_ONLY", "CLAWDBOT_ONLY"} {
		if !keys[expected] {
			t.Errorf("missing expected key %s", expected)
		}
	}
}

func TestRequiredSecrets_NoMetadata(t *testing.T) {
	s := &Skill{}
	secrets := s.RequiredSecrets()
	if len(secrets) != 0 {
		t.Fatalf("expected 0 secrets, got %d", len(secrets))
	}
}

func TestRequiredSecrets_MalformedMetadata(t *testing.T) {
	s := &Skill{
		Metadata: map[string]any{
			"openclaw": "not-a-map",
		},
	}
	secrets := s.RequiredSecrets()
	if len(secrets) != 0 {
		t.Fatalf("expected 0 secrets on malformed metadata, got %d", len(secrets))
	}
}
