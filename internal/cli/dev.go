package cli

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/bitbynik/exon/internal/engine"
	"github.com/bitbynik/exon/internal/step/all"
)

func devCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Manage the shared toolbox for your dev containers",
		Long: `Manage the shared toolbox for your dev containers.

The toolbox is one Docker volume that every dev container mounts. It holds your
dev tools, their logins and your shell settings, so you install and sign in once.`,
	}
	cmd.AddCommand(devInstallCmd())
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
