package app

// Release QA F1 to F3: a simulation over the three focus worlds and grades 1 to 4 on the real seed. Children play
// whole chapters with perfect, mixed and random answers, some after mastering skills in the other worlds. The planner
// must never leave a child at a dead end, never serve a school year above the child's, never serve a bare drill from
// grade 3 on (beyond the review share), never repeat a question inside a chapter, and the journey must agree with
// what the planner serves.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/seedload"
)

// seedFull loads the real worlds, quests and questions (no asset files) into the fixture database.
func seedFull(t *testing.T, f feedFixture) {
	t.Helper()
	files, err := seedload.Read(testSeedDir, seedload.ReadOptions{SkipAssets: true, Exclude: []string{seedload.ExamplesFile}})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range files.Questions() {
		q.Media = nil // the asset files are not loaded here, so a picture reference could not resolve
	}
	if _, err := seedload.Apply(t.Context(), f.s.Content, f.s.Assets, f.s.game().graph, files, seedload.Options{Actor: "test"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

type kid struct {
	id, tok string
	grade   int
}

func (f feedFixture) newKid(t *testing.T, grade int) kid {
	t.Helper()
	id := uuid.NewString()
	f.sql(t, `INSERT INTO children (id, parent_id, display_name, timezone, grade) VALUES (?, 'p-1', 'K', 'UTC', ?)`, id, grade)
	f.sql(t, `INSERT INTO consents (id, parent_id, child_id, notice_version) VALUES (?, 'p-1', ?, 'v1')`, uuid.NewString(), id)
	tok, _, err := f.s.Issuer.IssueChild("p-1", id)
	if err != nil {
		t.Fatal(err)
	}
	return kid{id, tok, grade}
}

// answerFor builds the right answer of the open beat, or a wrong one.
func (f feedFixture) answerFor(t *testing.T, beatID string, right bool) any {
	t.Helper()
	if !right {
		return "no such answer"
	}
	p := f.problemOf(t, beatID)
	if len(p.Curated) > 0 {
		var q content.Question
		if err := json.Unmarshal(p.Curated, &q); err != nil {
			t.Fatal(err)
		}
		return json.RawMessage(q.CorrectAnswer("fr"))
	}
	return p.Reponse
}

type simStats struct {
	beats, drills, above, noQuestion, startFail int
	years                                       map[int]int
	problems                                    []string
}

func (st *simStats) note(format string, a ...any) {
	if len(st.problems) < 12 {
		st.problems = append(st.problems, fmt.Sprintf(format, a...))
	}
}

// chapter plays one chapter for k in world with a skill chosen by the planner; mode is perfect, mixed or random.
func (f feedFixture) chapter(t *testing.T, k kid, world, mode, label string, st *simStats) {
	t.Helper()
	body := fmt.Sprintf(`{"world":%q,"kind":"chapter"}`, world)
	code, sess, raw := f.gcall(t, "POST", "/v1/children/"+k.id+"/sessions", k.tok, body, nil)
	if code != 201 {
		st.startFail++
		st.note("%s g%d %s: start session %d %s", label, k.grade, world, code, raw)
		return
	}
	sid := sess["id"].(string)
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		code, beat, raw := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", k.tok, "{}", nil)
		if code != 201 && code != 200 {
			if strings.Contains(raw, "no_question_available") {
				st.noQuestion++
			}
			var skill string
			_ = f.s.Store.R.QueryRow(`SELECT json_extract(json,'$.skill_id') || '/' || json_extract(json,'$.reason') FROM sessions WHERE id = ?`, sid).Scan(&skill)
			st.note("%s g%d %s beat %d (skill %s): %d", label, k.grade, world, i, skill, code)
			return
		}
		beatID := beat["id"].(string)
		st.beats++
		p := f.problemOf(t, beatID)
		if len(p.Curated) > 0 {
			var q content.Question
			_ = json.Unmarshal(p.Curated, &q)
			if seen[q.ID] {
				st.note("%s g%d %s: %s repeated inside one chapter", label, k.grade, world, q.ID)
			}
			seen[q.ID] = true
			st.years[q.Grade]++
			if q.Grade > k.grade {
				st.above++
				st.note("%s g%d %s: %s is grade %d content", label, k.grade, world, q.ID, q.Grade)
			}
			if content.DerivedStyle(q.Skill, q.Grade, q.Style) == content.StyleDrill && k.grade >= 3 {
				st.drills++
			}
		}
		right := mode == "perfect" || (mode == "mixed" && i%2 == 0)
		if c, _, raw := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+"/answer", k.tok, answerJSON(f.answerFor(t, beatID, right)), map[string]string{"Idempotency-Key": uuid.NewString()}); c != 200 {
			st.note("%s g%d %s answer: %d %s", label, k.grade, world, c, raw)
			return
		}
	}
}

func answerJSON(v any) string {
	b, _ := json.Marshal(map[string]any{"answer": v})
	return string(b)
}

var focus = []string{"echecs", "medecine", "espace"}

func TestPlannerNeverDeadEndsAcrossWorldsAndGrades(t *testing.T) {
	if testing.Short() {
		t.Skip("simulation")
	}
	fastClock(t)
	f := newFeedFixture(t, true)
	f.setPlatform(t, func(p *appconfig.Platform) { p.GradeWindow = true })
	seedFull(t, f)
	total := &simStats{years: map[int]int{}}
	for grade := 1; grade <= 4; grade++ {
		for _, world := range focus {
			if f.gapCell(t, world, grade) { // a content gap: the world is closed at this grade, cleanly
				k := f.newKid(t, grade)
				code, out, _ := f.gcall(t, "POST", "/v1/children/"+k.id+"/sessions", k.tok, fmt.Sprintf(`{"world":%q,"kind":"chapter"}`, world), nil)
				if code != 422 {
					t.Errorf("gap cell g%d %s: start session %d %v, want a clean 422", grade, world, code, out)
				}
				t.Logf("data gap: %s has no playable content at grade %d", world, grade)
				continue
			}
			for _, mode := range []string{"perfect", "mixed", "random"} {
				for _, cross := range []bool{false, true} {
					k := f.newKid(t, grade)
					label := fmt.Sprintf("%s/cross=%v", mode, cross)
					st := &simStats{years: map[int]int{}}
					if cross { // master skills in the other worlds first, like a child who has played elsewhere
						for _, other := range focus {
							if other != world && !f.gapCell(t, other, grade) {
								for c := 0; c < 3; c++ {
									f.chapter(t, k, other, "perfect", "warmup", st)
								}
							}
						}
					}
					st.beats, st.drills, st.above = 0, 0, 0
					for c := 0; c < 8; c++ { // 40 beats in the world under test
						f.chapter(t, k, world, mode, label, st)
					}
					for _, p := range st.problems {
						total.problems = append(total.problems, p)
					}
					total.noQuestion += st.noQuestion
					total.startFail += st.startFail
					total.above += st.above
					if k.grade == 4 && st.drills > 0 {
						t.Errorf("%s grade 4 %s: %d drills", label, world, st.drills)
					}
					if k.grade == 3 && st.beats > 0 && st.drills*100 > st.beats*15 {
						t.Errorf("%s grade 3 %s: %d of %d beats are drills (more than the review share)", label, world, st.drills, st.beats)
					}
					if st.beats < 40 {
						t.Errorf("%s grade %d %s: only %d of 40 beats played", label, k.grade, world, st.beats)
					}
				}
			}
		}
	}
	if total.noQuestion+total.startFail+total.above > 0 || len(total.problems) > 0 {
		t.Fatalf("no_question_available=%d start failures=%d above-grade=%d\n%s", total.noQuestion, total.startFail, total.above, strings.Join(total.problems, "\n"))
	}
}

// The journey and the planner agree: every node a child may start (not locked) starts a chapter that serves a beat.
func TestJourneyAgreesWithThePlanner(t *testing.T) {
	if testing.Short() {
		t.Skip("simulation")
	}
	fastClock(t)
	f := newFeedFixture(t, true)
	f.setPlatform(t, func(p *appconfig.Platform) { p.GradeWindow = true })
	seedFull(t, f)
	var problems []string
	for grade := 1; grade <= 4; grade++ {
		for _, world := range focus {
			k := f.newKid(t, grade)
			code, j, raw := f.gcall(t, "GET", "/v1/children/"+k.id+"/journey?world="+world, k.tok, "", nil)
			if code != 200 {
				t.Fatalf("journey: %d %s", code, raw)
			}
			avail := 0
			for _, n := range j["nodes"].([]any) {
				node := n.(map[string]any)
				if node["state"] == "locked" {
					continue
				}
				avail++
				skill := node["skill_id"].(string)
				body := fmt.Sprintf(`{"world":%q,"kind":"chapter","skill_id":%q}`, world, skill)
				c, sess, raw := f.gcall(t, "POST", "/v1/children/"+k.id+"/sessions", k.tok, body, nil)
				if c != 201 {
					problems = append(problems, fmt.Sprintf("g%d %s %s: the journey offers it but start gives %d %s", grade, world, skill, c, raw))
					continue
				}
				c, beat, raw := f.gcall(t, "POST", "/v1/sessions/"+sess["id"].(string)+"/beats", k.tok, "{}", nil)
				if c != 201 {
					problems = append(problems, fmt.Sprintf("g%d %s %s: first beat %d %s", grade, world, skill, c, raw))
					continue
				}
				if p := f.problemOf(t, beat["id"].(string)); len(p.Curated) > 0 {
					var q content.Question
					_ = json.Unmarshal(p.Curated, &q)
					if q.Grade > grade {
						problems = append(problems, fmt.Sprintf("g%d %s %s: the journey offers a quest that serves grade %d content", grade, world, skill, q.Grade))
					}
				}
			}
			if avail == 0 && !f.gapCell(t, world, grade) {
				problems = append(problems, fmt.Sprintf("g%d %s: the journey offers nothing", grade, world))
			}
			if cur, ok := j["current_skill_id"].(string); ok {
				for _, n := range j["nodes"].([]any) {
					if node := n.(map[string]any); node["skill_id"] == cur && node["state"] == "locked" {
						problems = append(problems, fmt.Sprintf("g%d %s: current_skill_id %s is a locked node", grade, world, cur))
					}
				}
			}
		}
	}
	if len(problems) > 0 {
		t.Fatalf("%d disagreements:\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

// fastClock gives the next test server a rate limiter whose clock runs one second per request.
func fastClock(t *testing.T) {
	t.Helper()
	now := time.Now()
	testLimiter = httpapi.NewLimiterWithClock(func() time.Time { now = now.Add(time.Second); return now })
	t.Cleanup(func() { testLimiter = nil })
}

// gapCell reports whether the world has no published question at or below the grade: a content gap that makes the
// world unplayable at that grade (the planner must refuse cleanly, never serve a higher year).
func (f feedFixture) gapCell(t *testing.T, world string, grade int) bool {
	t.Helper()
	var n int
	if err := f.s.Store.R.QueryRow(`SELECT COUNT(*) FROM (SELECT skill_id FROM bank_questions WHERE world_id = ? AND status = 'published' AND grade <= ? GROUP BY skill_id HAVING COUNT(*) >= 8)`, world, grade).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 0 // no quest has the minimum number of questions at or below the grade
}
