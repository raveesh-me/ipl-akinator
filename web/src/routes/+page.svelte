<script lang="ts">
  import { akinator } from '$lib/client';
  import { Answer } from '$lib/gen/akinator/v1/akinator_pb';

  type Phase = 'idle' | 'asking' | 'guessing' | 'finished';

  let phase: Phase = $state('idle');
  let sessionId = $state('');
  let questionId = $state('');
  let questionText = $state('');
  let questionsRemaining = $state(0);
  let topCandidates = $state<{ name: string; probability: number }[]>([]);
  let guess = $state<{ name: string; confidence: number } | null>(null);
  let error = $state('');
  let busy = $state(false);

  async function start() {
    error = '';
    busy = true;
    try {
      const res = await akinator.startGame({});
      sessionId = res.sessionId;
      questionId = res.question?.questionId ?? '';
      questionText = res.question?.text ?? '';
      questionsRemaining = res.questionsRemaining;
      topCandidates = [];
      guess = null;
      phase = 'asking';
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }

  async function answer(a: Answer) {
    if (busy) return;
    busy = true;
    error = '';
    try {
      const res = await akinator.answerQuestion({
        sessionId,
        questionId,
        answer: a
      });
      questionsRemaining = res.questionsRemaining;
      topCandidates = res.topCandidates.map((c) => ({
        name: c.name,
        probability: c.probability
      }));
      if (res.finalGuess) {
        guess = { name: res.finalGuess.name, confidence: res.finalGuess.confidence };
        phase = 'finished';
      } else if (res.nextQuestion) {
        questionId = res.nextQuestion.questionId;
        questionText = res.nextQuestion.text;
      }
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }

  async function feedback(correct: boolean) {
    busy = true;
    try {
      await akinator.submitFeedback({
        sessionId,
        guessWasCorrect: correct,
        actualPlayerId: ''
      });
    } finally {
      busy = false;
      phase = 'idle';
    }
  }
</script>

<header>
  <h1>🏏 IPL Akinator</h1>
  <p class="subtitle">Think of any IPL player. I'll guess in 8 questions or fewer.</p>
</header>

{#if error}
  <div class="error">{error}</div>
{/if}

{#if phase === 'idle'}
  <button class="primary" onclick={start} disabled={busy}>Start Game</button>
{/if}

{#if phase === 'asking'}
  <div class="question-card">
    <div class="meta">Question · {8 - questionsRemaining + 1} of 8 max</div>
    <h2>{questionText}</h2>
    <div class="answers">
      <button onclick={() => answer(Answer.YES)} disabled={busy}>Yes</button>
      <button onclick={() => answer(Answer.NO)} disabled={busy}>No</button>
      <button onclick={() => answer(Answer.MAYBE)} disabled={busy}>Maybe</button>
      <button onclick={() => answer(Answer.DONT_KNOW)} disabled={busy}>Don't know</button>
    </div>
  </div>

  {#if topCandidates.length > 0}
    <details class="debug">
      <summary>Top candidates (debug)</summary>
      <ul>
        {#each topCandidates as c}
          <li>
            <span>{c.name}</span>
            <span class="prob">{(c.probability * 100).toFixed(1)}%</span>
          </li>
        {/each}
      </ul>
    </details>
  {/if}
{/if}

{#if phase === 'finished' && guess}
  <div class="guess-card">
    <h2>Are you thinking of…</h2>
    <p class="guess-name">{guess.name}?</p>
    <div class="confidence">Confidence: {(guess.confidence * 100).toFixed(0)}%</div>
    <div class="answers">
      <button onclick={() => feedback(true)} disabled={busy}>Yes! 🎯</button>
      <button onclick={() => feedback(false)} disabled={busy}>No, try again</button>
    </div>
  </div>
{/if}

<style>
  header h1 {
    font-size: 2rem;
    margin: 0 0 0.25rem;
  }
  .subtitle {
    color: #a8b2c0;
    margin-top: 0;
  }
  .error {
    background: #5a1f1f;
    padding: 0.75rem 1rem;
    border-radius: 6px;
    margin: 1rem 0;
  }
  .question-card,
  .guess-card {
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 12px;
    padding: 1.5rem;
    margin-top: 1.5rem;
  }
  .meta {
    color: #a8b2c0;
    font-size: 0.85rem;
    margin-bottom: 0.5rem;
  }
  h2 {
    margin: 0 0 1rem;
    font-weight: 500;
    line-height: 1.3;
  }
  .answers {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  button {
    background: #3d5a80;
    color: #fff;
    border: 0;
    border-radius: 8px;
    padding: 0.65rem 1.1rem;
    font-size: 0.95rem;
    cursor: pointer;
    transition: background 0.15s;
  }
  button:hover:not(:disabled) {
    background: #4a6f9d;
  }
  button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  button.primary {
    background: #ee6c4d;
    padding: 0.85rem 1.5rem;
    font-size: 1.05rem;
  }
  button.primary:hover:not(:disabled) {
    background: #f08068;
  }
  .guess-name {
    font-size: 2rem;
    margin: 0.5rem 0;
    font-weight: 600;
  }
  .confidence {
    color: #a8b2c0;
    margin-bottom: 1rem;
  }
  .debug {
    margin-top: 1rem;
    color: #a8b2c0;
    font-size: 0.9rem;
  }
  .debug ul {
    list-style: none;
    padding-left: 0;
  }
  .debug li {
    display: flex;
    justify-content: space-between;
    padding: 0.25rem 0;
  }
  .prob {
    color: #ee6c4d;
    font-variant-numeric: tabular-nums;
  }
</style>
