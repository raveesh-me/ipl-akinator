// Package engine implements the probabilistic Akinator core: a belief state
// over candidate IPL players, Bayesian updates per answer, and information-gain
// driven question selection. There is intentionally no decision tree.
package engine

import (
	"math"
	"sort"

	"github.com/raveesh/ai-akinator/internal/data"
)

// Answer mirrors the proto enum.
type Answer int

const (
	AnswerUnknown Answer = iota
	AnswerYes
	AnswerNo
	AnswerMaybe
	AnswerDontKnow
)

// likelihoodSmoothing keeps strict 0/1 predicates from collapsing the posterior
// when the user mis-answers a single question. Tune to taste.
const likelihoodSmoothing = 0.05

// MaybeWeight is how strongly a "maybe" answer pushes toward Yes.
const maybeYesWeight = 0.65

// MaxQuestions is the hard ceiling per the brief.
const MaxQuestions = 8

// ConfidenceThreshold is when we stop and commit to a final guess.
const ConfidenceThreshold = 0.80

// Likelihood returns P(answer | player gives Yes-prob = q).
//
// q is the predicate output for the player. We smooth so the user can be wrong
// on any individual question without zeroing out a strong candidate.
func Likelihood(q float64, ans Answer) float64 {
	q = clamp(q, likelihoodSmoothing, 1-likelihoodSmoothing)
	switch ans {
	case AnswerYes:
		return q
	case AnswerNo:
		return 1 - q
	case AnswerMaybe:
		return maybeYesWeight*q + (1-maybeYesWeight)*(1-q)
	case AnswerDontKnow:
		return 1.0 // no evidence; flat
	default:
		return 1.0
	}
}

// Update applies a Bayesian update of the belief vector given an answer to q.
func Update(beliefs map[string]float64, players map[string]data.Player, q Question, ans Answer) {
	var z float64
	for id, prior := range beliefs {
		p := players[id]
		l := Likelihood(q.Predicate(p), ans)
		beliefs[id] = prior * l
		z += beliefs[id]
	}
	if z <= 0 {
		// All zero — restore uniform to avoid total collapse on contradictory answers.
		n := float64(len(beliefs))
		for id := range beliefs {
			beliefs[id] = 1.0 / n
		}
		return
	}
	for id := range beliefs {
		beliefs[id] /= z
	}
}

// Entropy of a discrete distribution (Shannon, base 2).
func Entropy(beliefs map[string]float64) float64 {
	var h float64
	for _, p := range beliefs {
		if p <= 0 {
			continue
		}
		h -= p * math.Log2(p)
	}
	return h
}

// ExpectedInfoGain returns the expected entropy reduction if we ask q next,
// marginalizing over the {Yes, No} hypothetical user answers under the current
// belief state.
func ExpectedInfoGain(beliefs map[string]float64, players map[string]data.Player, q Question) float64 {
	h0 := Entropy(beliefs)

	// P(Yes) = sum_p beliefs[p] * predicate(p) (smoothed).
	var pYes float64
	for id, b := range beliefs {
		pYes += b * clamp(q.Predicate(players[id]), likelihoodSmoothing, 1-likelihoodSmoothing)
	}
	pNo := 1 - pYes

	// Posterior under Yes.
	postYes := make(map[string]float64, len(beliefs))
	postNo := make(map[string]float64, len(beliefs))
	var zY, zN float64
	for id, prior := range beliefs {
		predicate := q.Predicate(players[id])
		ly := Likelihood(predicate, AnswerYes)
		ln := Likelihood(predicate, AnswerNo)
		postYes[id] = prior * ly
		postNo[id] = prior * ln
		zY += postYes[id]
		zN += postNo[id]
	}
	if zY > 0 {
		for id := range postYes {
			postYes[id] /= zY
		}
	}
	if zN > 0 {
		for id := range postNo {
			postNo[id] /= zN
		}
	}
	hY := Entropy(postYes)
	hN := Entropy(postNo)
	return h0 - (pYes*hY + pNo*hN)
}

// SelectNextQuestion picks the unasked question with highest expected info gain.
// Ties broken by lower category-frequency to encourage variety.
func SelectNextQuestion(
	beliefs map[string]float64,
	players map[string]data.Player,
	library []Question,
	asked map[string]bool,
	categoryUsed map[string]int,
) (Question, float64, bool) {
	var best Question
	var bestGain = math.Inf(-1)
	var bestCatCount = math.MaxInt32
	found := false
	for _, q := range library {
		if asked[q.ID] {
			continue
		}
		gain := ExpectedInfoGain(beliefs, players, q)
		catCount := categoryUsed[q.Category]
		// Pick highest gain; on near-ties, prefer underused categories.
		if gain > bestGain+1e-9 || (math.Abs(gain-bestGain) < 1e-9 && catCount < bestCatCount) {
			best = q
			bestGain = gain
			bestCatCount = catCount
			found = true
		}
	}
	return best, bestGain, found
}

// TopK returns the K highest-probability candidates.
func TopK(beliefs map[string]float64, players map[string]data.Player, k int) []Candidate {
	cs := make([]Candidate, 0, len(beliefs))
	for id, p := range beliefs {
		cs = append(cs, Candidate{
			ID:          id,
			Name:        players[id].Name,
			Probability: p,
		})
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].Probability > cs[j].Probability })
	if k > 0 && k < len(cs) {
		cs = cs[:k]
	}
	return cs
}

// Candidate is a ranked player from the belief state.
type Candidate struct {
	ID          string
	Name        string
	Probability float64
}

// InitialBeliefs builds a prior over players using popularity as a soft weight.
// Popularity matters: more popular players are *a priori* more likely to be the
// one the user is thinking of, which lets early questions discriminate among
// realistic candidates rather than long-tail ones.
func InitialBeliefs(players []data.Player) map[string]float64 {
	beliefs := make(map[string]float64, len(players))
	var sum float64
	for _, p := range players {
		w := p.Popularity
		if w <= 0 {
			w = 0.5
		}
		beliefs[p.ID] = w
		sum += w
	}
	for id := range beliefs {
		beliefs[id] /= sum
	}
	return beliefs
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
