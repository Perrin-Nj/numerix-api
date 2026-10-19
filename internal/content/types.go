// Package content is the pure core of the question bank: the seed/admin JSON types, the canonical hash,
// the exact `calc` evaluator, the validator (docs/CONTENT_PLAN.md section 3.5) and the answer checker.
// It does no I/O and imports nothing from the database or HTTP layers, so the seed loader, the admin API,
// the game engine and the tests all share the same rules.
//
// Go for Spring people: this package is like a set of Jackson-annotated POJOs plus a Bean Validation
// validator. A struct tag such as `json:"fr"` is the equivalent of @JsonProperty("fr"). A capitalised name
// is public (Java: public), a lowercase name is private to the package.
package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Question types (what the child sees) and answer kinds (how the answer is checked), plan section 3.2.
const (
	TypeNumeric   = "numeric"
	TypeChoice    = "choice"
	TypeTrueFalse = "truefalse"
	TypeOrder     = "order"
	TypeMatch     = "match"
	TypeWord      = "word"
	TypeVisual    = "visual"
	TypeEstimate  = "estimate"

	KindNumeric   = "numeric"
	KindChoice    = "choice"
	KindTrueFalse = "truefalse"
	KindOrder     = "order"
	KindMatch     = "match"
	KindRange     = "range"
)

// Question statuses (the lifecycle: draft -> review -> published -> retired).
const (
	StatusDraft     = "draft"
	StatusReview    = "review"
	StatusPublished = "published"
	StatusRetired   = "retired"
)

// AllTypes lists the eight question types in a stable order.
var AllTypes = []string{TypeNumeric, TypeChoice, TypeTrueFalse, TypeOrder, TypeMatch, TypeWord, TypeVisual, TypeEstimate}

// AllowedKinds maps a type to the answer kinds it may use (plan 3.2).
var AllowedKinds = map[string][]string{
	TypeNumeric:   {KindNumeric},
	TypeChoice:    {KindChoice},
	TypeTrueFalse: {KindTrueFalse},
	TypeOrder:     {KindOrder},
	TypeMatch:     {KindMatch},
	TypeWord:      {KindNumeric, KindChoice},
	TypeVisual:    {KindNumeric, KindChoice},
	TypeEstimate:  {KindRange, KindChoice},
}

// LText is a text in both product languages. Both are always required for anything a child or parent reads.
type LText struct {
	FR string `json:"fr"`
	EN string `json:"en"`
}

// Get returns the text for "fr" or "en" (anything else gives French, the default language).
func (t LText) Get(lang string) string {
	if lang == "en" {
		return t.EN
	}
	return t.FR
}

// IsZero reports whether both languages are empty (used for optional texts such as hint).
func (t LText) IsZero() bool { return t.FR == "" && t.EN == "" }

// Visual is a data-driven picture the question reads (bars, pictograph, clock, ...). Pure data, no pixels.
type Visual struct {
	Kind string         `json:"kind"`
	Data map[string]any `json:"data,omitempty"`
}

// Choice is one option of a choice/estimate question.
type Choice struct {
	ID            string `json:"id"`
	Text          LText  `json:"text"`
	Misconception string `json:"misconception,omitempty"` // wrong option that models a known error
	Random        bool   `json:"random,omitempty"`        // wrong option that is not tied to an error (max 1 per question)
}

// Item is a card to order, or one side of a matching pair. Key is the optional sort key ("3/4", "0.5", "12").
type Item struct {
	ID   string `json:"id"`
	Text LText  `json:"text"`
	Key  string `json:"key,omitempty"`
}

// Items is polymorphic in the JSON: an array for `order`, an object {left,right} for `match`.
type Items struct {
	List  []Item // order
	Left  []Item // match, left column
	Right []Item // match, right column
}

// UnmarshalJSON reads either shape (Spring: a custom JsonDeserializer).
func (it *Items) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '[' {
		return json.Unmarshal(b, &it.List)
	}
	var m struct {
		Left  []Item `json:"left"`
		Right []Item `json:"right"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	it.Left, it.Right = m.Left, m.Right
	return nil
}

// MarshalJSON writes the shape that matches the content.
func (it Items) MarshalJSON() ([]byte, error) {
	if len(it.Left) > 0 || len(it.Right) > 0 {
		return json.Marshal(struct {
			Left  []Item `json:"left"`
			Right []Item `json:"right"`
		}{it.Left, it.Right})
	}
	if it.List == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(it.List)
}

// Answer is the answer spec. Which fields apply depends on Kind (plan 3.3).
type Answer struct {
	Kind          string            `json:"kind"`
	Value         json.RawMessage   `json:"value,omitempty"`       // numeric: number or [quotient, remainder]; truefalse: bool
	Unit          *string           `json:"unit,omitempty"`        // display unit, never graded
	Calc          string            `json:"calc,omitempty"`        // expression the server recomputes (3.4)
	Correct       string            `json:"correct,omitempty"`     // choice: id of the right option
	CalcChoice    map[string]string `json:"calc_choice,omitempty"` // choice: id -> expression that proves its value
	Sequence      []string          `json:"sequence,omitempty"`    // order: item ids in the right order
	By            string            `json:"by,omitempty"`          // order: asc | desc (checked against item keys)
	Pairs         [][2]string       `json:"pairs,omitempty"`       // match: [left id, right id]
	Min           json.Number       `json:"min,omitempty"`         // range: closed interval
	Max           json.Number       `json:"max,omitempty"`
	ExactForCheck json.Number       `json:"exact_for_check,omitempty"`
	Misconception string            `json:"misconception,omitempty"` // truefalse/order/match: code returned on a wrong answer
}

// CommonError is a typical wrong numeric answer with the misconception that explains it.
type CommonError struct {
	Value         json.RawMessage `json:"value"`
	Misconception string          `json:"misconception"`
	Feedback      LText           `json:"feedback"`
}

// Criteria selects media by facets instead of naming an asset (plan 4.4).
type Criteria struct {
	World   string `json:"world,omitempty"`
	Topic   string `json:"topic,omitempty"`
	Object  string `json:"object,omitempty"`
	Mood    string `json:"mood,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Setting string `json:"setting,omitempty"`
}

// Media is one image slot of a question: an explicit asset (id or alias) or criteria.
type Media struct {
	Role     string    `json:"role"` // scene | object | choice:<id> | visual
	Asset    string    `json:"asset,omitempty"`
	Criteria *Criteria `json:"criteria,omitempty"`
}

// Question is the whole seed/admin object of one question (plan 3.3). It is stored as canonical JSON.
type Question struct {
	ID           string        `json:"id"`
	Quest        string        `json:"quest"`
	Skill        string        `json:"skill"`
	Type         string        `json:"type"`
	Grade        int           `json:"grade"`
	Difficulty   int           `json:"difficulty"`
	Story        LText         `json:"story"`
	Prompt       LText         `json:"prompt"`
	MathText     string        `json:"math_text,omitempty"`
	Visual       *Visual       `json:"visual,omitempty"`
	Choices      []Choice      `json:"choices,omitempty"`
	Items        *Items        `json:"items,omitempty"`
	Answer       Answer        `json:"answer"`
	CommonErrors []CommonError `json:"common_errors,omitempty"`
	Hint         LText         `json:"hint"`
	Explain      LText         `json:"explain"`
	Tags         []string      `json:"tags,omitempty"`
	Media        []Media       `json:"media,omitempty"`
	Status       string        `json:"status"`
	Author       string        `json:"author"`
	Version      int           `json:"version"`
	Source       string        `json:"source"`
	AIDrafted    bool          `json:"ai_drafted,omitempty"`
	ReviewedBy   string        `json:"reviewed_by,omitempty"`
	// AllowOperandEqualsAnswer lets a number written in the statement equal the answer (reason goes in Source).
	AllowOperandEqualsAnswer bool `json:"allow_operand_equals_answer,omitempty"`
}

// World is one entry of seed/worlds.json (and a row of table worlds).
type World struct {
	ID          string `json:"id"`
	Ord         int    `json:"ord"`
	Name        LText  `json:"name"`
	Tagline     LText  `json:"tagline"`
	Hook        LText  `json:"hook"`
	AccentColor string `json:"accent_color"`
	GradeMin    int    `json:"grade_min"`
	GradeMax    int    `json:"grade_max"`
	Strand      string `json:"strand"`
	HeroAsset   string `json:"hero_asset,omitempty"`
	Status      string `json:"status,omitempty"` // draft | published | retired (default published)
}

// Quest is one entry of seed/quests.json: one skill inside one world.
type Quest struct {
	ID          string `json:"id"`
	World       string `json:"world"`
	Ord         int    `json:"ord"`
	Skill       string `json:"skill"`
	Name        LText  `json:"name"`
	Goal        LText  `json:"goal"`
	GradeMin    int    `json:"grade_min"`
	GradeMax    int    `json:"grade_max"`
	TargetCount int    `json:"target_count"`
	IconAsset   string `json:"icon_asset,omitempty"`
	Status      string `json:"status,omitempty"`
}

// Canonical returns the canonical JSON of any value: keys sorted, no whitespace, numbers kept as written.
// Two documents with the same meaning always give the same bytes, so their hash is a reliable change detector.
func Canonical(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canonical: marshal: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keep 0.10 as written instead of turning it into a float64
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("canonical: decode: %w", err)
	}
	out, err := json.Marshal(generic) // maps are written with sorted keys
	if err != nil {
		return nil, fmt.Errorf("canonical: encode: %w", err)
	}
	return out, nil
}

// Hash is the hex SHA-256 of the canonical JSON.
func Hash(v any) (string, error) {
	b, err := Canonical(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ContentHash hashes the content of a question only. Status, version, author, source and review fields are
// excluded: a seed re-run must not touch a row whose real content is unchanged (an admin may have published it).
func (q Question) ContentHash() (string, error) {
	q.Status, q.Version, q.Author, q.Source, q.ReviewedBy, q.AIDrafted = "", 0, "", "", "", false
	return Hash(q)
}

// DecodeStrict parses JSON into v and refuses unknown fields, so a typo in a seed file is an error, not a
// silently ignored key (Spring: FAIL_ON_UNKNOWN_PROPERTIES = true).
func DecodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("unexpected data after the JSON document")
	}
	return nil
}
