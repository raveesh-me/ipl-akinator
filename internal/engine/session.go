package engine

import (
	"sync"

	"github.com/raveesh/ai-akinator/internal/data"
)

// Session holds per-game state. Sessions are kept in-memory; for the hackathon
// this is sufficient. To horizontally scale, swap the Store for Firestore.
type Session struct {
	ID             string
	Beliefs        map[string]float64
	Players        map[string]data.Player
	Library        []Question
	libraryByID    map[string]Question
	Asked          map[string]bool
	History        []HistoryItem
	CategoryUsed   map[string]int
	QuestionsAsked int

	mu sync.Mutex
}

// HistoryItem records a Q/A pair for feedback / debugging / LLM context.
type HistoryItem struct {
	QuestionID    string
	QuestionText  string
	OptionID      string
	OptionLabel   string
}

// NewSession constructs a fresh game state from the player dataset.
func NewSession(id string, players []data.Player) *Session {
	pmap := make(map[string]data.Player, len(players))
	for _, p := range players {
		pmap[p.ID] = p
	}
	lib := BuildQuestions(players)
	libByID := make(map[string]Question, len(lib))
	for _, q := range lib {
		libByID[q.ID] = q
	}
	return &Session{
		ID:           id,
		Beliefs:      InitialBeliefs(players),
		Players:      pmap,
		Library:      lib,
		libraryByID:  libByID,
		Asked:        map[string]bool{},
		CategoryUsed: map[string]int{},
	}
}

// QuestionByID looks up a question from the library.
func (s *Session) QuestionByID(id string) (Question, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.libraryByID[id]
	return q, ok
}

// AddNovelQuestion injects an LLM-generated question into this session's
// library so the engine treats it as a first-class candidate.
func (s *Session) AddNovelQuestion(q Question) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.libraryByID[q.ID] = q
	s.Library = append(s.Library, q)
}

// NextQuestion picks the highest expected-info-gain unasked question.
func (s *Session) NextQuestion() (Question, float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SelectNextQuestion(s.Beliefs, s.Players, s.Library, s.Asked, s.CategoryUsed)
}

// ApplyAnswer updates the belief state and records the chosen option.
func (s *Session) ApplyAnswer(qID, optID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.libraryByID[qID]
	if !ok {
		return false
	}
	Update(s.Beliefs, s.Players, q, optID)
	s.Asked[qID] = true
	s.CategoryUsed[q.Category]++
	s.QuestionsAsked++

	var label string
	for _, o := range q.Options {
		if o.ID == optID {
			label = o.Label
			break
		}
	}
	s.History = append(s.History, HistoryItem{
		QuestionID:   qID,
		QuestionText: q.Text,
		OptionID:     optID,
		OptionLabel:  label,
	})
	return true
}

// BestGuess returns the highest-probability candidate.
func (s *Session) BestGuess() Candidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	top := TopK(s.Beliefs, s.Players, 1)
	if len(top) == 0 {
		return Candidate{}
	}
	return top[0]
}

// Top returns the top-K candidates (snapshot).
func (s *Session) Top(k int) []Candidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	return TopK(s.Beliefs, s.Players, k)
}

// ShouldGuess: confidence threshold OR budget exhausted.
func (s *Session) ShouldGuess() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.QuestionsAsked >= MaxQuestions {
		return true
	}
	top := TopK(s.Beliefs, s.Players, 1)
	if len(top) == 0 {
		return true
	}
	return top[0].Probability >= ConfidenceThreshold
}

// QuestionsRemaining returns the budget left.
func (s *Session) QuestionsRemaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := MaxQuestions - s.QuestionsAsked
	if r < 0 {
		return 0
	}
	return r
}

// Pool returns the player dataset for this session. The map is read-only;
// callers must not mutate it.
func (s *Session) Pool() map[string]data.Player {
	return s.Players
}

// HistorySnapshot returns a copy of the Q/A trail for LLM/feedback context.
func (s *Session) HistorySnapshot() []HistoryItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HistoryItem, len(s.History))
	copy(out, s.History)
	return out
}

// Store is a tiny in-memory session registry.
type Store struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewStore() *Store {
	return &Store{sessions: map[string]*Session{}}
}

func (s *Store) Put(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
}

func (s *Store) Get(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}
