package tool

// FZF is the fuzzy finder. The shell gets its key bindings from the program
// itself (`fzf --bash`), so nothing else is downloaded.
func FZF() Tool {
	return Tool{
		Name:    "fzf",
		Summary: "the fuzzy finder",
		Kind:    Archive,
		Repo:    "junegunn/fzf",
		Asset:   func(tag string) string { return "fzf-" + bare(tag) + "-linux_arm64.tar.gz" },
		Member:  func(string) string { return "fzf" },
	}
}
