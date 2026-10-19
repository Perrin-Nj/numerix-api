// Package drafter asks a language model for a DRAFT of a question. It is used only by the admin API, never by
// the child-facing API. The model proposes words and distractors; the server recomputes every number and runs
// the full validator, and a human publishes (docs/CONTENT_PLAN.md section 8). The LLM never does math.
//
// Go for Spring people: Provider is an interface like narrator.Provider (Spring: a @Service interface with a real
// and a fake implementation). Anthropic is the real one and talks to the Messages API over plain HTTP.
package drafter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Request is everything the model may use. It carries no child data of any kind.
type Request struct {
	World          string `json:"world"`
	Quest          string `json:"quest"`
	QuestGoal      string `json:"quest_goal"` // what the quest teaches, in French
	Skill          string `json:"skill"`
	SkillLabel     string `json:"skill_label"`
	Type           string `json:"type"` // numeric, choice, ...
	Kind           string `json:"answer_kind"`
	Grade          int    `json:"grade"`
	Difficulty     int    `json:"difficulty"`
	TopicHint      string `json:"topic_hint,omitempty"`
	Primary        string `json:"language_primary"` // fr or en: the language to write first
	Misconceptions []Hint `json:"misconceptions"`   // the only codes the model may use
}

// Hint is one allowed misconception code with its meaning.
type Hint struct {
	Code string `json:"code"`
	Desc string `json:"desc"`
}

// Provider produces the JSON of a question draft.
type Provider interface {
	Name() string  // for the author field: "ai:<name>"
	Model() string // model id, written to the audit trail
	Draft(ctx context.Context, r Request) (json.RawMessage, error)
}

// ErrNotConfigured is returned (as a 503 by the admin API) when no provider is configured.
var ErrNotConfigured = errors.New("drafter: ANTHROPIC_API_KEY and NARRATOR_MODEL are not both set")

// AnthropicConfig is read from the environment by FromEnv.
type AnthropicConfig struct {
	APIKey    string        // ANTHROPIC_API_KEY
	Model     string        // ANTHROPIC_DRAFT_MODEL, else NARRATOR_MODEL
	BaseURL   string        // ANTHROPIC_BASE_URL (tests)
	Timeout   time.Duration // DRAFT_TIMEOUT_MS, default 60 s
	MaxTokens int           // DRAFT_MAX_TOKENS, default 2500
}

// Anthropic implements Provider with the Messages API.
type Anthropic struct {
	cfg    AnthropicConfig
	client *http.Client
}

// FromEnv builds the provider only when ANTHROPIC_API_KEY and NARRATOR_MODEL are both set; otherwise nil.
// ANTHROPIC_DRAFT_MODEL, when set, replaces NARRATOR_MODEL as the model used for drafts.
func FromEnv(getenv func(string) string, log *slog.Logger) Provider {
	key, narr := strings.TrimSpace(getenv("ANTHROPIC_API_KEY")), strings.TrimSpace(getenv("NARRATOR_MODEL"))
	if key == "" || narr == "" {
		log.Info("AI question drafts are off (ANTHROPIC_API_KEY and NARRATOR_MODEL are not both set)")
		return nil
	}
	model := narr
	if m := strings.TrimSpace(getenv("ANTHROPIC_DRAFT_MODEL")); m != "" {
		model = m
	}
	return NewAnthropic(AnthropicConfig{APIKey: key, Model: model, BaseURL: strings.TrimRight(getenv("ANTHROPIC_BASE_URL"), "/")})
}

// NewAnthropic applies the defaults.
func NewAnthropic(cfg AnthropicConfig) *Anthropic {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 2500
	}
	return &Anthropic{cfg: cfg, client: &http.Client{}}
}

// Name is the provider name.
func (*Anthropic) Name() string { return "anthropic" }

// Model is the configured model id.
func (a *Anthropic) Model() string { return a.cfg.Model }

// systemPrompt fixes the role and the output format. The schema mirrors seed/schema/question.schema.json.
const systemPrompt = `You write DRAFTS of maths questions for children aged 6 to 12 in Quebec (French first, then Canadian English).
A human reviews and publishes every draft. You never calculate the final answer for the reader: you also give a "calc"
expression and the server recomputes it. Reply with ONE JSON object and nothing else (no markdown fence).

Fields: story {fr,en} (one short sentence that gives the numbers a purpose), prompt {fr,en} (what to answer), math_text (optional,
language neutral, like "8 + 7 = ?"), choices (for choice questions: 3 to 5 objects {id, text {fr,en}, misconception?, random?}),
items (order: list of {id,text{fr,en},key}; match: {left:[...],right:[...]}), answer, common_errors (numeric answers:
[{value, misconception, feedback {fr,en}}]), hint {fr,en} (guides, never states the answer), explain {fr,en} (teaches the
strategy), tags (2 or more short english words).
answer by kind: numeric {kind:"numeric", value:<number or [quotient, remainder]>, calc:"<expression>"}; choice {kind:"choice",
correct:"<choice id>", calc_choice:{"<id>":"<expression>"}}; truefalse {kind:"truefalse", value:true|false}; order {kind:"order",
sequence:[ids], by:"asc"|"desc"}; match {kind:"match", pairs:[[left id,right id],...]}; range {kind:"range", min, max,
exact_for_check, calc}.
calc uses only numbers, + - * / ( ) and divmod, min, max, abs, round, gcd, pow, sum.

Rules: write kind, short sentences (at most 14 words for ages 6 to 8, 18 for ages 9 to 10, 22 above). No timers, no hurry words,
no em dash, no emoji, no brand names. The story, prompt and hint must not contain the answer in digits or in words. Use
first names common in Quebec. Every wrong choice carries a misconception code taken ONLY from the list you are given,
or "random": true (at most one). Both languages are required and must differ (except digits).`

// Draft calls the model and returns the JSON object it wrote.
func (a *Anthropic) Draft(ctx context.Context, r Request) (json.RawMessage, error) {
	user, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"model": a.cfg.Model, "max_tokens": a.cfg.MaxTokens, "system": systemPrompt,
		"messages": []map[string]string{{"role": "user", "content": "Write one draft for this request:\n" + string(user)}},
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", a.cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call anthropic: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read anthropic response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic status %d", resp.StatusCode) // the body is not logged: it may echo the prompt
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	var text strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return ExtractJSON(text.String())
}

// ExtractJSON finds the JSON object in a model reply (tolerates a markdown fence or a sentence around it).
func ExtractJSON(s string) (json.RawMessage, error) {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return nil, errors.New("the reply holds no JSON object")
	}
	obj := s[start : end+1]
	if !json.Valid([]byte(obj)) {
		return nil, errors.New("the reply holds invalid JSON")
	}
	return json.RawMessage(obj), nil
}
