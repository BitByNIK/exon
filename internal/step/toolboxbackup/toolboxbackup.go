// Package toolboxbackup holds the steps that save and bring back the data in
// the toolbox volume: logins, settings, sessions and indexes. The tools
// themselves are not part of it; exon dev install downloads them again.
package toolboxbackup

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitbynik/exon/internal/dev/backup"
	"github.com/bitbynik/exon/internal/dev/volume"
	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/usererr"
)

// size says how big n bytes are, roughly.
func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", (n+(1<<20)-1)>>20)
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", (n+(1<<10)-1)>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

// helperImage plans the one-time download of the helper image, if it is needed.
func helperImage(ctx context.Context, vol volume.Volume, p *plan) error {
	present, err := vol.ImagePresent(ctx)
	if err != nil {
		return err
	}
	if !present {
		p.pullImage = true
		p.items = append(p.items, step.PlanItem{Kind: step.Add, Action: "download", Subject: "helper image", Detail: "once, about 100 MB"})
	}
	return nil
}

// warnIfInUse adds a warning when containers are running on the volume: a
// database that is being written may be copied half-done.
func warnIfInUse(ctx context.Context, vol volume.Volume, p *plan, action, why string) {
	if used, err := vol.UsedBy(ctx); err == nil && len(used) > 0 {
		p.items = append(p.items, step.PlanItem{Kind: step.Warn, Action: action, Subject: strings.Join(used, ", "), Detail: why})
	}
}

type plan struct {
	uid, gid  int // the Mac user, who owns the volume
	items     []step.PlanItem
	pullImage bool
	run       func(ctx context.Context, env step.Env, vol volume.Volume) error
}

func (p *plan) Items() []step.PlanItem { return p.items }

func (*plan) Questions() []prompt.Question { return nil }

// Apply pulls the helper image if the plan said so, then does the plan's work.
func (p *plan) Apply(ctx context.Context, env step.Env, _ prompt.Answers) error {
	vol := volumeFor(env, p)
	if p.pullImage {
		env.Report.Notice("downloading the helper image (one time, about 100 MB)")
		if err := vol.PullImage(ctx); err != nil {
			return err
		}
	}
	return p.run(ctx, env, vol)
}

// volumeFor makes a fresh volume handle from the Apply env: the planning one could only read.
func volumeFor(env step.Env, p *plan) volume.Volume {
	return volume.New(env.Run, env.FS, p.uid, p.gid)
}

// Backup saves the volume's data to a file.
type Backup struct {
	uid, gid int
	dir      string
	now      time.Time
}

// NewBackup returns the step that writes a backup into dir, named for now.
func NewBackup(uid, gid int, dir string, now time.Time) *Backup {
	return &Backup{uid: uid, gid: gid, dir: dir, now: now}
}

// Name implements step.Step.
func (*Backup) Name() string { return "backup" }

// Summary implements step.Step.
func (*Backup) Summary() string { return "Save the toolbox's logins and settings to a file" }

// Requires implements step.Step.
func (*Backup) Requires() []string { return nil }

// Prepare checks that there is a volume to save and plans the file.
func (s *Backup) Prepare(ctx context.Context, env step.Env) (step.Plan, error) {
	vol := volume.New(env.Run, env.FS, s.uid, s.gid)
	if err := vol.Check(ctx); err != nil {
		return nil, err
	}
	exists, err := vol.Exists(ctx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, usererr.New("there is no toolbox volume to back up", "Run exon dev install first.", nil)
	}

	file := backup.FileName(s.now)
	path := filepath.Join(s.dir, file)
	p := &plan{uid: s.uid, gid: s.gid}
	p.items = append(p.items,
		step.PlanItem{Kind: step.Add, Action: "save", Subject: path, Detail: "logins, settings and indexes; not the tools"},
		step.PlanItem{
			Kind: step.Note, Action: "private", Subject: "the file holds your login tokens",
			Detail: "only you can read it; keep it somewhere safe",
		},
	)
	warnIfInUse(ctx, vol, p, "running", "containers are using the toolbox; close your omp sessions in them for a clean copy")
	if err := helperImage(ctx, vol, p); err != nil {
		return nil, err
	}

	p.run = func(ctx context.Context, env step.Env, vol volume.Volume) error {
		env.Report.StartUntimed(path)
		if err := env.FS.MkdirAll(s.dir, 0o700); err != nil {
			env.Report.Fail(path, err, nil)
			return nil
		}
		if err := vol.Backup(ctx, s.dir, file); err != nil {
			env.Report.Fail(path, err, nil)
			return nil
		}
		detail := ""
		if info, err := env.FS.Stat(path); err == nil {
			detail = size(info.Size())
		}
		env.Report.Done(path, detail)
		return nil
	}
	return p, nil
}

// Restore puts a backup back into the volume.
type Restore struct {
	uid, gid int
	file     string
	force    bool
}

// NewRestore returns the step that restores the backup in file. With force it
// may put the backup over data that is already in the volume.
func NewRestore(uid, gid int, file string, force bool) *Restore {
	return &Restore{uid: uid, gid: gid, file: file, force: force}
}

// Name implements step.Step.
func (*Restore) Name() string { return "restore" }

// Summary implements step.Step.
func (*Restore) Summary() string { return "Put a backup back into the toolbox" }

// Requires implements step.Step.
func (*Restore) Requires() []string { return nil }

// Prepare checks the backup file and the volume, and plans the restore.
func (s *Restore) Prepare(ctx context.Context, env step.Env) (step.Plan, error) {
	info, err := backup.Inspect(env.FS, s.file)
	if err != nil {
		return nil, err
	}
	if len(info.Problems) > 0 {
		shown := info.Problems[:min(3, len(info.Problems))]
		hint := strings.Join(shown, "\n")
		if more := len(info.Problems) - len(shown); more > 0 {
			hint += fmt.Sprintf("\nand %d more", more)
		}
		return nil, usererr.New("this backup is not safe to restore", hint, nil)
	}

	vol := volume.New(env.Run, env.FS, s.uid, s.gid)
	if err := vol.Check(ctx); err != nil {
		return nil, err
	}
	hasData, err := vol.HasData(ctx)
	if err != nil {
		return nil, err
	}
	if hasData && !s.force {
		return nil, usererr.New("the toolbox already holds logins and settings",
			"Restoring puts the backup's files over them: a file with the same name is replaced.\nUse --force if that is what you want.", nil)
	}

	p := &plan{uid: s.uid, gid: s.gid}
	kind := step.Add
	if hasData {
		kind = step.Change
	}
	p.items = append(p.items,
		step.PlanItem{Kind: kind, Action: "restore", Subject: s.file, Detail: fmt.Sprintf("%d files, %s", info.Files, size(info.Bytes))},
		step.PlanItem{Kind: step.Note, Action: "afterwards", Subject: "exon dev install", Detail: "puts the tools back; a backup does not hold them"},
	)
	warnIfInUse(ctx, vol, p, "running", "containers are using the toolbox; restart them after the restore")
	if err := helperImage(ctx, vol, p); err != nil {
		return nil, err
	}

	p.run = func(ctx context.Context, env step.Env, vol volume.Volume) error {
		env.Report.StartUntimed(s.file)
		if err := vol.Restore(ctx, filepath.Dir(s.file), filepath.Base(s.file)); err != nil {
			env.Report.Fail(s.file, err, nil)
			return nil
		}
		env.Report.Done(s.file, fmt.Sprintf("%d files", info.Files))
		return nil
	}
	return p, nil
}
