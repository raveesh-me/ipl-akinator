package engine

import (
	"fmt"
	"sort"

	"github.com/raveesh/ai-akinator/internal/data"
)

// likelihoodSmoothing keeps strict 0/1 predicates from collapsing the posterior
// when the user mis-answers a single question. Tune to taste.
const likelihoodSmoothing = 0.08

// Smoothed likelihoods for binary trait questions. The choices below imply:
//
//   - A player whose feature is TRUE has P(answer=Yes) ≈ 0.84, P(No) ≈ 0.08,
//     P(Maybe) ≈ 0.08. The "Yes" mass leaks slightly so a single user error
//     can't permanently zero out a strong candidate.
//   - A player whose feature is FALSE mirrors that distribution toward "No".
//   - "Don't know" has a flat likelihood of 1.0 — the Bayesian update is a no-op.
const (
	binYesIfTrue  = 0.84
	binNoIfTrue   = 0.08
	binMaybeMass  = 0.08 // probability either kind of player picks "Maybe"
	multiCorrect  = 0.85 // P(option matches bucket | player in that bucket)
	multiSpillage = 0.05 // P(option matches other bucket | player not in that bucket)
)

// dontKnow returns the conventional flat-likelihood option that every question
// includes.
func dontKnow() Option {
	return Option{
		ID:    DontKnowID,
		Label: "Don't know",
		Likelihood: func(data.Player) float64 {
			return 1.0
		},
	}
}

// binaryOptions emits Yes / No / Maybe / Don't-know for a 0-or-1 predicate.
func binaryOptions(pred func(data.Player) bool) []Option {
	yes := func(p data.Player) float64 {
		if pred(p) {
			return binYesIfTrue
		}
		return binNoIfTrue
	}
	no := func(p data.Player) float64 {
		if pred(p) {
			return binNoIfTrue
		}
		return binYesIfTrue
	}
	maybe := func(data.Player) float64 { return binMaybeMass }
	return []Option{
		{ID: "yes", Label: "Yes", Likelihood: yes},
		{ID: "no", Label: "No", Likelihood: no},
		{ID: "maybe", Label: "Maybe", Likelihood: maybe},
		dontKnow(),
	}
}

// partitionOptions emits one option per bucket plus a "Don't know" tail. Each
// option's likelihood prefers players whose bucket-test returns true.
func partitionOptions(buckets []struct {
	ID    string
	Label string
	Test  func(data.Player) bool
}) []Option {
	opts := make([]Option, 0, len(buckets)+1)
	for _, b := range buckets {
		b := b
		opts = append(opts, Option{
			ID:    b.ID,
			Label: b.Label,
			Likelihood: func(p data.Player) float64 {
				if b.Test(p) {
					return multiCorrect
				}
				return multiSpillage
			},
		})
	}
	opts = append(opts, dontKnow())
	return opts
}

// BuildQuestions derives the question library directly from the dataset, so
// adding new players or attributes to players.json automatically expands the
// question space.
func BuildQuestions(players []data.Player) []Question {
	var qs []Question

	// Multi-way: primary role (4 buckets) — high info per question.
	qs = append(qs, Question{
		ID:       "role",
		Text:     "What's your player's primary role?",
		Category: "role",
		Source:   "static",
		Options: partitionOptions([]struct {
			ID, Label string
			Test      func(data.Player) bool
		}{
			{"batsman", "Batter", func(p data.Player) bool { return p.Role == "batsman" }},
			{"bowler", "Bowler", func(p data.Player) bool { return p.Role == "bowler" }},
			{"allrounder", "All-rounder", func(p data.Player) bool { return p.Role == "allrounder" }},
			{"wk", "Wicket-keeper", func(p data.Player) bool { return p.Role == "wicketkeeper" }},
		}),
	})

	// Bowling style (4 buckets including "doesn't bowl").
	qs = append(qs, Question{
		ID:       "bowling_style",
		Text:     "If your player bowls, what style?",
		Category: "style",
		Source:   "static",
		Options: partitionOptions([]struct {
			ID, Label string
			Test      func(data.Player) bool
		}{
			{"pace", "Pace", func(p data.Player) bool { return p.BowlingStyle == "pace" }},
			{"spin", "Spin", func(p data.Player) bool { return p.BowlingStyle == "spin" }},
			{"none", "Doesn't bowl", func(p data.Player) bool { return p.BowlingStyle == "none" }},
		}),
	})

	// Nationality.
	qs = append(qs, Question{
		ID:       "is_overseas",
		Text:     "Is your player from outside India?",
		Category: "nationality",
		Source:   "static",
		Options:  binaryOptions(func(p data.Player) bool { return p.Nationality == "overseas" }),
	})

	// Batting hand.
	qs = append(qs, Question{
		ID:       "left_hand_bat",
		Text:     "Is your player a left-handed batter?",
		Category: "style",
		Source:   "static",
		Options:  binaryOptions(func(p data.Player) bool { return p.BattingHand == "left" }),
	})

	// Teams as a multi-way question — top-3 most-frequent teams in the dataset
	// + "Other". Always emits 4 informative buckets + Don't know.
	if topTeams := topTeams(players, 3); len(topTeams) > 0 {
		buckets := make([]struct {
			ID, Label string
			Test      func(data.Player) bool
		}, 0, len(topTeams)+1)
		isTop := map[string]bool{}
		for _, t := range topTeams {
			t := t
			isTop[t] = true
			buckets = append(buckets, struct {
				ID, Label string
				Test      func(data.Player) bool
			}{
				ID:    "team_" + t,
				Label: t,
				Test: func(p data.Player) bool {
					for _, pt := range p.Teams {
						if pt == t {
							return true
						}
					}
					return false
				},
			})
		}
		buckets = append(buckets, struct {
			ID, Label string
			Test      func(data.Player) bool
		}{
			ID: "team_other", Label: "Other team",
			Test: func(p data.Player) bool {
				for _, pt := range p.Teams {
					if isTop[pt] {
						return false
					}
				}
				return true
			},
		})
		qs = append(qs, Question{
			ID:       "primary_team",
			Text:     "Which IPL franchise has your player played for most?",
			Category: "team",
			Source:   "static",
			Options:  partitionOptions(buckets),
		})
	}

	// Per-team binary questions for the long tail (skip ones already covered in
	// the multi-way question's top-N).
	teams := uniqueStringsSlice(players, func(p data.Player) []string { return p.Teams })
	for _, team := range teams {
		team := team
		qs = append(qs, Question{
			ID:       "team_" + team,
			Text:     fmt.Sprintf("Has your player ever played for %s?", team),
			Category: "team",
			Source:   "static",
			Options: binaryOptions(func(p data.Player) bool {
				for _, t := range p.Teams {
					if t == team {
						return true
					}
				}
				return false
			}),
		})
	}

	// Era / awards / traits — all binary.
	qs = append(qs,
		Question{
			ID: "captain", Text: "Has your player ever captained an IPL franchise?",
			Category: "trait", Source: "static",
			Options: binaryOptions(func(p data.Player) bool { return p.Captain }),
		},
		Question{
			ID: "won_title_as_captain", Text: "Has your player led an IPL team to a title?",
			Category: "trait", Source: "static",
			Options: binaryOptions(func(p data.Player) bool { return p.WonTitleAsCaptain }),
		},
		Question{
			ID: "orange_cap", Text: "Has your player ever won the Orange Cap?",
			Category: "award", Source: "static",
			Options: binaryOptions(func(p data.Player) bool { return p.WonOrangeCap }),
		},
		Question{
			ID: "purple_cap", Text: "Has your player ever won the Purple Cap?",
			Category: "award", Source: "static",
			Options: binaryOptions(func(p data.Player) bool { return p.WonPurpleCap }),
		},
		Question{
			ID: "active_2024", Text: "Is your player still active in IPL 2024?",
			Category: "era", Source: "static",
			Options: binaryOptions(func(p data.Player) bool { return p.Active2024 }),
		},
		Question{
			ID: "is_finisher", Text: "Is your player known for finishing matches?",
			Category: "trait", Source: "static",
			Options: binaryOptions(func(p data.Player) bool { return p.IsFinisher }),
		},
		Question{
			ID: "top_order", Text: "Does your player typically bat in the top order?",
			Category: "trait", Source: "static",
			Options: binaryOptions(func(p data.Player) bool { return p.BatsTopOrder }),
		},
		Question{
			ID: "death_overs", Text: "Does your player frequently bowl in the death overs?",
			Category: "trait", Source: "static",
			Options: binaryOptions(func(p data.Player) bool { return p.BowlsDeathOvers }),
		},
		Question{
			ID: "debuted_pre_2013", Text: "Did your player debut in the IPL before 2013?",
			Category: "era", Source: "static",
			Options: binaryOptions(func(p data.Player) bool { return p.FirstSeason < 2013 }),
		},
		Question{
			ID: "is_legend", Text: "Would you call your player an all-time IPL great?",
			Category: "trait", Source: "static",
			Options: binaryOptions(func(p data.Player) bool { return p.IsLegend }),
		},
	)

	return qs
}

// topTeams returns the K most-frequent teams in the dataset.
func topTeams(players []data.Player, k int) []string {
	counts := map[string]int{}
	for _, p := range players {
		for _, t := range p.Teams {
			counts[t]++
		}
	}
	type kv struct {
		Team  string
		Count int
	}
	kvs := make([]kv, 0, len(counts))
	for t, c := range counts {
		kvs = append(kvs, kv{t, c})
	}
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].Count != kvs[j].Count {
			return kvs[i].Count > kvs[j].Count
		}
		return kvs[i].Team < kvs[j].Team
	})
	if k > len(kvs) {
		k = len(kvs)
	}
	out := make([]string, 0, k)
	for i := 0; i < k; i++ {
		out = append(out, kvs[i].Team)
	}
	return out
}

func uniqueStringsSlice(players []data.Player, get func(data.Player) []string) []string {
	seen := map[string]struct{}{}
	for _, p := range players {
		for _, v := range get(p) {
			if v == "" {
				continue
			}
			seen[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

