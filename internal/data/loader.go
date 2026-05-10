package data

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed players.json
var playersJSON []byte

// Player is the dataset row. Fields here are the *static* feature space the engine
// reasons over. The LLM may propose additional ad-hoc questions when the static
// space cannot discriminate the remaining candidates.
type Player struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Role              string   `json:"role"`         // batsman | bowler | allrounder | wicketkeeper
	Nationality       string   `json:"nationality"`  // indian | overseas
	Country           string   `json:"country"`
	BattingHand       string   `json:"batting_hand"` // left | right
	BowlingStyle      string   `json:"bowling_style"`// pace | spin | none
	Teams             []string `json:"teams"`
	FirstSeason       int      `json:"first_season"`
	LastSeason        int      `json:"last_season"`
	Active2024        bool     `json:"active_2024"`
	Captain           bool     `json:"captain"`
	WonTitleAsCaptain bool     `json:"won_title_as_captain"`
	WonOrangeCap      bool     `json:"won_orange_cap"`
	WonPurpleCap      bool     `json:"won_purple_cap"`
	IsFinisher        bool     `json:"is_finisher"`
	BatsTopOrder      bool     `json:"bats_top_order"`
	BowlsDeathOvers   bool     `json:"bowls_death_overs"`
	IsLegend          bool     `json:"is_legend"`
	Popularity        float64  `json:"popularity"` // prior weight; 0..1
}

// Load parses the embedded dataset.
func Load() ([]Player, error) {
	var players []Player
	if err := json.Unmarshal(playersJSON, &players); err != nil {
		return nil, fmt.Errorf("decode players.json: %w", err)
	}
	if len(players) == 0 {
		return nil, fmt.Errorf("players.json is empty")
	}
	return players, nil
}
