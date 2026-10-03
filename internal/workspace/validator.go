package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Validator confines file access to one canonical repository root.
type Validator struct {
	root string
}

// NewValidator validates and canonicalizes repositoryRoot.
func NewValidator(repositoryRoot string) (*Validator, error) {
	if strings.TrimSpace(repositoryRoot) == "" {
		return nil, newWorkspaceError("validate repository root", repositoryRoot, fmt.Errorf("repository root is empty"))
	}
	absoluteRoot, err := filepath.Abs(filepath.Clean(repositoryRoot))
	if err != nil {
		return nil, newWorkspaceError("validate repository root", repositoryRoot, err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return nil, newWorkspaceError("validate repository root", repositoryRoot, err)
	}
	info, err := os.Stat(canonicalRoot)
	if err != nil {
		return nil, newWorkspaceError("validate repository root", repositoryRoot, err)
	}
	if !info.IsDir() {
		return nil, newWorkspaceError("validate repository root", repositoryRoot, fmt.Errorf("repository root is not a directory"))
	}
	return &Validator{root: canonicalRoot}, nil
}

// Root returns the canonical absolute repository root.
func (v *Validator) Root() string { return v.root }

// Validate resolves a repository-relative file path and rejects traversal,
// absolute paths, symlink escapes, and deny-listed secret names.
func (v *Validator) Validate(path string) (string, error) {
	if path == "" {
		return "", newWorkspaceError("validate path", path, fmt.Errorf("path is empty"))
	}
	if filepath.IsAbs(path) {
		return "", newWorkspaceError("validate path", path, fmt.Errorf("absolute paths are not allowed"))
	}

	cleaned := filepath.Clean(path)
	if cleaned == "." || escapesRoot(cleaned) {
		return "", newWorkspaceError("validate path", path, fmt.Errorf("path traversal is not allowed"))
	}
	if secretComponent(cleaned) {
		return "", newWorkspaceError("validate path", path, fmt.Errorf("secret files are denied"))
	}

	candidate := filepath.Join(v.root, cleaned)
	canonicalPath, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", newWorkspaceError("resolve path", path, err)
	}
	if !withinRoot(v.root, canonicalPath) {
		return "", newWorkspaceError("validate path", path, fmt.Errorf("path resolves outside repository root"))
	}

	relativeCanonical, err := filepath.Rel(v.root, canonicalPath)
	if err != nil {
		return "", newWorkspaceError("validate path", path, err)
	}
	if secretComponent(relativeCanonical) {
		return "", newWorkspaceError("validate path", path, fmt.Errorf("secret files are denied"))
	}
	return canonicalPath, nil
}

// ValidateOverlay resolves a repository-relative target while allowing the
// leaf to be absent (new or deleted files are valid overlay states).
func (v *Validator) ValidateOverlay(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) {
		return "", newWorkspaceError("validate overlay path", path, fmt.Errorf("path must be repository-relative"))
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." || escapesRoot(cleaned) || secretComponent(cleaned) {
		return "", newWorkspaceError("validate overlay path", path, fmt.Errorf("path traversal or secret path is denied"))
	}
	candidate := filepath.Join(v.root, cleaned)
	probe := candidate
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			if !withinRoot(v.root, resolved) {
				return "", newWorkspaceError("validate overlay path", path, fmt.Errorf("path resolves outside repository root"))
			}
			rel, relErr := filepath.Rel(v.root, resolved)
			if relErr != nil || secretComponent(rel) {
				return "", newWorkspaceError("validate overlay path", path, fmt.Errorf("secret path is denied"))
			}
			return candidate, nil
		}
		if _, statErr := os.Lstat(probe); statErr == nil {
			return "", newWorkspaceError("validate overlay path", path, fmt.Errorf("unresolvable symlink is denied"))
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", newWorkspaceError("validate overlay path", path, err)
		}
		probe = parent
	}
}

func withinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !escapesRoot(relative)
}

func escapesRoot(path string) bool {
	return path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func secretComponent(path string) bool {
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		name := strings.ToLower(component)
		if name == ".env" || (strings.HasPrefix(name, ".env.") && !strings.Contains(name, ".env.example")) ||
			strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") ||
			name == "id_rsa" || name == "id_ed25519" ||
			strings.HasPrefix(name, "credentials") || strings.HasPrefix(name, "secrets") {
			return true
		}
	}
	return false
}

// WorkspaceError classifies repository validation and read failures.
type WorkspaceError struct {
	Operation string
	Path      string
	Err       error
}

func newWorkspaceError(operation, path string, err error) *WorkspaceError {
	return &WorkspaceError{Operation: operation, Path: path, Err: err}
}

func (e *WorkspaceError) Error() string {
	return fmt.Sprintf("WorkspaceError: %s %q: %v", e.Operation, e.Path, e.Err)
}

func (e *WorkspaceError) Unwrap() error { return e.Err }
