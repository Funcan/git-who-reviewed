// Package github wraps the GitHub API (via google/go-github) with the narrow
// set of operations git-who-reviewed needs: finding the pull requests that
// included a given commit, and listing the reviews on a pull request.
//
// The client supports both github.com and GitHub Enterprise via a configurable
// host.
package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	gh "github.com/google/go-github/v68/github"
)

// PullRequest is a minimal view of a GitHub pull request.
type PullRequest struct {
	Number   int
	Title    string
	MergedAt time.Time
}

// Review is a minimal view of a pull request review.
type Review struct {
	// Reviewer is the GitHub login of the reviewer.
	Reviewer string
	// State is the review state, e.g. APPROVED, CHANGES_REQUESTED, COMMENTED.
	State string
	// SubmittedAt is when the review was submitted.
	SubmittedAt time.Time
}

// Client is a thin wrapper around the go-github client scoped to a single
// owner/repo.
type Client struct {
	api   *gh.Client
	owner string
	repo  string
}

// Options configures a Client.
type Options struct {
	// Owner is the repository owner (user or organization). Required.
	Owner string
	// Repo is the repository name. Required.
	Repo string
	// Token is a GitHub personal access token used for authentication.
	Token string
	// Host is the GitHub host, e.g. "github.com" or "github.example.com".
	// Empty defaults to github.com. Used to derive Enterprise API URLs.
	Host string
}

// New creates a Client. For non-github.com hosts the appropriate Enterprise
// API and upload URLs are derived from Host.
func New(ctx context.Context, opts Options) (*Client, error) {
	if opts.Owner == "" || opts.Repo == "" {
		return nil, fmt.Errorf("github: owner and repo are required")
	}

	api := gh.NewClient(nil)
	if opts.Token != "" {
		api = api.WithAuthToken(opts.Token)
	}

	if host := normalizeHost(opts.Host); host != "" && host != "github.com" && host != "www.github.com" {
		base := fmt.Sprintf("https://%s/api/v3/", host)
		upload := fmt.Sprintf("https://%s/api/uploads/", host)
		var err error
		api, err = api.WithEnterpriseURLs(base, upload)
		if err != nil {
			return nil, fmt.Errorf("github: configuring enterprise host %q: %w", host, err)
		}
		if opts.Token != "" {
			api = api.WithAuthToken(opts.Token)
		}
	}

	return &Client{api: api, owner: opts.Owner, repo: opts.Repo}, nil
}

// normalizeHost strips scheme/path noise from a host value.
func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.Contains(host, "://") {
		if u, err := url.Parse(host); err == nil && u.Host != "" {
			return u.Host
		}
	}
	host = strings.TrimSuffix(host, "/")
	return host
}

// PullsForCommit returns the pull requests associated with the given commit SHA.
func (c *Client) PullsForCommit(ctx context.Context, sha string) ([]PullRequest, error) {
	var out []PullRequest
	opt := &gh.ListOptions{PerPage: 100}
	for {
		prs, resp, err := c.api.PullRequests.ListPullRequestsWithCommit(ctx, c.owner, c.repo, sha, opt)
		if err != nil {
			return nil, fmt.Errorf("github: listing pulls for commit %s: %w", sha, err)
		}
		for _, pr := range prs {
			out = append(out, PullRequest{
				Number:   pr.GetNumber(),
				Title:    pr.GetTitle(),
				MergedAt: pr.GetMergedAt().Time,
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return out, nil
}

// ReviewsForPR returns the reviews submitted on the given pull request number.
func (c *Client) ReviewsForPR(ctx context.Context, number int) ([]Review, error) {
	var out []Review
	opt := &gh.ListOptions{PerPage: 100}
	for {
		reviews, resp, err := c.api.PullRequests.ListReviews(ctx, c.owner, c.repo, number, opt)
		if err != nil {
			return nil, fmt.Errorf("github: listing reviews for PR #%d: %w", number, err)
		}
		for _, r := range reviews {
			out = append(out, Review{
				Reviewer:    r.GetUser().GetLogin(),
				State:       r.GetState(),
				SubmittedAt: r.GetSubmittedAt().Time,
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return out, nil
}
