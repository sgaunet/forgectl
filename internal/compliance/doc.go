// Package compliance evaluates a repository against the declared conventions and
// builds the plan that would converge it. It is read-only by construction: it
// imports no execution path, so check can modify nothing.
package compliance
