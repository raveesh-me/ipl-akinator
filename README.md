# IPL Akinator

Hackathon project. AI-driven guessing game that identifies an IPL cricketer in ≤ 8 questions using probabilistic reasoning (no decision trees).

See [CLAUDE.md](./CLAUDE.md) for the full brief and engine design.

## Stack

- **Proto contract**: buf v2 → ConnectRPC (Go + TS clients).
- **Backend**: Go 1.25, ConnectRPC over HTTP/2 (h2c).
- **Engine**: Bayesian belief over candidates, expected-info-gain question selection.
- **LLM**: Gemini for question phrasing (optional, falls back if `GEMINI_API_KEY` is unset).
- **Frontend**: SvelteKit 2 + Svelte 5 runes, Connect web client.

## Layout

```
proto/akinator/v1/akinator.proto  service contract
gen/                              generated Go (gitignored)
cmd/server/main.go                HTTP server entrypoint
internal/server/                  ConnectRPC handlers
internal/engine/                  belief state + info-gain selector
internal/llm/                     Gemini client
internal/data/                    embedded player dataset
web/                              SvelteKit app
```

## Run

```bash
# 1. Generate proto stubs (Go + TS)
make gen

# 2. Backend (port 8080)
make run

# 3. Frontend (port 5173)
cd web && npm install && npm run dev
```

Open [http://localhost:5173](http://localhost:5173).

To enable Gemini-based question phrasing:

```bash
export GEMINI_API_KEY=...
make run
```

## Curl smoke test

```bash
curl -s http://localhost:8080/healthz
curl -s -X POST -H "Content-Type: application/json" -d '{}' \
  http://localhost:8080/akinator.v1.AkinatorService/StartGame
```

## Extending

- **More players**: edit [internal/data/players.json](./internal/data/players.json). Question library auto-derives from the dataset.
- **More features**: add fields to `Player`, then add a `Question` in [internal/engine/questions.go](./internal/engine/questions.go).
- **Persistent learning**: replace `engine.NewStore()` with a Firestore-backed store; persist `Session.History` + correct answer on `SubmitFeedback` and re-weight feature priors.
