// Package all lists every step in the order they run.
// Adding a step: create its package under internal/step and add it here.
package all

import (
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/apps"
	"github.com/bitbynik/exon/internal/step/configs"
	"github.com/bitbynik/exon/internal/step/git"
	"github.com/bitbynik/exon/internal/step/ssh"
	"github.com/bitbynik/exon/internal/step/tmux"
	"github.com/bitbynik/exon/internal/step/toolbox"
)

// Steps returns all steps in run order.
func Steps() []step.Step {
	return []step.Step{
		apps.New(),
		configs.New(),
		git.New(),  // after configs: a replaced ~/.gitconfig then gets the name and email back
		ssh.New(),  // after configs, for the same reason with ~/.ssh/config
		tmux.New(), // after apps, which may install tmux
	}
}

// Dev returns the steps that `exon dev install` runs, for the Mac user with
// the given ids. They are kept apart from Steps: `exon setup` never runs them.
func Dev(uid, gid int) []step.Step {
	return []step.Step{toolbox.New(uid, gid)}
}

// Names returns the step names in run order.
func Names() []string {
	steps := Steps()
	names := make([]string, len(steps))
	for i, s := range steps {
		names[i] = s.Name()
	}
	return names
}
