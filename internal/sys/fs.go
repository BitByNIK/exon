package sys

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// FS reads and writes files.
type FS interface {
	Lstat(name string) (fs.FileInfo, error)
	Stat(name string) (fs.FileInfo, error)
	ReadFile(name string) ([]byte, error)
	// Open streams a file that may be too big to read whole, such as a backup.
	Open(name string) (io.ReadCloser, error)
	Readlink(name string) (string, error)
	WalkDir(root string, fn fs.WalkDirFunc) error

	// WriteFile replaces name atomically: a crash never leaves a half-written file.
	// If name is a symlink, the link itself is replaced, not its target.
	WriteFile(name string, data []byte, perm fs.FileMode) error
	MkdirAll(name string, perm fs.FileMode) error
	Symlink(oldname, newname string) error
	Chmod(name string, mode fs.FileMode) error
	// RemoveAll deletes name and anything inside it. A path that does not exist is not an error.
	RemoveAll(name string) error
}

// OSFS is the real file system.
type OSFS struct{}

// Lstat wraps os.Lstat.
func (OSFS) Lstat(name string) (fs.FileInfo, error) { return os.Lstat(name) }

// Open wraps os.Open.
func (OSFS) Open(name string) (io.ReadCloser, error) { return os.Open(name) }

// Stat wraps os.Stat.
func (OSFS) Stat(name string) (fs.FileInfo, error) { return os.Stat(name) }

// ReadFile wraps os.ReadFile.
func (OSFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

// Readlink wraps os.Readlink.
func (OSFS) Readlink(name string) (string, error) { return os.Readlink(name) }

// WalkDir wraps filepath.WalkDir.
func (OSFS) WalkDir(root string, fn fs.WalkDirFunc) error { return filepath.WalkDir(root, fn) }

// MkdirAll wraps os.MkdirAll.
func (OSFS) MkdirAll(name string, perm fs.FileMode) error { return os.MkdirAll(name, perm) }

// Symlink wraps os.Symlink.
func (OSFS) Symlink(oldname, newname string) error { return os.Symlink(oldname, newname) }

// Chmod wraps os.Chmod.
func (OSFS) Chmod(name string, mode fs.FileMode) error { return os.Chmod(name, mode) }

// RemoveAll wraps os.RemoveAll.
func (OSFS) RemoveAll(name string) error { return os.RemoveAll(name) }

// WriteFile writes to a temporary file in the same folder, then renames it over name.
func (OSFS) WriteFile(name string, data []byte, perm fs.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(name), ".exon-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), name)
}

// ReadOnlyFS returns an FS whose changing methods always fail with ErrReadOnly.
func ReadOnlyFS(f FS) FS { return readOnlyFS{f} }

type readOnlyFS struct{ FS }

func (readOnlyFS) WriteFile(name string, _ []byte, _ fs.FileMode) error {
	return fmt.Errorf("%w: write %s", ErrReadOnly, name)
}

func (readOnlyFS) MkdirAll(name string, _ fs.FileMode) error {
	return fmt.Errorf("%w: mkdir %s", ErrReadOnly, name)
}

func (readOnlyFS) Symlink(_, newname string) error {
	return fmt.Errorf("%w: symlink %s", ErrReadOnly, newname)
}

func (readOnlyFS) Chmod(name string, _ fs.FileMode) error {
	return fmt.Errorf("%w: chmod %s", ErrReadOnly, name)
}

func (readOnlyFS) RemoveAll(name string) error {
	return fmt.Errorf("%w: remove %s", ErrReadOnly, name)
}
