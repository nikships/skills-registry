// Package cache exposes the on-disk location where the Python MCP
// server caches downloaded skills. The Go CLI never reads or writes
// this directory directly — only `skills-registry-mcp` (Python) does —
// but the Settings view inside the hub surfaces the path so users can
// inspect or clean it manually.
//
// The path resolution mirrors src/skills_mcp/cache.py::cache_root so
// the value displayed in the TUI matches what the Python side actually
// uses at runtime.
package cache

import (
	"os"
	"path/filepath"
)

// CacheRoot returns the directory where skill payloads are cached.
//
// Resolution (matching cache.py):
//  1. $XDG_CACHE_HOME/skills-mcp/skills if XDG_CACHE_HOME is set.
//  2. $HOME/.cache/skills-mcp/skills otherwise.
//
// The path is returned verbatim — neither stat-ed nor created. The
// hub's Settings view treats it as a display string only.
func CacheRoot() string {
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, "skills-mcp", "skills")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "skills-mcp", "skills")
}
