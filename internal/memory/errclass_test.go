package memory

import "testing"

func TestClassifyHTTP(t *testing.T) {
	cases := []struct {
		name   string
		status int
		env    *ResponseEnvelope
		want   ErrorClass
	}{
		{"200 ok", 200, &ResponseEnvelope{Reason: "ok"}, ClassOK},
		{"200 no_data", 200, &ResponseEnvelope{Reason: "no_data"}, ClassOK},
		{"200 degraded", 200, &ResponseEnvelope{Reason: "degraded"}, ClassOK},
		{"401 auth", 401, &ResponseEnvelope{Error: &ErrorObject{Code: "auth", Details: map[string]any{"sub_code": "invalid_api_key"}}}, ClassPermanent},
		{"403 forbidden", 403, &ResponseEnvelope{Error: &ErrorObject{Code: "auth", Details: map[string]any{"sub_code": "token_expired"}}}, ClassPermanent},
		{"503 not_ready", 503, &ResponseEnvelope{Error: &ErrorObject{Code: "not_ready"}}, ClassUnavailable},
		{"503 incompatible", 503, &ResponseEnvelope{Error: &ErrorObject{Code: "incompatible_bundle", Details: map[string]any{"sub_code": "version_out_of_range"}}}, ClassPermanent},
		{"503 missing_artifact", 503, &ResponseEnvelope{Error: &ErrorObject{Code: "incompatible_bundle", Details: map[string]any{"sub_code": "missing_artifact"}}}, ClassPermanent},
		{"422 schema", 422, &ResponseEnvelope{Error: &ErrorObject{Code: "validation_error", Details: map[string]any{"sub_code": "schema_validation"}}}, ClassPermanent},
		{"400 unsupported_protocol", 400, &ResponseEnvelope{Error: &ErrorObject{Code: "validation_error", Details: map[string]any{"sub_code": "unsupported_protocol"}}}, ClassPermanent},
		{"409 reload_in_progress", 409, &ResponseEnvelope{Error: &ErrorObject{Code: "bundle_load_error", Details: map[string]any{"sub_code": "reload_in_progress"}}}, ClassRetryable},
		{"500 query_failed", 500, &ResponseEnvelope{Error: &ErrorObject{Code: "internal_error", Details: map[string]any{"sub_code": "query_failed"}}}, ClassRetryable},
		{"unknown 5xx no sub_code", 599, &ResponseEnvelope{Error: &ErrorObject{Code: "x"}}, ClassRetryable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyHTTP(tc.status, tc.env); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestClassifyTransportError(t *testing.T) {
	if ClassifyTransportError(nil) != ClassOK {
		t.Fatal("nil should be OK")
	}
	if ClassifyTransportError(ErrTransport) != ClassUnavailable {
		t.Fatal("transport sentinel should be Unavailable")
	}
}
