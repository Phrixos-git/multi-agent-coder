package agent

import (
	"errors"
	"testing"
)

func TestParseRole(t *testing.T) {
	for _, value := range []string{"coder", "reviewer", "tester", "fixer"} {
		role, err := ParseRole(value)
		if err != nil || string(role) != value {
			t.Fatalf("ParseRole(%q) = %q, %v", value, role, err)
		}
	}

	_, err := ParseRole("architect")
	var roleErr *InvalidRoleError
	if !errors.As(err, &roleErr) {
		t.Fatalf("ParseRole() error = %v, want InvalidRoleError", err)
	}
}
