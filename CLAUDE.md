# AI Akinator for IPL Players

Hackathon project. AI-powered guessing game that identifies an IPL cricketer (past or present) via adaptive, information-maximizing questioning.

## Brief

### Problem
Build an Akinator-style system strictly scoped to the IPL ecosystem (players, roles, teams, match contexts). Identify a player the user is thinking of in **as few questions as possible** — no fixed budget, just stop as soon as we're confident or further probing won't help.

### Hard rules
- **No hardcoded decision trees.** Reasoning must be probabilistic / dynamic.
- IPL players only (2008–present, Indian + overseas, all roles).
- Stop and guess when **any one of** holds:
  - `max p ≥ 0.80` (confident).
  - Best unasked question has expected info gain < `MinInfoGainBits` (0.05) — no remaining question carries enough signal to be worth a turn.
  - `MaxQuestions = 15` safety ceiling (only hit on pathological input).
- Learn from feedback (wrong-guess corrections feed future sessions).

### Engine design (this repo)
1. **Candidate pool** — every IPL player loaded from `internal/data/players.json` with a feature vector (role, nationality, teams, eras, captaincy, finisher, death-overs bowler, awards, etc.).
2. **Belief state** — probability distribution over candidates. Updated via Bayesian update on each Yes/No/Maybe/Don't-know answer using per-feature likelihood.
3. **Question selection** — for each candidate feature/question, compute expected entropy reduction (info gain) over the current belief state; pick the highest. The LLM (Gemini) is used to *phrase* the chosen feature-question naturally and to *generate novel discriminating questions* when the top candidates differ on attributes not in the static feature set.
4. **Stopping** — emit final guess when `max p ≥ 0.8` OR best next-question info gain < 0.05 bits OR safety cap of 15 hit.
5. **Feedback** — wrong guesses persist (player, question history, correct answer) so future sessions can re-weight features.

### Tech stack
- **Proto**: buf v2 (`buf.yaml`, `buf.gen.yaml` — both `version: v2`).
- **Backend**: Go + ConnectRPC (`connectrpc.com/connect`). Single binary at `cmd/server`.
- **Frontend**: SvelteKit + ConnectRPC web client (`@connectrpc/connect-web`).
- **LLM**: Gemini via `GEMINI_API_KEY` env var (HTTP call, no heavy SDK).
- **Storage**: JSON file for player dataset (swap to Firestore/BigQuery later).

### Layout
```
proto/akinator/v1/akinator.proto   service contract
gen/                               buf-generated Go (gitignored)
cmd/server/main.go                 connectrpc HTTP server
internal/server/                   RPC handlers
internal/engine/                   candidate pool, probability updates, info gain
internal/llm/                      Gemini client (question phrasing + novel Qs)
internal/data/                     player dataset + loader
web/                               SvelteKit app
```

### Common commands
- `make gen` — regenerate proto stubs (Go + TS).
- `make run` — start backend on `:8080`.
- `make web` — start SvelteKit dev server on `:5173`.
- `make tidy` — `go mod tidy`.

### Evaluation focus (per brief)
- AI reasoning is the highest-weighted axis: keep all branching probabilistic, never `if role == "batsman"` style.
- Question intelligence: prefer high-info-gain features; let the LLM phrase them, not pick them.
- Learning: persist feedback; re-weight on next session.
