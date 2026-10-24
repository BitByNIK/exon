// Package ssh asks for SSH hosts and whether to use the 1Password SSH agent,
// and adds them to ~/.ssh/config. It never copies or creates keys.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
)

// Config is the [ssh] section of exon.toml.
type Config struct {
	// AskForHosts asks for SSH hosts to add, one after another.
	AskForHosts bool `toml:"ask_for_hosts"`
	// AskAbout1Password asks whether to use the 1Password SSH agent.
	AskAbout1Password bool `toml:"ask_about_1password"`
}

// Question IDs.
const (
	qAgent   = "ssh.1password"
	qHosts   = "ssh.hosts"
	qAlias   = "alias"
	qAddr    = "hostname"
	qUser    = "user"
	qPort    = "port"
	qReplace = "replace"
)

const agentItem = "1Password SSH agent"

// agentSocket is where 1Password's SSH agent listens.
const agentSocket = `~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock`

// Step is the ssh step.
type Step struct {
	// AppDir is where 1Password.app is looked for. Tests point it elsewhere.
	AppDir string
}

// New returns the ssh step.
func New() *Step { return &Step{AppDir: "/Applications"} }

// Name implements step.Step.
func (*Step) Name() string { return "ssh" }

// Summary implements step.Step.
func (*Step) Summary() string { return "Add SSH hosts and the 1Password SSH agent" }

// Requires implements step.Step.
func (*Step) Requires() []string { return nil }

// Prepare reads ~/.ssh/config to know which hosts already exist and whether
// the 1Password agent is already set up.
func (s *Step) Prepare(_ context.Context, env step.Env) (step.Plan, error) {
	var cfg Config
	if err := env.Config.Decode(&cfg); err != nil {
		return nil, err
	}
	p := &plan{cfg: cfg}
	if !cfg.AskForHosts && !cfg.AskAbout1Password {
		return p, nil
	}
	text, err := readConfig(env)
	if err != nil {
		return nil, err
	}
	p.existing = hostAliases(text)
	p.agentSet = hasAgent(text)
	if _, err := env.FS.Stat(filepath.Join(s.AppDir, "1Password.app")); err == nil {
		p.has1Password = true
	}
	return p, nil
}

type plan struct {
	cfg          Config
	existing     []string // single-name Host entries in ~/.ssh/config
	agentSet     bool
	has1Password bool
}

func (p *plan) Items() []step.PlanItem {
	var items []step.PlanItem
	if p.cfg.AskAbout1Password {
		// Asked even when it's set up now: configs runs first and may replace
		// ~/.ssh/config, so Apply checks the file again before saving.
		switch {
		case p.agentSet:
			items = append(items, step.PlanItem{Kind: step.Change, Action: "ask", Subject: agentItem, Detail: "set up now; Enter keeps it"})
		case !p.has1Password:
			items = append(items, step.PlanItem{Kind: step.Add, Action: "ask", Subject: agentItem, Detail: "1Password isn't installed yet"})
		default:
			items = append(items, step.PlanItem{Kind: step.Add, Action: "ask", Subject: agentItem})
		}
	}
	if p.cfg.AskForHosts {
		detail := "none yet"
		if len(p.existing) > 0 {
			detail = "existing: " + strings.Join(p.existing, ", ")
		}
		items = append(items, step.PlanItem{Kind: step.Add, Action: "ask", Subject: "SSH hosts to add", Detail: detail})
	}
	return items
}

func (p *plan) Questions() []prompt.Question {
	var qs []prompt.Question
	if p.cfg.AskAbout1Password {
		def := "no"
		if p.agentSet || p.has1Password {
			def = "yes"
		}
		qs = append(qs, prompt.Question{ID: qAgent, Text: "Use the 1Password SSH agent for SSH keys?", Default: def})
	}
	if p.cfg.AskForHosts {
		exists := func(e map[string]string) bool { return slices.Contains(p.existing, e[qAlias]) }
		qs = append(qs, prompt.Question{
			ID:    qHosts,
			Text:  "Add an SSH host?",
			Again: "Add another SSH host?",
			Repeat: []prompt.Question{
				{ID: qAlias, Text: "  Name (what you'll type after ssh)", Input: true, Validate: validAlias},
				{ID: qReplace, Text: "  That name is already in ~/.ssh/config. Replace it?", Default: "no", When: exists},
				{ID: qAddr, Text: "  Address (IP or host name)", Input: true, Validate: validAddress, When: notKept},
				{ID: qUser, Text: "  User (Enter for your Mac user name)", Input: true, Optional: true, Validate: validUser, When: notKept},
				{ID: qPort, Text: "  Port", Input: true, Default: "22", Validate: validPort, When: notKept},
			},
		})
	}
	return qs
}

// notKept skips the remaining questions for a host the user chose not to replace.
func notKept(e map[string]string) bool { return e[qReplace] != "no" }

// host is one SSH host to add.
type host struct{ alias, addr, user, port string }

func (h host) block() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Host %s\n  HostName %s\n", h.alias, h.addr)
	if h.user != "" {
		fmt.Fprintf(&b, "  User %s\n", h.user)
	}
	if h.port != "" && h.port != "22" {
		fmt.Fprintf(&b, "  Port %s\n", h.port)
	}
	return b.String()
}

// Apply re-reads ~/.ssh/config (configs may have replaced it since planning),
// adds what was asked for, and writes it back once.
func (p *plan) Apply(_ context.Context, env step.Env, answers prompt.Answers) error {
	useAgent := answers[qAgent] == "yes"
	var hosts []host
	for _, e := range answers.Entries(qHosts) {
		if e[qReplace] == "no" {
			env.Report.Skip("SSH host "+e[qAlias], "kept the existing one")
			continue
		}
		h := host{alias: e[qAlias], addr: e[qAddr], user: e[qUser], port: e[qPort]}
		// A later answer for the same name wins.
		hosts = slices.DeleteFunc(hosts, func(o host) bool { return o.alias == h.alias })
		hosts = append(hosts, h)
	}
	if !useAgent && len(hosts) == 0 {
		return nil
	}

	path := filepath.Join(env.Home, ".ssh", "config")
	old, err := readConfig(env)
	if err != nil {
		return err
	}
	text := old
	var items []string
	if useAgent {
		if hasAgent(text) {
			env.Report.Skip(agentItem, "already set up")
		} else {
			text = appendBlock(text, "Host *\n  IdentityAgent \""+agentSocket+"\"\n")
			items = append(items, agentItem)
		}
	}
	for _, h := range hosts {
		text = appendBlock(removeHost(text, h.alias), h.block())
		items = append(items, "SSH host "+h.alias)
	}
	if len(items) == 0 {
		return nil
	}

	for _, it := range items {
		env.Report.Start(it)
	}
	if err := write(env, path, old, text); err != nil {
		for _, it := range items {
			env.Report.Fail(it, err, nil)
		}
		return nil
	}
	for _, it := range items {
		env.Report.Done(it, "added to ~/.ssh/config")
	}
	if useAgent && !p.has1Password {
		env.Report.Notice("1Password isn't installed: the agent works once it is, with its SSH agent turned on")
	}
	return nil
}

// readConfig returns ~/.ssh/config, or "" if it doesn't exist.
func readConfig(env step.Env) (string, error) {
	data, err := env.FS.ReadFile(filepath.Join(env.Home, ".ssh", "config"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read ~/.ssh/config: %w", err)
	}
	return string(data), nil
}

// write backs up the old file (if any) and writes the new one, keeping ~/.ssh private.
func write(env step.Env, path, old, text string) error {
	if old != "" {
		backup := filepath.Join(env.BackupDir, ".ssh", "config")
		if err := env.FS.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
			return err
		}
		if err := env.FS.WriteFile(backup, []byte(old), 0o600); err != nil {
			return err
		}
	}
	dir := filepath.Dir(path)
	if err := env.FS.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := env.FS.WriteFile(path, []byte(text), 0o600); err != nil {
		return err
	}
	return env.FS.Chmod(dir, 0o700)
}

// fields splits a config line into keyword and arguments, ignoring comments.
func fields(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}
	return strings.Fields(line)
}

// isBlockStart reports whether a line starts a Host or Match block.
func isBlockStart(f []string) bool {
	return len(f) > 0 && (strings.EqualFold(f[0], "Host") || strings.EqualFold(f[0], "Match"))
}

// hostAliases returns the names of Host blocks that have exactly one name.
func hostAliases(text string) []string {
	var out []string
	for line := range strings.Lines(text) {
		f := fields(line)
		if len(f) == 2 && strings.EqualFold(f[0], "Host") && f[1] != "*" {
			out = append(out, f[1])
		}
	}
	return out
}

// hasAgent reports whether the 1Password SSH agent is already configured.
func hasAgent(text string) bool {
	for line := range strings.Lines(text) {
		f := fields(line)
		if len(f) > 1 && strings.EqualFold(f[0], "IdentityAgent") && strings.Contains(strings.ToLower(line), "1password") {
			return true
		}
	}
	return false
}

// removeHost drops the block that starts with "Host <alias>" (exactly that one
// name), up to the next Host or Match line.
func removeHost(text, alias string) string {
	var out strings.Builder
	skipping := false
	for line := range strings.Lines(text) {
		f := fields(line)
		if isBlockStart(f) {
			skipping = len(f) == 2 && strings.EqualFold(f[0], "Host") && f[1] == alias
		}
		if !skipping {
			out.WriteString(line)
		}
	}
	return out.String()
}

// appendBlock adds a block at the end, separated by one blank line.
func appendBlock(text, block string) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return block
	}
	return text + "\n\n" + block
}

func validAlias(s string) error {
	if strings.ContainsAny(s, " \t*?!,") || strings.HasPrefix(s, "-") {
		return errors.New("use one word, without spaces or * ? ! ,")
	}
	return nil
}

func validAddress(s string) error {
	if strings.ContainsAny(s, " \t") {
		return errors.New("an address has no spaces")
	}
	return nil
}

func validUser(s string) error {
	if strings.ContainsAny(s, " \t") {
		return errors.New("a user name has no spaces")
	}
	return nil
}

func validPort(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("a port is a number from 1 to 65535")
	}
	return nil
}
