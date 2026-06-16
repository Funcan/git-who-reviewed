package gitlog

import "testing"

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		host     string
		owner    string
		repoName string
		wantErr  bool
	}{
		{name: "https", raw: "https://github.com/octocat/Hello-World.git", host: "github.com", owner: "octocat", repoName: "Hello-World"},
		{name: "https no suffix", raw: "https://github.com/octocat/Hello-World", host: "github.com", owner: "octocat", repoName: "Hello-World"},
		{name: "scp", raw: "git@github.com:octocat/Hello-World.git", host: "github.com", owner: "octocat", repoName: "Hello-World"},
		{name: "enterprise https", raw: "https://github.example.com/org/team-repo.git", host: "github.example.com", owner: "org", repoName: "team-repo"},
		{name: "enterprise scp", raw: "git@github.example.com:org/team-repo.git", host: "github.example.com", owner: "org", repoName: "team-repo"},
		{name: "ssh url", raw: "ssh://git@github.com/octocat/Hello-World.git", host: "github.com", owner: "octocat", repoName: "Hello-World"},
		{name: "https with port", raw: "https://github.example.com:8443/org/repo.git", host: "github.example.com", owner: "org", repoName: "repo"},
		{name: "empty", raw: "", wantErr: true},
		{name: "garbage", raw: "not a url", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseRemoteURL(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %+v", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Host != tc.host || got.Owner != tc.owner || got.Name != tc.repoName {
				t.Fatalf("got %+v, want host=%s owner=%s name=%s", got, tc.host, tc.owner, tc.repoName)
			}
		})
	}
}
