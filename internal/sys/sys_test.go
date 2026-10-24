package sys_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitbynik/exon/internal/sys"
)

func TestWriteFileReplacesSymlinkNotTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := (sys.OSFS{}).WriteFile(link, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(target); string(got) != "original" {
		t.Errorf("symlink target changed to %q; only the link should be replaced", got)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("link is still a symlink; want a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("found %d entries, want 2: a temp file was left behind", len(entries))
	}
}

func TestReadOnlyRejectsChanges(t *testing.T) {
	fs := sys.ReadOnlyFS(sys.OSFS{})
	dir := t.TempDir()
	if err := fs.WriteFile(filepath.Join(dir, "x"), nil, 0o644); !errors.Is(err, sys.ErrReadOnly) {
		t.Errorf("WriteFile err = %v, want ErrReadOnly", err)
	}
	if err := fs.MkdirAll(filepath.Join(dir, "d"), 0o755); !errors.Is(err, sys.ErrReadOnly) {
		t.Errorf("MkdirAll err = %v, want ErrReadOnly", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "d")); !os.IsNotExist(err) {
		t.Error("read-only MkdirAll created the folder")
	}
	run := sys.ReadOnlyRunner(&sys.OSRunner{})
	if _, err := run.Exec(context.Background(), sys.Cmd{Name: "true"}); !errors.Is(err, sys.ErrReadOnly) {
		t.Errorf("Exec err = %v, want ErrReadOnly", err)
	}
}

func TestRetry(t *testing.T) {
	transient := errors.New("Could not resolve host")
	fatal := errors.New("no such package")
	isNet := func(err error) bool { return sys.IsNetworkError(err, nil) }

	tests := []struct {
		name      string
		errs      []error // returned by successive calls; nil = success
		attempts  int
		wantCalls int
		wantErr   error
	}{
		{"succeeds after transient failures", []error{transient, transient, nil}, 4, 3, nil},
		{"gives up after attempts", []error{transient, transient, transient}, 3, 3, transient},
		{"does not retry real errors", []error{fatal, nil}, 4, 1, fatal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			err := sys.Retry(context.Background(), tt.attempts, time.Millisecond, isNet, func() error {
				calls++
				return tt.errs[calls-1]
			})
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if calls != tt.wantCalls {
				t.Errorf("calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestRetryStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := sys.Retry(ctx, 5, time.Hour, func(error) bool { return true }, func() error {
		calls++
		cancel()
		return errors.New("Connection reset")
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Errorf("err = %v after %d calls, want context.Canceled after 1", err, calls)
	}
}
