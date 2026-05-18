// Package engine implements the probabilistic Akinator core: a belief state
// over candidate IPL players, Bayesian updates per answer, and information-gain
// driven question selection. There is intentionally no decision tree.
//
// Each Question carries 2..N Options. An Option has a per-player likelihood
// function L_o(p) = P(player p would pick this option). Choosing option o
// multiplies the prior by L_o(p) and renormalises. Expected info gain
// marginalises over all options of an unasked question.
package engine

import (
	"math"
	"sort"

	"github.com/raveesh/ai-akinator/internal/data"
)

// MaxQuestions is a safety ceiling, not a target. The engine aims to stop as
// early as possible via ConfidenceThreshold or MinInfoGainBits — this constant
// only exists to bound pathological cases (e.g. user gives contradictory
// answers and confidence never converges).
const MaxQuestions = 15

// ConfidenceThreshold stops the game and commits to a guess.
const ConfidenceThreshold = 0.80

// MinInfoGainBits is the floor below which an unasked question is considered
// not worth asking — the engine should just guess with what it has rather
// than waste a turn. Applied only after the first question, so the engine
// always gets a chance to probe the belief space.
const MinInfoGainBits = 0.05

// DontKnowID is the conventional option id for a flat (no-update) answer.
// Every Question we emit includes one of these so the user can punt cleanly.
const DontKnowID = "dont_know"

// Option is a single answer the user can choose for a question.
type Option struct {
	ID    string
	Label string
	// Likelihood returns P(player p picks this option | feature vector of p).
	// For "Don't know", return 1.0 to make the Bayesian update a no-op.
	Likelihood func(p data.Player) float64
}

// Question is an atomic, machine-evaluable query over the candidate pool.
// IMPORTANT: this is a *feature library*, not a decision tree. The engine
// chooses among Questions at runtime via expected info gain on the live belief
// state.
type Question struct {
	ID       string
	Text     string
	Category string
	Source   string // "static" or "novel"
	Options  []Option
}

// optionByID finds an option within a question.
func (q Question) optionByID(id string) (Option, bool) {
	for _, o := range q.Options {
		if o.ID == id {
			return o, true
		}
	}
	return Option{}, false
}

// Update applies a Bayesian update of the belief vector given that the user
// selected option `optID` on question q.
func Update(beliefs map[string]float64, players map[string]data.Player, q Question, optID string) {
	o, ok := q.optionByID(optID)
	if !ok {
		return
	}
	var z float64
	for id, prior := range beliefs {
		l := o.Likelihood(players[id])
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
// marginalising over all of q's *informative* options (we exclude the flat
// "Don't know" option from the expectation since it carries no signal).
func ExpectedInfoGain(beliefs map[string]float64, players map[string]data.Player, q Question) float64 {
	h0 := Entropy(beliefs)

	// For each non-flat option, compute the marginal P(option) under the current
	// belief and the posterior entropy. The flat option (Likelihood = 1 for all
	// players) is excluded because it contributes no information.
	var expectedH float64
	var massInformative float64

	for _, o := range q.Options {
		if isFlatOption(o, players) {
			continue
		}
		var pO float64
		post := make(map[string]float64, len(beliefs))
		for id, prior := range beliefs {
			l := o.Likelihood(players[id])
			post[id] = prior * l
			pO += post[id]
		}
		if pO <= 0 {
			continue
		}
		for id := range post {
			post[id] /= pO
		}
		expectedH += pO * Entropy(post)
		massInformative += pO
	}
	if massInformative <= 0 {
		return 0
	}
	expectedH /= massInformative
	return h0 - expectedH
}

// isFlatOption returns true if the option's likelihood is constant across the
// candidate pool — i.e., it tells us nothing.
func isFlatOption(o Option, players map[string]data.Player) bool {
	first := math.NaN()
	for _, p := range players {
		v := o.Likelihood(p)
		if math.IsNaN(first) {
			first = v
			continue
		}
		if math.Abs(v-first) > 1e-9 {
			return false
		}
	}
	return true
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
		if gain > bestGain+1e-9 || (math.Abs(gain-bestGain) < 1e-9 && catCount < bestCatCount) {
			best = q
			bestGain = gain
			bestCatCount = catCount
			found = true
		}
	}
	return best, bestGain, found
}

// Candidate is a ranked player from the belief state.
type Candidate struct {
	ID          string
	Name        string
	Probability float64
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

// InitialBeliefs builds a prior over players using popularity as a soft weight.
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

