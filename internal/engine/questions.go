package engine

import (
	"fmt"
	"sort"

	"github.com/raveesh/ai-akinator/internal/data"
)

// Question is an atomic, machine-evaluable query over the candidate pool.
//
// `Predicate` returns the probability that a player whose true attributes match
// `p` would answer "Yes" to this question. For most static features this is
// {0.0, 1.0}; we apply smoothing at update time so users can be wrong.
//
// IMPORTANT: this is a *feature library*, not a decision tree. The engine
// chooses among these questions at runtime via expected-info-gain on the live
// belief state. The order in which they are asked is data-dependent.
type Question struct {
	ID        string
	Text      string
	Category  string // role | nationality | team | award | era | style | trait
	Predicate func(p data.Player) float64
}

// BuildQuestions derives the question library directly from the dataset, so
// adding new players or attributes to players.json automatically expands the
// question space.
func BuildQuestions(players []data.Player) []Question {
	var qs []Question

	// Role questions — one per role observed in the dataset.
	roles := uniqueStrings(players, func(p data.Player) string { return p.Role })
	for _, role := range roles {
		role := role
		qs = append(qs, Question{
			ID:       "role_" + role,
			Text:     fmt.Sprintf("Is your player primarily a %s?", roleNoun(role)),
			Category: "role",
			Predicate: func(p data.Player) float64 {
				if p.Role == role {
					return 1.0
				}
				return 0.0
			},
		})
	}

	// Nationality.
	qs = append(qs, Question{
		ID:       "is_overseas",
		Text:     "Is your player from outside India?",
		Category: "nationality",
		Predicate: func(p data.Player) float64 {
			if p.Nationality == "overseas" {
				return 1.0
			}
			return 0.0
		},
	})

	// Bowling style.
	for _, style := range []string{"pace", "spin"} {
		style := style
		qs = append(qs, Question{
			ID:       "bowls_" + style,
			Text:     fmt.Sprintf("Does your player bowl %s?", style),
			Category: "style",
			Predicate: func(p data.Player) float64 {
				if p.BowlingStyle == style {
					return 1.0
				}
				return 0.0
			},
		})
	}

	// Batting hand.
	qs = append(qs, Question{
		ID:       "left_hand_bat",
		Text:     "Is your player a left-handed batter?",
		Category: "style",
		Predicate: func(p data.Player) float64 {
			if p.BattingHand == "left" {
				return 1.0
			}
			return 0.0
		},
	})

	// Teams — one per franchise observed.
	teams := uniqueStringsSlice(players, func(p data.Player) []string { return p.Teams })
	for _, team := range teams {
		team := team
		qs = append(qs, Question{
			ID:       "team_" + team,
			Text:     fmt.Sprintf("Has your player ever played for %s?", team),
			Category: "team",
			Predicate: func(p data.Player) float64 {
				for _, t := range p.Teams {
					if t == team {
						return 1.0
					}
				}
				return 0.0
			},
		})
	}

	// Awards & captaincy.
	qs = append(qs,
		Question{
			ID:       "captain",
			Text:     "Has your player ever captained an IPL franchise?",
			Category: "trait",
			Predicate: boolFeature(func(p data.Player) bool { return p.Captain }),
		},
		Question{
			ID:       "won_title_as_captain",
			Text:     "Has your player led an IPL team to a title?",
			Category: "trait",
			Predicate: boolFeature(func(p data.Player) bool { return p.WonTitleAsCaptain }),
		},
		Question{
			ID:       "orange_cap",
			Text:     "Has your player ever won the Orange Cap?",
			Category: "award",
			Predicate: boolFeature(func(p data.Player) bool { return p.WonOrangeCap }),
		},
		Question{
			ID:       "purple_cap",
			Text:     "Has your player ever won the Purple Cap?",
			Category: "award",
			Predicate: boolFeature(func(p data.Player) bool { return p.WonPurpleCap }),
		},
		Question{
			ID:       "active_2024",
			Text:     "Is your player still active in IPL 2024?",
			Category: "era",
			Predicate: boolFeature(func(p data.Player) bool { return p.Active2024 }),
		},
		Question{
			ID:       "is_finisher",
			Text:     "Is your player known for finishing matches?",
			Category: "trait",
			Predicate: boolFeature(func(p data.Player) bool { return p.IsFinisher }),
		},
		Question{
			ID:       "top_order",
			Text:     "Does your player typically bat in the top order?",
			Category: "trait",
			Predicate: boolFeature(func(p data.Player) bool { return p.BatsTopOrder }),
		},
		Question{
			ID:       "death_overs",
			Text:     "Does your player frequently bowl in the death overs?",
			Category: "trait",
			Predicate: boolFeature(func(p data.Player) bool { return p.BowlsDeathOvers }),
		},
		Question{
			ID:       "debuted_pre_2013",
			Text:     "Did your player debut in the IPL before 2013?",
			Category: "era",
			Predicate: boolFeature(func(p data.Player) bool { return p.FirstSeason < 2013 }),
		},
		Question{
			ID:       "is_legend",
			Text:     "Would you call your player an all-time IPL great?",
			Category: "trait",
			Predicate: boolFeature(func(p data.Player) bool { return p.IsLegend }),
		},
	)

	return qs
}

func boolFeature(fn func(data.Player) bool) func(data.Player) float64 {
	return func(p data.Player) float64 {
		if fn(p) {
			return 1.0
		}
		return 0.0
	}
}

func uniqueStrings(players []data.Player, get func(data.Player) string) []string {
	seen := map[string]struct{}{}
	for _, p := range players {
		v := get(p)
		if v == "" {
			continue
		}
		seen[v] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
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

func roleNoun(role string) string {
	switch role {
	case "wicketkeeper":
		return "wicket-keeper"
	case "allrounder":
		return "all-rounder"
	default:
		return role
	}
}
