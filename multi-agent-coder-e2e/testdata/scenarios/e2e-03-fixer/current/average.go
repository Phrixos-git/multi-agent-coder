package calculator

import (
	"errors"
	"math"
)

var (
	ErrEmptyInput     = errors.New("calculator: input slice must not be empty")
	ErrNonFiniteInput = errors.New("calculator: input slice must not contain non-finite values")
)

func Average(values []float64) (float64, error) {
	if len(values) == 0 {
		return 0, ErrEmptyInput
	}
	var sum float64
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, ErrNonFiniteInput
		}
		sum += value
	}
	return sum / float64(len(values)), nil
}
