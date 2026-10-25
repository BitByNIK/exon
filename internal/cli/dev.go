package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bitbynik/exon/internal/dev/backup"
	"github.com/bitbynik/exon/internal/dev/project"
	"github.com/bitbynik/exon/internal/engine"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/all"
	"github.com/bitbynik/exon/internal/step/projectinit"
	"github.com/bitbynik/exon/internal/step/toolbox"
	"github.com/bitbynik/exon/internal/step/toolboxbackup"
)

func devCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Manage the shared toolbox for your dev containers",
		Long: `Manage the shared toolbox for your dev containers.

The toolbox is one Docker volume that every dev container mounts. It holds your
dev tools, their logins and your shell settings, so you install and sign in once.`,
	}
	cmd.AddCommand(devInstallCmd(), devUpdateCmd(), devInitCmd(), devBackupCmd(), devRestoreCmd())
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

type devBackupFlags struct {
	devFlags
	to string
}

func devBackupCmd() *cobra.Command {
	var f devBackupFlags
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Save the toolbox's logins and settings to a file",
		Long: `Save the toolbox's logins and settings to a file.

The backup holds what you cannot download again: your logins, settings, omp
sessions and code indexes, and your shell settings. It does not hold the tools;
exon dev install gets those back. The file is for you only (it contains login
tokens), and goes in ~/Backups/exon unless you say --to.

Close your omp sessions in dev containers first for a clean copy: a database
that is being written may be copied half-done.`,
		Example: `  exon dev backup
  exon dev backup --to ~/Documents/backups`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code, err := runDevBackup(cmd.Context(), f, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if code != engine.ExitOK {
				return exitError{code}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&f.to, "to", "", "the folder for the backup (default ~/Backups/exon)")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "show what would change, change nothing")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "don't ask for confirmation")
	cmd.Flags().BoolVarP(&f.verbose, "verbose", "v", false, "also show the output of every command")
	return cmd
}

func runDevBackup(parent context.Context, f devBackupFlags, out io.Writer) (int, error) {
	r, err := begin(parent, out, "exon dev backup", f.verbose, "dry_run", f.dryRun)
	if err != nil {
		return 0, err
	}
	defer r.close()

	dir := backup.DefaultDir(r.home)
	if f.to != "" {
		if dir, err = filepath.Abs(f.to); err != nil {
			return 0, err
		}
	}
	steps := []step.Step{toolboxbackup.NewBackup(os.Getuid(), os.Getgid(), dir, r.now)}
	_, code := engine.Run(r.ctx, r.options(steps, f.dryRun, f.yes))
	return code, nil
}

type devRestoreFlags struct {
	devFlags
	force bool
}

func devRestoreCmd() *cobra.Command {
	var f devRestoreFlags
	cmd := &cobra.Command{
		Use:   "restore FILE",
		Short: "Put a backup back into the toolbox",
		Long: `Put a backup back into the toolbox.

FILE is a backup made by exon dev backup. It is checked first: a file that
tries to write outside the toolbox, or over the installed tools, is refused.

A toolbox that already holds logins is not touched unless you pass --force;
then the files in the backup replace files of the same name. The tools are not
in a backup: run exon dev install afterwards.`,
		Example: `  exon dev restore ~/Backups/exon/toolbox-2026-10-25_105503.tar.gz
  exon dev restore ~/Backups/exon/toolbox-2026-10-25_105503.tar.gz --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := runDevRestore(cmd.Context(), f, args[0], cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if code != engine.ExitOK {
				return exitError{code}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&f.force, "force", false, "restore over logins and settings that are already in the toolbox")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "show what would change, change nothing")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "don't ask for confirmation")
	cmd.Flags().BoolVarP(&f.verbose, "verbose", "v", false, "also show the output of every command")
	return cmd
}

func runDevRestore(parent context.Context, f devRestoreFlags, file string, out io.Writer) (int, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return 0, err
	}
	r, err := begin(parent, out, "exon dev restore", f.verbose, "file", abs, "force", f.force, "dry_run", f.dryRun)
	if err != nil {
		return 0, err
	}
	defer r.close()

	steps := []step.Step{toolboxbackup.NewRestore(os.Getuid(), os.Getgid(), abs, f.force)}
	_, code := engine.Run(r.ctx, r.options(steps, f.dryRun, f.yes))
	return code, nil
}
