package main_test

import (
	"errors"
	"fmt"
	"net"
	"testing"

	main "github.com/sgaunet/forgectl/cmd/forgectl"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/gitrepo"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "no error is compliance", err: nil, want: main.ExitOK},

		// Exit 2 — the invocation or configuration was wrong and nothing was
		// attempted. Every one of these is a sentinel a domain package exports.
		{name: "an unknown flag", err: fmt.Errorf("%w: unknown flag --bogus", main.ErrUsage), want: main.ExitUsage},
		{name: "invalid configuration", err: config.ErrInvalid, want: main.ExitUsage},
		{name: "wide config permissions", err: config.ErrPermissions, want: main.ExitUsage},
		{name: "values file not ignored", err: config.ErrValuesInRepo, want: main.ExitUsage},
		{name: "unknown profile", err: config.ErrUnknownProfile, want: main.ExitUsage},
		{name: "unknown host", err: config.ErrUnknownHost, want: main.ExitUsage},
		{name: "unset credential", err: config.ErrNoCredential, want: main.ExitUsage},
		{name: "outside a working copy", err: gitrepo.ErrNotARepo, want: main.ExitUsage},
		{name: "no commits", err: gitrepo.ErrNoCommits, want: main.ExitUsage},
		{name: "unknown remote", err: gitrepo.ErrNoRemote, want: main.ExitUsage},
		{name: "git missing from PATH", err: gitrepo.ErrGitMissing, want: main.ExitUsage},
		{name: "unparseable remote URL", err: gitrepo.ErrRemoteURL, want: main.ExitUsage},

		// A sentinel stays classifiable through any amount of wrapping, which is
		// the whole reason Constitution IV requires %w.
		{
			name: "a deeply wrapped usage sentinel",
			err:  fmt.Errorf("loading: %w", fmt.Errorf("reading: %w", config.ErrPermissions)),
			want: main.ExitUsage,
		},

		// Exit 3 — the command succeeded and drift remains.
		{name: "drift", err: main.ErrDrift, want: main.ExitDrift},

		// Exit 1 — work began and something broke.
		{name: "a network failure", err: &net.OpError{Op: "dial"}, want: main.ExitRuntime},
		{name: "an unclassified error", err: errors.New("the platform said no"), want: main.ExitRuntime},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := main.Classify(tt.err); got != tt.want {
				t.Errorf("Classify(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestLowestNonZeroCodeWins(t *testing.T) {
	// CLI-002: where more than one applies, the lowest code that is not 0 wins.
	// A runtime failure during a drifted check exits 1, not 3; a usage error
	// outranks both.
	runtimeAndDrift := errors.Join(errors.New("the platform said no"), main.ErrDrift)
	if got := main.Classify(runtimeAndDrift); got != main.ExitRuntime {
		t.Errorf("a runtime failure alongside drift = %d, want %d", got, main.ExitRuntime)
	}

	usageAndDrift := errors.Join(config.ErrInvalid, main.ErrDrift)
	if got := main.Classify(usageAndDrift); got != main.ExitUsage {
		t.Errorf("a usage error alongside drift = %d, want %d", got, main.ExitUsage)
	}
}

func TestExitCodesAreTheDocumentedFour(t *testing.T) {
	codes := map[string]int{
		"compliant":       main.ExitOK,
		"runtime failure": main.ExitRuntime,
		"usage error":     main.ExitUsage,
		"drift found":     main.ExitDrift,
	}

	want := map[string]int{
		"compliant": 0, "runtime failure": 1, "usage error": 2, "drift found": 3,
	}

	for name, got := range codes {
		if got != want[name] {
			t.Errorf("%s = %d, want %d (CLI-002)", name, got, want[name])
		}
	}
}
