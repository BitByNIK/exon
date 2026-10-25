package cli

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bitbynik/exon/internal/config"
	"github.com/bitbynik/exon/internal/dev/settings"
	"github.com/bitbynik/exon/internal/engine"
	"github.com/bitbynik/exon/internal/logfile"
	"github.com/bitbynik/exon/internal/preflight"
	"github.com/bitbynik/exon/internal/source"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/all"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/ui/plain"
	"github.com/bitbynik/exon/internal/usererr"
)

// startOptions describe a command that works from a config repo.
type startOptions struct {
	command string // for the log, "exon setup"
	source  string // the SOURCE argument
	dryRun  bool
	verbose bool
	// alwaysNetwork says the command needs the network even for a dry run
	// from a local folder, because it asks GitHub while planning.
	alwaysNetwork bool
	minFreeBytes  uint64
	// skipHomebrew leaves out the Homebrew checks, for commands that never use it.
	skipHomebrew bool
	logAttrs     []any // extra fields for the log's first line
}

// runCtx is what a command has once it has started: a log, a display, the
// config repo fetched and read.
type runCtx struct {
	ctx      context.Context
	home     string
	stateDir string
	dataDir  string
	cacheDir string // exon's folder for downloads that are safe to delete
	now      time.Time
	log      *slog.Logger
	logPath  string
	runner   *sys.OSRunner
	ui       *plain.Renderer
	repo     string
	cfg      *config.File
	closers  []func()
}

// close releases everything start opened, in reverse order.
func (r *runCtx) close() {
	for i := len(r.closers) - 1; i >= 0; i-- {
		r.closers[i]()
	}
	r.closers = nil
}

// begin opens what every command starts with: the log and the display. The
// caller must call close, unless begin returned an error.
func begin(parent context.Context, out io.Writer, command string, verbose bool, logAttrs ...any) (*runCtx, error) {
	if parent == nil {
		parent = context.Background()
	}
	r := &runCtx{now: time.Now()}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	r.ctx = ctx
	r.closers = append(r.closers, stop)
	ready := false
	defer func() {
		if !ready {
			r.close()
		}
	}()

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	r.home = home
	r.stateDir = xdgDir("XDG_STATE_HOME", home, ".local/state")
	r.dataDir = xdgDir("XDG_DATA_HOME", home, ".local/share")
	r.cacheDir = filepath.Join(xdgDir("XDG_CACHE_HOME", home, ".cache"), "exon")

	logFile, err := logfile.Open(filepath.Join(r.stateDir, "exon", "logs"), r.now, 10)
	if err != nil {
		return nil, usererr.New("cannot create the log file", "", err)
	}
	r.closers = append(r.closers, func() { _ = logFile.Close() })
	r.logPath = logFile.Name()
	r.log = slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r.log.Info(command, append([]any{"version", version}, logAttrs...)...)

	r.runner = &sys.OSRunner{Log: logFile}
	if verbose {
		r.runner.Echo = os.Stderr
	}
	r.ui = plain.New(out, home)
	r.cfg = &config.File{} // no config repo: every section is empty
	ready = true
	return r, nil
}

// start does what every command that works from a config repo begins with:
// open the log, run the checks, fetch and read the config repo. A nil result
// means the command must end now, with the returned exit code and error.
// Otherwise the caller must call close.
func start(parent context.Context, out io.Writer, o startOptions) (*runCtx, int, error) {
	r, err := begin(parent, out, o.command, o.verbose,
		append([]any{"source", o.source, "dry_run", o.dryRun}, o.logAttrs...)...)
	if err != nil {
		return nil, 0, err
	}
	if code, err := r.loadConfig(o); err != nil || code != engine.ExitOK {
		r.close()
		return nil, code, err
	}
	return r, 0, nil
}

// loadConfig runs the checks, then fetches and reads the config repo. A
// non-zero exit code with no error means a check failed.
func (r *runCtx) loadConfig(o startOptions) (int, error) {
	src, err := source.Parse(o.source, r.home)
	if err != nil {
		return 0, err
	}

	checks := preflight.Run(r.ctx, preflight.OSHost{}, preflight.Options{
		Home:         r.home,
		FS:           sys.OSFS{},
		NeedNetwork:  o.alwaysNetwork || !o.dryRun || src.Remote(),
		MinFreeBytes: o.minFreeBytes,
		SkipHomebrew: o.skipHomebrew,
	})
	r.ui.Checks(checks)
	for _, c := range checks {
		r.log.Info("check", "name", c.Name, "status", c.Status, "detail", c.Detail)
	}
	if preflight.Failed(checks) {
		return engine.ExitPreflight, nil
	}

	repo, cleanup, err := source.Fetch(r.ctx, src, r.runner, filepath.Join(r.dataDir, "exon", "repos"), o.dryRun)
	if err != nil {
		return 0, err
	}
	r.closers = append(r.closers, cleanup)
	r.repo = repo

	known := append(all.Names(), settings.SectionName, "checklist")
	r.cfg, err = config.Load(filepath.Join(repo, config.FileName), known)
	return 0, err
}

// options returns the engine options every command shares.
func (r *runCtx) options(steps []step.Step, dryRun, yes bool) engine.Options {
	return engine.Options{
		Steps:     steps,
		Config:    r.cfg,
		DryRun:    dryRun,
		Run:       r.runner,
		FS:        sys.OSFS{},
		Net:       sys.OSDownloader{},
		Prompt:    prompter(yes),
		UI:        r.ui,
		Log:       r.log,
		Repo:      r.repo,
		Home:      r.home,
		BackupDir: filepath.Join(r.stateDir, "exon", "backups", r.now.Format(logfile.Stamp)),
		CacheDir:  r.cacheDir,
		LogPath:   r.logPath,
	}
}
