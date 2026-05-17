"""
Build internal/data/players.json from Cricsheet IPL JSON archive.

Inputs (sibling files):
  ipl_json/*.json          — every IPL match (Cricsheet v1.x)
  t20s_male_json/*.json    — men's T20Is (optional, for nationality)

Output:
  ../../internal/data/players.json

Schema target: loader.go Player struct.
"""

from __future__ import annotations

import json
import os
import re
import sys
from collections import Counter, defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent
IPL_DIR = ROOT / "ipl_json"
T20I_DIR = ROOT / "t20s_male_json"
OUT_PATH = ROOT.parent.parent / "internal" / "data" / "players.json"

# Canonical IPL team code map. Covers historical renames.
TEAM_CODE = {
    "Mumbai Indians": "MI",
    "Chennai Super Kings": "CSK",
    "Royal Challengers Bangalore": "RCB",
    "Royal Challengers Bengaluru": "RCB",
    "Kolkata Knight Riders": "KKR",
    "Rajasthan Royals": "RR",
    "Sunrisers Hyderabad": "SRH",
    "Deccan Chargers": "DCH",          # Deccan Chargers, 2008-2012 (different franchise from SRH)
    "Delhi Daredevils": "DC",
    "Delhi Capitals": "DC",
    "Kings XI Punjab": "PBKS",
    "Punjab Kings": "PBKS",
    "Gujarat Titans": "GT",
    "Lucknow Super Giants": "LSG",
    "Gujarat Lions": "GL",
    "Rising Pune Supergiant": "RPS",
    "Rising Pune Supergiants": "RPS",
    "Pune Warriors": "PWI",
    "Pune Warriors India": "PWI",
    "Kochi Tuskers Kerala": "KTK",
}

DEATH_OVER_START = 16  # overs 16..19 (zero-indexed → 16..19)

# ----------------------------- helpers ---------------------------------------


def slugify(name: str) -> str:
    """Turn a player name like 'AB de Villiers' into 'ab_devilliers'."""
    s = name.lower()
    s = re.sub(r"[^a-z0-9 ]+", "", s)
    s = re.sub(r"\s+", "_", s.strip())
    return s


def canon_team(name: str) -> str | None:
    code = TEAM_CODE.get(name)
    if code is None:
        # silently skip unknown team names (warmup teams etc.)
        return None
    return code


def match_year(info: dict) -> int:
    """The IPL year of a match is the calendar year of its first date.

    Cricsheet labels seasons like '2007/08' for IPL 2008 (Apr-Jun 2008) but
    also '2020/21' for IPL 2020 (Oct-Nov 2020). Date-based is unambiguous.
    """
    dates = info.get("dates") or []
    if dates:
        return int(str(dates[0])[:4])
    season = info.get("season")
    if isinstance(season, int):
        return season
    s = str(season)
    if "/" in s:
        a, _ = s.split("/")
        return int(a) + 1
    return int(s)


# ----------------------------- aggregation -----------------------------------


def empty_player():
    return {
        "name": None,
        "registry_id": None,
        "teams_seasons": defaultdict(set),  # team_code -> {season}
        "seasons": set(),
        "innings_count": 0,
        "balls_faced": 0,
        "balls_faced_death": 0,
        "runs_scored": 0,
        "batting_positions": [],
        "balls_bowled": 0,
        "balls_bowled_death": 0,
        "runs_conceded": 0,
        "wickets": 0,
        "matches_played": set(),         # match_id set
        "matches_won_in_xi": set(),      # match_id set (in playing XI of winning side)
        "match_seasons": set(),
        "stumpings_made": 0,             # fielded a stumping → wicketkeeper
        "catches_made": 0,
    }


def process_ipl_match(path: Path, players: dict, season_aggregates: dict, finals_seasons: dict):
    with path.open() as f:
        m = json.load(f)
    info = m["info"]
    season = match_year(info)
    teams = info.get("teams", [])
    registry = info.get("registry", {}).get("people", {})
    xi = info.get("players", {})  # team -> [names]
    outcome = info.get("outcome", {})
    winner = outcome.get("winner")
    event = info.get("event", {}) or {}
    stage = (event.get("stage") or event.get("match_number") or "")
    is_final = isinstance(stage, str) and stage.lower() == "final"

    match_id = path.stem
    team_codes = {t: canon_team(t) for t in teams}

    # Track XI per team (matches played + nationality team affinity)
    for team_name, names in xi.items():
        code = team_codes.get(team_name)
        for name in names:
            pid = registry.get(name)
            if not pid:
                continue
            p = players.setdefault(pid, empty_player())
            p["name"] = name
            p["registry_id"] = pid
            p["matches_played"].add(match_id)
            p["match_seasons"].add(season)
            p["seasons"].add(season)
            if code:
                p["teams_seasons"][code].add(season)
            if winner and team_name == winner:
                p["matches_won_in_xi"].add(match_id)

    if is_final and winner:
        finals_seasons[season] = {
            "winner": winner,
            "winning_xi": [registry.get(n) for n in xi.get(winner, []) if registry.get(n)],
            "match_id": match_id,
        }

    # Walk innings to capture batting & bowling stats
    for inn_idx, inn in enumerate(m["innings"]):
        # Track batting order per innings via the order in which batters first appear.
        batting_order = []
        seen_batters = set()

        for over in inn["overs"]:
            over_num = over["over"]
            is_death = over_num >= DEATH_OVER_START
            for d in over["deliveries"]:
                batter = d.get("batter")
                bowler = d.get("bowler")
                runs = d.get("runs", {})
                batter_runs = runs.get("batter", 0)
                extras = d.get("extras", {}) or {}
                # A legitimate delivery for "balls faced" excludes wides.
                is_wide = "wides" in extras
                is_noball = "noballs" in extras

                # Track batting order
                if batter and batter not in seen_batters:
                    batting_order.append(batter)
                    seen_batters.add(batter)

                # Batter stats
                if batter:
                    pid = registry.get(batter)
                    if pid:
                        p = players.setdefault(pid, empty_player())
                        p["name"] = batter
                        p["registry_id"] = pid
                        if not is_wide:
                            p["balls_faced"] += 1
                            if is_death:
                                p["balls_faced_death"] += 1
                        p["runs_scored"] += batter_runs
                        # season aggregate (orange cap)
                        season_aggregates[(season, "runs")][pid] = (
                            season_aggregates[(season, "runs")].get(pid, 0) + batter_runs
                        )

                # Bowler stats
                if bowler:
                    pid = registry.get(bowler)
                    if pid:
                        p = players.setdefault(pid, empty_player())
                        p["name"] = bowler
                        p["registry_id"] = pid
                        if not is_wide and not is_noball:
                            p["balls_bowled"] += 1
                            if is_death:
                                p["balls_bowled_death"] += 1
                        # runs conceded (batter + wides + noballs) — keep simple
                        p["runs_conceded"] += runs.get("total", 0)
                        # wickets
                        for w in d.get("wickets", []) or []:
                            kind = w.get("kind", "")
                            # Bowler doesn't get credit for run outs / retired etc.
                            if kind not in ("run out", "retired hurt", "retired out", "obstructing the field"):
                                p["wickets"] += 1
                                season_aggregates[(season, "wkts")][pid] = (
                                    season_aggregates[(season, "wkts")].get(pid, 0) + 1
                                )

                # Fielder credits — stumpings identify wicketkeepers unambiguously.
                for w in d.get("wickets", []) or []:
                    kind = w.get("kind", "")
                    fielders = w.get("fielders") or []
                    for f in fielders:
                        fname = f.get("name")
                        if not fname:
                            continue
                        fpid = registry.get(fname)
                        if not fpid:
                            continue
                        fp = players.setdefault(fpid, empty_player())
                        fp["name"] = fname
                        fp["registry_id"] = fpid
                        if kind == "stumped":
                            fp["stumpings_made"] += 1
                        elif kind == "caught":
                            fp["catches_made"] += 1

        # Commit batting positions for this innings
        for pos, name in enumerate(batting_order, start=1):
            pid = registry.get(name)
            if pid:
                p = players.setdefault(pid, empty_player())
                p["batting_positions"].append(pos)
                p["innings_count"] += 1


# ----------------------------- nationality -----------------------------------


EXHIBITION_TEAMS = {
    "ICC World XI", "Africa XI", "Asia XI",
}

# Cricsheet excludes Afghanistan matches per policy
# (https://cricsheet.org/withheld-matches), so T20I lookup misses Afghan
# players. Override by cricsheet pid for the IPL regulars.
COUNTRY_OVERRIDE_BY_PID = {
    "5f547c8b": "Afghanistan",  # Rashid Khan
    # Filled at runtime below by NAME, since pids are stable but I have names handy.
}

# Override by exact IPL name when pid isn't known up front.
COUNTRY_OVERRIDE_BY_NAME = {
    "Rashid Khan":      "Afghanistan",
    "Mohammad Nabi":    "Afghanistan",
    "Naveen-ul-Haq":    "Afghanistan",
    "Mujeeb Ur Rahman": "Afghanistan",
    "Noor Ahmad":       "Afghanistan",
    "Karim Janat":      "Afghanistan",
    "Hazratullah Zazai":"Afghanistan",
    "Fazalhaq Farooqi": "Afghanistan",
    "Gulbadin Naib":    "Afghanistan",
    "Najibullah Zadran":"Afghanistan",
    "Qais Ahmad":       "Afghanistan",
    "Fareed Ahmad":     "Afghanistan",
    "Sharafuddin Ashraf":"Afghanistan",
}


def build_nationality_index(t20i_dir: Path) -> dict[str, str]:
    """Map registry_id -> country, derived from men's T20I appearances.

    Uses the most-frequent national team across all T20Is the player appeared
    in. Exhibition teams (World XI, Asia XI) are ignored so a Rashid Khan
    doesn't end up labelled 'ICC World XI'.
    """
    counts: dict[str, Counter] = defaultdict(Counter)
    if not t20i_dir.exists():
        return {}
    for path in t20i_dir.glob("*.json"):
        try:
            with path.open() as f:
                m = json.load(f)
        except Exception:
            continue
        info = m.get("info", {})
        if info.get("team_type") != "international":
            continue
        registry = info.get("registry", {}).get("people", {})
        xi = info.get("players", {}) or {}
        for team_name, names in xi.items():
            if team_name in EXHIBITION_TEAMS:
                continue
            for n in names:
                pid = registry.get(n)
                if pid:
                    counts[pid][team_name] += 1
    return {pid: c.most_common(1)[0][0] for pid, c in counts.items()}


# ----------------------------- feature derivation ----------------------------


def derive(player_data: dict, nat_idx: dict[str, str], finals_seasons: dict,
           season_aggregates: dict, latest_season: int) -> dict | None:
    """Turn aggregated data into a Player dict matching loader.go."""
    name = player_data["name"]
    if not name:
        return None
    pid = player_data["registry_id"]
    seasons = player_data["seasons"]
    if not seasons:
        return None

    first_season = min(seasons)
    last_season = max(seasons)
    active_2024 = 2024 in seasons or 2025 in seasons or 2026 in seasons

    # Teams: present across all seasons appeared in.
    teams = sorted(player_data["teams_seasons"].keys())

    balls_bowled = player_data["balls_bowled"]
    balls_bowled_death = player_data["balls_bowled_death"]
    balls_faced = player_data["balls_faced"]
    balls_faced_death = player_data["balls_faced_death"]
    runs_scored = player_data["runs_scored"]
    runs_conceded = player_data["runs_conceded"]
    wickets = player_data["wickets"]
    positions = player_data["batting_positions"]
    avg_pos = sum(positions) / len(positions) if positions else 99.0
    innings_count = player_data["innings_count"]
    matches = len(player_data["matches_played"])

    # Role classification — purely from behaviour.
    bowls_any = balls_bowled >= 24                # at least 4 overs in career
    bowls_serious = balls_bowled >= 240           # ≥40 overs total → part of role identity
    bats_any = balls_faced >= 30
    bats_serious = balls_faced >= 200             # meaningful batting career
    # Bowling load per innings — separates genuine all-rounders from part-timers
    balls_per_inn = balls_bowled / max(innings_count, 1)
    # Wicketkeeper: ≥5 stumpings is unambiguous (only keepers can stump).
    # Catches part-time keepers (Rahul/PBKS, Bairstow) without false positives.
    is_keeper = player_data["stumpings_made"] >= 5

    if is_keeper:
        role = "wicketkeeper"
    elif bowls_serious and bats_serious and avg_pos <= 7 and balls_per_inn >= 6:
        role = "allrounder"
    elif bats_serious and (not bowls_serious or balls_per_inn < 6):
        # Serious batsman, only part-time bowling at most.
        role = "batsman"
    elif bowls_serious:
        role = "bowler"
    elif bats_any and not bowls_any:
        role = "batsman"
    elif bowls_any:
        role = "bowler"
    else:
        role = "batsman" if balls_faced >= balls_bowled else "bowler"

    # Wicketkeeper detection is impossible from ball-by-ball; leave it to a
    # post-pass override list (top-K keepers). The engine treats role as one
    # categorical feature; over-classifying wk would be noise.

    bats_top_order = avg_pos <= 4.0 and balls_faced >= 60
    bowls_death_overs = (
        balls_bowled >= 60
        and balls_bowled_death / max(balls_bowled, 1) >= 0.25
    )

    sr = (runs_scored / balls_faced * 100) if balls_faced else 0
    is_finisher = (
        4.5 <= avg_pos <= 7.5
        and balls_faced >= 120
        and sr >= 130
    )

    # Honors
    won_title_as_captain = False  # captain not reliably in cricsheet
    won_title = any(pid in fs["winning_xi"] for fs in finals_seasons.values())

    # Orange cap: top run-scorer per season
    won_orange_cap = False
    won_purple_cap = False
    for s in seasons:
        runs_season = season_aggregates.get((s, "runs"), {})
        wkts_season = season_aggregates.get((s, "wkts"), {})
        if runs_season:
            top_runs_pid = max(runs_season.items(), key=lambda kv: kv[1])[0]
            if top_runs_pid == pid:
                won_orange_cap = True
        if wkts_season:
            top_wkts_pid = max(wkts_season.items(), key=lambda kv: kv[1])[0]
            if top_wkts_pid == pid:
                won_purple_cap = True

    # Legend: a soft prior. Top 10% of careers by either runs or wickets.
    is_legend = (runs_scored >= 2500) or (wickets >= 100) or won_orange_cap or won_purple_cap

    # Popularity: 0..1, log-scaled on career impact.
    impact = runs_scored / 6000.0 + wickets / 200.0 + matches / 250.0
    popularity = min(1.0, round(impact / 3.0, 3))
    if is_legend:
        popularity = max(popularity, 0.7)

    # Nationality — explicit override beats T20I lookup; T20I beats default.
    country = COUNTRY_OVERRIDE_BY_NAME.get(name) or nat_idx.get(pid)
    if country == "India":
        nationality = "indian"
    elif country:
        nationality = "overseas"
    else:
        # No international appearance → uncapped; almost always Indian domestic.
        nationality = "indian"
        country = "India"

    return {
        "id": f"{slugify(name)}_{pid[:6]}",
        "name": name,
        "role": role,
        "nationality": nationality,
        "country": country,
        "batting_hand": "unknown",      # not derivable from cricsheet
        # bowling_style cannot be derived from ball-by-ball; mark unknown when they bowl.
        "bowling_style": "unknown" if bowls_any else "none",
        "teams": teams,
        "first_season": first_season,
        "last_season": last_season,
        "active_2024": active_2024,
        "captain": False,                # not in cricsheet match files
        "won_title_as_captain": won_title_as_captain,
        "won_orange_cap": won_orange_cap,
        "won_purple_cap": won_purple_cap,
        "is_finisher": bool(is_finisher),
        "bats_top_order": bool(bats_top_order),
        "bowls_death_overs": bool(bowls_death_overs),
        "is_legend": bool(is_legend),
        "popularity": popularity,
        # derived stats kept for downstream inspection (loader.go ignores)
        "_stats": {
            "matches": matches,
            "innings": innings_count,
            "runs": runs_scored,
            "wickets": wickets,
            "balls_faced": balls_faced,
            "balls_bowled": balls_bowled,
            "avg_batting_pos": round(avg_pos, 2) if positions else None,
            "strike_rate": round(sr, 1) if balls_faced else None,
        },
    }


# ----------------------------- main ------------------------------------------


def main():
    if not IPL_DIR.exists():
        print(f"missing {IPL_DIR}", file=sys.stderr)
        sys.exit(1)

    files = sorted(IPL_DIR.glob("*.json"))
    print(f"processing {len(files)} IPL matches…")
    players: dict = {}
    season_aggregates: dict = defaultdict(dict)
    # ensure subkeys are dicts (defaultdict creates empty dict for missing)
    finals_seasons: dict = {}
    for i, path in enumerate(files):
        if i % 200 == 0:
            print(f"  {i}/{len(files)}")
        if "README" in path.name:
            continue
        try:
            process_ipl_match(path, players, season_aggregates, finals_seasons)
        except Exception as e:
            print(f"  skip {path.name}: {e}", file=sys.stderr)

    print(f"unique players (registry ids): {len(players)}")
    print(f"finals found: {sorted(finals_seasons.keys())}")

    print("building nationality index from T20Is (if present)…")
    nat_idx = build_nationality_index(T20I_DIR)
    print(f"  T20I players resolved: {len(nat_idx)}")

    latest_season = max((max(p['seasons']) for p in players.values() if p['seasons']), default=2024)

    out = []
    for pid, pdata in players.items():
        rec = derive(pdata, nat_idx, finals_seasons, season_aggregates, latest_season)
        if rec:
            out.append(rec)

    # Filter: only players who actually batted or bowled meaningfully OR were in an XI more than once.
    out = [
        r for r in out
        if r["_stats"]["matches"] >= 1
    ]
    out.sort(key=lambda r: (-r["popularity"], r["name"]))

    print(f"emitting {len(out)} players → {OUT_PATH}")
    OUT_PATH.write_text(json.dumps(out, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
