package calculator

import (
	"errors"
	"testing"
)

func TestDivideByZeroContract(t *testing.T) {
	result, err := Divide(1, 0)
	if result != 0 {
		t.Fatalf("Divide(1, 0) result = %v, want 0", result)
	}
	if err == nil {
		t.Fatalf("Divide(1, 0) error = nil, want ErrDivisionByZero")
	}
	if !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("Divide(1, 0) error = %v, want ErrDivisionByZero", err)
	}
}

func TestDivideNormal(t *testing.T) {
	result, err := Divide(10, 2)
	if err != nil || result != 5 {
		t.Fatalf("Divide(10, 2) = (%v, %v), want (5, nil)", result, err)
	}
}
