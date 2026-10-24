// Package systest provides a fake sys.Runner for tests.
package systest

import (
	"context"
	"os/exec"
	"strings"
	"sync"

	"github.com/bitbynik/exon/internal/sys"
)

// Fake is a sys.Runner that records commands and answers them with Handler.
type Fake struct {
	// Handler answers every command. If nil, every command succeeds with no output.
	Handler func(c sys.Cmd) (sys.Result, error)
	// Paths maps command names to paths for LookPath; missing names are not found.
	Paths map[string]string

	mu      sync.Mutex
	queries []sys.Cmd
	execs   []sys.Cmd
}

// Query records c and answers it.
func (f *Fake) Query(_ context.Context, c sys.Cmd) (sys.Result, error) {
	f.mu.Lock()
	f.queries = append(f.queries, c)
	f.mu.Unlock()
	return f.answer(c)
}

// Exec records c and answers it.
func (f *Fake) Exec(_ context.Context, c sys.Cmd) (sys.Result, error) {
	f.mu.Lock()
	f.execs = append(f.execs, c)
	f.mu.Unlock()
	return f.answer(c)
}

// LookPath resolves name from Paths.
func (f *Fake) LookPath(name string) (string, error) {
	if p, ok := f.Paths[name]; ok {
		return p, nil
	}
	return "", exec.ErrNotFound
}

func (f *Fake) answer(c sys.Cmd) (sys.Result, error) {
	if f.Handler == nil {
		return sys.Result{}, nil
	}
	return f.Handler(c)
}

// Execs returns the changing commands run so far, as "name arg arg" strings.
func (f *Fake) Execs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.execs))
	for i, c := range f.execs {
		out[i] = c.String()
	}
	return out
}

// Queries returns the read-only commands run so far, as strings.
func (f *Fake) Queries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.queries))
	for i, c := range f.queries {
		out[i] = c.String()
	}
	return out
}

// Fail returns an ExitError for c with the given stderr, as a failed command would.
func Fail(c sys.Cmd, stderr string) (sys.Result, error) {
	return sys.Result{Stderr: []byte(stderr)}, &sys.ExitError{Cmd: c, Code: 1, Stderr: []byte(stderr)}
}

// Is reports whether c's command line starts with prefix.
func Is(c sys.Cmd, prefix string) bool { return strings.HasPrefix(c.String(), prefix) }
