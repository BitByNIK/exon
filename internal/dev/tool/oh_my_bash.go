package tool

// OhMyBash is the oh-my-bash framework. It is a folder of scripts, not a
// program, so it is cloned once into the volume; the user's shell settings
// load it from there.
func OhMyBash() Tool {
	return Tool{
		Name:    "oh-my-bash",
		Summary: "the oh-my-bash shell framework",
		Kind:    Clone,
		Repo:    "ohmybash/oh-my-bash",
	}
}
