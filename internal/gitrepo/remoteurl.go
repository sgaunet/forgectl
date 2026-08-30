package gitrepo

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrRemoteURL reports a remote URL forgectl cannot derive a repository from.
// It is a usage error: nothing was attempted (CLI-002).
var ErrRemoteURL = errors.New("remote URL not understood")

// RemoteRef is the repository a remote URL points at (FR-002).
type RemoteRef struct {
	Host  string
	Owner string
	Repo  string
}

// String renders the reference as owner/name, the form both platforms use as a
// project identifier and the form the report carries.
func (r RemoteRef) String() string {
	return r.Owner + "/" + r.Repo
}

// ParseRemoteURL derives the host, owner, and repository name from a git remote
// URL. It accepts the scp-like SSH form (git@host:owner/repo.git), the SSH URL
// form with an optional explicit port (ssh://git@host:2222/owner/repo.git), and
// the HTTP(S) form, stripping a trailing ".git" (FR-002).
func ParseRemoteURL(raw string) (RemoteRef, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return RemoteRef{}, fmt.Errorf("%w: the remote URL is empty", ErrRemoteURL)
	}

	host, path, err := splitHostPath(trimmed)
	if err != nil {
		return RemoteRef{}, err
	}

	owner, repo, err := splitOwnerRepo(path, trimmed)
	if err != nil {
		return RemoteRef{}, err
	}

	return RemoteRef{Host: strings.ToLower(host), Owner: owner, Repo: repo}, nil
}

// splitHostPath separates the host from the repository path, handling the two
// URL shapes git accepts. The scp-like form is not a URL, so it is recognised
// before net/url is given a chance to misread it.
func splitHostPath(raw string) (string, string, error) {
	if !strings.Contains(raw, "://") {
		return splitSCPLike(raw)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("%w: %q: %w", ErrRemoteURL, raw, err)
	}

	switch parsed.Scheme {
	case "ssh", "git", "http", "https":
	default:
		return "", "", fmt.Errorf("%w: %q uses unsupported scheme %q", ErrRemoteURL, raw, parsed.Scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return "", "", fmt.Errorf("%w: %q names no host", ErrRemoteURL, raw)
	}

	return host, parsed.Path, nil
}

// splitSCPLike handles git@host:owner/repo.git, where the colon separates the
// host from the path rather than introducing a port.
func splitSCPLike(raw string) (string, string, error) {
	hostPart, path, found := strings.Cut(raw, ":")
	if !found {
		return "", "", fmt.Errorf("%w: %q is not a remote URL forgectl understands", ErrRemoteURL, raw)
	}

	// Drop the user, which carries no information forgectl needs.
	if _, after, ok := strings.Cut(hostPart, "@"); ok {
		hostPart = after
	}

	if hostPart == "" || strings.ContainsAny(hostPart, "/\\") {
		return "", "", fmt.Errorf("%w: %q names no host", ErrRemoteURL, raw)
	}

	return hostPart, path, nil
}

// splitOwnerRepo takes the repository path and returns the owner — which may be
// a nested GitLab subgroup path — and the repository name.
func splitOwnerRepo(path, raw string) (string, string, error) {
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")

	// Cut at the LAST separator: the owner is everything before it, which keeps
	// a nested GitLab subgroup path intact.
	cut := strings.LastIndex(path, "/")
	if cut < 0 {
		return "", "", fmt.Errorf("%w: %q names no owner and repository", ErrRemoteURL, raw)
	}

	owner, repo := path[:cut], path[cut+1:]
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("%w: %q names no owner and repository", ErrRemoteURL, raw)
	}

	return owner, repo, nil
}
