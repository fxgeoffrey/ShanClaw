package cloudflow

import (
	"context"
	"errors"
	"testing"
)

// nilGateway exercises the early-return path when no gateway is configured.
func TestRun_NoGateway_ReturnsError(t *testing.T) {
	_, err := Run(context.Background(), Request{
		Gateway: nil,
		APIKey:  "",
		Query:   "anything",
	}, nil)
	if err == nil {
		t.Fatalf("expected error when Gateway is nil, got nil")
	}
	if !errors.Is(err, ErrGatewayNotConfigured) {
		t.Fatalf("expected ErrGatewayNotConfigured, got: %v", err)
	}
}
