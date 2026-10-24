// Package sys is the only place exon touches the operating system: running
// commands and reading or writing files. Steps receive these through step.Env,
// so the engine can hand them read-only versions while planning.
package sys

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Cmd describes a command to run.
type Cmd struct {
	Name string
	Args []string
	Dir  string
	// Env holds extra KEY=VALUE pairs appended to the current environment.
	Env []string
	// Interactive connects the command to the user's terminal instead of
	// capturing its output. Used for commands that may ask for a password.
	Interactive bool
}

func (c Cmd) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Result is the captured output of a finished command.
type Result struct {
	Stdout []byte
	Stderr []byte
}

// Output returns stdout followed by stderr.
func (r Result) Output() []byte {
	return append(append([]byte{}, r.Stdout...), r.Stderr...)
}

// ExitError reports a command that ran but exited with a non-zero status.
type ExitError struct {
	Cmd    Cmd
	Code   int
	Stderr []byte
}

func (e *ExitError) Error() string {
	msg := strings.TrimSpace(lastLines(string(e.Stderr), 1))
	if msg == "" {
		return fmt.Sprintf("%s: exit status %d", e.Cmd, e.Code)
	}
	return fmt.Sprintf("%s: exit status %d: %s", e.Cmd, e.Code, msg)
}

// Runner runs commands.
type Runner interface {
	// Query runs a command that only reads state. It runs even while planning.
	Query(ctx context.Context, c Cmd) (Result, error)
	// Exec runs a command that changes the system.
	Exec(ctx context.Context, c Cmd) (Result, error)
	// LookPath reports where a command is installed.
	LookPath(name string) (string, error)
}

// OSRunner runs real commands.
type OSRunner struct {
	// Log receives every command line and the output of Exec commands. May be nil.
	Log io.Writer
	// Echo, if set, also receives command output as it happens (--verbose).
	Echo io.Writer
}

// Query runs c. Its stdout is not logged, as it is usually machine-readable and large.
func (r *OSRunner) Query(ctx context.Context, c Cmd) (Result, error) {
	return r.run(ctx, c, false)
}

// Exec runs c, logging all of its output.
func (r *OSRunner) Exec(ctx context.Context, c Cmd) (Result, error) {
	return r.run(ctx, c, true)
}

// LookPath wraps exec.LookPath.
func (r *OSRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (r *OSRunner) run(ctx context.Context, c Cmd, logStdout bool) (Result, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	// On Ctrl+C, ask the command to stop cleanly first; kill it only if it hangs.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 30 * time.Second

	if r.Log != nil {
		fmt.Fprintf(r.Log, "$ %s\n", c)
	}
	var stdout, stderr bytes.Buffer
	if c.Interactive {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	} else {
		cmd.Stdout = r.writer(&stdout, logStdout)
		cmd.Stderr = r.writer(&stderr, true)
	}

	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return res, nil
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		if r.Log != nil {
			fmt.Fprintf(r.Log, "  exit status %d\n", ee.ExitCode())
		}
		return res, &ExitError{Cmd: c, Code: ee.ExitCode(), Stderr: res.Stderr}
	}
	return res, fmt.Errorf("run %s: %w", c, err)
}

func (r *OSRunner) writer(buf *bytes.Buffer, log bool) io.Writer {
	ws := []io.Writer{buf}
	if log && r.Log != nil {
		ws = append(ws, r.Log)
	}
	if r.Echo != nil {
		ws = append(ws, r.Echo)
	}
	return io.MultiWriter(ws...)
}

// ErrReadOnly is returned when something tries to change the system through a
// read-only Runner or FS. Seeing it means a step tried to change things while planning.
var ErrReadOnly = errors.New("change attempted while planning")

// ReadOnlyRunner returns a Runner whose Exec always fails with ErrReadOnly.
func ReadOnlyRunner(r Runner) Runner { return readOnlyRunner{r} }

type readOnlyRunner struct{ Runner }

func (readOnlyRunner) Exec(_ context.Context, c Cmd) (Result, error) {
	return Result{}, fmt.Errorf("%w: %s", ErrReadOnly, c)
}

// LastLines returns the last n non-empty lines of s.
func LastLines(s string, n int) string { return lastLines(s, n) }

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	out := make([]string, 0, n)
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			out = append(out, lines[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return strings.Join(out, "\n")
}
