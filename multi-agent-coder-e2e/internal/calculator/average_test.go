package calculator

import (
	"errors"
	"math"
	"testing"
)

func TestAverage(t *testing.T) {
	const tolerance = 1e-12
	tests := []struct {
		name    string
		values  []float64
		want    float64
		wantErr error
	}{
		{name: "normal average", values: []float64{1, 2, 3, 4}, want: 2.5},
		{name: "single element", values: []float64{42}, want: 42},
		{name: "negative values", values: []float64{-1, -2, -3}, want: -2},
		{name: "mixed signs", values: []float64{-10, 10, 5}, want: 5.0 / 3.0},
		{name: "empty slice", values: []float64{}, wantErr: ErrEmptySlice},
		{name: "nil slice", values: nil, wantErr: ErrEmptySlice},
		{name: "NaN first", values: []float64{math.NaN(), 1}, wantErr: ErrNonFiniteInput},
		{name: "NaN last", values: []float64{1, math.NaN()}, wantErr: ErrNonFiniteInput},
		{name: "positive infinity middle", values: []float64{1, math.Inf(1), 2}, wantErr: ErrNonFiniteInput},
		{name: "negative infinity first", values: []float64{math.Inf(-1), 1}, wantErr: ErrNonFiniteInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := append([]float64(nil), tt.values...)
			got, err := Average(tt.values)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Average() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if got != 0 {
					t.Fatalf("Average() result on error = %v, want 0", got)
				}
			} else if math.Abs(got-tt.want) > tolerance {
				t.Fatalf("Average() = %v, want %v", got, tt.want)
			}
			if len(tt.values) != len(before) {
				t.Fatalf("Average() changed input length")
			}
			for i := range before {
				if math.Float64bits(tt.values[i]) != math.Float64bits(before[i]) {
					t.Fatalf("Average() mutated input at index %d", i)
				}
			}
		})
	}
}
