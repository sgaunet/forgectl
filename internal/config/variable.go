package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// The generator defaults of FR-012, applied to any generated variable that does
// not override them.
const (
	DefaultTokenName    = "forgectl"
	DefaultRole         = AccessMaintainer
	DefaultExpiresIn    = Days(180)
	DefaultRotateBefore = Days(60)
)

// DefaultScopes is the scope set a generated token carries unless the profile
// names another (FR-012).
func DefaultScopes() []string { return []string{"api"} }

// variableYAML mirrors the on-disk shape of a variable. Every field is a
// pointer so "absent" is distinguishable from "set to the zero value", which is
// what lets `secret` default to true while still honouring `secret: false`.
//
// The generator fields sit beside the variable's own in the file, per
// config.schema.json, and are lifted into a Generator here.
type variableYAML struct {
	Name      *string `yaml:"name"`
	Value     *string `yaml:"value"`
	ValueRef  *string `yaml:"value_ref"`
	Generator *string `yaml:"generator"`

	Secret    *bool `yaml:"secret"`
	Masked    *bool `yaml:"masked"`
	Protected *bool `yaml:"protected"`

	TokenName     *string   `yaml:"token_name"`
	Scopes        *[]string `yaml:"scopes"`
	Role          *string   `yaml:"role"`
	ExpiresIn     *string   `yaml:"expires_in"`
	RotateBefore  *string   `yaml:"rotate_before"`
	RevokeRotated *bool     `yaml:"revoke_rotated"`
}

// UnmarshalYAML reads a variable definition and applies the attribute defaults
// of FR-011 and the generator defaults of FR-012. It records what the file
// declared; whether that declaration is coherent is Validate's business, so
// every rule of FR-009 is reported with the rest rather than one at a time.
func (v *VariableDefinition) UnmarshalYAML(node *yaml.Node) error {
	// yaml.v3 does not carry the decoder's KnownFields setting into a custom
	// unmarshaller, so the schema's additionalProperties: false is enforced
	// here by hand. It is what refuses a variable naming a platform or an
	// instance, which one run must never do (FR-032).
	if err := rejectUnknownVariableFields(node); err != nil {
		return err
	}

	var raw variableYAML
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("%w: variable: %w", ErrInvalid, err)
	}

	*v = VariableDefinition{
		Secret:    true,  // FR-011
		Masked:    false, // FR-011
		Protected: false, // FR-011
	}

	if raw.Name != nil {
		v.Name = *raw.Name
	}
	if raw.Value != nil {
		v.Value = *raw.Value
	}
	if raw.ValueRef != nil {
		v.ValueRef = *raw.ValueRef
	}
	if raw.Secret != nil {
		v.Secret = *raw.Secret
	}
	if raw.Masked != nil {
		v.Masked = *raw.Masked
	}
	if raw.Protected != nil {
		v.Protected = *raw.Protected
	}

	if raw.Generator == nil {
		return nil
	}

	gen, err := generatorFrom(raw, v.Name)
	if err != nil {
		return err
	}
	v.Generator = gen

	return nil
}

// knownVariableFields is every key a variable definition may carry, per
// config.schema.json. Notably absent, and deliberately so: platform and
// instance (FR-032).
var knownVariableFields = map[string]bool{
	"name": true, "value": true, "value_ref": true, "generator": true,
	"secret": true, "masked": true, "protected": true,
	"token_name": true, "scopes": true, "role": true,
	"expires_in": true, "rotate_before": true, "revoke_rotated": true,
}

// rejectUnknownVariableFields refuses any key the schema does not declare,
// naming both the key and the variable it was found on.
func rejectUnknownVariableFields(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%w: a variable must be a mapping, at line %d", ErrInvalid, node.Line)
	}

	name := "(unnamed)"
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "name" {
			name = node.Content[i+1].Value
		}
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if knownVariableFields[key] {
			continue
		}

		return fmt.Errorf(
			"%w: variable %q declares unknown field %q; a variable carries no platform "+
				"or instance field, because one run targets exactly one instance",
			ErrInvalid, name, key)
	}

	return nil
}

// generatorFrom lifts the generator fields that sit beside the variable's own
// into a Generator, filling in every default of FR-012.
func generatorFrom(raw variableYAML, name string) (*Generator, error) {
	gen := &Generator{
		Kind:          *raw.Generator,
		TokenName:     DefaultTokenName,
		Scopes:        DefaultScopes(),
		Role:          DefaultRole,
		ExpiresIn:     DefaultExpiresIn,
		RotateBefore:  DefaultRotateBefore,
		RevokeRotated: true,
	}

	if raw.TokenName != nil {
		gen.TokenName = *raw.TokenName
	}
	if raw.Scopes != nil {
		gen.Scopes = *raw.Scopes
	}
	if raw.RevokeRotated != nil {
		gen.RevokeRotated = *raw.RevokeRotated
	}

	if raw.Role != nil {
		role, err := ParseAccessLevel(*raw.Role)
		if err != nil {
			return nil, fmt.Errorf("variable %q: %w", name, err)
		}
		gen.Role = role
	}

	if raw.ExpiresIn != nil {
		days, err := ParseDays(*raw.ExpiresIn)
		if err != nil {
			return nil, fmt.Errorf("variable %q: expires_in: %w", name, err)
		}
		gen.ExpiresIn = days
	}

	if raw.RotateBefore != nil {
		days, err := ParseDays(*raw.RotateBefore)
		if err != nil {
			return nil, fmt.Errorf("variable %q: rotate_before: %w", name, err)
		}
		gen.RotateBefore = days
	}

	return gen, nil
}

// Equal reports whether two definitions of the same variable name agree in
// every field. Two profiles may declare one name only if they agree exactly;
// any difference is a configuration error (FR-018).
func (v VariableDefinition) Equal(other VariableDefinition) bool {
	if v.Name != other.Name ||
		v.Value != other.Value ||
		v.ValueRef != other.ValueRef ||
		v.Secret != other.Secret ||
		v.Masked != other.Masked ||
		v.Protected != other.Protected {
		return false
	}

	return generatorsEqual(v.Generator, other.Generator)
}

// generatorsEqual compares two optional generators field by field.
func generatorsEqual(a, b *Generator) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	}

	if a.Kind != b.Kind ||
		a.TokenName != b.TokenName ||
		a.Role != b.Role ||
		a.ExpiresIn != b.ExpiresIn ||
		a.RotateBefore != b.RotateBefore ||
		a.RevokeRotated != b.RevokeRotated ||
		len(a.Scopes) != len(b.Scopes) {
		return false
	}

	for i := range a.Scopes {
		if a.Scopes[i] != b.Scopes[i] {
			return false
		}
	}

	return true
}
