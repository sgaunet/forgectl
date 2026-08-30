package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
	"github.com/sgaunet/forgectl/internal/forge/github"
	"github.com/sgaunet/forgectl/internal/forge/gitlab"
	"github.com/sgaunet/forgectl/internal/gitrepo"
)

// session is everything a command needs after the shared preamble: the resolved
// configuration, the working copy, the instance, and the selected profiles.
//
// Every command begins the same way — find the repository, load and validate
// the configuration, resolve the instance and its credential, select the
// profiles — so that sequence lives here once rather than in four commands.
type session struct {
	resolved  *config.Resolved
	workCopy  *gitrepo.WorkingCopy
	ref       gitrepo.RemoteRef
	target    forge.Target
	selection config.Selection
}

// begin runs the shared preamble. Everything it can fail on is a usage error:
// nothing has been attempted yet, and no network call has been made.
func (o *options) begin(ctx context.Context, cmd *cobra.Command, profiles []string) (*session, error) {
	resolved, err := o.loadConfig(cmd)
	if err != nil {
		return nil, err
	}

	cwd, err := workingDirectory()
	if err != nil {
		return nil, err
	}

	workCopy, err := gitrepo.Discover(ctx, cwd, resolved.Remote)
	if err != nil {
		return nil, err //nolint:wrapcheck // the sentinel must stay reachable for classify
	}

	ref, err := workCopy.Ref()
	if err != nil {
		return nil, err //nolint:wrapcheck // the sentinel must stay reachable for classify
	}

	target, err := forge.Resolve(&resolved.Config, ref.Host, ref.Owner, ref.Repo)
	if err != nil {
		return nil, err //nolint:wrapcheck // the sentinel must stay reachable for classify
	}

	selection, err := config.SelectProfiles(&resolved.Config, profiles, workCopy.Root)
	if err != nil {
		return nil, err //nolint:wrapcheck // the sentinel must stay reachable for classify
	}

	return &session{
		resolved:  resolved,
		workCopy:  workCopy,
		ref:       ref,
		target:    target,
		selection: selection,
	}, nil
}

// openForge builds the client for the detected platform.
//
// This switch is the one place the two implementations are named. It lives in
// the CLI layer because internal/forge holds the interface the platform
// packages import, so a factory there would be an import cycle.
func (s *session) openForge() (forge.Reader, error) {
	switch s.target.Instance.Platform {
	case config.PlatformGitHub:
		client, err := github.New(s.target)
		if err != nil {
			return nil, err //nolint:wrapcheck // already described by the constructor
		}

		return client, nil
	case config.PlatformGitLab:
		client, err := gitlab.New(s.target)
		if err != nil {
			return nil, err //nolint:wrapcheck // already described by the constructor
		}

		return client, nil
	default:
		return nil, fmt.Errorf("%w: instance %q declares platform %q",
			config.ErrInvalid, s.target.Instance.Name, s.target.Instance.Platform)
	}
}

// newReport builds a report already carrying what the run detected, so every
// command's output starts from the same header.
func (s *session) newReport(command string) *compliance.Report {
	return &compliance.Report{
		Command:    command,
		Repository: s.ref.String(),
		Instance:   s.target.Instance,
		Profiles:   s.selection.Names,
	}
}

// evaluator builds the read-only evaluator for this session.
func (s *session) evaluator(f forge.Reader) *compliance.Evaluator {
	return &compliance.Evaluator{
		Forge:      f,
		Config:     &s.resolved.Config,
		Selection:  s.selection,
		Instance:   s.target.Instance,
		Repository: s.ref.String(),
	}
}
