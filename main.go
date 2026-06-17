// Command git-who-reviewed suggests reviewers for changes to a file.
//
// It finds the pull requests that touched the given file, gathers who reviewed
// those PRs, and ranks reviewers weighted by how often and how recently they
// reviewed. Install the binary on your PATH to invoke it as `git who-reviewed`.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"golang.org/x/sync/errgroup"

	"git-who-reviewed/internal/github"
	"git-who-reviewed/internal/gitlog"
	"git-who-reviewed/internal/weighting"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, gitlog.ErrNoRemote) {
			fmt.Fprintln(os.Stderr, "No remote origin configured so unable to find previous reviewers")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "git-who-reviewed:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("git-who-reviewed", flag.ContinueOnError)
	var (
		limit    = fs.Int("limit", 10, "maximum number of reviewers to suggest")
		halfLife = fs.Duration("half-life", weighting.DefaultHalfLife, "recency half-life for weighting (e.g. 720h)")
		token    = fs.String("token", "", "GitHub token (defaults to $GITHUB_TOKEN, then `gh auth token`)")
		host     = fs.String("host", "", "GitHub host (defaults to the host of the origin remote)")
		remote   = fs.String("remote", "origin", "git remote to resolve owner/repo from")
		maxPRs   = fs.Int("max-prs", 50, "maximum number of PRs to inspect (0 = no limit)")
		workers  = fs.Int("concurrency", 8, "number of concurrent GitHub API requests")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: git who-reviewed [flags] [<file>...]\n\n"+
			"With no files, uses the files changed on the current branch relative to\n"+
			"the default branch (errors if run on the default branch).\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()

	repo, err := gitlog.RemoteRepo(*remote)
	if err != nil {
		return err
	}

	files := fs.Args()
	if len(files) == 0 {
		files, err = changedFilesOnBranch(*remote)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return fmt.Errorf("no files changed on the current branch")
		}
	}

	resolvedHost := *host
	if resolvedHost == "" {
		resolvedHost = repo.Host
	}

	tok := resolveToken(*token, resolvedHost)
	if tok == "" {
		return fmt.Errorf("no GitHub token: set --token, $GITHUB_TOKEN, or run `gh auth login`")
	}

	client, err := github.New(ctx, github.Options{
		Owner: repo.Owner,
		Repo:  repo.Name,
		Token: tok,
		Host:  resolvedHost,
	})
	if err != nil {
		return err
	}

	// Gather commits across all files, de-duplicating shared commits so a PR
	// that touched several of the given files isn't double-counted.
	commits, err := commitsForFiles(files)
	if err != nil {
		return err
	}

	reviews, prCount, err := collectReviews(ctx, client, commits, *maxPRs, *workers)
	if err != nil {
		return err
	}
	if len(reviews) == 0 {
		fmt.Printf("No reviews found across %d PR(s) touching %s.\n", prCount, describeFiles(files))
		return nil
	}

	w := weighting.New(*halfLife)
	suggestions := w.Rank(reviews)

	printSuggestions(os.Stdout, describeFiles(files), prCount, suggestions, *limit)
	return nil
}

// changedFilesOnBranch resolves the files changed on the current branch
// relative to the default branch. It errors if HEAD is on the default branch
// (there is no feature branch to derive changes from).
func changedFilesOnBranch(remote string) ([]string, error) {
	branch, err := gitlog.CurrentBranch()
	if err != nil {
		return nil, err
	}
	defaultBranch, err := gitlog.DefaultBranch(remote)
	if err != nil {
		return nil, err
	}
	if branch == defaultBranch {
		return nil, fmt.Errorf("on the default branch %q: switch to a feature branch or pass files explicitly", branch)
	}
	return gitlog.ChangedFiles(remote, defaultBranch)
}

// commitsForFiles returns the union of commits touching any of the given files,
// preserving first-seen order and de-duplicating commits shared across files.
func commitsForFiles(files []string) ([]string, error) {
	seen := map[string]bool{}
	var commits []string
	for _, f := range files {
		fileCommits, err := gitlog.CommitsForFile(f)
		if err != nil {
			return nil, err
		}
		for _, sha := range fileCommits {
			if seen[sha] {
				continue
			}
			seen[sha] = true
			commits = append(commits, sha)
		}
	}
	return commits, nil
}

// describeFiles renders the file list for human-readable output.
func describeFiles(files []string) string {
	if len(files) == 1 {
		return fmt.Sprintf("%q", files[0])
	}
	quoted := make([]string, len(files))
	for i, f := range files {
		quoted[i] = fmt.Sprintf("%q", f)
	}
	return strings.Join(quoted, ", ")
}

// collectReviews maps commits -> PRs -> reviews, de-duplicating PRs. The two
// API-bound phases (resolving PRs for each commit, then fetching reviews for
// each PR) are executed concurrently with a bounded worker pool to reduce
// latency, especially when many files/commits are involved.
func collectReviews(ctx context.Context, client *github.Client, commits []string, maxPRs, concurrency int) ([]weighting.Review, int, error) {
	if concurrency < 1 {
		concurrency = 1
	}

	// Phase 1: resolve the unique set of PRs that included the commits.
	type prInfo struct {
		number   int
		mergedAt time.Time
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)
	var (
		mu     sync.Mutex
		seenPR = map[int]bool{}
		prs    []prInfo
	)
	for _, sha := range commits {
		sha := sha
		g.Go(func() error {
			found, err := client.PullsForCommit(gctx, sha)
			if err != nil {
				return err
			}
			mu.Lock()
			for _, pr := range found {
				if !seenPR[pr.Number] {
					seenPR[pr.Number] = true
					prs = append(prs, prInfo{number: pr.Number, mergedAt: pr.MergedAt})
				}
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, 0, err
	}

	// Sort by PR number descending and cap, so the limit deterministically
	// keeps the most recent PRs.
	sort.Slice(prs, func(i, j int) bool { return prs[i].number > prs[j].number })
	if maxPRs > 0 && len(prs) > maxPRs {
		prs = prs[:maxPRs]
	}

	// Phase 2: fetch reviews for each unique PR concurrently.
	g2, g2ctx := errgroup.WithContext(ctx)
	g2.SetLimit(concurrency)
	var (
		mu2     sync.Mutex
		reviews []weighting.Review
	)
	for _, pr := range prs {
		pr := pr
		g2.Go(func() error {
			prReviews, err := client.ReviewsForPR(g2ctx, pr.number)
			if err != nil {
				return err
			}
			mu2.Lock()
			for _, r := range prReviews {
				if r.Reviewer == "" || isExcludedReviewer(r.Reviewer) {
					continue
				}
				when := r.SubmittedAt
				if when.IsZero() {
					when = pr.mergedAt
				}
				reviews = append(reviews, weighting.Review{
					Reviewer:    r.Reviewer,
					SubmittedAt: when,
				})
			}
			mu2.Unlock()
			return nil
		})
	}
	if err := g2.Wait(); err != nil {
		return nil, 0, err
	}

	return reviews, len(prs), nil
}

// isExcludedReviewer reports whether a reviewer login should be omitted from
// suggestions. The GitHub Copilot review bot is never a useful human reviewer
// suggestion, so it is filtered out regardless of how it is spelled.
func isExcludedReviewer(login string) bool {
	l := strings.ToLower(login)
	switch l {
	case "copilot", "copilot-pull-request-reviewer[bot]", "copilot-pull-request-reviewer", "github-copilot[bot]":
		return true
	}
	return strings.HasPrefix(l, "copilot") && strings.HasSuffix(l, "[bot]")
}

func printSuggestions(out *os.File, target string, prCount int, suggestions []weighting.Suggestion, limit int) {
	fmt.Fprintf(out, "Suggested reviewers for %s (from %d PR(s)):\n\n", target, prCount)
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "REVIEWER\tSCORE\tREVIEWS")
	for i, s := range suggestions {
		if limit > 0 && i >= limit {
			break
		}
		fmt.Fprintf(tw, "%s\t%.2f\t%d\n", s.Reviewer, s.Score, s.Count)
	}
	tw.Flush()
}

// resolveToken picks a token from the flag, environment, or `gh auth token`.
func resolveToken(flagToken, host string) string {
	if flagToken != "" {
		return flagToken
	}
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t
	}
	if t := strings.TrimSpace(os.Getenv("GH_TOKEN")); t != "" {
		return t
	}
	args := []string{"auth", "token"}
	if host != "" && host != "github.com" {
		args = append(args, "--hostname", host)
	}
	cmd := exec.Command("gh", args...)
	cmd.Stderr = nil
	// Best-effort; ignore errors and fall through to empty.
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.Output()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		return ""
	}
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
