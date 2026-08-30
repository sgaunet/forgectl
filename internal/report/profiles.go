package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/sgaunet/forgectl/internal/config"
)

// ProfileListing is the machine-readable shape of `profiles list`.
type ProfileListing struct {
	Profiles []ProfileSummary `json:"profiles"`
}

// ProfileSummary names one available profile and says where it came from.
type ProfileSummary struct {
	Name string `json:"name"`
	// Source is "builtin" or "configured", which is the question a maintainer
	// asks next after seeing a name they do not recognise.
	Source string `json:"source"`
	// Variables is how many variables the profile manages.
	Variables int `json:"variables"`
}

// ProfileDetail is the machine-readable shape of `profiles show`.
//
// It carries a value-source KIND for each variable and no value at all. That is
// the whole point of the command: someone who did not write the configuration
// can learn where each value comes from without being shown one (FR-020,
// SC-009).
type ProfileDetail struct {
	Name          string            `json:"name"`
	Source        string            `json:"source"`
	ProtectedTags []string          `json:"protected_tags"`
	Variables     []ProfileVariable `json:"variables"`
}

// ProfileVariable describes one managed variable, without its value.
type ProfileVariable struct {
	Name      string `json:"name"`
	Secret    bool   `json:"secret"`
	Masked    bool   `json:"masked"`
	Protected bool   `json:"protected"`
	// ValueSource is "literal", "value_ref", or "generator" — the KIND of the
	// source, never what it holds.
	ValueSource string `json:"value_source"`
	// Generator names the generator kind, set only for a generated variable.
	Generator string `json:"generator,omitempty"`
}

// The two places a profile can come from.
const (
	SourceBuiltin    = "builtin"
	SourceConfigured = "configured"
)

// ListingOf builds the listing from the resolved configuration.
//
// A configured profile whose name matches a built-in one appears ONCE,
// reflecting the configured definition: the merge already replaced it, so there
// is only one profile of that name to list (FR-008, FR-020).
func ListingOf(cfg *config.Config) ProfileListing {
	names := config.ProfileNames(cfg)

	listing := ProfileListing{Profiles: make([]ProfileSummary, 0, len(names))}
	for _, name := range names {
		profile := cfg.Profiles[name]
		listing.Profiles = append(listing.Profiles, ProfileSummary{
			Name:      name,
			Source:    sourceOf(cfg, name),
			Variables: len(profile.Variables),
		})
	}

	return listing
}

// DetailOf builds the detail view of one profile.
func DetailOf(cfg *config.Config, profile config.Profile, name string) ProfileDetail {
	detail := ProfileDetail{
		Name:          name,
		Source:        sourceOf(cfg, name),
		ProtectedTags: nonNilStrings(profile.ProtectedTags),
		Variables:     make([]ProfileVariable, 0, len(profile.Variables)),
	}

	for _, v := range profile.Variables {
		entry := ProfileVariable{
			Name:        v.Name,
			Secret:      v.Secret,
			Masked:      v.Masked,
			Protected:   v.Protected,
			ValueSource: v.Source().String(),
		}
		if v.Generator != nil {
			entry.Generator = v.Generator.Kind
		}

		detail.Variables = append(detail.Variables, entry)
	}

	sort.Slice(detail.Variables, func(i, j int) bool {
		return detail.Variables[i].Name < detail.Variables[j].Name
	})

	return detail
}

// sourceOf reports whether a profile is one forgectl ships or one the
// maintainer wrote. A configured profile of a built-in name is reported as
// configured: it is the maintainer's definition that is in force.
func sourceOf(cfg *config.Config, name string) string {
	if !config.IsBuiltin(name) {
		return SourceConfigured
	}

	builtin, err := config.BuiltinProfiles()
	if err != nil {
		return SourceBuiltin
	}

	if sameProfile(builtin[name], cfg.Profiles[name]) {
		return SourceBuiltin
	}

	return SourceConfigured
}

// sameProfile reports whether the effective profile is still the built-in one.
func sameProfile(builtin, effective config.Profile) bool {
	if len(builtin.Variables) != len(effective.Variables) ||
		len(builtin.ProtectedTags) != len(effective.ProtectedTags) {
		return false
	}

	for i := range builtin.Variables {
		if !builtin.Variables[i].Equal(effective.Variables[i]) {
			return false
		}
	}

	for i := range builtin.ProtectedTags {
		if builtin.ProtectedTags[i] != effective.ProtectedTags[i] {
			return false
		}
	}

	return true
}

// WriteListingText renders the listing for a human reader.
func WriteListingText(w io.Writer, listing ProfileListing) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	fmt.Fprintln(tw, "PROFILE\tSOURCE\tVARIABLES")
	for _, profile := range listing.Profiles {
		fmt.Fprintf(tw, "%s\t%s\t%d\n", profile.Name, profile.Source, profile.Variables)
	}

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing the profile listing: %w", err)
	}

	return nil
}

// WriteDetailText renders one profile for a human reader.
//
// Every column is an attribute or a source kind. There is no column a value
// could go in, which is how FR-020's "MUST NOT show any value" is met by
// construction rather than by remembering.
func WriteDetailText(w io.Writer, detail ProfileDetail) error {
	var b strings.Builder

	fmt.Fprintf(&b, "Profile   %s (%s)\n", detail.Name, detail.Source)

	if len(detail.ProtectedTags) > 0 {
		fmt.Fprintf(&b, "Tags      %s\n", strings.Join(detail.ProtectedTags, ", "))
	} else {
		fmt.Fprintln(&b, "Tags      none")
	}

	if len(detail.Variables) > 0 {
		b.WriteString("\n")

		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "VARIABLE\tSECRET\tMASKED\tPROTECTED\tVALUE SOURCE")

		for _, v := range detail.Variables {
			source := v.ValueSource
			if v.Generator != "" {
				source = v.ValueSource + " (" + v.Generator + ")"
			}
			fmt.Fprintf(tw, "%s\t%t\t%t\t%t\t%s\n",
				v.Name, v.Secret, v.Masked, v.Protected, source)
		}

		_ = tw.Flush()
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("writing the profile detail: %w", err)
	}

	return nil
}
