package seedload

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/seed"
)

// legacyWorlds are the 8 worlds that existed before the content programme; their colours are kept as they were
// (some are below the 4.5:1 contrast the 12 new worlds respect).
var legacyWorlds = []string{"nature", "espace", "cuisine", "sport", "construction", "medecine", "science", "echecs"}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Contrast returns the WCAG contrast ratio of a #RRGGBB colour against white text.
func Contrast(hex string) float64 {
	lum := func(h string) float64 {
		c, _ := strconv.ParseUint(h, 16, 8)
		v := float64(c) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	l := 0.2126*lum(hex[1:3]) + 0.7152*lum(hex[3:5]) + 0.0722*lum(hex[5:7])
	return 1.05 / (l + 0.05)
}

// WordLists returns the embedded deny and names lists folded for matching, plus any seed/schema additions.
func (f *Files) wordLists() ([]string, map[string]bool) {
	deny := []string{}
	for _, w := range seed.Denylist() {
		deny = append(deny, content.Fold(w))
	}
	deny = append(deny, f.Denylist...)
	names := map[string]bool{}
	for _, n := range seed.Names() {
		names[content.Fold(n)] = true
	}
	for n := range f.Names {
		names[n] = true
	}
	return deny, names
}

// Env builds the validator environment for a seed directory.
func (f *Files) Env(g *engine.Graph) *content.Env {
	quests := map[string]content.Quest{}
	for _, q := range f.Quests {
		quests[q.ID] = q
	}
	codes := map[string]bool{}
	for _, m := range CurriculumMisconceptions(g) {
		codes[m.Code] = true
	}
	for _, m := range f.Misconception {
		codes[m.Code] = true
	}
	assets := map[string]bool{}
	for _, a := range f.Assets.Assets {
		assets[a.ID] = true
		if a.Alias != "" {
			assets[a.Alias] = true
		}
	}
	env := &content.Env{
		Skill: func(id string) (content.SkillInfo, bool) {
			sk, err := g.Get(id)
			if err != nil {
				return content.SkillInfo{}, false
			}
			return content.SkillInfo{YearIntro: sk.YearIntro}, true
		},
		Quest: func(id string) (content.QuestInfo, bool) {
			q, ok := quests[id]
			return content.QuestInfo{World: q.World, Skill: q.Skill, GradeMin: q.GradeMin, GradeMax: q.GradeMax, TargetCount: q.TargetCount}, ok
		},
		Misconception: func(c string) bool { return codes[c] },
		Asset:         func(ref string) bool { return assets[ref] },
		SeedFile:      true,
	}
	env.Denylist, env.Names = f.wordLists()
	if len(f.Assets.Vocab) > 0 {
		vocab := map[[2]string]bool{}
		for _, v := range f.Assets.Vocab {
			vocab[[2]string{v.Facet, v.Value}] = true
		}
		env.Vocab = func(facet, v string) bool { return vocab[[2]string{facet, v}] }
	}
	return env
}

// Validate runs every rule over the whole seed and returns the issues. Field names start with the id of the object.
func (f *Files) Validate(g *engine.Graph) []content.Issue {
	var out []content.Issue
	add := func(code, id, msg string, warn bool) {
		out = append(out, content.Issue{Code: code, Field: id, Message: msg, Warning: warn})
	}
	// worlds
	worlds := map[string]content.World{}
	for _, w := range f.Worlds {
		if _, dup := worlds[w.ID]; dup {
			add("world_duplicate", w.ID, "world id appears twice", false)
		}
		worlds[w.ID] = w
		if w.Name.FR == "" || w.Name.EN == "" || w.Tagline.FR == "" || w.Tagline.EN == "" || w.Hook.FR == "" || w.Hook.EN == "" {
			add("world_text", w.ID, "name, tagline and hook are required in fr and en", false)
		}
		if !colorRe.MatchString(w.AccentColor) {
			add("world_color", w.ID, "accent_color must look like #RRGGBB", false)
		} else if c := Contrast(w.AccentColor); c < 4.5 {
			add("world_contrast", w.ID, fmt.Sprintf("contrast with white is %.2f, below 4.5", c), slices.Contains(legacyWorlds, w.ID))
		}
		if w.GradeMin < 1 || w.GradeMax > 6 || w.GradeMin > w.GradeMax {
			add("world_grades", w.ID, "grade range must satisfy 1 <= grade_min <= grade_max <= 6", false)
		}
		for _, t := range []string{w.Name.FR, w.Name.EN, w.Tagline.FR, w.Tagline.EN, w.Hook.FR, w.Hook.EN} {
			if strings.ContainsAny(t, "—–") {
				add("world_dash", w.ID, "no em dash or en dash in kid text", false)
			}
		}
	}
	// quests
	quests := map[string]content.Quest{}
	ordSeen := map[string]bool{}
	for _, q := range f.Quests {
		if _, dup := quests[q.ID]; dup {
			add("quest_duplicate", q.ID, "quest id appears twice", false)
		}
		quests[q.ID] = q
		w, ok := worlds[q.World]
		switch {
		case !ok:
			add("quest_world", q.ID, "world "+q.World+" does not exist", false)
		case !strings.HasPrefix(q.ID, q.World+"."):
			add("quest_id_prefix", q.ID, "quest id must start with its world id", false)
		case q.GradeMin < w.GradeMin || q.GradeMax > w.GradeMax:
			add("quest_world_grades", q.ID, "quest grades must sit inside the world grades", true)
		}
		k := q.World + "#" + strconv.Itoa(q.Ord)
		if ordSeen[k] {
			add("quest_ord", q.ID, "two quests of the world share ord "+strconv.Itoa(q.Ord), false)
		}
		ordSeen[k] = true
		sk, err := g.Get(q.Skill)
		switch {
		case err != nil:
			add("quest_skill", q.ID, "skill "+q.Skill+" is not in the curriculum", false)
		case q.GradeMin < sk.YearIntro:
			add("quest_grade_skill", q.ID, fmt.Sprintf("grade_min %d is before the skill is introduced (year %d)", q.GradeMin, sk.YearIntro), false)
		}
		if q.TargetCount < 1 || q.GradeMin < 1 || q.GradeMax > 6 || q.GradeMin > q.GradeMax {
			add("quest_numbers", q.ID, "target_count >= 1 and 1 <= grade_min <= grade_max <= 6 are required", false)
		}
		if q.Name.FR == "" || q.Name.EN == "" || q.Goal.FR == "" || q.Goal.EN == "" {
			add("quest_text", q.ID, "name and goal are required in fr and en", false)
		}
		for _, t := range []string{q.Name.FR, q.Name.EN, q.Goal.FR, q.Goal.EN} {
			if strings.ContainsAny(t, "—–") {
				add("quest_dash", q.ID, "no em dash or en dash in kid text", false)
			}
		}
	}
	// misconception codes unique
	seenCode := map[string]bool{}
	for _, m := range f.Misconception {
		if seenCode[m.Code] {
			add("misconception_duplicate", m.Code, "code appears twice", false)
		}
		seenCode[m.Code] = true
	}
	// questions
	env := f.Env(g)
	var all []*content.Question
	for _, qf := range f.QuestionFiles {
		for _, q := range qf.Questions {
			all = append(all, q)
			if qf.World != ExamplesFile && !strings.HasPrefix(q.ID, qf.World+".") {
				add("file_world", q.ID, fmt.Sprintf("question is in the %s file but its id names another world", qf.World), false)
			}
			if qf.World != ExamplesFile {
				if w, ok := quests[q.Quest]; ok && w.World != qf.World {
					add("file_world", q.ID, "quest belongs to another world than the file", false)
				}
			}
			for _, is := range env.Validate(q) {
				is.Field = q.ID + "#" + is.Field
				out = append(out, is)
			}
		}
		if qf.World != ExamplesFile {
			for _, is := range env.WorldQuota(qf.World, qf.Questions, f.Quests) {
				is.Field = qf.Name + ":" + is.Field
				out = append(out, is)
			}
		}
	}
	out = append(out, env.ValidateSet(all)...)
	return out
}
