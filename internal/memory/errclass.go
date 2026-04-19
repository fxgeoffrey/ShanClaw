package memory

import "errors"

type ErrorClass int

const (
	ClassOK ErrorClass = iota
	ClassRetryable
	ClassPermanent
	ClassUnavailable
)

func (c ErrorClass) String() string {
	return [...]string{"ok", "retryable", "permanent", "unavailable"}[c]
}

// ErrTransport is returned by Client when the call failed before the
// sidecar could reply (refused, EOF, timeout, ctx cancel, decode failure).
var ErrTransport = errors.New("memory: transport failure")

func ClassifyTransportError(err error) ErrorClass {
	if err == nil {
		return ClassOK
	}
	return ClassUnavailable
}

// ClassifyHTTP maps (status, envelope) → ErrorClass per the design's §8 table.
// Unknown sub_codes default to ClassRetryable (fail-open).
func ClassifyHTTP(status int, env *ResponseEnvelope) ErrorClass {
	if status >= 200 && status < 300 {
		return ClassOK
	}
	sub := ""
	code := ""
	if env != nil && env.Error != nil {
		sub = env.Error.SubCode()
		code = env.Error.Code
	}
	switch status {
	case 400:
		return ClassPermanent
	case 401, 403:
		return ClassPermanent
	case 409:
		return ClassRetryable
	case 422:
		return ClassPermanent
	case 503:
		if code == "not_ready" || sub == "not_ready" {
			return ClassUnavailable
		}
		return ClassPermanent
	case 500:
		return ClassRetryable
	}
	return ClassRetryable
}
