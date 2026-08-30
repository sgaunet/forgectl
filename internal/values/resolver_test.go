package values_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/values"
)

// stubPrompter answers with a fixed value and records that it was asked.
type stubPrompter struct {
	answer string
	asked  []string
}

func (p *stubPrompter) Prompt(name string) (string, error) {
	p.asked = append(p.asked, name)

	return p.answer, nil
}

// selectionOf builds a selection from the given definitions.
func selectionOf(defs ...config.VariableDefinition) config.Selection {
	return config.Selection{Names: []string{"demo"}, Variables: defs}
}

func TestResolutionOrder(t *testing.T) {
	// FR-044: --var-file beats the configuration, which beats the prompt.
	def := config.VariableDefinition{Name: "TOKEN", ValueRef: "shared", Secret: true}
	store := map[string]string{"shared": "from-store"}

	tests := []struct {
		name     string
		varFile  map[string]string
		store    map[string]string
		prompter values.Prompter
		want     string
	}{
		{
			name:    "the override file wins",
			varFile: map[string]string{"TOKEN": "from-var-file"},
			store:   store,
			want:    "from-var-file",
		},
		{
			name:  "the value store is next",
			store: store,
			want:  "from-store",
		},
		{
			name:     "a store key declared but blank falls through to the prompt",
			store:    map[string]string{"shared": ""},
			prompter: &stubPrompter{answer: "from-prompt"},
			want:     "from-prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := values.NewResolver(selectionOf(def), tt.store, tt.varFile, tt.prompter)

			got, err := r.Resolve(context.Background(), "TOKEN")
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got != tt.want {
				t.Errorf("Resolve = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInlineLiteralIsUsed(t *testing.T) {
	def := config.VariableDefinition{Name: "MODE", Value: "release", Secret: false}

	r := values.NewResolver(selectionOf(def), nil, nil, nil)

	got, err := r.Resolve(context.Background(), "MODE")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "release" {
		t.Errorf("Resolve = %q, want release", got)
	}
}

func TestNoTerminalMeansAClearFailureRatherThanAHang(t *testing.T) {
	// FR-044: with no source and no terminal, the run fails cleanly. A prompt
	// that can never be answered would hang a CI job forever.
	def := config.VariableDefinition{Name: "TOKEN", ValueRef: "absent"}

	r := values.NewResolver(selectionOf(def), map[string]string{}, nil, nil)

	_, err := r.Resolve(context.Background(), "TOKEN")
	if err == nil {
		t.Fatal("Resolve succeeded with no source and no terminal")
	}
	if !errors.Is(err, values.ErrMissingValues) {
		t.Errorf("error %v does not wrap ErrMissingValues", err)
	}
	if !strings.Contains(err.Error(), "no terminal") {
		t.Errorf("message %q does not explain that there is nobody to ask", err.Error())
	}
}

func TestCheckCompleteListsEveryMissingValueAtOnce(t *testing.T) {
	// FR-044: apply fails listing EVERY missing value, before making any
	// change. Discovering them one run at a time would be three edits.
	defs := []config.VariableDefinition{
		{Name: "ALPHA", ValueRef: "absent-a"},
		{Name: "BETA", ValueRef: "present"},
		{Name: "GAMMA", ValueRef: "absent-c"},
	}

	r := values.NewResolver(
		selectionOf(defs...),
		map[string]string{"present": "value"},
		nil, nil,
	)

	err := r.CheckComplete(context.Background(), []string{"ALPHA", "BETA", "GAMMA"})
	if err == nil {
		t.Fatal("CheckComplete succeeded with two values missing")
	}
	if !errors.Is(err, values.ErrMissingValues) {
		t.Fatalf("error %v does not wrap ErrMissingValues", err)
	}

	msg := err.Error()
	for _, want := range []string{"ALPHA", "GAMMA"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not name %s", msg, want)
		}
	}
	if strings.Contains(msg, "BETA") {
		t.Errorf("message %q names BETA, which resolves", msg)
	}
	// And it says how to supply them.
	if !strings.Contains(msg, "--var-file") {
		t.Errorf("message %q does not say how to supply the missing values", msg)
	}
}

func TestCheckCompletePassesWhenEverythingResolves(t *testing.T) {
	defs := []config.VariableDefinition{
		{Name: "ALPHA", ValueRef: "a"},
		{Name: "BETA", Value: "inline"},
	}

	r := values.NewResolver(selectionOf(defs...), map[string]string{"a": "value"}, nil, nil)

	if err := r.CheckComplete(context.Background(), []string{"ALPHA", "BETA"}); err != nil {
		t.Errorf("CheckComplete: %v", err)
	}
}

func TestAGeneratedVariableIsNotResolvedHere(t *testing.T) {
	// A generated value comes from creating a token, which mutates the
	// platform. That belongs to the execution layer, not to a resolver the
	// read-only path could call.
	def := config.VariableDefinition{
		Name:      "GITLAB_TOKEN",
		Generator: &config.Generator{Kind: config.GeneratorKindGitLabPAT},
	}

	r := values.NewResolver(selectionOf(def), nil, nil, nil)

	if _, err := r.Resolve(context.Background(), "GITLAB_TOKEN"); err == nil {
		t.Error("Resolve produced a value for a generated variable")
	}

	// And CheckComplete does not treat it as missing: its value is not the
	// resolver's to supply.
	if err := r.CheckComplete(context.Background(), []string{"GITLAB_TOKEN"}); err != nil {
		t.Errorf("CheckComplete counted a generated variable as missing: %v", err)
	}
}

func TestThePrompterIsAskedByName(t *testing.T) {
	def := config.VariableDefinition{Name: "SSH_PRIVATE_KEY", ValueRef: "blank"}
	prompter := &stubPrompter{answer: "asked-for-it"}

	r := values.NewResolver(selectionOf(def), map[string]string{"blank": ""}, nil, prompter)

	if _, err := r.Resolve(context.Background(), "SSH_PRIVATE_KEY"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(prompter.asked) != 1 || prompter.asked[0] != "SSH_PRIVATE_KEY" {
		t.Errorf("asked = %v, want the variable's name", prompter.asked)
	}
}

func TestAnErrorNeverCarriesAValue(t *testing.T) {
	// FR-054: not in an error string either.
	const sentinel = "s3cr3t-value-never-printed"

	def := config.VariableDefinition{Name: "TOKEN", ValueRef: "absent"}

	r := values.NewResolver(selectionOf(def), map[string]string{"other": sentinel}, nil, nil)

	_, err := r.Resolve(context.Background(), "TOKEN")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Errorf("the error carries a value: %q", err.Error())
	}
}

func TestAnUnknownVariableIsRejected(t *testing.T) {
	r := values.NewResolver(selectionOf(), nil, nil, nil)

	if _, err := r.Resolve(context.Background(), "NOT_SELECTED"); err == nil {
		t.Error("Resolve produced a value for a variable no profile declares")
	}
}
