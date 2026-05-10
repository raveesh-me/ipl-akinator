// Package llm wraps a thin client for Gemini. Two responsibilities:
//
//  1. Phrase a chosen feature-question naturally (so the engine never ships raw
//     IDs like "team_CSK" to the user).
//  2. When the engine's static feature library can no longer discriminate the
//     remaining top candidates, ask Gemini to propose a *novel* yes/no question
//     that splits them — mapped back to a per-player Yes-probability.
//
// If GEMINI_API_KEY is unset the client returns deterministic fallbacks so the
// rest of the system stays functional offline.
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
	"time"
)

const (
	defaultModel    = "gemini-2.0-flash"
	endpointPattern = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s"
)

// Client talks to Gemini over HTTP.
type Client struct {
	apiKey string
	model  string
	http   *http.Client
}

// NewClient reads GEMINI_API_KEY from the environment. If empty, the client
// runs in offline-fallback mode.
func NewClient() *Client {
	return &Client{
		apiKey: os.Getenv("GEMINI_API_KEY"),
		model:  envOr("GEMINI_MODEL", defaultModel),
		http:   &http.Client{Timeout: 12 * time.Second},
	}
}

// Enabled reports whether a real API call will be made.
func (c *Client) Enabled() bool { return c.apiKey != "" }

// PhraseQuestion rewrites the engine's neutral question text into something
// more conversational. On error, returns the original text untouched.
func (c *Client) PhraseQuestion(ctx context.Context, base string, topCandidates []string) (string, error) {
	if !c.Enabled() {
		return base, nil
	}
	prompt := fmt.Sprintf(
		"You are an Akinator-style guide for an IPL cricketer guessing game. "+
			"Rewrite this question so it feels natural and engaging, but keep its meaning unchanged. "+
			"Reply with ONLY the rewritten question, no preamble.\n\n"+
			"Question: %q\n"+
			"Hint — current top candidates: %v",
		base, topCandidates,
	)
	out, err := c.generate(ctx, prompt)
	if err != nil || out == "" {
		return base, err
	}
	return out, nil
}

// NovelQuestion is what we get back when asking Gemini to invent a discriminator.
type NovelQuestion struct {
	Text          string             `json:"text"`
	YesPlayerIDs  []string           `json:"yes_player_ids"`
	NoPlayerIDs   []string           `json:"no_player_ids"`
	Confidence    float64            `json:"confidence"`
}

// ProposeNovelQuestion asks Gemini to invent a yes/no question that splits the
// supplied candidates and to label each candidate's expected answer. The engine
// then turns those labels into a Predicate and treats the question as a
// first-class library entry for this session.
func (c *Client) ProposeNovelQuestion(ctx context.Context, candidates []CandidateBrief) (*NovelQuestion, error) {
	if !c.Enabled() {
		return nil, errors.New("llm disabled")
	}
	candJSON, _ := json.Marshal(candidates)
	prompt := fmt.Sprintf(
		`You help an IPL cricket guessing game. Below are the remaining candidate players. `+
			`Invent ONE yes/no question that maximally splits them — the question MUST be answerable by a normal IPL fan, `+
			`MUST NOT mention any player by name, and MUST NOT repeat the obvious facts already given. `+
			`Then partition the candidates by expected answer.`+"\n\n"+
			`Candidates: %s`+"\n\n"+
			`Respond with ONLY a JSON object of the form: `+
			`{"text": "...", "yes_player_ids": ["..."], "no_player_ids": ["..."], "confidence": 0.0-1.0}`,
		string(candJSON),
	)
	raw, err := c.generate(ctx, prompt)
	if err != nil {
		return nil, err
	}
	var nq NovelQuestion
	if err := json.Unmarshal([]byte(stripCodeFence(raw)), &nq); err != nil {
		return nil, fmt.Errorf("parse novel question: %w (raw=%q)", err, raw)
	}
	if nq.Text == "" {
		return nil, errors.New("empty novel question")
	}
	return &nq, nil
}

// CandidateBrief is the projection passed to the LLM. We keep it small so the
// prompt stays cheap and on-topic.
type CandidateBrief struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Role        string  `json:"role"`
	Nationality string  `json:"nationality"`
	Teams       []string `json:"teams"`
	Probability float64 `json:"probability"`
}

// --- internal ---

type generatePart struct {
	Text string `json:"text"`
}
type generateContent struct {
	Parts []generatePart `json:"parts"`
}
type generateRequest struct {
	Contents []generateContent `json:"contents"`
}
type generateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []generatePart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func (c *Client) generate(ctx context.Context, prompt string) (string, error) {
	body, _ := json.Marshal(generateRequest{
		Contents: []generateContent{{Parts: []generatePart{{Text: prompt}}}},
	})
	url := fmt.Sprintf(endpointPattern, c.model, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("gemini %d: %s", resp.StatusCode, string(raw))
	}
	var gr generateResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", nil
	}
	return gr.Candidates[0].Content.Parts[0].Text, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// stripCodeFence handles Gemini occasionally wrapping JSON in ```json fences.
func stripCodeFence(s string) string {
	const fence = "```"
	if i := indexOf(s, fence); i >= 0 {
		s = s[i+len(fence):]
		if j := indexOf(s, fence); j >= 0 {
			s = s[:j]
		}
	}
	// Drop a leading "json" tag if present.
	if len(s) > 4 && (s[:4] == "json" || s[:4] == "JSON") {
		s = s[4:]
	}
	return s
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
