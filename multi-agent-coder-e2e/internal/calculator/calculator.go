// Package calculator provides a minimal, deterministic arithmetic domain.
//
// It is the E2E fixture baseline for multi-agent-coder-e2e. Average is
// intentionally omitted so that later E2E phases can add it.
package calculator

import "errors"

// ErrDivisionByZero is returned by Divide when the divisor is zero.
var ErrDivisionByZero = errors.New("calculator: division by zero")

// Add returns a + b.
func Add(a, b float64) float64 {
	return a + b
}

// Subtract returns a - b.
func Subtract(a, b float64) float64 {
	return a - b
}

// Multiply returns a * b.
func Multiply(a, b float64) float64 {
	return a * b
}

// Divide returns a / b. It returns ErrDivisionByZero when b is zero.
func Divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, ErrDivisionByZero
	}
	return a / b, nil
}
