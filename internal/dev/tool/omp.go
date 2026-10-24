package tool

// OMP is the omp coding agent. Its logins, settings and sessions live in ~/.omp.
func OMP() Tool {
	return Tool{
		Name:    "omp",
		Summary: "the omp coding agent",
		Kind:    Program,
		Repo:    "can1357/oh-my-pi",
		Asset:   func(string) string { return "omp-linux-arm64" },
		Links:   []Link{{State: "omp", Home: ".omp"}},
	}
}
