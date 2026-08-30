package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Validate checks every rule the configuration must satisfy, before any
// platform call is made (FR-016). Every failure names the offending element so
// the maintainer can find it without reading the whole file.
//
// All failures are reported together rather than one per run: a configuration
// with three mistakes should take one edit to fix, not three.
func Validate(cfg *Config) error {
	problems := make([]string, 0, len(cfg.Instances)+len(cfg.Profiles))

	problems = append(problems, validateInstances(cfg.Instances)...)
	problems = append(problems, validateSettings(cfg.Settings)...)
	problems = append(problems, validateProfiles(cfg)...)

	if len(problems) == 0 {
		return nil
	}

	sort.Strings(problems)

	return fmt.Errorf("%w:\n  - %s", ErrInvalid, strings.Join(problems, "\n  - "))
}

// validateInstances checks that instance names and hosts are unique and that
// every platform is one of the two known kinds.
func validateInstances(instances []Instance) []string {
	var problems []string

	names := make(map[string]bool, len(instances))
	hosts := make(map[string]bool, len(instances))

	for _, inst := range instances {
		switch {
		case inst.Name == "":
			problems = append(problems, "an instance declares no name")
		case names[inst.Name]:
			problems = append(problems, fmt.Sprintf("instance %q is declared more than once", inst.Name))
		default:
			names[inst.Name] = true
		}

		switch {
		case inst.Host == "":
			problems = append(problems, fmt.Sprintf("instance %q declares no host", inst.Name))
		case hosts[strings.ToLower(inst.Host)]:
			problems = append(problems, fmt.Sprintf(
				"host %q is declared by more than one instance", inst.Host))
		default:
			hosts[strings.ToLower(inst.Host)] = true
		}

		if _, err := ParsePlatform(string(inst.Platform)); err != nil {
			problems = append(problems, fmt.Sprintf(
				"instance %q: platform %q is not one of github, gitlab", inst.Name, inst.Platform))
		}

		if inst.TokenEnv == "" {
			problems = append(problems, fmt.Sprintf(
				"instance %q names no token_env; forgectl reads credentials only from the environment",
				inst.Name))
		}
	}

	return problems
}

// validateSettings checks the conventions that apply to every repository.
func validateSettings(s Settings) []string {
	if strings.TrimSpace(s.DefaultBranch) == "" {
		return []string{"settings.default_branch is empty"}
	}

	return nil
}

// validateProfiles checks every variable of every profile: exactly one value
// source, a resolvable reference, and a coherent generator.
func validateProfiles(cfg *Config) []string {
	var problems []string

	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		profile := cfg.Profiles[name]
		seen := make(map[string]bool, len(profile.Variables))

		for _, v := range profile.Variables {
			problems = append(problems, validateVariable(cfg, name, v)...)

			if v.Name != "" && seen[v.Name] {
				problems = append(problems, fmt.Sprintf(
					"profile %q declares variable %q more than once", name, v.Name))
			}
			seen[v.Name] = true
		}
	}

	return problems
}

// validateVariable checks one variable definition against FR-009 through
// FR-013 and FR-032.
//
// It deliberately does NOT check that a value_ref resolves. That check belongs
// to the selected profiles alone (see ValidateSelection): a built-in profile
// references a key the maintainer declares only when they actually use that
// profile, so validating every profile that merely exists would make forgectl
// unusable with no configuration file, which SC-001 requires to work.
func validateVariable(_ *Config, profile string, v VariableDefinition) []string {
	var problems []string

	if v.Name == "" {
		problems = append(problems, fmt.Sprintf("profile %q declares a variable with no name", profile))
	}

	sources := 0
	if v.Value != "" {
		sources++
	}
	if v.ValueRef != "" {
		sources++
	}
	if v.Generator != nil {
		sources++
	}

	switch {
	case sources == 0:
		problems = append(problems, fmt.Sprintf(
			"profile %q, variable %q: declares no value source; set exactly one of value, value_ref, generator",
			profile, v.Name))
	case sources > 1:
		problems = append(problems, fmt.Sprintf(
			"profile %q, variable %q: declares more than one value source; set exactly one of value, value_ref, generator",
			profile, v.Name))
	}

	if v.Generator != nil {
		problems = append(problems, validateGenerator(profile, v.Name, v.Generator)...)
	}

	return problems
}

// validateGenerator checks a generator's kind and the one relationship between
// its durations that would otherwise report drift forever.
func validateGenerator(profile, variable string, g *Generator) []string {
	var problems []string

	if g.Kind != GeneratorKindGitLabPAT {
		problems = append(problems, fmt.Sprintf(
			"profile %q, variable %q: generator %q is not %s, the only kind this version implements",
			profile, variable, g.Kind, GeneratorKindGitLabPAT))
	}

	if g.ExpiresIn <= 0 {
		problems = append(problems, fmt.Sprintf(
			"profile %q, variable %q: expires_in must be at least 1d", profile, variable))
	}

	if g.RotateBefore >= g.ExpiresIn {
		problems = append(problems, fmt.Sprintf(
			"profile %q, variable %q: rotate_before (%s) must be less than expires_in (%s), "+
				"or every check reports drift forever",
			profile, variable, g.RotateBefore, g.ExpiresIn))
	}

	if len(g.Scopes) == 0 {
		problems = append(problems, fmt.Sprintf(
			"profile %q, variable %q: a generated token needs at least one scope", profile, variable))
	}

	if g.TokenName == "" {
		problems = append(problems, fmt.Sprintf(
			"profile %q, variable %q: token_name is empty", profile, variable))
	}

	return problems
}

// ValidationProblems reports whether err is a validation failure, so a caller
// can distinguish "the configuration is wrong" from any other load failure.
func ValidationProblems(err error) bool {
	return errors.Is(err, ErrInvalid)
}

// ValidateSelection checks the rules that can only be judged once the run knows
// which profiles it operates on: every value_ref of a selected variable must
// resolve against the shared value store, naming both the variable and the
// missing key (FR-010). It runs before any platform call (FR-016).
func ValidateSelection(cfg *Config, sel Selection) error {
	var problems []string

	for _, v := range sel.Variables {
		if v.ValueRef == "" {
			continue
		}
		if _, ok := cfg.Values[v.ValueRef]; !ok {
			problems = append(problems, fmt.Sprintf(
				"variable %q: value_ref %q resolves against no key in the value store; "+
					"declare it under `values` in %s",
				v.Name, v.ValueRef, configPathForMessage(cfg)))
		}
	}

	if len(problems) == 0 {
		return nil
	}

	sort.Strings(problems)

	return fmt.Errorf("%w:\n  - %s", ErrInvalid, strings.Join(problems, "\n  - "))
}
