package intervals

import "errors"

type Interval struct {
	Start int
	End   int
}

var ErrInvalidInterval = errors.New("invalid interval")

func Normalize(input []Interval) ([]Interval, error) {
	return nil, errors.New("Normalize is not implemented")
}
