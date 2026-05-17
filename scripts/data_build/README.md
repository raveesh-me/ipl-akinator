# Player dataset builder

Produces `internal/data/players.json` (the static candidate pool the engine
reasons over) by processing Cricsheet's open IPL match data.

## Run

```bash
cd scripts/data_build

# 1. Fetch raw inputs (free, public, ~14MB total)
curl -O https://cricsheet.org/downloads/ipl_json.zip
curl -O https://cricsheet.org/downloads/t20s_male_json.zip
unzip -q ipl_json.zip -d ipl_json
unzip -q t20s_male_json.zip -d t20s_male_json

# 2. Regenerate players.json
python3 build_players.py
```

The downloads in `ipl_json/`, `t20s_male_json/`, and the zips are gitignored
— anyone can regenerate them.

## What each feature is derived from

| Field                  | Source                                                       |
|------------------------|--------------------------------------------------------------|
| `name`, `id`           | cricsheet match registry (canonical name + 6-char pid suffix)|
| `teams`                | every IPL team the player appeared in a XI for               |
| `first_season`, `last_season`, `active_2024` | year of first / last match date         |
| `role`                 | derived from career batting position, balls faced/bowled, balls/innings, stumpings |
| `bats_top_order`       | average batting position ≤ 4 across all IPL innings          |
| `bowls_death_overs`    | ≥25% of balls bowled landed in overs 16–19 (min 60 balls)    |
| `is_finisher`          | average batting position 4.5–7.5, strike rate ≥ 130, ≥120 balls faced |
| `won_orange_cap`       | top run-scorer of any IPL season                              |
| `won_purple_cap`       | top wicket-taker of any IPL season                            |
| `nationality`/`country`| most-frequent national T20I team across cricsheet T20I data  |
| `is_legend`            | ≥2500 IPL runs OR ≥100 IPL wickets OR orange/purple cap holder |
| `popularity`           | log-scaled prior on career runs+wickets+matches              |

Wicketkeeper detection uses an unambiguous signal: only the keeper can effect
a stumping, so players with ≥5 stumpings are tagged as `wicketkeeper`. This
catches full-time keepers and serious part-timers (Rahul/PBKS, Bairstow) without
false positives.

## Known gaps

- **`batting_hand` is `"unknown"`** for everyone. Cricsheet ball-by-ball
  doesn't record hand. Backfill via Wikipedia scrape or LLM if needed.
- **`bowling_style` is `"unknown"`** if the player bowled at all, else `"none"`.
  Pace-vs-spin is not in cricsheet ball-by-ball either.
- **`captain` / `won_title_as_captain`** are `false` for everyone. Cricsheet
  match files don't reliably mark captain. Could be derived from toss decision
  attribution or backfilled from Wikipedia.
- **Afghan players** are overridden by name because Cricsheet excludes all
  Afghanistan matches per their policy
  (https://cricsheet.org/withheld-matches), so T20I lookup misses them.
- **Dual-role keepers** (Buttler, Bairstow, sometimes Rahul) may classify as
  `batsman` if they keep too rarely to clear the 5-stumping threshold. These
  are genuinely ambiguous — the engine's Maybe/Don't-know answer handles them.

## What's in `_stats`

Each player record includes a `_stats` block (matches, innings, runs, wickets,
balls faced/bowled, batting position avg, strike rate). The Go loader silently
ignores it via `encoding/json`'s default behaviour, but it's useful for
inspection and for the LLM when generating novel discriminating questions.
