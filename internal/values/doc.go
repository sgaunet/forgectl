// Package values resolves a variable's value through the ordered chain of
// override file, configuration, generator, and concealed prompt.
//
// A resolved value is held in memory only, passed to exactly one write, and
// never logged, wrapped into an error, or written to disk (FR-050, FR-054).
// That is why nothing here returns a value in an error, and why the resolver
// hands out one value per call rather than a map a caller could hold.
package values
