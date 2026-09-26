package agent

import "fmt"

// Role identifies an independent logical agent.
type Role string

const (
	RoleCoder    Role = "coder"
	RoleReviewer Role = "reviewer"
	RoleTester   Role = "tester"
	RoleFixer    Role = "fixer"
)

var validRoles = map[Role]struct{}{
	RoleCoder:    {},
	RoleReviewer: {},
	RoleTester:   {},
	RoleFixer:    {},
}

// ParseRole validates a role name.
func ParseRole(value string) (Role, error) {
	role := Role(value)
	if _, ok := validRoles[role]; !ok {
		return "", &InvalidRoleError{Role: value}
	}
	return role, nil
}

// Valid reports whether the role is supported in Version 1.
func (r Role) Valid() bool {
	_, ok := validRoles[r]
	return ok
}

// InvalidRoleError reports an unsupported agent role.
type InvalidRoleError struct {
	Role string
}

func (e *InvalidRoleError) Error() string {
	return fmt.Sprintf("InvalidRoleError: unsupported role %q", e.Role)
}
