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

// DenouementText returns the line shown after an answer. When the typical error that matched has its own
// feedback, that message comes first. variant picks one of the lines (use the beat index plus the session seed).
func DenouementText(outcome, lang string, feedback LText, variant uint32) string {
	if outcome == OutcomeTypicalError {
		if fb := strings.TrimSpace(feedback.Get(lang)); fb != "" {
			return fb
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
