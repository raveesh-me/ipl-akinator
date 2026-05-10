package engine

import (
	"sync"

	"github.com/raveesh/ai-akinator/internal/data"
)

// Session holds per-game state. Sessions are kept in-memory; for the hackathon
// this is sufficient. To horizontally scale, swap the SessionStore for Firestore.
type Session struct {
	ID            string
	Beliefs       map[string]float64
	Players       map[string]data.Player
	Library       []Question
	libraryByID   map[string]Question
	Asked         map[string]bool
	Answers       map[string]Answer
	History       []HistoryItem
	CategoryUsed  map[string]int
	QuestionsAsked int

	mu sync.Mutex
}

// HistoryItem records a Q/A pair for feedback / debugging.
type HistoryItem struct {
	QuestionID   string
	QuestionText string
	Answer       Answer
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
		Answers:      map[string]Answer{},
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

// NextQuestion picks the highest expected-info-gain unasked question.
func (s *Session) NextQuestion() (Question, float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SelectNextQuestion(s.Beliefs, s.Players, s.Library, s.Asked, s.CategoryUsed)
}

// ApplyAnswer updates the belief state and records the answer.
func (s *Session) ApplyAnswer(qID string, ans Answer) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.libraryByID[qID]
	if !ok {
		return false
	}
	Update(s.Beliefs, s.Players, q, ans)
	s.Asked[qID] = true
	s.Answers[qID] = ans
	s.CategoryUsed[q.Category]++
	s.QuestionsAsked++
	s.History = append(s.History, HistoryItem{
		QuestionID:   qID,
		QuestionText: q.Text,
		Answer:       ans,
	})
	return true
}

// BestGuess returns the highest-probability candidate and its confidence.
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

// ShouldGuess returns true when the engine has reached confidence or runs out
// of question budget.
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

// QuestionsRemaining is the budget left.
func (s *Session) QuestionsRemaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := MaxQuestions - s.QuestionsAsked
	if r < 0 {
		return 0
	}
	return r
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
