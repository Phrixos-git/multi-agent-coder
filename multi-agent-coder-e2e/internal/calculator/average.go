package calculator

import (
	"errors"
	"math"
)

// ErrEmptySlice is returned by Average when the input slice is empty.
var ErrEmptySlice = errors.New("calculator: input slice is empty")

// ErrNonFiniteInput is returned by Average when any input value is NaN
// or positive or negative infinity.
var ErrNonFiniteInput = errors.New("calculator: input contains NaN or infinity")

// Average returns the arithmetic mean of values.
//
// It returns ErrEmptySlice if values is empty and ErrNonFiniteInput if any
// value is NaN or positive or negative infinity.
func Average(values []float64) (float64, error) {
	if len(values) == 0 {
		return 0, ErrEmptySlice
	}
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, ErrNonFiniteInput
		}
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values)), nil
}
