package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bitbynik/exon/internal/dev/project"
	"github.com/bitbynik/exon/internal/engine"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/all"
	"github.com/bitbynik/exon/internal/step/projectinit"
	"github.com/bitbynik/exon/internal/step/toolbox"
)

func devCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Manage the shared toolbox for your dev containers",
		Long: `Manage the shared toolbox for your dev containers.

The toolbox is one Docker volume that every dev container mounts. It holds your
dev tools, their logins and your shell settings, so you install and sign in once.`,
	}
	cmd.AddCommand(devInstallCmd(), devUpdateCmd(), devInitCmd())
	return cmd
}

type devFlags struct {
	dryRun, yes, verbose bool
}

func devInstallCmd() *cobra.Command {
	var f devFlags
	cmd := &cobra.Command{
		Use:   "install SOURCE",
		Short: "Put your dev tools in the toolbox volume",
		Long: `Put your dev tools in the toolbox volume.

SOURCE is your config repo: a GitHub repo (owner/repo) or a local folder. Its
exon.toml says which tools to install ([dev] tools = [...]; all of them if
that is missing), and its toolbox/ folder holds starter files, which are copied
into the volume only where the volume has none yet.

Tools already in the volume are left alone. Docker must be running.`,
		Example: `  exon dev install bitbynik/exon-config
  exon dev install ~/Projects/exon-config --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := runDevInstall(cmd.Context(), f, args[0], cmd.OutOrStdout())
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
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "don't ask for confirmation")
	cmd.Flags().BoolVarP(&f.verbose, "verbose", "v", false, "also show the output of every command")
	return cmd
}

func runDevInstall(parent context.Context, f devFlags, srcArg string, out io.Writer) (int, error) {
	r, code, err := start(parent, out, startOptions{
		command:       "exon dev install",
		source:        srcArg,
		dryRun:        f.dryRun,
		verbose:       f.verbose,
		alwaysNetwork: true, // planning asks GitHub for the newest versions
		skipHomebrew:  true,
		minFreeBytes:  3 << 30,
	})
	if r == nil {
		return code, err
	}
	defer r.close()

	_, code = engine.Run(r.ctx, r.options(all.Dev(os.Getuid(), os.Getgid()), f.dryRun, f.yes))
	return code, nil
}

type devInitFlags struct {
	devFlags
	image, user string
}

func devInitCmd() *cobra.Command {
	var f devInitFlags
	cmd := &cobra.Command{
		Use:   "init [DIR]",
		Short: "Add a personal dev container to a project",
		Long: `Add a personal dev container to a project.

DIR is the project folder (the current folder if left out). Two files are made
in .devcontainer/personal/: a devcontainer.json that uses the toolbox volume,
and a post-create.sh that runs once when the container is created. A line in
the project's .git/info/exclude keeps them out of git, so they are only yours.

Edit them afterwards: set the image the project needs, and add its own steps
to the script. Files that already exist are never overwritten.`,
		Example: `  exon dev init
  exon dev init ~/Projects/api --image mcr.microsoft.com/devcontainers/typescript-node:22-bookworm --user node`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			code, err := runDevInit(cmd.Context(), f, dir, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if code != engine.ExitOK {
				return exitError{code}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&f.image, "image", project.DefaultImage, "the container image (Debian or Ubuntu, with git and sudo)")
	cmd.Flags().StringVar(&f.user, "user", project.DefaultUser, "the user that image runs as")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "show what would change, change nothing")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "don't ask for confirmation")
	cmd.Flags().BoolVarP(&f.verbose, "verbose", "v", false, "also show the output of every command")
	return cmd
}

func runDevInit(parent context.Context, f devInitFlags, dir string, out io.Writer) (int, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	r, err := begin(parent, out, "exon dev init", f.verbose, "dir", abs, "dry_run", f.dryRun)
	if err != nil {
		return 0, err
	}
	defer r.close()

	steps := []step.Step{projectinit.New(abs, f.image, f.user)}
	_, code := engine.Run(r.ctx, r.options(steps, f.dryRun, f.yes))
	return code, nil
}

type devUpdateFlags struct {
	devFlags
	version string
}

func devUpdateCmd() *cobra.Command {
	var f devUpdateFlags
	cmd := &cobra.Command{
		Use:   "update SOURCE [TOOL...]",
		Short: "Update your dev tools in the toolbox volume",
		Long: `Update your dev tools in the toolbox volume.

SOURCE is your config repo, as for exon dev install. Without TOOL names every
tool in [dev] tools is checked; each one that has a newer version is updated,
and the plan shows the old and the new version. Tools that are not installed
are left to exon dev install.

--version puts a particular version of one tool in place of the newest, which
is also how to go back to an older one. Close your sessions of the
codebase-memory MCP first: the plan warns when containers have it open.`,
		Example: `  exon dev update bitbynik/exon-config
  exon dev update bitbynik/exon-config omp gh
  exon dev update bitbynik/exon-config omp --version v18.8.7`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := runDevUpdate(cmd.Context(), f, args[0], args[1:], cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if code != engine.ExitOK {
				return exitError{code}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&f.version, "version", "", "put this version of the tool instead of the newest (needs exactly one TOOL)")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "show what would change, change nothing")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "don't ask for confirmation")
	cmd.Flags().BoolVarP(&f.verbose, "verbose", "v", false, "also show the output of every command")
	return cmd
}

func runDevUpdate(parent context.Context, f devUpdateFlags, srcArg string, tools []string, out io.Writer) (int, error) {
	r, code, err := start(parent, out, startOptions{
		command:       "exon dev update",
		source:        srcArg,
		dryRun:        f.dryRun,
		verbose:       f.verbose,
		alwaysNetwork: true, // planning asks GitHub for the newest versions
		skipHomebrew:  true,
		minFreeBytes:  3 << 30,
		logAttrs:      []any{"tools", tools, "version", f.version},
	})
	if r == nil {
		return code, err
	}
	defer r.close()

	steps := []step.Step{toolbox.NewUpdate(os.Getuid(), os.Getgid(), tools, f.version)}
	_, code = engine.Run(r.ctx, r.options(steps, f.dryRun, f.yes))
	return code, nil
}
