// Package gitlog provides local git operations used by git-who-reviewed:
// resolving the commits that touched a file and determining the GitHub
// owner/repo (and host) from the configured remote.
package gitlog

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Repo identifies a GitHub repository derived from a git remote.
type Repo struct {
	Host  string
	Owner string
	Name  string
}

// CommitsForFile returns the commit SHAs that modified the given file, most
// recent first. History is followed across renames.
func CommitsForFile(path string) ([]string, error) {
	out, err := runGit("log", "--follow", "--format=%H", "--", path)
	if err != nil {
		return nil, err
	}
	var shas []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			shas = append(shas, line)
		}
	}
	if len(shas) == 0 {
		return nil, fmt.Errorf("gitlog: no commits found for %q", path)
	}
	return shas, nil
}

// RemoteRepo parses the URL of the named remote (e.g. "origin") into a Repo.
func RemoteRepo(remote string) (Repo, error) {
	out, err := runGit("remote", "get-url", remote)
	if err != nil {
		return Repo{}, err
	}
	return parseRemoteURL(strings.TrimSpace(out))
}

var (
	scpLikeRe = regexp.MustCompile(`^(?:[^@]+@)?([^:/]+):(.+?)(?:\.git)?$`)
	urlLikeRe = regexp.MustCompile(`^[a-zA-Z]+://(?:[^@]+@)?([^/:]+)(?::\d+)?/(.+?)(?:\.git)?$`)
)

// parseRemoteURL handles SCP-like (git@host:owner/repo.git) and URL-like
// (https://host/owner/repo.git) remotes.
func parseRemoteURL(raw string) (Repo, error) {
	if raw == "" {
		return Repo{}, fmt.Errorf("gitlog: empty remote URL")
	}

	var host, path string
	if m := urlLikeRe.FindStringSubmatch(raw); m != nil {
		host, path = m[1], m[2]
	} else if m := scpLikeRe.FindStringSubmatch(raw); m != nil {
		host, path = m[1], m[2]
	} else {
		return Repo{}, fmt.Errorf("gitlog: unrecognized remote URL %q", raw)
	}

	path = strings.Trim(path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Repo{}, fmt.Errorf("gitlog: cannot parse owner/repo from %q", raw)
	}

	return Repo{
		Host:  host,
		Owner: parts[0],
		Name:  strings.TrimSuffix(parts[1], ".git"),
	}, nil
}

func runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("gitlog: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("gitlog: git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
