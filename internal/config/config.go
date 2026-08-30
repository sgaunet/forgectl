package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrInvalid reports a configuration forgectl refuses to act on. Every
// validation failure wraps it, and cmd/forgectl maps it to exit 2: the
// configuration was wrong and nothing was attempted (R11, CLI-002).
var ErrInvalid = errors.New("invalid configuration")

// Platform is the kind of forge an instance runs.
type Platform string

// The two platforms this version supports.
const (
	PlatformGitHub Platform = "github"
	PlatformGitLab Platform = "gitlab"
)

// String renders the platform as it appears in configuration and output.
func (p Platform) String() string { return string(p) }

// ParsePlatform converts a configured string into a Platform, rejecting any
// value that is not one of the two known kinds (R13).
func ParsePlatform(s string) (Platform, error) {
	switch Platform(s) {
	case PlatformGitHub:
		return PlatformGitHub, nil
	case PlatformGitLab:
		return PlatformGitLab, nil
	default:
		return "", fmt.Errorf("%w: platform %q is not one of github, gitlab", ErrInvalid, s)
	}
}

// AccessLevel is the push or role level a protection rule grants. It is a
// GitLab concept; GitHub has no equivalent and ignores it (FR-026).
type AccessLevel string

// The access levels configuration may name.
const (
	AccessNone       AccessLevel = "none"
	AccessDeveloper  AccessLevel = "developer"
	AccessMaintainer AccessLevel = "maintainer"
)

// String renders the access level as it appears in configuration and output.
func (a AccessLevel) String() string { return string(a) }

// GitLab maps the access level onto the numeric level the GitLab API takes:
// 0 none, 30 developer, 40 maintainer (R9).
func (a AccessLevel) GitLab() int {
	switch a {
	case AccessNone:
		return 0
	case AccessDeveloper:
		return 30
	case AccessMaintainer:
		return 40
	default:
		return 0
	}
}

// ParseAccessLevel converts a configured string into an AccessLevel, rejecting
// unknown values (R13).
func ParseAccessLevel(s string) (AccessLevel, error) {
	switch AccessLevel(s) {
	case AccessNone:
		return AccessNone, nil
	case AccessDeveloper:
		return AccessDeveloper, nil
	case AccessMaintainer:
		return AccessMaintainer, nil
	default:
		return "", fmt.Errorf(
			"%w: access level %q is not one of none, developer, maintainer", ErrInvalid, s)
	}
}

// UnmarshalYAML parses an access level, rejecting unknown values at load time.
func (a *AccessLevel) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf("%w: access level: %w", ErrInvalid, err)
	}

	parsed, err := ParseAccessLevel(s)
	if err != nil {
		return err
	}
	*a = parsed

	return nil
}

// Days is a whole number of days, the only duration form configuration accepts
// (FR-013). It is deliberately not a time.Duration, so a lifetime in days is
// never confused with one in nanoseconds (Constitution IV).
type Days int

// String renders the duration in the <N>d form configuration uses.
func (d Days) String() string { return strconv.Itoa(int(d)) + "d" }

// ParseDays parses the <N>d form. Any other form is a configuration error.
func ParseDays(s string) (Days, error) {
	digits, ok := strings.CutSuffix(s, "d")
	if !ok || digits == "" {
		return 0, fmt.Errorf(
			"%w: duration %q must be a whole number of days in the form <N>d", ErrInvalid, s)
	}

	n, err := strconv.Atoi(digits)
	if err != nil || n < 0 {
		return 0, fmt.Errorf(
			"%w: duration %q must be a whole number of days in the form <N>d", ErrInvalid, s)
	}

	return Days(n), nil
}

// UnmarshalYAML parses a duration, rejecting any form but <N>d at load time.
func (d *Days) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf("%w: duration: %w", ErrInvalid, err)
	}

	parsed, err := ParseDays(s)
	if err != nil {
		return err
	}
	*d = parsed

	return nil
}

// Instance is a declared forge endpoint.
type Instance struct {
	Name     string   `yaml:"name"`
	Host     string   `yaml:"host"`
	Platform Platform `yaml:"platform"`
	APIURL   string   `yaml:"api_url"`
	TokenEnv string   `yaml:"token_env"`
}

// Settings holds the conventions that apply to every repository.
type Settings struct {
	DefaultBranch string `yaml:"default_branch"`
}

// BranchProtection holds the protection applied to the target branch.
type BranchProtection struct {
	Enabled         bool        `yaml:"enabled"`
	AllowForcePush  bool        `yaml:"allow_force_push"`
	AllowDelete     bool        `yaml:"allow_delete"`
	PushAccessLevel AccessLevel `yaml:"push_access_level"`
}

// Generator describes the project access token forgectl creates and rotates.
// It exists only for the gitlab-pat kind, the only kind in this version.
type Generator struct {
	Kind          string      `yaml:"kind"`
	TokenName     string      `yaml:"token_name"`
	Scopes        []string    `yaml:"scopes"`
	Role          AccessLevel `yaml:"role"`
	ExpiresIn     Days        `yaml:"expires_in"`
	RotateBefore  Days        `yaml:"rotate_before"`
	RevokeRotated bool        `yaml:"revoke_rotated"`
}

// GeneratorKindGitLabPAT is the only generator kind this version implements.
const GeneratorKindGitLabPAT = "gitlab-pat"

// ValueSource names where a variable's value comes from, without ever carrying
// the value itself. It is what `profiles show` displays (FR-020).
type ValueSource string

// The three value sources a variable definition may declare.
const (
	SourceLiteral   ValueSource = "literal"
	SourceRef       ValueSource = "value_ref"
	SourceGenerator ValueSource = "generator"
)

// String renders the value source as it appears in output.
func (v ValueSource) String() string { return string(v) }

// VariableDefinition is one CI variable a profile manages.
type VariableDefinition struct {
	Name      string     `yaml:"name"`
	Value     string     `yaml:"value"`
	ValueRef  string     `yaml:"value_ref"`
	Generator *Generator `yaml:"-"`

	Secret    bool `yaml:"secret"`
	Masked    bool `yaml:"masked"`
	Protected bool `yaml:"protected"`
}

// Source reports which of the three value sources this definition declares.
// Validation has already guaranteed exactly one is set (FR-009).
func (v VariableDefinition) Source() ValueSource {
	switch {
	case v.Generator != nil:
		return SourceGenerator
	case v.ValueRef != "":
		return SourceRef
	default:
		return SourceLiteral
	}
}

// Profile is a named project type: the variables it manages and the tag
// patterns its release pipeline needs protected.
type Profile struct {
	Name          string               `yaml:"-"`
	ProtectedTags []string             `yaml:"protected_tags"`
	Variables     []VariableDefinition `yaml:"variables"`
}

// Config is the whole configuration tree after the four-layer merge.
type Config struct {
	Instances        []Instance         `yaml:"instances"`
	Settings         Settings           `yaml:"settings"`
	Values           map[string]string  `yaml:"values"`
	BranchProtection BranchProtection   `yaml:"branch_protection"`
	Profiles         map[string]Profile `yaml:"profiles"`

	// Path records where the configuration was loaded from, for error messages.
	Path string `yaml:"-"`
}

// DefaultPath is where forgectl looks when no path is given (FR-006).
const DefaultPath = "forgectl/config.yaml"

// Options are the values the CLI layer resolved from flags and the environment,
// carrying for each whether it was actually set. The merge reads these rather
// than comparing against the zero value, so `--remote ""` is distinguishable
// from an absent flag (R14).
type Options struct {
	Path             string
	PathSet          bool
	Remote           string
	RemoteSet        bool
	Output           string
	OutputSet        bool
	AllowInsecure    bool
	AllowInsecureSet bool
}

// Resolved is the effective configuration after every layer has been applied.
type Resolved struct {
	Config Config
	Remote string
	Output string
}

// Defaults returns the bottom layer of the merge: the conventions that hold
// when nothing is configured (FR-015, FR-003).
func Defaults() Config {
	return Config{
		// The built-in instances are deliberately NOT seeded here. FR-003 makes
		// them a fallback consulted only when no configured instance matches,
		// which is what lets a maintainer declare github.com with an enterprise
		// API URL without colliding with the built-in of the same host.
		Settings: Settings{DefaultBranch: "main"},
		Values:   map[string]string{},
		BranchProtection: BranchProtection{
			Enabled:         true,
			AllowForcePush:  false,
			AllowDelete:     false,
			PushAccessLevel: AccessMaintainer,
		},
		Profiles: map[string]Profile{},
	}
}

// BuiltinInstances are the two public hosts forgectl knows without any
// configuration at all (FR-003, SC-001).
func BuiltinInstances() []Instance {
	return []Instance{
		{
			Name:     "github.com",
			Host:     "github.com",
			Platform: PlatformGitHub,
			APIURL:   "https://api.github.com/",
			TokenEnv: "GITHUB_TOKEN",
		},
		{
			Name:     "gitlab.com",
			Host:     "gitlab.com",
			Platform: PlatformGitLab,
			APIURL:   "https://gitlab.com/api/v4",
			TokenEnv: "GITLAB_TOKEN",
		},
	}
}

// DefaultConfigPath is the fixed location under the user's configuration
// directory that FR-006 names.
func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating the user configuration directory: %w", err)
	}

	return filepath.Join(dir, DefaultPath), nil
}

// Load resolves the configuration through the four layers of R14 —
// defaults, then the configuration file, then the environment, then flags —
// each layer overwriting only what it sets, and validates the result before
// returning it. Nothing here touches the network (FR-016).
//
// A configuration file that does not exist is not an error: the three built-in
// profiles and the two built-in instances make forgectl usable with no file at
// all (SC-001).
func Load(opts Options, env Environment) (*Resolved, error) {
	cfg := Defaults()

	path, err := resolvePath(opts, env)
	if err != nil {
		return nil, err
	}

	fileCfg, found, err := readFile(path, opts.AllowInsecure || env.AllowInsecure)
	if err != nil {
		return nil, err
	}
	if found {
		cfg.Path = path
		mergeFile(&cfg, fileCfg)
	}

	if err := applyBuiltinProfiles(&cfg); err != nil {
		return nil, err
	}

	resolved := &Resolved{
		Config: cfg,
		Remote: firstNonEmpty(pick(opts.Remote, opts.RemoteSet), env.Remote, "origin"),
		Output: firstNonEmpty(pick(opts.Output, opts.OutputSet), env.Output, "text"),
	}

	if err := validateOutput(resolved.Output); err != nil {
		return nil, err
	}

	if err := Validate(&resolved.Config); err != nil {
		return nil, err
	}

	return resolved, nil
}

// Environment holds the FORGECTL_-prefixed overrides, read once by the CLI
// layer so the merge itself never reaches for os.Getenv (R14, CLI-004).
type Environment struct {
	Path          string
	Remote        string
	Output        string
	AllowInsecure bool
}

// ReadEnvironment collects the environment layer.
func ReadEnvironment() Environment {
	return Environment{
		Path:   os.Getenv("FORGECTL_CONFIG"),
		Remote: os.Getenv("FORGECTL_REMOTE"),
		Output: os.Getenv("FORGECTL_OUTPUT"),
	}
}

// resolvePath applies the precedence chain to the configuration file location.
func resolvePath(opts Options, env Environment) (string, error) {
	if p := pick(opts.Path, opts.PathSet); p != "" {
		return p, nil
	}
	if env.Path != "" {
		return env.Path, nil
	}

	return DefaultConfigPath()
}

// readFile reads and parses the configuration file, after checking that its
// permissions are no wider than 0600 (FR-007).
func readFile(path string, allowInsecure bool) (Config, bool, error) {
	if err := CheckPermissions(path, allowInsecure); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, false, nil
		}

		return Config{}, false, err
	}

	data, err := os.ReadFile(path) //nolint:gosec // the path is the user's own configuration
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, false, nil
		}

		return Config{}, false, fmt.Errorf("reading %s: %w", path, err)
	}

	cfg, err := decodeStrict(data)
	if err != nil {
		return Config{}, false, fmt.Errorf("%w: %s: %w", ErrInvalid, path, err)
	}

	return cfg, true, nil
}

// mergeFile overlays the configuration file onto the defaults, field by field,
// so a file that sets nothing changes nothing.
func mergeFile(cfg *Config, file Config) {
	cfg.Instances = append(cfg.Instances, file.Instances...)

	if file.Settings.DefaultBranch != "" {
		cfg.Settings.DefaultBranch = file.Settings.DefaultBranch
	}

	for k, v := range file.Values {
		cfg.Values[k] = v
	}

	if file.BranchProtection.PushAccessLevel != "" {
		cfg.BranchProtection.PushAccessLevel = file.BranchProtection.PushAccessLevel
	}

	for name, profile := range file.Profiles {
		profile.Name = name
		// A configured profile replaces its built-in namesake entirely: there
		// is no field-level merge, so a partial override cannot silently
		// inherit a variable the maintainer meant to drop (FR-008).
		cfg.Profiles[name] = profile
	}
}

// pick returns the value only when the flag was actually set.
func pick(value string, set bool) string {
	if !set {
		return ""
	}

	return value
}

// firstNonEmpty returns the first value that carries something, which is how
// each layer of the precedence chain yields to the one below it.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

// validateOutput rejects an output format that has no renderer.
func validateOutput(format string) error {
	if format != "text" && format != "json" {
		return fmt.Errorf("%w: --output must be text or json, got %q", ErrInvalid, format)
	}

	return nil
}

// decodeStrict parses the configuration document rejecting any field the schema
// does not declare. config.schema.json sets additionalProperties: false
// throughout, and a silently ignored field is worse than a refusal: a
// maintainer who mistypes `default_branch` would otherwise be told their
// convention holds while forgectl checks another.
func decodeStrict(data []byte) (Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			// An empty file configures nothing, which is not a mistake.
			return Config{}, nil
		}

		return Config{}, err //nolint:wrapcheck // the caller adds the path and ErrInvalid
	}

	return cfg, nil
}
