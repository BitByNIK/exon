// Package cli wires exon's commands to the engine.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/bitbynik/exon/internal/engine"
	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/all"
	"github.com/bitbynik/exon/internal/ui/plain"
	"github.com/bitbynik/exon/internal/usererr"
)

// version is set at build time with -ldflags.
var version = "dev"

// exitError carries an exit code out of a command.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

// Execute runs exon and returns the process exit code.
func Execute() (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "✗ exon crashed: %v\n  This is a bug. Please report it with the log file from ~/.local/state/exon/logs.\n", r)
			if f, err := os.OpenFile(crashPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				fmt.Fprintf(f, "%s panic: %v\n%s\n", time.Now().Format(time.RFC3339), r, debug.Stack())
				_ = f.Close()
			}
			code = engine.ExitProblems
		}
	}()

	root := &cobra.Command{
		Use:           "exon",
		Short:         "Set up a freshly reset Mac from your config repo",
		SilenceUsage:  true,
		SilenceErrors: true,
		// exon runs once after a reset; shell completion scripts aren't worth shipping.
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	root.AddCommand(setupCmd(), devCmd(), versionCmd())

	err := root.Execute()
	if err == nil {
		return engine.ExitOK
	}
	if ee, ok := errors.AsType[exitError](err); ok {
		return ee.code
	}
	plain.New(os.Stderr, "").Error(err)
	if _, ok := usererr.As(err); !ok {
		// Cobra usage errors (unknown flag, missing argument): point at the help of the command that was typed.
		used, _, _ := root.Find(os.Args[1:])
		fmt.Fprintf(os.Stderr, "  Run '%s --help' for usage.\n", used.CommandPath())
	}
	return engine.ExitProblems
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the exon version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "exon", version)
		},
	}
}

type setupFlags struct {
	dryRun, yes, verbose bool
}

func setupCmd() *cobra.Command {
	var f setupFlags
	cmd := &cobra.Command{
		Use:   "setup SOURCE [STEP...]",
		Short: "Run setup steps from a config repo",
		Long: `Run setup steps from a config repo.

SOURCE is a GitHub repo (owner/repo) or a local folder (./my-config).
STEP limits the run to some steps; they always run in this order:
  ` + strings.Join(all.Names(), ", "),
		Example: `  exon setup bitbynik/exon-config
  exon setup ~/Projects/exon-config --dry-run
  exon setup bitbynik/exon-config configs`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := runSetup(cmd.Context(), f, args[0], args[1:], cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if code != engine.ExitOK {
				return exitError{code}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "show what would change, change nothing")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "don't ask for confirmation; answer questions with their defaults")
	cmd.Flags().BoolVarP(&f.verbose, "verbose", "v", false, "also show the output of every command")
	return cmd
}

func runSetup(parent context.Context, f setupFlags, srcArg string, stepArgs []string, out io.Writer) (int, error) {
	steps, err := selectSteps(stepArgs)
	if err != nil {
		return 0, err
	}
	r, code, err := start(parent, out, startOptions{
		command:      "exon setup",
		source:       srcArg,
		dryRun:       f.dryRun,
		verbose:      f.verbose,
		minFreeBytes: 10 << 30,
		logAttrs:     []any{"steps", stepArgs},
	})
	if r == nil {
		return code, err
	}
	defer r.close()

	var checklist struct {
		Items []string `toml:"items"`
	}
	if err := r.cfg.Section("checklist").Decode(&checklist); err != nil {
		return 0, err
	}

	opts := r.options(steps, f.dryRun, f.yes)
	opts.Checklist = checklist.Items
	_, code = engine.Run(r.ctx, opts)
	return code, nil
}

// selectSteps returns the named steps in run order, or all steps if none are named.
func selectSteps(names []string) ([]step.Step, error) {
	steps := all.Steps()
	if len(names) == 0 {
		return steps, nil
	}
	valid := all.Names()
	for _, n := range names {
		if !slices.Contains(valid, n) {
			return nil, usererr.New(fmt.Sprintf("unknown step %q", n), "Steps: "+strings.Join(valid, ", "), nil)
		}
	}
	return slices.DeleteFunc(steps, func(s step.Step) bool { return !slices.Contains(names, s.Name()) }), nil
}

func prompter(yes bool) prompt.Prompter {
	switch {
	case yes:
		return prompt.Defaults{}
	case isTerminal(os.Stdin):
		return prompt.NewTerminal(os.Stdin, os.Stdout)
	default:
		return prompt.Unavailable{}
	}
}

// isTerminal reports whether f is an interactive terminal. A plain
// character-device check is not enough: /dev/null is one too.
func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

func xdgDir(env, home, fallback string) string {
	if d := os.Getenv(env); filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(home, fallback)
}

func crashPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(xdgDir("XDG_STATE_HOME", home, ".local/state"), "exon", "crash.log")
}
