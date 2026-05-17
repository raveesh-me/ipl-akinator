// Package llm wraps a thin client for OpenRouter (OpenAI-compatible API),
// routing to a free Gemini model by default. Two responsibilities:
//
//  1. Phrase the engine's chosen feature-question naturally (so the engine
//     never ships raw IDs like "team_CSK" to the user).
//  2. When the engine's static feature library can no longer discriminate the
//     remaining top candidates, ask the LLM to propose a *novel* multiple-choice
//     question and to label each top candidate's expected option. The engine
//     then propagates those labels to the full pool via the vector store.
//
// If OPENROUTER_API_KEY is unset, the client runs in offline-fallback mode and
// returns deterministic defaults so the rest of the system stays functional.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultEndpoint = "https://openrouter.ai/api/v1/chat/completions"
	defaultModel    = "google/gemini-2.0-flash-exp:free"
)

// Client talks to OpenRouter's OpenAI-compatible chat endpoint.
type Client struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

// NewClient reads OPENROUTER_API_KEY (and optional OPENROUTER_MODEL,
// OPENROUTER_ENDPOINT) from the environment. If the key is empty, the client
// runs in offline-fallback mode (PhraseQuestion returns input untouched,
// ProposeNovelQuestion returns ErrLLMDisabled).
func NewClient() *Client {
	return &Client{
		apiKey:   os.Getenv("OPENROUTER_API_KEY"),
		model:    envOr("OPENROUTER_MODEL", defaultModel),
		endpoint: envOr("OPENROUTER_ENDPOINT", defaultEndpoint),
		http:     &http.Client{Timeout: 15 * time.Second},
	}
}

// ErrLLMDisabled is returned when an LLM-required operation is called but no
// API key is configured.
var ErrLLMDisabled = errors.New("llm disabled: OPENROUTER_API_KEY not set")

// Enabled reports whether a real API call will be made.
func (c *Client) Enabled() bool { return c.apiKey != "" }

// PhraseQuestion rewrites the engine's neutral question text into something
// more conversational. On error, returns the original text untouched.
func (c *Client) PhraseQuestion(ctx context.Context, base string, options []string, topNames []string) (string, error) {
	if !c.Enabled() {
		return base, nil
	}
	system := "You rewrite questions for an IPL cricketer guessing game. " +
		"Keep meaning identical. Output ONLY the rewritten question, nothing else. " +
		"It must be answerable by exactly the options provided."
	user := fmt.Sprintf(
		"Question: %q\nOptions: %v\nCurrent top suspects (do NOT name them): %v\nRewrite:",
		base, options, topNames,
	)
	out, err := c.chat(ctx, system, user)
	if err != nil || out == "" {
		return base, err
	}
	return strings.TrimSpace(out), nil
}

// CandidateBrief is the projection passed to the LLM when proposing a novel
// question. We keep it small so the prompt stays cheap and on-topic.
type CandidateBrief struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Nationality string   `json:"nationality"`
	Teams       []string `json:"teams"`
	Description string   `json:"description,omitempty"`
	Probability float64  `json:"probability"`
}

// NovelOption is one option in an LLM-proposed novel question.
type NovelOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// NovelQuestion is the LLM's response when asked to invent a discriminator.
// `Labels` maps candidate.id -> chosen option id for each top candidate the
// LLM was shown. The engine extrapolates labels to the rest of the pool via
// the vector store.
type NovelQuestion struct {
	Text    string            `json:"text"`
	Options []NovelOption     `json:"options"`
	Labels  map[string]string `json:"labels"`
}

// ProposeNovelQuestion asks the LLM to invent a 3-option multiple-choice
// question that splits the supplied candidates.
func (c *Client) ProposeNovelQuestion(ctx context.Context, candidates []CandidateBrief, askedSummary []string) (*NovelQuestion, error) {
	if !c.Enabled() {
		return nil, ErrLLMDisabled
	}
	candJSON, _ := json.Marshal(candidates)
	prevJSON, _ := json.Marshal(askedSummary)

	system := "You design discriminating multiple-choice questions for an IPL cricketer guessing game. " +
		"You will receive: (a) the remaining candidate players with profile data, (b) questions already asked. " +
		"Your job is to invent ONE question that maximally splits the candidates. Rules:\n" +
		"- The question MUST be answerable by an average IPL fan.\n" +
		"- The question MUST NOT mention any specific player by name.\n" +
		"- The question MUST be substantively NEW vs. the asked list.\n" +
		"- Provide 3 options. Include an explicit 'don't know' as a 4th option in the engine, not in your response.\n" +
		"- For every candidate id supplied, label it with the option you'd expect them to map to.\n" +
		"Respond with ONLY a JSON object: " +
		`{"text": "...", "options": [{"id":"a","label":"..."},{"id":"b","label":"..."},{"id":"c","label":"..."}], "labels": {"player_id":"a", ...}}`

	user := fmt.Sprintf("Candidates: %s\nAlready asked: %s", candJSON, prevJSON)

	raw, err := c.chat(ctx, system, user)
	if err != nil {
		return nil, err
	}
	cleaned := stripCodeFence(raw)
	var nq NovelQuestion
	if err := json.Unmarshal([]byte(cleaned), &nq); err != nil {
		return nil, fmt.Errorf("parse novel question: %w (raw=%q)", err, raw)
	}
	if nq.Text == "" || len(nq.Options) == 0 {
		return nil, errors.New("empty novel question from llm")
	}
	if nq.Labels == nil {
		nq.Labels = map[string]string{}
	}
	return &nq, nil
}

// ResolvePlayerName uses the LLM to map a free-text user-typed player name to
// the closest candidate ID. Returns "" if no confident match. This is a fallback
// for when the vector store's semantic search is inconclusive.
func (c *Client) ResolvePlayerName(ctx context.Context, name string, candidates []CandidateBrief) (string, error) {
	if !c.Enabled() {
		return "", ErrLLMDisabled
	}
	candJSON, _ := json.Marshal(candidates)
	system := "You map a user-typed cricketer name to the closest IPL player id from a list. " +
		"Return ONLY the chosen id, or the literal string 'none' if no good match. No JSON, no preamble."
	user := fmt.Sprintf("User typed: %q\nCandidates: %s", name, candJSON)
	raw, err := c.chat(ctx, system, user)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(strings.ToLower(raw))
	if id == "none" || id == "" {
		return "", nil
	}
	return id, nil
}

// --- internal ---

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}
type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *Client) chat(ctx context.Context, system, user string) (string, error) {
	body, _ := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://ai-akinator.local")
	req.Header.Set("X-Title", "AI Akinator IPL")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("openrouter %d: %s", resp.StatusCode, string(raw))
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("openrouter: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", nil
	}
	return cr.Choices[0].Message.Content, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// stripCodeFence handles models that wrap JSON in ```json fences.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	const fence = "```"
	if i := strings.Index(s, fence); i >= 0 {
		s = s[i+len(fence):]
		if j := strings.Index(s, fence); j >= 0 {
			s = s[:j]
		}
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "json") {
		s = strings.TrimSpace(s[4:])
	}
	return s
}
