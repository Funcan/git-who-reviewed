package weighting

import (
	"math"
	"testing"
	"time"
)

func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestRankFrequencyAccumulates(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w := &Weigher{HalfLife: 30 * 24 * time.Hour, Now: fixedNow(now)}

	// alice reviewed twice, bob once, all at the same instant.
	reviews := []Review{
		{Reviewer: "alice", SubmittedAt: now},
		{Reviewer: "alice", SubmittedAt: now},
		{Reviewer: "bob", SubmittedAt: now},
	}

	got := w.Rank(reviews)
	if len(got) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(got))
	}
	if got[0].Reviewer != "alice" {
		t.Fatalf("expected alice first, got %q", got[0].Reviewer)
	}
	if got[0].Count != 2 {
		t.Fatalf("expected alice count 2, got %d", got[0].Count)
	}
	// Two zero-age reviews => score 2.0; one => 1.0.
	if math.Abs(got[0].Score-2.0) > 1e-9 {
		t.Fatalf("expected alice score 2.0, got %v", got[0].Score)
	}
	if math.Abs(got[1].Score-1.0) > 1e-9 {
		t.Fatalf("expected bob score 1.0, got %v", got[1].Score)
	}
}

func TestRankRecencyOrdering(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w := &Weigher{HalfLife: 30 * 24 * time.Hour, Now: fixedNow(now)}

	reviews := []Review{
		{Reviewer: "recent", SubmittedAt: now.Add(-1 * 24 * time.Hour)},
		{Reviewer: "stale", SubmittedAt: now.Add(-365 * 24 * time.Hour)},
	}

	got := w.Rank(reviews)
	if got[0].Reviewer != "recent" {
		t.Fatalf("expected recent reviewer first, got %q", got[0].Reviewer)
	}
	if got[0].Score <= got[1].Score {
		t.Fatalf("expected recent score > stale score, got %v <= %v", got[0].Score, got[1].Score)
	}
}

func TestRankHalfLifeDecay(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	halfLife := 30 * 24 * time.Hour
	w := &Weigher{HalfLife: halfLife, Now: fixedNow(now)}

	// A single review exactly one half-life old should score ~0.5.
	reviews := []Review{
		{Reviewer: "alice", SubmittedAt: now.Add(-halfLife)},
	}
	got := w.Rank(reviews)
	if math.Abs(got[0].Score-0.5) > 1e-9 {
		t.Fatalf("expected score 0.5 at one half-life, got %v", got[0].Score)
	}

	// Two half-lives old => ~0.25.
	reviews = []Review{
		{Reviewer: "alice", SubmittedAt: now.Add(-2 * halfLife)},
	}
	got = w.Rank(reviews)
	if math.Abs(got[0].Score-0.25) > 1e-9 {
		t.Fatalf("expected score 0.25 at two half-lives, got %v", got[0].Score)
	}
}

func TestRankFutureReviewClamped(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w := &Weigher{HalfLife: 30 * 24 * time.Hour, Now: fixedNow(now)}

	reviews := []Review{
		{Reviewer: "alice", SubmittedAt: now.Add(48 * time.Hour)},
	}
	got := w.Rank(reviews)
	if math.Abs(got[0].Score-1.0) > 1e-9 {
		t.Fatalf("expected future review clamped to score 1.0, got %v", got[0].Score)
	}
}

func TestRankSkipsEmptyReviewer(t *testing.T) {
	w := New(0) // default half-life
	got := w.Rank([]Review{{Reviewer: "", SubmittedAt: time.Now()}})
	if len(got) != 0 {
		t.Fatalf("expected empty reviewer skipped, got %d suggestions", len(got))
	}
}

func TestNewFallsBackToDefaultHalfLife(t *testing.T) {
	w := New(-5)
	if w.HalfLife != DefaultHalfLife {
		t.Fatalf("expected default half-life, got %v", w.HalfLife)
	}
}
