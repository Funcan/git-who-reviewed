package main

import "testing"

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
