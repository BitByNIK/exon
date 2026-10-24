package tool

// EZA is the modern replacement for ls.
func EZA() Tool {
	return Tool{
		Name:    "eza",
		Summary: "the modern ls",
		Kind:    Archive,
		Repo:    "eza-community/eza",
		Asset:   func(string) string { return "eza_aarch64-unknown-linux-gnu.tar.gz" },
		Member:  func(string) string { return "./eza" },
	}
}
