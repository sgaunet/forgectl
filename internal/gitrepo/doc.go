// Package gitrepo inspects the local git working copy: discovery from any
// subdirectory, remote URL parsing, ignore lookups, and the branch commands apply
// runs. Every operation shells out to the git binary, which must be on PATH.
package gitrepo
