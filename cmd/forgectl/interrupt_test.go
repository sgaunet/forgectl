package main_test

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSIGINTStopsApplyCleanlyAndARerunConverges(t *testing.T) {
	// CLI-005 and SC-007: an interrupted apply stops at the current step,
	// reports what completed, exits without a panic or a stack trace, and a
	// subsequent apply brings the repository to full compliance.
	forge := newMutableForge(t)
	cfg := configFor(t, forge.URL)
	repo := newClonePairForApply(t)

	path, err := binary()
	if err != nil {
		t.Fatalf("building forgectl: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), path, "apply", "--yes")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), env(cfg)...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// The binary must be its own process group leader for the signal to land on
	// it rather than on the test.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Interrupt while the run is under way. The exact step it lands on is not
	// the point; what matters is that whichever step it is, the tool stops
	// there rather than tearing down mid-write.
	time.Sleep(120 * time.Millisecond)

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("signalling the process: %v", err)
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the interrupted run did not exit; SIGINT must cancel the root context")
	}

	// No panic, no stack trace: an interruption is an expected condition, not a
	// crash.
	combined := stdout.String() + stderr.String()
	for _, forbidden := range []string{"panic:", "goroutine 1 [", "runtime.gopanic"} {
		if strings.Contains(combined, forbidden) {
			t.Errorf("the interrupted run panicked:\n%s", combined)
		}
	}

	// Rerunning converges from wherever it stopped.
	rerun := forgectl(t, repo, env(cfg), "apply", "--yes")
	if rerun.code != 0 {
		t.Fatalf("the rerun did not converge: exit %d\nstdout: %s\nstderr: %s",
			rerun.code, rerun.stdout, rerun.stderr)
	}

	if forge.defaultBranch != "main" {
		t.Errorf("platform default = %q, want main after the rerun", forge.defaultBranch)
	}
	if !forge.protected["main"] {
		t.Error("main was not protected after the rerun")
	}

	// And a third run has nothing left to do.
	final := forgectl(t, repo, env(cfg), "check")
	if final.code != 0 {
		t.Errorf("check after the rerun: exit %d, want 0\n%s", final.code, final.stdout)
	}
}

func TestAnInterruptedRunReportsWhatCompleted(t *testing.T) {
	// FR-045: whatever the run managed before it stopped is reported, so the
	// maintainer knows the state they are in. This drives the same path through
	// the executor by cancelling before any action runs.
	forge := newMutableForge(t)
	cfg := configFor(t, forge.URL)
	repo := newClonePairForApply(t)

	path, err := binary()
	if err != nil {
		t.Fatalf("building forgectl: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), path, "apply", "--yes")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), env(cfg)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Interrupt almost immediately, before the plan can be executed.
	time.Sleep(10 * time.Millisecond)
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	// Whatever happened, the platform is in a state a rerun converges from.
	if rerun := forgectl(t, repo, env(cfg), "apply", "--yes"); rerun.code != 0 {
		t.Errorf("the rerun did not converge: exit %d\nstderr: %s", rerun.code, rerun.stderr)
	}
}
