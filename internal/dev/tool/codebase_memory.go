package tool

// CodebaseMemory is the codebase-memory MCP server. It keeps its code
// indexes in ~/.cache/codebase-memory-mcp.
func CodebaseMemory() Tool {
	return Tool{
		Name:    "codebase-memory-mcp",
		Summary: "the codebase-memory MCP server",
		Kind:    Archive,
		Repo:    "DeusData/codebase-memory-mcp",
		Asset:   func(string) string { return "codebase-memory-mcp-linux-arm64.tar.gz" },
		Member:  func(string) string { return "codebase-memory-mcp" },
		Links:   []Link{{State: "codebase-memory-mcp", Home: ".cache/codebase-memory-mcp"}},
	}
}
