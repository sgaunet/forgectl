package config

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

// builtinProfiles carries the three profiles shipped inside the binary. It is
// embedded rather than read from disk so a clean install has them with no file
// to place (Constitution III, FR-008).
//
//go:embed builtin_profiles.yaml
var builtinProfiles []byte

// BuiltinProfiles parses the embedded profiles. It returns a fresh map on every
// call, so a caller that overrides one cannot mutate what the next caller sees.
func BuiltinProfiles() (map[string]Profile, error) {
	var doc struct {
		Profiles map[string]Profile `yaml:"profiles"`
	}

	if err := yaml.Unmarshal(builtinProfiles, &doc); err != nil {
		return nil, fmt.Errorf("%w: the embedded profiles: %w", ErrInvalid, err)
	}

	profiles := make(map[string]Profile, len(doc.Profiles))
	for name, profile := range doc.Profiles {
		profile.Name = name
		profiles[name] = profile
	}

	return profiles, nil
}

// applyBuiltinProfiles adds every built-in profile the configuration has not
// already replaced. A configured profile of the same name wins outright; a new
// name extends the set (FR-008).
func applyBuiltinProfiles(cfg *Config) error {
	builtin, err := BuiltinProfiles()
	if err != nil {
		return err
	}

	for name, profile := range builtin {
		if _, overridden := cfg.Profiles[name]; overridden {
			continue
		}
		cfg.Profiles[name] = profile
	}

	return nil
}

// IsBuiltin reports whether a profile name is one forgectl ships. It is used by
// the listing to say where a profile came from, never to change behaviour.
func IsBuiltin(name string) bool {
	builtin, err := BuiltinProfiles()
	if err != nil {
		return false
	}
	_, ok := builtin[name]

	return ok
}
