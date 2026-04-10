package runstatus

import (
	"context"
	"errors"
	"strings"
)

type Code string

const (
	CodeNone                  Code = ""
	CodeUserCancelled         Code = "user_cancelled"
	CodeDeadlineExceeded      Code = "deadline_exceeded"
	CodeRateLimited           Code = "rate_limited"
	CodeProviderOverloaded    Code = "provider_overloaded"
	CodeServiceTemporaryError Code = "service_temporary_error"
	CodeNetworkInterrupted    Code = "network_interrupted"
	CodeIterationLimit        Code = "iteration_limit"
	CodeUnexpected            Code = "unexpected"
)

var friendlyMessages = map[Code]string{
	CodeUserCancelled:         "The request was cancelled.",
	CodeDeadlineExceeded:      "The request timed out.",
	CodeRateLimited:           "Sorry, the AI service is currently rate-limited. Please try again in a moment.",
	CodeProviderOverloaded:    "Sorry, the AI service is temporarily overloaded. Please try again shortly.",
	CodeServiceTemporaryError: "Sorry, the AI service encountered a temporary error. Please try again.",
	CodeNetworkInterrupted:    "Sorry, the connection to the AI service was interrupted. Please try again.",
	CodeIterationLimit:        "The request reached its iteration limit and returned a partial result.",
	CodeUnexpected:            "Sorry, an unexpected error occurred. Please try again.",
}

func FriendlyMessage(code Code) string {
	if msg, ok := friendlyMessages[code]; ok {
		return msg
	}
	return friendlyMessages[CodeUnexpected]
}

func IsFriendlyMessage(text string) bool {
	for _, msg := range friendlyMessages {
		if text == msg {
			return true
		}
	}
	return false
}

func CodeFromError(err error) Code {
	if err == nil {
		return CodeNone
	}
	if errors.Is(err, context.Canceled) {
		return CodeUserCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeDeadlineExceeded
	}

	msg := err.Error()
	switch {
	case strings.Contains(msg, "429"):
		return CodeRateLimited
	case strings.Contains(msg, "529") || strings.Contains(msg, "overloaded"):
		return CodeProviderOverloaded
	case strings.Contains(msg, "500") || strings.Contains(msg, "502") || strings.Contains(msg, "503"):
		return CodeServiceTemporaryError
	case strings.Contains(msg, "request failed:") || strings.Contains(msg, "stream read error"):
		return CodeNetworkInterrupted
	default:
		return CodeUnexpected
	}
}
