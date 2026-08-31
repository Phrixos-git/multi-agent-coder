package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNewValidatorValidatesRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	validator, err := NewValidator(root)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if validator.Root() != canonicalRoot {
		t.Fatalf("Root() = %q, want %q", validator.Root(), canonicalRoot)
	}

	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, invalidRoot := range []string{"", filepath.Join(t.TempDir(), "missing"), file} {
		if _, err := NewValidator(invalidRoot); err == nil {
			t.Errorf("NewValidator(%q) error = nil", invalidRoot)
		}
	}
}

func TestValidatorRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	validator, err := NewValidator(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"../outside.txt",
		"../../outside.txt",
		filepath.Join(root, "absolute.txt"),
	} {
		t.Run(path, func(t *testing.T) {
			_, err := validator.Validate(path)
			var workspaceErr *WorkspaceError
			if !errors.As(err, &workspaceErr) {
				t.Fatalf("Validate(%q) error = %v, want WorkspaceError", path, err)
			}
		})
	}
}

func TestValidatorRejectsSecretFiles(t *testing.T) {
	root := t.TempDir()
	validator, err := NewValidator(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		".env",
		"config/.env.local",
		"certs/client.pem",
		"certs/client.key",
		".ssh/id_rsa",
		".ssh/id_ed25519",
		"credentials.json",
		"secrets.yaml",
	} {
		t.Run(path, func(t *testing.T) {
			_, err := validator.Validate(path)
			var workspaceErr *WorkspaceError
			if !errors.As(err, &workspaceErr) {
				t.Fatalf("Validate(%q) error = %v, want WorkspaceError", path, err)
			}
		})
	}
}

func TestValidatorAllowsRegularFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "main.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package internal"), 0o600); err != nil {
		t.Fatal(err)
	}
	validator, err := NewValidator(root)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := validator.Validate("internal/main.go")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if validated != path {
		t.Fatalf("Validate() = %q, want %q", validated, path)
	}
}
