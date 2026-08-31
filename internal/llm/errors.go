package llm

import "fmt"

// ErrorKind classifies errors returned by Client.
type ErrorKind string

const (
	ErrorConnection ErrorKind = "LLMConnectionError"
	ErrorTimeout    ErrorKind = "LLMTimeoutError"
	ErrorResponse   ErrorKind = "LLMResponseError"
)

// Error is a classified LLM client error. StatusCode is set for HTTP errors.
type Error struct {
	Kind       ErrorKind
	StatusCode int
	Err        error
}

func (e *Error) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s: HTTP status %d: %v", e.Kind, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Kind, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }
