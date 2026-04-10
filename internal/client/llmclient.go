package client

import "context"

// LLMClient is the common interface for LLM completion backends.
// Satisfied by *GatewayClient and *OllamaClient.
type LLMClient interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	CompleteStream(ctx context.Context, req CompletionRequest, onDelta func(StreamDelta)) (*CompletionResponse, error)
}
