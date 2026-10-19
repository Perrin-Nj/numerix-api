package contentdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/Perrin-Nj/numerix-api/internal/content"
)

// Filter selects questions for the admin list. Zero values mean "any".
type Filter struct {
	World, Quest, Skill, Type, Status, Author, Tag, Text string
	Grade, Difficulty                                    int
	AI                                                   *bool
	Page, PageSize                                       int
}

// Summary is one line of the admin question list.
type Summary struct {
	ID         string `json:"id"`
	World      string `json:"world"`
	Quest      string `json:"quest"`
	Skill      string `json:"skill"`
	Type       string `json:"type"`
	Grade      int    `json:"grade"`
	Difficulty int    `json:"difficulty"`
	Status     string `json:"status"`
	Version    int    `json:"version"`
	Author     string `json:"author"`
	AIDrafted  bool   `json:"ai_drafted"`
	PromptFR   string `json:"prompt_fr"`
	PromptEN   string `json:"prompt_en"`
	UpdatedAt  string `json:"updated_at"`
}

// where builds the SQL condition and arguments of a filter. Only fixed column names reach the SQL text;
// every value is a bound parameter.
func (f Filter) where() (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, a ...any) {
		conds = append(conds, cond)
		args = append(args, a...)
	}
	for col, v := range map[string]string{"world_id": f.World, "quest_id": f.Quest, "skill_id": f.Skill, "type": f.Type, "status": f.Status, "author": f.Author} {
		if v != "" {
			add(col+" = ?", v)
		}
	}
	if f.Grade > 0 {
		add("grade = ?", f.Grade)
	}
	if f.Difficulty > 0 {
		add("difficulty = ?", f.Difficulty)
	}
	if f.AI != nil {
		add("ai_drafted = ?", boolInt(*f.AI))
	}
	if f.Tag != "" {
		add("EXISTS (SELECT 1 FROM question_tags t WHERE t.question_id = bank_questions.id AND t.tag = ?)", strings.ToLower(f.Tag))
	}
	if f.Text != "" {
		add(`(instr(lower(json_extract(content_json,'$.prompt.fr')), lower(?)) > 0 OR instr(lower(json_extract(content_json,'$.prompt.en')), lower(?)) > 0
			OR instr(lower(json_extract(content_json,'$.story.fr')), lower(?)) > 0 OR instr(lower(json_extract(content_json,'$.story.en')), lower(?)) > 0 OR instr(id, ?) > 0)`,
			f.Text, f.Text, f.Text, f.Text, strings.ToLower(f.Text))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// List returns one page of summaries and the total number of matches.
func (r *Repo) List(ctx context.Context, q Querier, f Filter) ([]Summary, int, error) {
	where, args := f.where()
	var total int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM bank_questions`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count questions: %w", err)
	}
	size := min(200, max(1, f.PageSize))
	off := max(0, f.Page-1) * size
	rows, err := q.QueryContext(ctx, `SELECT id, world_id, quest_id, skill_id, type, grade, difficulty, status, version, author, ai_drafted,
		COALESCE(json_extract(content_json,'$.prompt.fr'),''), COALESCE(json_extract(content_json,'$.prompt.en'),''), updated_at
		FROM bank_questions`+where+` ORDER BY quest_id, id LIMIT ? OFFSET ?`, append(args, size, off)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list questions: %w", err)
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var s Summary
		var ai int
		if err := rows.Scan(&s.ID, &s.World, &s.Quest, &s.Skill, &s.Type, &s.Grade, &s.Difficulty, &s.Status, &s.Version, &s.Author, &ai, &s.PromptFR, &s.PromptEN, &s.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan summary: %w", err)
		}
		s.AIDrafted = ai == 1
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// Coverage is how a quest stands against its target.
type Coverage struct {
	Quest    string         `json:"quest"`
	World    string         `json:"world"`
	Target   int            `json:"target_count"`
	ByStatus map[string]int `json:"by_status"`
	ByType   map[string]int `json:"by_type"`
}

// Coverage counts questions per quest by status and (published only) by type.
func (r *Repo) Coverage(ctx context.Context, q Querier) ([]Coverage, error) {
	qs, err := r.Quests(ctx, q, "")
	if err != nil {
		return nil, err
	}
	idx := map[string]*Coverage{}
	out := make([]Coverage, len(qs))
	for i, x := range qs {
		out[i] = Coverage{Quest: x.ID, World: x.World, Target: x.TargetCount, ByStatus: map[string]int{}, ByType: map[string]int{}}
		idx[x.ID] = &out[i]
	}
	rows, err := q.QueryContext(ctx, `SELECT quest_id, status, type, COUNT(*) FROM bank_questions GROUP BY quest_id, status, type`)
	if err != nil {
		return nil, fmt.Errorf("coverage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var quest, status, typ string
		var n int
		if err := rows.Scan(&quest, &status, &typ, &n); err != nil {
			return nil, err
		}
		if c := idx[quest]; c != nil {
			c.ByStatus[status] += n
			if status == content.StatusPublished {
				c.ByType[typ] += n
			}
		}
	}
	return out, rows.Err()
}

// Export returns the full question objects of a world (or all worlds) with the given status ("" = any non-retired).
func (r *Repo) Export(ctx context.Context, q Querier, world, status string) ([]content.Question, error) {
	rows, err := q.QueryContext(ctx, rowSelect+` WHERE (? = '' OR world_id = ?) AND ((? = '' AND status <> 'retired') OR status = ?) ORDER BY quest_id, id`, world, world, status, status)
	if err != nil {
		return nil, fmt.Errorf("export questions: %w", err)
	}
	defer rows.Close()
	out := []content.Question{}
	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row.Q)
	}
	return out, rows.Err()
}
