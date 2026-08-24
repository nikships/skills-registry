// Package trust classifies where an imported skill came from, so a caller can
// tell "a folder I already own" apart from "a stranger's SKILL.md".
//
// The distinction matters because importing a skill has two very different
// effects. Publishing it into the user's own registry is recoverable: it is one
// commit in a repository they control. Durably installing it into agent
// dot-folders is not comparable: every agent then loads that SKILL.md as
// instructions on every session, with no further prompt. The second step is
// only defensible for a source the user already trusts, so the two need to be
// separable, and that requires naming the source's origin.
//
// This package is deliberately offline and allocation-light: it parses the
// source string and compares owners. It never reaches the network, so callers
// can classify before deciding whether to fetch anything at all.
package trust

import (
	"regexp"
	"strings"

	"github.com/nikships/skills-registry/cli/internal/registry"
)

// Origin names where an add source came from.
type Origin string

const (
	// OriginLocalPath is a directory on this machine. The user already has
	// the files; nothing new is being introduced.
	OriginLocalPath Origin = "local_path"

	// OriginOwnRepo is a GitHub repository under an owner the caller listed
	// as the user's own, in any accepted shape (shorthand, repo URL, tree or
	// blob URL).
	OriginOwnRepo Origin = "own_repo"

	// OriginPublicRepo is a GitHub repository owned by somebody else.
	OriginPublicRepo Origin = "public_repo"

	// OriginRemoteGit is any other remote git URL (GitLab, a `git@` remote, a
	// self-hosted forge). Ownership cannot be established from the URL, so it
	// is treated as third-party.
	OriginRemoteGit Origin = "remote_git"

	// OriginDiscover is a row picked out of the public skill index. It is
	// third-party by construction, whatever URL shape it carries.
	OriginDiscover Origin = "discover"
)

// Untrusted reports whether an origin must go through the import gate.
func (o Origin) Untrusted() bool {
	switch o {
	case OriginLocalPath, OriginOwnRepo:
		return false
	default:
		return true
	}
}

// Options describes the caller's context for one classification.
type Options struct {
	// Owners are the GitHub logins the user controls, compared
	// case-insensitively. Callers pass at least the owner of the configured
	// registry repository; an empty list makes every GitHub source
	// third-party, which fails safe.
	Owners []string

	// FromDiscover marks a source the user picked out of the public index
	// rather than typed. Such a source is untrusted regardless of its shape.
	FromDiscover bool
}

// Assessment is the result of classifying one source.
type Assessment struct {
	// Source is the input, unchanged.
	Source string
	// Origin is the classification.
	Origin Origin
	// Untrusted mirrors Origin.Untrusted(), so a caller can branch on one
	// field without switching over origins.
	Untrusted bool
	// Owner is the GitHub owner when the source named one, else empty.
	Owner string
	// Reason is a short human-readable justification, suitable for a
	// confirmation screen or a JSON payload.
	Reason string
}

// shorthandRe matches the `owner/repo` shorthand `add` accepts. Exactly two
// segments of GitHub-legal characters, so neither a path (`./a/b`) nor a URL
// can match.
var shorthandRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// ParseOwnerRepo splits the `owner/repo` shorthand. The bool is false for
// every other shape.
func ParseOwnerRepo(source string) (owner, repo string, ok bool) {
	s := strings.TrimSpace(source)
	if !shorthandRe.MatchString(s) {
		return "", "", false
	}
	owner, repo, _ = strings.Cut(s, "/")
	return owner, strings.TrimSuffix(repo, ".git"), true
}

// IsLocalPath reports whether the source names a filesystem path rather than a
// remote. Mirrors what `add` treats as a local directory.
func IsLocalPath(source string) bool {
	s := strings.TrimSpace(source)
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "../") || strings.HasPrefix(s, "~")
}

// Assess classifies one add source.
//
// A GitHub source under one of opts.Owners is the user's own and stays
// trusted, whichever URL shape it arrived in: pinning a folder of your own
// repository is not a third-party import. Everything else remote is
// untrusted, including a non-GitHub git URL, because nothing in the string
// establishes who wrote it.
func Assess(source string, opts Options) Assessment {
	a := Assessment{Source: source}
	switch {
	case opts.FromDiscover:
		a.Origin = OriginDiscover
		a.Reason = "picked from the public skill index"
		if owner, ok := githubOwner(source); ok {
			a.Owner = owner
		}
	case IsLocalPath(source):
		a.Origin = OriginLocalPath
		a.Reason = "a local directory on this machine"
	default:
		a = assessRemote(a, source, opts.Owners)
	}
	a.Untrusted = a.Origin.Untrusted()
	return a
}

// assessRemote classifies the non-local shapes: `owner/repo` shorthand, a
// github.com URL, or any other git URL.
func assessRemote(a Assessment, source string, owners []string) Assessment {
	owner, ok := githubOwner(source)
	if !ok {
		a.Origin = OriginRemoteGit
		a.Reason = "a third-party git remote; ownership cannot be established from the URL"
		return a
	}
	a.Owner = owner
	if ownedBy(owner, owners) {
		a.Origin = OriginOwnRepo
		a.Reason = "a GitHub repository owned by " + owner
		return a
	}
	a.Origin = OriginPublicRepo
	a.Reason = "a public GitHub repository owned by " + owner
	return a
}

// githubOwner extracts the owner from either accepted GitHub shape.
func githubOwner(source string) (string, bool) {
	if owner, _, ok := ParseOwnerRepo(source); ok {
		return owner, true
	}
	if target, ok := registry.ParseGitHubURL(source); ok {
		return target.Owner, true
	}
	return "", false
}

// ownedBy compares a GitHub owner against the user's logins.
// GitHub logins are case-insensitive, so the comparison is too.
func ownedBy(owner string, owners []string) bool {
	if strings.TrimSpace(owner) == "" {
		return false
	}
	for _, own := range owners {
		if strings.EqualFold(strings.TrimSpace(own), owner) {
			return true
		}
	}
	return false
}
