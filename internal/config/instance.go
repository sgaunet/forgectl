package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

var (
	// ErrUnknownHost reports a remote host matching no configured instance and
	// neither built-in one (FR-003).
	ErrUnknownHost = errors.New("no instance declared for host")

	// ErrNoCredential reports an instance whose credential environment
	// variable is unset or empty. It is raised before any network call
	// (FR-005).
	ErrNoCredential = errors.New("credential environment variable is unset")
)

// ResolveInstance matches a remote host against the configured instances,
// falling back to the built-in definitions for the two public hosts. A host
// matching nothing fails with a message naming the host and the configuration
// file in which to declare it (FR-003).
//
// Configured instances are searched first, so declaring github.com explicitly
// — with an enterprise API URL, say — overrides the built-in definition.
func ResolveInstance(cfg *Config, host string) (Instance, error) {
	wanted := strings.ToLower(host)

	// Configured instances are searched first, so declaring github.com with an
	// enterprise API URL overrides the built-in definition of the same host.
	for _, inst := range cfg.Instances {
		if strings.EqualFold(inst.Host, wanted) {
			return withAPIDefault(inst), nil
		}
	}

	// Only then the two built-ins, which is what makes forgectl usable against
	// the public hosts with no configuration file at all (FR-003, SC-001).
	for _, inst := range BuiltinInstances() {
		if strings.EqualFold(inst.Host, wanted) {
			return inst, nil
		}
	}

	return Instance{}, fmt.Errorf(
		"%w: %q; declare it under `instances` in %s",
		ErrUnknownHost, host, configPathForMessage(cfg))
}

// withAPIDefault fills in the API base URL an instance did not state, using the
// platform's convention for its host.
func withAPIDefault(inst Instance) Instance {
	if inst.APIURL != "" {
		return inst
	}

	switch inst.Platform {
	case PlatformGitHub:
		inst.APIURL = "https://" + inst.Host + "/api/v3/"
	case PlatformGitLab:
		inst.APIURL = "https://" + inst.Host + "/api/v4"
	}

	return inst
}

// Credential reads the instance's credential from the environment variable the
// instance names. It is never read from a flag or the configuration file, and a
// missing or empty variable fails before any network call (FR-005, CLI-004).
//
// The credential itself never appears in an error message.
func Credential(inst Instance) (string, error) {
	if inst.TokenEnv == "" {
		return "", fmt.Errorf(
			"%w: instance %q names no token_env", ErrNoCredential, inst.Name)
	}

	token := os.Getenv(inst.TokenEnv)
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf(
			"%w: %s is unset or empty; instance %q reads its credential from it",
			ErrNoCredential, inst.TokenEnv, inst.Name)
	}

	return token, nil
}

// configPathForMessage names the file the maintainer should edit, falling back
// to the default location when no file was loaded.
func configPathForMessage(cfg *Config) string {
	if cfg.Path != "" {
		return cfg.Path
	}

	path, err := DefaultConfigPath()
	if err != nil {
		return "the forgectl configuration file"
	}

	return path
}

// KnownHosts lists the hosts the configuration can resolve, for error messages
// that help rather than merely refuse.
func KnownHosts(cfg *Config) []string {
	seen := make(map[string]bool)
	for _, inst := range cfg.Instances {
		seen[inst.Host] = true
	}
	for _, inst := range BuiltinInstances() {
		seen[inst.Host] = true
	}

	hosts := make([]string, 0, len(seen))
	for host := range seen {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	return hosts
}
