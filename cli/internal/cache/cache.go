// Package cache exposes the on-disk location where the CLI caches
// downloaded skills. The Settings view also surfaces this path.
package cache

import (
	"os"
	"path/filepath"
)

// CacheRoot returns the directory where skill payloads are cached.
//
// Resolution:
//  1. $XDG_CACHE_HOME/skills-registry/skills if XDG_CACHE_HOME is set.
//  2. $HOME/.cache/skills-registry/skills otherwise.
//
// The path is returned verbatim — neither stat-ed nor created. The
// hub's Settings view treats it as a display string only.
func CacheRoot() string {
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, "skills-registry", "skills")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "skills-registry", "skills")
}
