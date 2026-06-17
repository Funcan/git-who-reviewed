// Package gitlog provides local git operations used by git-who-reviewed:
// resolving the commits that touched a file and determining the GitHub
// owner/repo (and host) from the configured remote.
package gitlog

import (
	"errors"
	"fmt"
	"os"
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

// CurrentBranch returns the name of the currently checked-out branch. It
// returns an error if HEAD is detached (not on a branch).
func CurrentBranch() (string, error) {
	out, err := runGit("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("gitlog: not on a branch (detached HEAD?): %w", err)
	}
	branch := strings.TrimSpace(out)
	if branch == "" {
		return "", fmt.Errorf("gitlog: could not determine current branch")
	}
	return branch, nil
}

// DefaultBranch returns the default branch of the named remote (e.g. "main").
//
// It asks the remote for its authoritative default via `git ls-remote --symref`
// rather than relying solely on the local refs/remotes/<remote>/HEAD pointer,
// which is frequently stale (e.g. still pointing at "master" after a rename to
// "main"). If the remote cannot be queried (offline), it falls back to the
// local symbolic ref and then to probing common default branch names.
func DefaultBranch(remote string) (string, error) {
	if name, err := remoteDefaultBranch(remote); err == nil && name != "" {
		return name, nil
	}

	out, err := runGit("symbolic-ref", "--quiet", "--short", "refs/remotes/"+remote+"/HEAD")
	if err == nil {
		ref := strings.TrimSpace(out)
		// ref looks like "origin/main"; strip the remote prefix.
		if i := strings.IndexByte(ref, '/'); i >= 0 {
			return ref[i+1:], nil
		}
		if ref != "" {
			return ref, nil
		}
	}
	for _, name := range []string{"main", "master"} {
		if _, err := runGit("rev-parse", "--verify", "--quiet", "refs/remotes/"+remote+"/"+name); err == nil {
			return name, nil
		}
		if _, err := runGit("rev-parse", "--verify", "--quiet", "refs/heads/"+name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("gitlog: could not determine default branch for remote %q", remote)
}

// remoteDefaultBranch queries the remote for the branch its HEAD points at,
// e.g. "main". It does not prompt for credentials; if the remote is
// unreachable it returns an error so the caller can fall back.
func remoteDefaultBranch(remote string) (string, error) {
	out, err := runGitEnv([]string{"GIT_TERMINAL_PROMPT=0"}, "ls-remote", "--symref", remote, "HEAD")
	if err != nil {
		return "", err
	}
	// Output contains a line like: "ref: refs/heads/main\tHEAD".
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		const prefix = "ref:"
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, prefix))
		if len(fields) == 0 {
			continue
		}
		ref := fields[0]
		if name := strings.TrimPrefix(ref, "refs/heads/"); name != ref {
			return name, nil
		}
	}
	return "", fmt.Errorf("gitlog: no symref HEAD in ls-remote output for %q", remote)
}

// ChangedFiles returns the files changed on the current branch relative to the
// merge-base with the default branch. It prefers the remote-tracking ref
// (e.g. "origin/main") and falls back to a local branch of the same name, so it
// works even when no local default branch exists. The result is deduplicated.
func ChangedFiles(remote, base string) ([]string, error) {
	baseRef, err := resolveBaseRef(remote, base)
	if err != nil {
		return nil, err
	}
	out, err := runGit("diff", "--name-only", baseRef+"...HEAD")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !seen[line] {
			seen[line] = true
			files = append(files, line)
		}
	}
	return files, nil
}

// resolveBaseRef returns a ref for the default branch that exists in this repo,
// preferring the remote-tracking ref over a local branch of the same name.
func resolveBaseRef(remote, base string) (string, error) {
	candidates := []string{remote + "/" + base, base}
	for _, ref := range candidates {
		if _, err := runGit("rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
			return ref, nil
		}
	}
	return "", fmt.Errorf("gitlog: default branch %q not found (tried %s)", base, strings.Join(candidates, ", "))
}

// ErrNoRemote indicates the requested git remote is not configured.
var ErrNoRemote = errors.New("gitlog: remote not configured")

// RemoteRepo parses the URL of the named remote (e.g. "origin") into a Repo.
// If the remote does not exist, it returns an error wrapping ErrNoRemote.
func RemoteRepo(remote string) (Repo, error) {
	if !remoteExists(remote) {
		return Repo{}, fmt.Errorf("%w: %q", ErrNoRemote, remote)
	}
	out, err := runGit("remote", "get-url", remote)
	if err != nil {
		return Repo{}, err
	}
	return parseRemoteURL(strings.TrimSpace(out))
}

// remoteExists reports whether the named remote is configured.
func remoteExists(remote string) bool {
	out, err := runGit("remote")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == remote {
			return true
		}
	}
	return false
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
	return runGitEnv(nil, args...)
}

func runGitEnv(extraEnv []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("gitlog: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("gitlog: git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
