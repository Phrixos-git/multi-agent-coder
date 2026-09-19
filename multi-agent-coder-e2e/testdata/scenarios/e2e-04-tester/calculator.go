// Package calculator is an intentionally incompatible E2E-04 candidate.
package calculator

import "errors"

var ErrDivisionByZero = errors.New("calculator: division by zero")

func Add(a, b float64) float64 { return a + b }

// Divide contains the seeded compatibility regression: division by zero no
// longer reports ErrDivisionByZero.
func Divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, nil
	}
	return a / b, nil
}
