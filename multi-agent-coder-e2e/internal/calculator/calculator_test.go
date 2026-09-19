package calculator

import (
	"errors"
	"testing"
)

func TestAdd(t *testing.T) {
	tests := []struct {
		name string
		a, b float64
		want float64
	}{
		{name: "positive values", a: 2, b: 3, want: 5},
		{name: "negative values", a: -2, b: -3, want: -5},
		{name: "mixed signs", a: -2, b: 3, want: 1},
		{name: "zero", a: 0, b: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Add(tt.a, tt.b); got != tt.want {
				t.Fatalf("Add(%g, %g) = %g, want %g", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSubtract(t *testing.T) {
	tests := []struct {
		name string
		a, b float64
		want float64
	}{
		{name: "positive values", a: 5, b: 3, want: 2},
		{name: "negative result", a: 3, b: 5, want: -2},
		{name: "negative values", a: -2, b: -3, want: 1},
		{name: "zero", a: 0, b: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Subtract(tt.a, tt.b); got != tt.want {
				t.Fatalf("Subtract(%g, %g) = %g, want %g", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestMultiply(t *testing.T) {
	tests := []struct {
		name string
		a, b float64
		want float64
	}{
		{name: "positive values", a: 2, b: 3, want: 6},
		{name: "negative values", a: -2, b: -3, want: 6},
		{name: "mixed signs", a: -2, b: 3, want: -6},
		{name: "zero", a: 5, b: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Multiply(tt.a, tt.b); got != tt.want {
				t.Fatalf("Multiply(%g, %g) = %g, want %g", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestDivide(t *testing.T) {
	tests := []struct {
		name string
		a, b float64
		want float64
	}{
		{name: "positive values", a: 6, b: 3, want: 2},
		{name: "fraction", a: 1, b: 4, want: 0.25},
		{name: "mixed signs", a: 6, b: -3, want: -2},
		{name: "negative divisor", a: -6, b: 3, want: -2},
		{name: "zero dividend", a: 0, b: 3, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Divide(tt.a, tt.b)
			if err != nil {
				t.Fatalf("Divide(%g, %g) returned unexpected error: %v", tt.a, tt.b, err)
			}
			if got != tt.want {
				t.Fatalf("Divide(%g, %g) = %g, want %g", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestDivideByZero(t *testing.T) {
	got, err := Divide(1, 0)
	if err == nil {
		t.Fatalf("Divide(1, 0) = %v, want error", got)
	}
	if !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("Divide(1, 0) error = %v, want errors.Is match with ErrDivisionByZero", err)
	}
	if got != 0 {
		t.Fatalf("Divide(1, 0) = %v, want 0 on error", got)
	}
}
