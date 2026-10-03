package workspace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReaderReadsRepositoryFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "safe.go"), []byte("safe content"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	content, err := reader.ReadFile("safe.go")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "safe content" {
		t.Fatalf("ReadFile() = %q", content)
	}
}

func TestReaderCannotReadOutsideRepository(t *testing.T) {
	rootParent := t.TempDir()
	root := filepath.Join(rootParent, "repository")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(rootParent, "outside.txt")
	secretContent := []byte("must-not-be-returned")
	if err := os.WriteFile(outsidePath, secretContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	paths := []string{
		"../outside.txt",
		"../../outside.txt",
		outsidePath,
		"escape",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			content, err := reader.ReadFile(path)
			var workspaceErr *WorkspaceError
			if !errors.As(err, &workspaceErr) {
				t.Fatalf("ReadFile(%q) error = %v, want WorkspaceError", path, err)
			}
			if content != nil || bytes.Contains(content, secretContent) {
				t.Fatalf("ReadFile(%q) returned outside content %q", path, content)
			}
		})
	}
}

func TestReaderRejectsSymlinkAliasForSecret(t *testing.T) {
	root := t.TempDir()
	secretContent := []byte("secret")
	if err := os.WriteFile(filepath.Join(root, ".env"), secretContent, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"apparently-safe", ".env.example"} {
		if err := os.Symlink(".env", filepath.Join(root, alias)); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	for _, alias := range []string{"apparently-safe", ".env.example"} {
		content, err := reader.ReadFile(alias)
		if err == nil || content != nil || bytes.Contains(content, secretContent) {
			t.Fatalf("ReadFile(%q) content = %q, error = %v", alias, content, err)
		}
		if _, err := reader.validator.ValidateOverlay(alias); err == nil {
			t.Fatalf("ValidateOverlay(%q) accepted symlink to secret", alias)
		}
	}
}

func TestReaderRejectsSymlinkEscapeAfterValidation(t *testing.T) {
	rootParent := t.TempDir()
	root := filepath.Join(rootParent, "repository")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	insidePath := filepath.Join(root, "inside.txt")
	if err := os.WriteFile(insidePath, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideContent := []byte("outside-must-not-be-returned")
	outsidePath := filepath.Join(rootParent, "outside.txt")
	if err := os.WriteFile(outsidePath, outsideContent, 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(root, "changing-link")
	if err := os.Symlink("inside.txt", linkPath); err != nil {
		t.Fatal(err)
	}

	reader, err := NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if _, err := reader.validator.Validate("changing-link"); err != nil {
		t.Fatalf("initial Validate() error = %v", err)
	}
	if err := os.Remove(linkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Fatal(err)
	}

	content, err := reader.readConfined("changing-link")
	if err == nil {
		t.Fatal("readConfined() error = nil after symlink escape replacement")
	}
	if content != nil || bytes.Contains(content, outsideContent) {
		t.Fatalf("readConfined() returned outside content %q", content)
	}
}
