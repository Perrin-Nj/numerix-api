package content

// Neutral encouragement texts for curated questions. A curated question has no language model behind it, so the
// text shown after an answer comes from the question itself (the message written for a typical error) or from
// these short lines. They contain no number, never say the child failed, and never create time pressure.

import "strings"

var denouementLines = map[string][2][]string{
	OutcomeCorrect: {
		{"Bravo, tu as trouvé ! L’aventure continue.", "Bien joué, ton calcul fait avancer l’histoire.", "Super, tu as réussi ! On passe à la suite."},
		{"Well done, you found it! The adventure goes on.", "Nicely done, your maths moves the story forward.", "Great, you did it! Let’s go on."},
	},
	OutcomeTypicalError: {
		{"Pas tout à fait, mais tu y étais presque. On continue ensemble.", "Un petit détour dans l’histoire. Regardons ça ensemble.", "Tu as bien cherché. La prochaine fois sera la bonne."},
		{"Not quite, but you were close. Let’s go on together.", "A little detour in the story. Let’s look at it together.", "You searched well. Next time will be the one."},
	},
	OutcomeRandomError: {
		{"Ce n’est pas grave, on regarde ça ensemble et on avance.", "Un imprévu dans l’histoire. Respire, on continue.", "On essaie une autre idée à la prochaine question."},
		{"No worries, we will look at it together and move on.", "A surprise in the story. Take a breath, we go on.", "We will try another idea on the next question."},
	},
}

// DenouementText returns the line shown after an answer. The line is about THIS question. A right answer gets the
// reaction written for it (or, without one, the explanation), a typical error gets its own feedback, any other
// miss gets the written reaction (or the hint). Only a question with none of these falls back to a neutral line.
// variant picks one of the neutral lines (use the beat index plus the session seed).
func DenouementText(outcome, lang string, q *Question, feedback LText, variant uint32) string {
	yes, close, tip := "Oui ! ", "Pas tout à fait. ", "Un indice : "
	if lang == "en" {
		yes, close, tip = "Yes! ", "Not quite. ", "A hint: "
	}
	var right, wrong LText // the written reaction, empty when the question has none
	if q.Reaction != nil {
		right, wrong = q.Reaction.Right, q.Reaction.Wrong
	}
	switch outcome {
	case OutcomeCorrect:
		if r := strings.TrimSpace(right.Get(lang)); r != "" {
			return r
		}
		if ex := strings.TrimSpace(q.Explain.Get(lang)); ex != "" {
			return yes + ex
		}
	case OutcomeTypicalError:
		if fb := strings.TrimSpace(feedback.Get(lang)); fb != "" {
			return close + fb
		}
		fallthrough // no feedback written for this error: treat it like any other miss
	default:
		if w := strings.TrimSpace(wrong.Get(lang)); w != "" {
			return w
		}
		if h := strings.TrimSpace(q.Hint.Get(lang)); h != "" {
			return close + tip + h
		}
	}
	lines, ok := denouementLines[outcome]
	if !ok {
		lines = denouementLines[OutcomeRandomError]
	}
	li := 0
	if lang == "en" {
		li = 1
	}
	pool := lines[li]
	return pool[int(variant)%len(pool)]
}
