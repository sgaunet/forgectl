package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// RepoFileName is the per-repository file that names which profiles apply, read
// from the repository root when the command line names none (FR-017).
const RepoFileName = ".forgectl.yaml"

// ErrUnknownProfile reports a profile name that matches nothing built in and
// nothing configured. It is a usage error: nothing was attempted (CLI-002).
var ErrUnknownProfile = errors.New("unknown profile")

// Selection is the set of profiles a run operates on, and the union of the
// variables and tag patterns they declare.
type Selection struct {
	// Names are the selected profile names, in the order they were given.
	Names []string
	// Variables is the union across the selected profiles, deduplicated by
	// name and sorted so a report is stable between runs (FR-018).
	Variables []VariableDefinition
	// ProtectedTags is the union of the tag patterns those profiles declare,
	// deduplicated and sorted (FR-014, FR-025).
	ProtectedTags []string
}

// Empty reports whether no profile was selected, in which case only the branch
// and protection checks run and a warning is emitted (FR-019).
func (s Selection) Empty() bool { return len(s.Names) == 0 }

// SelectProfiles resolves which profiles apply: the names given on the command
// line, else those listed in .forgectl.yaml at the repository root, else none
// (FR-017). It then combines them into a deduplicated union (FR-018).
func SelectProfiles(cfg *Config, args []string, repoRoot string) (Selection, error) {
	names := args
	if len(names) == 0 && repoRoot != "" {
		fromFile, err := readRepoFile(filepath.Join(repoRoot, RepoFileName))
		if err != nil {
			return Selection{}, err
		}
		names = fromFile
	}

	if len(names) == 0 {
		return Selection{}, nil
	}

	sel, err := combine(cfg, names)
	if err != nil {
		return Selection{}, err
	}

	// A reference is checked only once the run knows which profiles it uses,
	// so an unused built-in profile cannot refuse to start (FR-010, SC-001).
	if err := ValidateSelection(cfg, sel); err != nil {
		return Selection{}, err
	}

	return sel, nil
}

// readRepoFile reads the per-repository profile list. A file that is not there
// simply selects nothing.
func readRepoFile(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is inside the user's own working copy
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var doc struct {
		Profiles []string `yaml:"profiles"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInvalid, path, err)
	}

	return doc.Profiles, nil
}

// combine unions the named profiles, deduplicating variables by name. Two
// profiles may declare one variable name only if every field agrees; any
// difference is a configuration error naming the variable and both profiles
// (FR-018).
func combine(cfg *Config, names []string) (Selection, error) {
	sel := Selection{Names: dedupeStrings(names)}

	byName := make(map[string]VariableDefinition)
	declaredIn := make(map[string]string)
	tags := make(map[string]bool)

	for _, name := range sel.Names {
		profile, ok := cfg.Profiles[name]
		if !ok {
			return Selection{}, fmt.Errorf("%w: %q; run `forgectl profiles list` to see the available ones",
				ErrUnknownProfile, name)
		}

		for _, pattern := range profile.ProtectedTags {
			tags[pattern] = true
		}

		for _, v := range profile.Variables {
			existing, seen := byName[v.Name]
			if !seen {
				byName[v.Name] = v
				declaredIn[v.Name] = name

				continue
			}

			if !existing.Equal(v) {
				return Selection{}, fmt.Errorf(
					"%w: profiles %q and %q both declare variable %q with differing attributes "+
						"or value sources; make them identical or select only one profile",
					ErrInvalid, declaredIn[v.Name], name, v.Name)
			}
		}
	}

	sel.Variables = sortedVariables(byName)
	sel.ProtectedTags = sortedKeys(tags)

	return sel, nil
}

// dedupeStrings keeps the first occurrence of each name, preserving the order
// the maintainer gave.
func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))

	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}

	return out
}

// sortedVariables returns the union in a stable order, so two runs against the
// same configuration produce the same report.
func sortedVariables(byName map[string]VariableDefinition) []VariableDefinition {
	out := make([]VariableDefinition, 0, len(byName))
	for _, v := range byName {
		out = append(out, v)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// sortedKeys returns a set's members in a stable order.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)

	return out
}

// ProfileNames lists every available profile, built-in and configured, in a
// stable order (FR-020).
func ProfileNames(cfg *Config) []string {
	return sortedKeys(profileSet(cfg))
}

// profileSet builds the name set behind ProfileNames.
func profileSet(cfg *Config) map[string]bool {
	set := make(map[string]bool, len(cfg.Profiles))
	for name := range cfg.Profiles {
		set[name] = true
	}

	return set
}

// LookupProfile finds one profile by name.
func LookupProfile(cfg *Config, name string) (Profile, error) {
	profile, ok := cfg.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("%w: %q; run `forgectl profiles list` to see the available ones",
			ErrUnknownProfile, name)
	}

	return profile, nil
}
