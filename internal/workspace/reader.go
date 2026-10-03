package workspace

import (
	"io"
	"os"
	"path/filepath"
)

// Reader reads files only after repository and secret-file validation.
type Reader struct {
	validator *Validator
	root      *os.Root
}

// NewReader constructs a repository-confined file reader.
func NewReader(repositoryRoot string) (*Reader, error) {
	validator, err := NewValidator(repositoryRoot)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(validator.Root())
	if err != nil {
		return nil, newWorkspaceError("open repository root", repositoryRoot, err)
	}
	return &Reader{validator: validator, root: root}, nil
}

// Close releases the repository root handle.
func (r *Reader) Close() error {
	if err := r.root.Close(); err != nil {
		return newWorkspaceError("close repository root", r.validator.Root(), err)
	}
	return nil
}

// ReadFile reads one repository-relative file. It never returns file content
// when validation fails.
func (r *Reader) ReadFile(path string) ([]byte, error) {
	if _, err := r.validator.Validate(path); err != nil {
		return nil, err
	}
	return r.readConfined(path)
}

// ValidatePath checks a requested path without reading its content.
func (r *Reader) ValidatePath(path string) error { _, err := r.validator.Validate(path); return err }

// ValidateOverlayPath permits missing leaves for Pi fixer new/deleted files.
func (r *Reader) ValidateOverlayPath(path string) error {
	_, err := r.validator.ValidateOverlay(path)
	return err
}

// readConfined performs the actual operation relative to an open directory
// handle. os.Root prevents symlink replacement between validation and use
// from redirecting the read outside the repository.
func (r *Reader) readConfined(path string) ([]byte, error) {
	file, err := r.root.Open(filepath.Clean(path))
	if err != nil {
		return nil, newWorkspaceError("read file", path, err)
	}
	content, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, newWorkspaceError("read file", path, readErr)
	}
	if closeErr != nil {
		return nil, newWorkspaceError("close file", path, closeErr)
	}
	return content, nil
}
