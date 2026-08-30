package compliance_test

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/forge"
)

// forbidden are the packages the evaluation layer must never be able to reach.
//
// internal/apply is the only package that executes a plan. If internal/compliance
// could reach it, FR-031's "check MUST NOT modify any local or platform state"
// would rest on nobody calling the wrong function. Keeping the edge out of the
// import graph makes it a property a reviewer can verify at a glance, and this
// test is what keeps it that way.
var forbidden = []string{
	"github.com/sgaunet/forgectl/internal/apply",
	"github.com/sgaunet/forgectl/internal/values",
}

// deps returns the full transitive import list of a package.
func deps(t *testing.T, pkg string) []string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}

	return strings.Fields(string(out))
}

func TestComplianceCannotReachTheExecutionLayer(t *testing.T) {
	imported := deps(t, "github.com/sgaunet/forgectl/internal/compliance")

	for _, dep := range imported {
		for _, banned := range forbidden {
			if dep == banned {
				t.Errorf("internal/compliance imports %s; check would no longer be "+
					"read-only by construction (FR-031)", banned)
			}
		}
	}
}

func TestTheEvaluatorIsGivenNoWriteMethod(t *testing.T) {
	// The other half of the guarantee: the evaluator holds a forge.Reader, and
	// a Reader carries no method that writes. A future edit cannot make check
	// mutate anything without first widening that interface, which is a visible
	// change this test refuses.
	reader := reflect.TypeOf((*forge.Reader)(nil)).Elem()

	for _, method := range []string{"SetDefaultBranch", "SetProtection", "ProtectTag", "SetVariable"} {
		if _, found := reader.MethodByName(method); found {
			t.Errorf("forge.Reader gained %s; the evaluation layer can now write", method)
		}
	}

	// And the evaluator's field really is a Reader, not the full Forge.
	field, ok := reflect.TypeOf(compliance.Evaluator{}).FieldByName("Forge")
	if !ok {
		t.Fatal("Evaluator has no Forge field")
	}
	if field.Type != reader {
		t.Errorf("Evaluator.Forge is %s, want forge.Reader", field.Type)
	}
}
