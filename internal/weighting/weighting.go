// Package weighting computes reviewer suggestions from a set of past reviews.
//
// Each reviewer's score is the sum, over all of their reviews, of a
// recency-decayed weight. The weight uses exponential decay with a configurable
// half-life, so recent reviews count more heavily and frequent reviewers
// accumulate a higher total score.
package weighting

import (
	"math"
	"sort"
	"time"
)

// Review is a single review event attributed to a reviewer.
type Review struct {
	// Reviewer is the login/identity of the person who reviewed.
	Reviewer string
	// SubmittedAt is when the review was submitted.
	SubmittedAt time.Time
}

// Suggestion is a scored reviewer suggestion.
type Suggestion struct {
	// Reviewer is the login/identity of the suggested reviewer.
	Reviewer string
	// Score is the recency-weighted total across the reviewer's reviews.
	Score float64
	// Count is the number of reviews attributed to the reviewer.
	Count int
}

// DefaultHalfLife is the default decay half-life used when none is configured.
const DefaultHalfLife = 90 * 24 * time.Hour

// Weigher computes weighted reviewer suggestions.
type Weigher struct {
	// HalfLife is the age at which a review's weight is halved. Must be > 0.
	HalfLife time.Duration
	// Now returns the reference time used to compute review ages. If nil,
	// time.Now is used.
	Now func() time.Time
}

// New returns a Weigher with the given half-life. A non-positive half-life
// falls back to DefaultHalfLife.
func New(halfLife time.Duration) *Weigher {
	if halfLife <= 0 {
		halfLife = DefaultHalfLife
	}
	return &Weigher{HalfLife: halfLife}
}

// Rank scores the given reviews and returns suggestions sorted by descending
// score. Ties are broken by review count (descending) and then by reviewer name
// (ascending) for stable, deterministic output.
//
// Reviews submitted in the future relative to Now are clamped to age 0 (full
// weight) rather than being amplified.
func (w *Weigher) Rank(reviews []Review) []Suggestion {
	halfLife := w.HalfLife
	if halfLife <= 0 {
		halfLife = DefaultHalfLife
	}
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	ref := now()

	// lambda such that weight = exp(-lambda * age) halves every half-life.
	lambda := math.Ln2 / float64(halfLife)

	type agg struct {
		score float64
		count int
	}
	byReviewer := make(map[string]*agg)
	for _, r := range reviews {
		if r.Reviewer == "" {
			continue
		}
		age := ref.Sub(r.SubmittedAt)
		if age < 0 {
			age = 0
		}
		weight := math.Exp(-lambda * float64(age))

		a := byReviewer[r.Reviewer]
		if a == nil {
			a = &agg{}
			byReviewer[r.Reviewer] = a
		}
		a.score += weight
		a.count++
	}

	suggestions := make([]Suggestion, 0, len(byReviewer))
	for reviewer, a := range byReviewer {
		suggestions = append(suggestions, Suggestion{
			Reviewer: reviewer,
			Score:    a.score,
			Count:    a.count,
		})
	}

	sort.Slice(suggestions, func(i, j int) bool {
		if suggestions[i].Score != suggestions[j].Score {
			return suggestions[i].Score > suggestions[j].Score
		}
		if suggestions[i].Count != suggestions[j].Count {
			return suggestions[i].Count > suggestions[j].Count
		}
		return suggestions[i].Reviewer < suggestions[j].Reviewer
	})

	return suggestions
}
