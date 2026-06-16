// Command git-who-reviewed suggests reviewers for changes to a file.
//
// It finds the pull requests that touched the given file, gathers who reviewed
// those PRs, and ranks reviewers weighted by how often and how recently they
// reviewed. Install the binary on your PATH to invoke it as `git who-reviewed`.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"git-who-reviewed/internal/github"
	"git-who-reviewed/internal/gitlog"
	"git-who-reviewed/internal/weighting"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
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
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: git who-reviewed [flags] <file>\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one <file> argument")
	}
	file := fs.Arg(0)

	ctx := context.Background()

	repo, err := gitlog.RemoteRepo(*remote)
	if err != nil {
		return err
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

	commits, err := gitlog.CommitsForFile(file)
	if err != nil {
		return err
	}

	reviews, prCount, err := collectReviews(ctx, client, commits, *maxPRs)
	if err != nil {
		return err
	}
	if len(reviews) == 0 {
		fmt.Printf("No reviews found across %d PR(s) touching %q.\n", prCount, file)
		return nil
	}

	w := weighting.New(*halfLife)
	suggestions := w.Rank(reviews)

	printSuggestions(os.Stdout, file, prCount, suggestions, *limit)
	return nil
}

// collectReviews maps commits -> PRs -> reviews, de-duplicating PRs.
func collectReviews(ctx context.Context, client *github.Client, commits []string, maxPRs int) ([]weighting.Review, int, error) {
	seenPR := map[int]bool{}
	var reviews []weighting.Review

	for _, sha := range commits {
		if maxPRs > 0 && len(seenPR) >= maxPRs {
			break
		}
		prs, err := client.PullsForCommit(ctx, sha)
		if err != nil {
			return nil, 0, err
		}
		for _, pr := range prs {
			if seenPR[pr.Number] {
				continue
			}
			if maxPRs > 0 && len(seenPR) >= maxPRs {
				break
			}
			seenPR[pr.Number] = true

			prReviews, err := client.ReviewsForPR(ctx, pr.Number)
			if err != nil {
				return nil, 0, err
			}
			for _, r := range prReviews {
				if r.Reviewer == "" || isExcludedReviewer(r.Reviewer) {
					continue
				}
				when := r.SubmittedAt
				if when.IsZero() {
					when = pr.MergedAt
				}
				reviews = append(reviews, weighting.Review{
					Reviewer:    r.Reviewer,
					SubmittedAt: when,
				})
			}
		}
	}
	return reviews, len(seenPR), nil
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

func printSuggestions(out *os.File, file string, prCount int, suggestions []weighting.Suggestion, limit int) {
	fmt.Fprintf(out, "Suggested reviewers for %q (from %d PR(s)):\n\n", file, prCount)
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
