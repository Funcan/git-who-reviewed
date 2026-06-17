package main

import (
	"reflect"
	"testing"

	"git-who-reviewed/internal/weighting"
)

func TestTopReviewers(t *testing.T) {
	suggestions := []weighting.Suggestion{
		{Reviewer: "alice", Score: 3},
		{Reviewer: "bob", Score: 2},
		{Reviewer: "carol", Score: 1},
	}
	if got := topReviewers(suggestions, 2); !reflect.DeepEqual(got, []string{"alice", "bob"}) {
		t.Errorf("limit 2: got %v", got)
	}
	if got := topReviewers(suggestions, 0); !reflect.DeepEqual(got, []string{"alice", "bob", "carol"}) {
		t.Errorf("limit 0 (all): got %v", got)
	}
	if got := topReviewers(suggestions, 10); !reflect.DeepEqual(got, []string{"alice", "bob", "carol"}) {
		t.Errorf("limit beyond len: got %v", got)
	}
}

func TestDescribeFiles(t *testing.T) {
	if got := describeFiles([]string{"a.go"}); got != `"a.go"` {
		t.Errorf("single file: got %s", got)
	}
	if got := describeFiles([]string{"a.go", "b.go"}); got != `"a.go", "b.go"` {
		t.Errorf("multiple files: got %s", got)
	}
}

func TestIsExcludedReviewer(t *testing.T) {
	excluded := []string{
		"Copilot",
		"copilot",
		"copilot-pull-request-reviewer[bot]",
		"Copilot-Pull-Request-Reviewer[bot]",
		"github-copilot[bot]",
		"copilot-anything[bot]",
	}
	for _, login := range excluded {
		if !isExcludedReviewer(login) {
			t.Errorf("expected %q to be excluded", login)
		}
	}

	allowed := []string{
		"alice",
		"bob",
		"copilotter",      // human-ish login, not a bot
		"my-copilot-fan",  // not a bot suffix
		"dependabot[bot]", // a different bot, still a real reviewer event
	}
	for _, login := range allowed {
		if isExcludedReviewer(login) {
			t.Errorf("expected %q to be allowed", login)
		}
	}
}
