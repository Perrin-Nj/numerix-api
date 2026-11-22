package app

// 3D scene delivery: a bank question may carry a scene spec (bank_questions.scene). The question phase gets the part without any
// answer-bound value (Question.scene); the reveal part (AnswerResult.scene_reveal) leaves only with a released answer.

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/Perrin-Nj/numerix-api/seed"
)

// approvedScenes lists the signed-off question ids (a var so tests can set it).
var approvedScenes = seed.SceneApproved()

// sceneParts returns the question and reveal parts of a question's scene, or nils when it has none, is not approved or the flag is off.
func (s *Server) sceneParts(ctx context.Context, id string) (question, reveal map[string]any) {
	if !approvedScenes[id] || !s.plConfig(ctx).ScenesEnabled {
		return nil, nil
	}
	var raw sql.NullString
	if err := s.Store.R.QueryRowContext(ctx, `SELECT scene FROM bank_questions WHERE id = ?`, id).Scan(&raw); err != nil || !raw.Valid {
		return nil, nil
	}
	var doc struct {
		Question map[string]any `json:"question"`
		Reveal   map[string]any `json:"reveal"`
	}
	if json.Unmarshal([]byte(raw.String), &doc) != nil || doc.Question == nil || doc.Reveal == nil {
		return nil, nil
	}
	return doc.Question, doc.Reveal
}
