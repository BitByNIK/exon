package tool

// Claude is the Claude Code coding agent. It comes from Anthropic's own
// release server, not GitHub.
//
// It keeps its login, settings and sessions in the folder CLAUDE_CONFIG_DIR
// names, so the launcher points that at state/claude in the volume. That is
// why Claude has no Links: a link at ~/.claude.json would be replaced by a
// plain file when Claude saves it, while the one folder is only written inside.
// DISABLE_UPDATES stops Claude from replacing the shared program itself:
// exon dev update is the one way to change its version.
func Claude() Tool {
	return Tool{
		Name:     "claude",
		Summary:  "the Claude Code agent",
		Kind:     Launched,
		Server:   "https://downloads.claude.ai/claude-code-releases",
		Platform: "linux-arm64",
		Launcher: claudeLauncher,
	}
}

// claudeLauncher is bin/claude. --mcp-config=<file> is written with "=" because
// the flag takes several files and would swallow the words after it.
const claudeLauncher = `#!/bin/sh
# Starts Claude Code with its login, settings and sessions in the toolbox volume,
# so every container shares them. Updates are blocked: exon dev update is the one
# way to change the version in the volume.
T=/opt/toolbox
export CLAUDE_CONFIG_DIR="$T/state/claude"
export DISABLE_UPDATES=1
if [ -f "$CLAUDE_CONFIG_DIR/mcp.json" ]; then
  exec "$T/lib/claude/claude" --mcp-config="$CLAUDE_CONFIG_DIR/mcp.json" "$@"
fi
exec "$T/lib/claude/claude" "$@"
`
