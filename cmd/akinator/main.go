// Command akinator is the console UI for the IPL Akinator. Pure Go, no
// browser, no RPC — the engine, dataset, and LLM client are all in-process.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/raveesh/ai-akinator/internal/data"
	"github.com/raveesh/ai-akinator/internal/engine"
	"github.com/raveesh/ai-akinator/internal/llm"
)

const banner = "" +
	"╔══════════════════════════════════════════════════════════════╗\n" +
	"║                                                              ║\n" +
	"║     🏏   I P L   A K I N A T O R   🎯                        ║\n" +
	"║                                                              ║\n" +
	"║     Think of any IPL player — past or present.               ║\n" +
	"║     I'll guess them in ≤ 8 questions. No hardcoded tree.     ║\n" +
	"║                                                              ║\n" +
	"╚══════════════════════════════════════════════════════════════╝\n"

const idleMascot = "" +
	"            .-\"\"\"\"-.\n" +
	"           /  ◕  ◕  \\\n" +
	"          |    ‿    |     \" Ready when you are. \"\n" +
	"           \\  \\_/   /\n" +
	"            '------'\n" +
	"               ||\n" +
	"             ──┴──   🏏\n"

func main() {
	players, err := data.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "load players:", err)
		os.Exit(1)
	}
	llmClient := llm.NewClient()
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<10), 1<<20)

	clearScreen()
	fmt.Print(banner)
	fmt.Print(idleMascot)
	fmt.Printf("\n  Loaded %d IPL players.  ", len(players))
	if llmClient.Enabled() {
		fmt.Println("[ LLM phrasing: ON ]")
	} else {
		fmt.Println("[ LLM phrasing: OFF — set OPENROUTER_API_KEY to enable ]")
	}
	fmt.Print("\n  Press Enter to begin… ")
	_ = in.Scan()

	for {
		runGame(in, players, llmClient)
		fmt.Print("\n  Play again? [y/N]: ")
		if !in.Scan() {
			break
		}
		if !strings.EqualFold(strings.TrimSpace(in.Text()), "y") {
			break
		}
	}
	clearScreen()
	fmt.Print(banner)
	fmt.Println("\n  ( ´ ▽ ` )ﾉ   Thanks for playing!")
}

func runGame(in *bufio.Scanner, players []data.Player, llmClient *llm.Client) {
	sess := engine.NewSession("local", players)
	for !sess.ShouldGuess() {
		q, gain, ok := sess.NextQuestion()
		if !ok {
			break
		}
		// No remaining question carries meaningful signal — stop and guess
		// with what we have rather than waste a turn.
		if sess.AskedCount() > 0 && gain < engine.MinInfoGainBits {
			break
		}
		askQuestion(in, sess, q, gain, llmClient)
	}
	finalGuess(in, sess)
}

func askQuestion(in *bufio.Scanner, sess *engine.Session, q engine.Question, gain float64, llmClient *llm.Client) {
	top := sess.Top(3)
	conf := 0.0
	if len(top) > 0 {
		conf = top[0].Probability
	}

	clearScreen()
	fmt.Print(banner)
	fmt.Println(mascotLine(conf))
	fmt.Println()

	text := phraseOrDefault(llmClient, q, top)
	fmt.Printf("  ❓  Q%d   %s\n", sess.AskedCount()+1, text)
	fmt.Printf("       ↳ info-gain ≈ %.3f bits\n\n", gain)

	for i, o := range q.Options {
		fmt.Printf("     [%d]  %s\n", i+1, o.Label)
	}
	fmt.Println()

	choice := readChoice(in, len(q.Options))
	sess.ApplyAnswer(q.ID, q.Options[choice-1].ID)
}

func finalGuess(in *bufio.Scanner, sess *engine.Session) {
	best := sess.BestGuess()
	top := sess.Top(5)

	clearScreen()
	fmt.Print(banner)
	fmt.Println()
	fmt.Println("       ╭───────────────────────────────────────╮")
	fmt.Println("       │   🎯  My guess:                       │")
	fmt.Println("       ╰───────────────────────────────────────╯")
	fmt.Println()
	fmt.Printf("              ┃  %s  ┃\n", best.Name)
	fmt.Printf("              confidence: %.1f%%\n\n", best.Probability*100)

	fmt.Println("  Top suspects after the interrogation:")
	for _, c := range top {
		bar := strings.Repeat("█", int(c.Probability*30+0.5))
		fmt.Printf("    %-28s %5.1f%%  %s\n", c.Name, c.Probability*100, bar)
	}
	fmt.Println()

	fmt.Print("  Was I right? [y/n]: ")
	if !in.Scan() {
		return
	}
	if strings.EqualFold(strings.TrimSpace(in.Text()), "y") {
		fmt.Println("\n       ╔════════════════════════════╗")
		fmt.Println("       ║   (•̀ᴗ•́)و    GOTCHA!       ║")
		fmt.Println("       ╚════════════════════════════╝")
		return
	}

	fmt.Print("  Aw. Who were you thinking of? ")
	if !in.Scan() {
		return
	}
	actual := strings.TrimSpace(in.Text())
	if actual != "" {
		appendFeedback(sess, best, actual)
		fmt.Println("\n       ╭──────────────────────────────╮")
		fmt.Println("       │   (╥﹏╥)   Logged for next time.  │")
		fmt.Println("       ╰──────────────────────────────╯")
	}
}

func phraseOrDefault(c *llm.Client, q engine.Question, top []engine.Candidate) string {
	if !c.Enabled() {
		return q.Text
	}
	opts := make([]string, 0, len(q.Options))
	for _, o := range q.Options {
		opts = append(opts, o.Label)
	}
	names := make([]string, 0, len(top))
	for _, t := range top {
		names = append(names, t.Name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	out, err := c.PhraseQuestion(ctx, q.Text, opts, names)
	if err != nil || strings.TrimSpace(out) == "" {
		return q.Text
	}
	return out
}

func mascotLine(confidence float64) string {
	face, mood := "( ◔_◔ )", "Tell me more…"
	switch {
	case confidence >= engine.ConfidenceThreshold:
		face, mood = "(•̀ᴗ•́)و", "I think I've got it!"
	case confidence >= 0.5:
		face, mood = "( ͡° ͜ʖ ͡°)", "Getting warmer…"
	case confidence >= 0.2:
		face, mood = "( ¬‿¬ )", "Narrowing it down…"
	}
	return fmt.Sprintf("    %s   \"%s\"", face, mood)
}

func readChoice(in *bufio.Scanner, max int) int {
	for {
		fmt.Printf("  Your answer [1-%d]: ", max)
		if !in.Scan() {
			os.Exit(0)
		}
		n, err := strconv.Atoi(strings.TrimSpace(in.Text()))
		if err == nil && n >= 1 && n <= max {
			return n
		}
		fmt.Printf("  ⚠  Enter a number between 1 and %d.\n", max)
	}
}

func appendFeedback(sess *engine.Session, guess engine.Candidate, actual string) {
	f, err := os.OpenFile("feedback.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	rec := struct {
		Time    string                 `json:"time"`
		Guess   string                 `json:"guess"`
		Actual  string                 `json:"actual"`
		History []engine.HistoryItem   `json:"history"`
	}{
		Time:    time.Now().Format(time.RFC3339),
		Guess:   guess.Name,
		Actual:  actual,
		History: sess.HistorySnapshot(),
	}
	_ = json.NewEncoder(f).Encode(&rec)
}

func clearScreen() {
	fmt.Print("\x1b[2J\x1b[H")
}
