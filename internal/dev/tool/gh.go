package tool

// GH is the GitHub command line. Its login is kept in ~/.config/gh, and git
// can ask it for GitHub credentials.
func GH() Tool {
	return Tool{
		Name:    "gh",
		Summary: "the GitHub command line",
		Kind:    Archive,
		Repo:    "cli/cli",
		Asset:   func(tag string) string { return "gh_" + bare(tag) + "_linux_arm64.tar.gz" },
		Member:  func(tag string) string { return "gh_" + bare(tag) + "_linux_arm64/bin/gh" },
		Links:   []Link{{State: "gh", Home: ".config/gh"}},
	}
}
