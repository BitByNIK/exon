package volume

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"path/filepath"
	"strings"

	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/usererr"
)

//go:embed install.sh
var installScript string

// Item is one tool to put into the volume.
type Item struct {
	Tool tool.Tool
	// Version is recorded as the installed version. For a Clone tool it is
	// ignored: the cloned commit is recorded.
	Version string
	// File is the downloaded release file, inside Job.StageDir. For a Clone
	// tool it is empty and the repository address is used instead.
	File string
}

// Job is everything one Install does.
type Job struct {
	// StageDir is a folder on the Mac that holds the downloaded files. It is
	// shared into the container, so it must be somewhere Docker can see
	// (under the user's home folder). Install adds a "plan" file to it.
	StageDir string
	Items    []Item
	// SeedDir, if not empty, is a folder on the Mac whose files are copied
	// into the volume when the volume lacks them.
	SeedDir string
	// Container, if not nil, puts the container files in the volume.
	Container *Container
}

// Outcome is what Install did, tool by tool.
type Outcome struct {
	// Failed maps the name of each tool that could not be installed to why.
	Failed map[string]string
	// Installed lists the tools that were installed, in plan order.
	Installed []string
	// Seeded lists the starter files that were copied into the volume.
	Seeded []string
	// Skipped lists starter files that were not copied because they belong
	// to places exon manages itself.
	Skipped []string
}

// Install runs one throwaway container that puts the job's tools in the
// volume and then gives the volume to the Mac user. The volume is created by
// Docker if it does not exist. A tool that fails is reported in the Outcome and
// the others still go in; an error means the whole run failed.
func (v Volume) Install(ctx context.Context, job Job) (Outcome, error) {
	// The script, the plan and the container files travel in the stage folder, so the command line stays short in the log.
	if err := v.FS.WriteFile(filepath.Join(job.StageDir, "plan"), []byte(planText(job.Items, job.Container)), 0o644); err != nil {
		return Outcome{}, err
	}
	if err := v.FS.WriteFile(filepath.Join(job.StageDir, "install.sh"), []byte(installScript), 0o755); err != nil {
		return Outcome{}, err
	}
	if job.Container != nil {
		dir := filepath.Join(job.StageDir, "container")
		if err := v.FS.MkdirAll(dir, 0o755); err != nil {
			return Outcome{}, err
		}
		for name, content := range job.Container.Files {
			if err := v.FS.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
				return Outcome{}, err
			}
		}
	}
	args := []string{
		"run", "--rm",
		"-e", "OWNER=" + v.Owner,
		"-v", v.Name + ":" + MountPoint,
		"-v", job.StageDir + ":/stage:ro",
	}
	if job.SeedDir != "" {
		args = append(args, "-v", job.SeedDir+":/seed:ro")
	}
	args = append(args, Image, "sh", "/stage/install.sh")

	res, err := v.Run.Exec(ctx, sys.Cmd{Name: "docker", Args: args})
	out := parseOutcome(res.Stdout)
	if err != nil {
		return out, usererr.New("could not put the tools in the toolbox volume",
			"Docker said:\n"+sys.LastLines(string(res.Output()), 5), err)
	}
	return out, nil
}

// planText is the plan file the script reads: one line per tool, then one for the container files.
func planText(items []Item, c *Container) string {
	var b strings.Builder
	line := func(fields ...string) { b.WriteString(strings.Join(fields, "|") + "\n") }
	for _, it := range items {
		t := it.Tool
		switch t.Kind {
		case tool.Program:
			line("program", t.Name, it.Version, it.File, "")
		case tool.Archive:
			line("archive", t.Name, it.Version, it.File, t.Member(it.Version))
		case tool.Clone:
			line("clone", t.Name, "", t.CloneURL(), "")
		}
		for _, l := range t.Links {
			line("state", l.State, "", "", "")
		}
	}
	if c != nil {
		line("container", ContainerName, c.Hash, "", "")
	}
	return b.String()
}

// parseOutcome reads the script's answer lines. Lines it does not know are ignored.
func parseOutcome(stdout []byte) Outcome {
	out := Outcome{Failed: map[string]string{}}
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	for sc.Scan() {
		kind, rest, _ := strings.Cut(sc.Text(), "|")
		switch kind {
		case "ok":
			out.Installed = append(out.Installed, rest)
		case "fail":
			name, why, _ := strings.Cut(rest, "|")
			out.Failed[name] = why
		case "seeded":
			out.Seeded = append(out.Seeded, rest)
		case "skipped":
			out.Skipped = append(out.Skipped, rest)
		}
	}
	return out
}
