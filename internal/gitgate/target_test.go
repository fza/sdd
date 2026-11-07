package gitgate_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/gitgate"
)

func TestTargetFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want gitgate.Target
	}{
		{"git@gitlab.com:group/project.git", gitgate.Target{Host: "gitlab.com", Repo: "gitlab.com/group/project", Protocol: gitgate.SSH}},
		{"git@GitLab.com:group/sub/project", gitgate.Target{Host: "gitlab.com", Repo: "gitlab.com/group/sub/project", Protocol: gitgate.SSH}},
		{"gitlab.com:/group/project.git/", gitgate.Target{Host: "gitlab.com", Repo: "gitlab.com/group/project", Protocol: gitgate.SSH}},
		{"ssh://git@gitlab.com/group/project.git", gitgate.Target{Host: "gitlab.com", Repo: "gitlab.com/group/project", Protocol: gitgate.SSH}},
		{"ssh://git@gitlab.example.com:2222/group/project", gitgate.Target{Host: "gitlab.example.com", Repo: "gitlab.example.com/group/project", Protocol: gitgate.SSH}},
		{"ssh://gitlab.com/group/project.git", gitgate.Target{Host: "gitlab.com", Repo: "gitlab.com/group/project", Protocol: gitgate.SSH}},
		{"https://github.com/Org/Repo.git", gitgate.Target{Host: "github.com", Repo: "github.com/Org/Repo", Protocol: gitgate.HTTPS}},
		{"https://user:token@GITHUB.com:443/org/repo", gitgate.Target{Host: "github.com", Repo: "github.com/org/repo", Protocol: gitgate.HTTPS}},
		{"http://git.example.org/org/repo.git", gitgate.Target{Host: "git.example.org", Repo: "git.example.org/org/repo", Protocol: gitgate.HTTPS}},
		{"file:///srv/git/project.git", gitgate.Target{Repo: "/srv/git/project.git", Protocol: gitgate.Local}},
		{"/srv/git/project.git", gitgate.Target{Repo: "/srv/git/project.git", Protocol: gitgate.Local}},
		{"../project", gitgate.Target{Repo: "../project", Protocol: gitgate.Local}},
		{"project", gitgate.Target{Repo: "project", Protocol: gitgate.Local}},
		{"./dir:with/colon", gitgate.Target{Repo: "./dir:with/colon", Protocol: gitgate.Local}},
		{`C:\repos\project`, gitgate.Target{Repo: `C:\repos\project`, Protocol: gitgate.Local}},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got, err := gitgate.TargetFromURL(tt.url)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("URL should parse into its target, got %+v", got)
			}
		})
	}

	for _, invalid := range []string{"", "git://gitlab.com/group/project.git", "https://gitlab.com/", "ssh:///group/project", "git@gitlab.com:"} {
		t.Run("invalid "+invalid, func(t *testing.T) {
			_, err := gitgate.TargetFromURL(invalid)
			if err == nil {
				t.Error("URL should be refused")
			}
		})
	}
}
