package gitlab_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/forge"
)

func TestReadVariableDisclosesItsValue(t *testing.T) {
	// FR-027: GitLab CI variable values are readable, so a differing value is
	// genuine drift.
	client, _ := newClient(t, routes{
		"GET " + projectPath + "/variables/TOKEN": replies(
			`{"key":"TOKEN","value":"on-the-platform","masked":true,"protected":false}`),
	})

	got, err := client.Variable(context.Background(), "TOKEN", true)
	if err != nil {
		t.Fatalf("Variable: %v", err)
	}

	if !got.Exists {
		t.Error("Exists = false")
	}
	if !got.ValueReadable {
		t.Error("ValueReadable = false; GitLab does disclose the value")
	}
	if got.Value != "on-the-platform" {
		t.Errorf("Value = %q", got.Value)
	}
	if !got.Masked || got.Protected {
		t.Errorf("masked=%v protected=%v, want the platform's own attributes",
			got.Masked, got.Protected)
	}
}

func TestAbsentVariableIsReportedAsMissing(t *testing.T) {
	client, _ := newClient(t, routes{
		"GET " + projectPath + "/variables/TOKEN": status(http.StatusNotFound),
	})

	got, err := client.Variable(context.Background(), "TOKEN", true)
	if err != nil {
		t.Fatalf("Variable returned an error for a 404: %v", err)
	}
	if got.Exists {
		t.Error("Exists = true for a 404")
	}
}

func TestCreateAndUpdateVariable(t *testing.T) {
	t.Run("create when absent", func(t *testing.T) {
		var sent []captured

		client, _ := newClient(t, routes{
			"GET " + projectPath + "/variables/TOKEN": status(http.StatusNotFound),
			"POST " + projectPath + "/variables":      capture(t, &sent, `{"key":"TOKEN"}`),
		})

		err := client.SetVariable(context.Background(), forge.VariableWrite{
			Name: "TOKEN", Value: "a-long-enough-value", Masked: true, Protected: true,
		})
		if err != nil {
			t.Fatalf("SetVariable: %v", err)
		}

		body := sent[0].Body
		if body["key"] != "TOKEN" {
			t.Errorf("key = %v, want TOKEN", body["key"])
		}
		if body["masked"] != true || body["protected"] != true {
			t.Errorf("masked=%v protected=%v, want both true", body["masked"], body["protected"])
		}
	})

	t.Run("update when present", func(t *testing.T) {
		var sent []captured

		client, _ := newClient(t, routes{
			"GET " + projectPath + "/variables/TOKEN": replies(`{"key":"TOKEN","value":"old"}`),
			"PUT " + projectPath + "/variables/TOKEN": capture(t, &sent, `{"key":"TOKEN"}`),
		})

		err := client.SetVariable(context.Background(), forge.VariableWrite{
			Name: "TOKEN", Value: "a-long-enough-value",
		})
		if err != nil {
			t.Fatalf("SetVariable: %v", err)
		}

		if len(sent) != 1 || sent[0].Method != http.MethodPut {
			t.Errorf("requests = %+v, want a single PUT", sent)
		}
	})
}

// rejectMasking answers the first masked write with GitLab's rejection and
// records how many writes it saw.
func rejectMasking(t *testing.T, message string, writes *[]captured) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		*writes = append(*writes, captured{Method: r.Method, Path: r.URL.Path, Body: body})

		w.Header().Set("Content-Type", "application/json")

		if masked, _ := body["masked"].(bool); masked {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"message":%q}`, message)

			return
		}

		fmt.Fprint(w, `{"key":"TOKEN"}`)
	}
}

func TestMaskingRejectionIsRetriedUnmasked(t *testing.T) {
	// R7 corrects the spec: GitLab rejects a masked value that is multiline,
	// that contains a space, OR that is shorter than eight characters. Keying
	// the retry on "multiline" alone would leave the other two failing hard —
	// and an SSH private key trips the first two.
	tests := []struct {
		name       string
		message    string
		wantReason string
	}{
		{
			name:       "multiline",
			message:    "Value must be a single line.",
			wantReason: "single line",
		},
		{
			name:       "contains a space",
			message:    "Masked variable value cannot contain a space",
			wantReason: "space",
		},
		{
			name:       "too short",
			message:    "Value must be at least 8 characters long",
			wantReason: "8 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var writes []captured

			client, _ := newClient(t, routes{
				"GET " + projectPath + "/variables/SSH_PRIVATE_KEY": status(http.StatusNotFound),
				"POST " + projectPath + "/variables":                rejectMasking(t, tt.message, &writes),
			})

			err := client.SetVariable(context.Background(), forge.VariableWrite{
				Name: "SSH_PRIVATE_KEY", Value: "-----BEGIN KEY-----\nline two\n",
				Masked: true, Protected: true,
			})

			// The write succeeded — unmasked — and the caller is told why.
			if !errors.Is(err, forge.ErrMaskRejected) {
				t.Fatalf("error = %v, want ErrMaskRejected reporting the retry", err)
			}
			if !strings.Contains(err.Error(), tt.wantReason) {
				t.Errorf("message %q does not name the constraint %q", err.Error(), tt.wantReason)
			}

			// Exactly two writes: the masked attempt and one unmasked retry.
			if len(writes) != 2 {
				t.Fatalf("writes = %d, want 2 (one attempt, one retry)", len(writes))
			}
			if writes[0].Body["masked"] != true {
				t.Error("the first write was not masked")
			}
			if writes[1].Body["masked"] != false {
				t.Error("the retry was not unmasked")
			}

			// protected is NEVER downgraded: masking hides a value in logs,
			// protection decides which refs see it at all.
			if writes[1].Body["protected"] != true {
				t.Error("the retry downgraded protected along with masked")
			}
		})
	}
}

func TestTheRetryIsBoundedAndOnlyAppliesToMaskedWrites(t *testing.T) {
	// Two properties of the single retry, which differ only in whether the
	// original write asked for masking:
	//
	//   - a platform that rejects the unmasked write too must fail, not loop
	//   - a write that was not masked has nothing to downgrade, so it fails
	//     outright rather than being retried identically
	tests := []struct {
		name       string
		masked     bool
		wantWrites int
	}{
		{name: "a masked write is retried once", masked: true, wantWrites: 2},
		{name: "an unmasked write is not retried", masked: false, wantWrites: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var writes []captured

			client, _ := newClient(t, routes{
				"GET " + projectPath + "/variables/TOKEN": status(http.StatusNotFound),
				"POST " + projectPath + "/variables": func(w http.ResponseWriter, r *http.Request) {
					writes = append(writes, captured{Method: r.Method, Body: readBody(t, r)})
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, `{"message":"Value must be a single line."}`)
				},
			})

			err := client.SetVariable(context.Background(), forge.VariableWrite{
				Name: "TOKEN", Value: "multi\nline", Masked: tt.masked,
			})
			if err == nil {
				t.Fatal("SetVariable succeeded though every write was rejected")
			}
			if len(writes) != tt.wantWrites {
				t.Errorf("writes = %d, want %d", len(writes), tt.wantWrites)
			}
		})
	}
}

func TestAnErrorNeverCarriesTheValue(t *testing.T) {
	const sentinel = "sentinel-value-never-printed"

	client, _ := newClient(t, routes{
		"GET " + projectPath + "/variables/TOKEN": status(http.StatusNotFound),
		"POST " + projectPath + "/variables":      status(http.StatusInternalServerError),
	})

	err := client.SetVariable(context.Background(), forge.VariableWrite{
		Name: "TOKEN", Value: sentinel,
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Errorf("the error carries the value: %q", err.Error())
	}
}
