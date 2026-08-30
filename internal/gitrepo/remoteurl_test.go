package gitrepo_test

import (
	"errors"
	"testing"

	"github.com/sgaunet/forgectl/internal/gitrepo"
)

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		host  string
		owner string
		repo  string
	}{
		{
			name:  "scp-like ssh",
			url:   "git@gitlab.example.com:acme/my-tool.git",
			host:  "gitlab.example.com",
			owner: "acme",
			repo:  "my-tool",
		},
		{
			name:  "scp-like ssh without .git",
			url:   "git@github.com:sgaunet/forgectl",
			host:  "github.com",
			owner: "sgaunet",
			repo:  "forgectl",
		},
		{
			name:  "ssh scheme with explicit port",
			url:   "ssh://git@gitlab.example.com:2222/acme/my-tool.git",
			host:  "gitlab.example.com",
			owner: "acme",
			repo:  "my-tool",
		},
		{
			name:  "ssh scheme without port",
			url:   "ssh://git@github.com/sgaunet/forgectl.git",
			host:  "github.com",
			owner: "sgaunet",
			repo:  "forgectl",
		},
		{
			name:  "https",
			url:   "https://gitlab.example.com/acme/my-tool.git",
			host:  "gitlab.example.com",
			owner: "acme",
			repo:  "my-tool",
		},
		{
			name:  "https with explicit port",
			url:   "https://gitlab.example.com:8443/acme/my-tool.git",
			host:  "gitlab.example.com",
			owner: "acme",
			repo:  "my-tool",
		},
		{
			name:  "https with credentials in the URL",
			url:   "https://user@github.com/sgaunet/forgectl.git",
			host:  "github.com",
			owner: "sgaunet",
			repo:  "forgectl",
		},
		{
			name:  "http",
			url:   "http://gitlab.example.com/acme/my-tool",
			host:  "gitlab.example.com",
			owner: "acme",
			repo:  "my-tool",
		},
		{
			name:  "nested subgroup keeps the full owner path",
			url:   "git@gitlab.example.com:acme/team/my-tool.git",
			host:  "gitlab.example.com",
			owner: "acme/team",
			repo:  "my-tool",
		},
		{
			name:  "trailing slash is tolerated",
			url:   "https://github.com/sgaunet/forgectl/",
			host:  "github.com",
			owner: "sgaunet",
			repo:  "forgectl",
		},
		{
			name:  "leading slash after scp colon",
			url:   "git@github.com:/sgaunet/forgectl.git",
			host:  "github.com",
			owner: "sgaunet",
			repo:  "forgectl",
		},
		{
			name:  "host case is normalised",
			url:   "git@GitHub.com:sgaunet/forgectl.git",
			host:  "github.com",
			owner: "sgaunet",
			repo:  "forgectl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := gitrepo.ParseRemoteURL(tt.url)
			if err != nil {
				t.Fatalf("ParseRemoteURL(%q) returned error: %v", tt.url, err)
			}
			if got.Host != tt.host {
				t.Errorf("host = %q, want %q", got.Host, tt.host)
			}
			if got.Owner != tt.owner {
				t.Errorf("owner = %q, want %q", got.Owner, tt.owner)
			}
			if got.Repo != tt.repo {
				t.Errorf("repo = %q, want %q", got.Repo, tt.repo)
			}
		})
	}
}

func TestParseRemoteURLRejects(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "empty", url: ""},
		{name: "no owner", url: "git@github.com:forgectl.git"},
		{name: "no path", url: "https://github.com"},
		{name: "local path", url: "/srv/git/forgectl.git"},
		{name: "unsupported scheme", url: "ftp://github.com/sgaunet/forgectl.git"},
		{name: "empty repo", url: "https://github.com/sgaunet/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gitrepo.ParseRemoteURL(tt.url)
			if err == nil {
				t.Fatalf("ParseRemoteURL(%q) succeeded, want an error", tt.url)
			}
			if !errors.Is(err, gitrepo.ErrRemoteURL) {
				t.Errorf("error %v does not wrap ErrRemoteURL", err)
			}
		})
	}
}

func TestRemoteRefString(t *testing.T) {
	ref := gitrepo.RemoteRef{Host: "github.com", Owner: "sgaunet", Repo: "forgectl"}
	if got, want := ref.String(), "sgaunet/forgectl"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
