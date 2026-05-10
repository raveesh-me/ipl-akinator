// Package server hosts the ConnectRPC handlers for AkinatorService. It is the
// thin glue between the proto contract and the probabilistic engine.
package server

import (
	"context"
	"errors"
	"log"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	akinatorv1 "github.com/raveesh/ai-akinator/gen/akinator/v1"
	"github.com/raveesh/ai-akinator/gen/akinator/v1/akinatorv1connect"
	"github.com/raveesh/ai-akinator/internal/data"
	"github.com/raveesh/ai-akinator/internal/engine"
	"github.com/raveesh/ai-akinator/internal/llm"
)

// Server implements akinatorv1connect.AkinatorServiceHandler.
type Server struct {
	akinatorv1connect.UnimplementedAkinatorServiceHandler

	Players []data.Player
	Store   *engine.Store
	LLM     *llm.Client
}

// New wires up dependencies. The dataset is loaded once at boot and shared
// across sessions (read-only).
func New(players []data.Player) *Server {
	return &Server{
		Players: players,
		Store:   engine.NewStore(),
		LLM:     llm.NewClient(),
	}
}

func (s *Server) StartGame(
	ctx context.Context,
	_ *connect.Request[akinatorv1.StartGameRequest],
) (*connect.Response[akinatorv1.StartGameResponse], error) {
	sess := engine.NewSession(uuid.NewString(), s.Players)
	s.Store.Put(sess)

	q, _, ok := sess.NextQuestion()
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("no questions available"))
	}
	text := s.phrase(ctx, sess, q)

	return connect.NewResponse(&akinatorv1.StartGameResponse{
		SessionId: sess.ID,
		Question: &akinatorv1.Question{
			QuestionId: q.ID,
			Text:       text,
		},
		QuestionsRemaining: int32(sess.QuestionsRemaining()),
	}), nil
}

func (s *Server) AnswerQuestion(
	ctx context.Context,
	req *connect.Request[akinatorv1.AnswerQuestionRequest],
) (*connect.Response[akinatorv1.AnswerQuestionResponse], error) {
	sess, ok := s.Store.Get(req.Msg.SessionId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("session not found"))
	}
	if !sess.ApplyAnswer(req.Msg.QuestionId, fromProtoAnswer(req.Msg.Answer)) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown question id"))
	}

	resp := &akinatorv1.AnswerQuestionResponse{
		QuestionsRemaining: int32(sess.QuestionsRemaining()),
		TopCandidates:      toProtoCandidates(sess.Top(5)),
	}

	if sess.ShouldGuess() {
		best := sess.BestGuess()
		resp.FinalGuess = &akinatorv1.FinalGuess{
			PlayerId:   best.ID,
			Name:       best.Name,
			Confidence: best.Probability,
		}
		return connect.NewResponse(resp), nil
	}

	q, _, ok := sess.NextQuestion()
	if !ok {
		// Out of questions to ask — fall back to best guess.
		best := sess.BestGuess()
		resp.FinalGuess = &akinatorv1.FinalGuess{
			PlayerId:   best.ID,
			Name:       best.Name,
			Confidence: best.Probability,
		}
		return connect.NewResponse(resp), nil
	}
	resp.NextQuestion = &akinatorv1.Question{
		QuestionId: q.ID,
		Text:       s.phrase(ctx, sess, q),
	}
	return connect.NewResponse(resp), nil
}

func (s *Server) SubmitFeedback(
	_ context.Context,
	req *connect.Request[akinatorv1.SubmitFeedbackRequest],
) (*connect.Response[akinatorv1.SubmitFeedbackResponse], error) {
	sess, ok := s.Store.Get(req.Msg.SessionId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("session not found"))
	}
	// Hackathon-grade learning hook: just log for now. Wire to a feedback store
	// (Firestore / SQLite) and adjust per-feature weights for future sessions.
	log.Printf(
		"feedback: session=%s correct=%v actual=%s history=%d",
		sess.ID, req.Msg.GuessWasCorrect, req.Msg.ActualPlayerId, len(sess.History),
	)
	s.Store.Delete(sess.ID)
	return connect.NewResponse(&akinatorv1.SubmitFeedbackResponse{}), nil
}

// phrase asks the LLM to make the question more conversational. Best-effort:
// errors fall back to the engine's neutral text so the game never stalls.
func (s *Server) phrase(ctx context.Context, sess *engine.Session, q engine.Question) string {
	if !s.LLM.Enabled() {
		return q.Text
	}
	top := sess.Top(3)
	names := make([]string, 0, len(top))
	for _, c := range top {
		names = append(names, c.Name)
	}
	out, err := s.LLM.PhraseQuestion(ctx, q.Text, names)
	if err != nil {
		log.Printf("llm phrase error: %v", err)
		return q.Text
	}
	return out
}

func fromProtoAnswer(a akinatorv1.Answer) engine.Answer {
	switch a {
	case akinatorv1.Answer_ANSWER_YES:
		return engine.AnswerYes
	case akinatorv1.Answer_ANSWER_NO:
		return engine.AnswerNo
	case akinatorv1.Answer_ANSWER_MAYBE:
		return engine.AnswerMaybe
	case akinatorv1.Answer_ANSWER_DONT_KNOW:
		return engine.AnswerDontKnow
	default:
		return engine.AnswerUnknown
	}
}

func toProtoCandidates(cs []engine.Candidate) []*akinatorv1.Candidate {
	out := make([]*akinatorv1.Candidate, 0, len(cs))
	for _, c := range cs {
		out = append(out, &akinatorv1.Candidate{
			PlayerId:    c.ID,
			Name:        c.Name,
			Probability: c.Probability,
		})
	}
	return out
}
