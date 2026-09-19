package calculator

import (
	"errors"
	"math"
	"testing"
)

func TestAverageAfterFix(t *testing.T) {
	tests := []struct {
		name    string
		values  []float64
		want    float64
		wantErr error
	}{
		{name: "normal", values: []float64{1, 2, 3, 4}, want: 2.5},
		{name: "empty", values: []float64{}, wantErr: ErrEmptyInput},
		{name: "NaN", values: []float64{1, math.NaN()}, wantErr: ErrNonFiniteInput},
		{name: "positive infinity", values: []float64{math.Inf(1)}, wantErr: ErrNonFiniteInput},
		{name: "negative infinity", values: []float64{math.Inf(-1)}, wantErr: ErrNonFiniteInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Average(tt.values)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Average() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && got != 0 {
				t.Fatalf("Average() result on error = %v, want 0", got)
			}
			if tt.wantErr == nil && got != tt.want {
				t.Fatalf("Average() = %v, want %v", got, tt.want)
			}
		})
	}
}
