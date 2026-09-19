// Package calculator is a controlled negative fixture for E2E-02.
// This is not production code: non-finite validation is intentionally absent.
package calculator

import "errors"

// ErrEmptyInput is returned for empty input.
var ErrEmptyInput = errors.New("calculator: input slice must not be empty")

// Average returns the arithmetic mean of values.
func Average(values []float64) (float64, error) {
	if len(values) == 0 {
		return 0, ErrEmptyInput
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values)), nil
}
