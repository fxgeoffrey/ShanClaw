package memory

import (
	"encoding/json"
	"testing"
)

func TestErrorObject_SubCode(t *testing.T) {
	cases := []struct {
		name string
		in   *ErrorObject
		want string
	}{
		{"nil", nil, ""},
		{"no details", &ErrorObject{Code: "x"}, ""},
		{"sub_code present", &ErrorObject{Details: map[string]any{"sub_code": "schema_validation"}}, "schema_validation"},
		{"sub_code wrong type", &ErrorObject{Details: map[string]any{"sub_code": 42}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.SubCode(); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestResponseEnvelope_RoundTrip(t *testing.T) {
	raw := `{"protocol_version":1,"bundle_version":"0.4.0","bundle_created_at":"2026-04-19T03:14:00Z","bundle_dir":"/x","request_id":"req-abc","candidates":[{"value":"v","score":0.87,"evidence":"observed","supporting_event_ids":["e1"],"entity_id":"e","scope":"s"}],"warnings":[],"reason":"ok","error":null,"latency_ms":4.2}`
	var env ResponseEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if env.Reason != "ok" || len(env.Candidates) != 1 || env.Candidates[0].Score != 0.87 {
		t.Fatalf("decode mismatch: %+v", env)
	}
	out, _ := json.Marshal(env)
	var env2 ResponseEnvelope
	if err := json.Unmarshal(out, &env2); err != nil {
		t.Fatal(err)
	}
	if env2.Reason != env.Reason {
		t.Fatal("round-trip mismatch")
	}
}
