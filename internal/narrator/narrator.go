// Package narrator writes the story text around a question. It never calculates: it receives a payload
// built by the engine (without the answer in phase 1) and returns words. Every text passes a leak check;
// when the LLM is not configured, fails, is too slow, over budget or leaks, a canned text is used instead.
//
// Go for Spring people: Provider is an interface (Spring: a @Service interface with two implementations,
// CannedProvider and AnthropicProvider). Narrator is the facade that picks one and applies the rules.
package narrator

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

// Phase of the narration.
const (
	PhaseScene      = "scene"
	PhaseDenouement = "denouement"
)

// Status values match the contract's NarrationStatus.
const (
	StatusReady    = "ready"
	StatusFallback = "fallback"
)

// PromptVersion is part of every cache key: bump it when prompts change so old texts are not reused.
const PromptVersion = "v1"

// Request is everything a provider may use. Payload comes from engine.ForLLMScene / ForLLMDenouement.
type Request struct {
	World    string         // nature, espace, ...
	Lang     string         // "fr" or "en"
	Phase    string         // PhaseScene or PhaseDenouement
	Outcome  string         // correct, erreur_typique, erreur_aleatoire (denouement only)
	Seed     uint32         // per-session seed: the canned bank varies deterministically with it
	Index    int            // beat index in the chapter
	MathText string         // the statement the child sees (never contains the answer)
	Payload  map[string]any // engine payload, answer keys already removed
	CacheKey string         // empty: do not cache
}

// Provider produces a text for a request.
type Provider interface {
	Name() string
	Generate(ctx context.Context, r Request) (string, error)
}

// Cache stores generated narrations (table `narrations`). Implemented by the app over SQLite.
type Cache interface {
	Get(ctx context.Context, key string) (text string, found bool, err error)
	Put(ctx context.Context, key, text, source string) error
}

// Result is the narration plus how it was obtained.
type Result struct {
	Text   string
	Status string // StatusReady or StatusFallback
	Source string // "canned", "anthropic", "cache"
}

// Narrator picks the provider and enforces the leak check.
type Narrator struct {
	Primary  Provider // optional LLM provider; nil means canned only
	Fallback Provider // canned bank, always present
	Cache    Cache    // optional
	Log      *slog.Logger
Off      func() bool // optional runtime switch (admin config): true = canned text only, the provider is never called
}

// New builds a Narrator with the canned bank as fallback.
func New(primary Provider, cache Cache, log *slog.Logger) *Narrator {
	if log == nil {
		log = slog.Default()
	}
	return &Narrator{Primary: primary, Fallback: NewCanned(), Cache: cache, Log: log}
}

// Mode reports "stub" (canned only) or the provider name, for GET /health.
func (n *Narrator) Mode() string {
	if n.Primary == nil {
		return "stub"
	}
	return n.Primary.Name()
}

// Narrate returns a text that passed the leak check. forbidden lists answer tokens that must not appear.
func (n *Narrator) Narrate(ctx context.Context, r Request, forbidden []string) Result {
	if n.Primary == nil || (n.Off != nil && n.Off()) { // canned only (no provider, or the admin switched the model off): nothing can fail except a template bug, which the leak check catches
		return n.canned(ctx, r, forbidden, StatusReady)
	}
	if n.Cache != nil && r.CacheKey != "" {
		if text, found, err := n.Cache.Get(ctx, r.CacheKey); err == nil && found && !Leaks(text, forbidden) {
			return Result{Text: text, Status: StatusReady, Source: "cache"}
		}
	}
	text, err := n.Primary.Generate(ctx, r)
	switch {
	case err != nil:
		n.Log.Warn("narrator provider failed, using canned text", "provider", n.Primary.Name(), "err", err)
	case strings.TrimSpace(text) == "":
		n.Log.Warn("narrator provider returned nothing, using canned text", "provider", n.Primary.Name())
	case Leaks(text, forbidden):
		n.Log.Warn("narration leaked the answer, using canned text", "provider", n.Primary.Name())
	default:
		if n.Cache != nil && r.CacheKey != "" {
			if err := n.Cache.Put(ctx, r.CacheKey, text, n.Primary.Name()); err != nil {
				n.Log.Warn("narration cache write failed", "err", err)
			}
		}
		return Result{Text: strings.TrimSpace(text), Status: StatusReady, Source: n.Primary.Name()}
	}
	return n.canned(ctx, r, forbidden, StatusFallback)
}

// canned renders the fallback bank and still runs the leak check.
func (n *Narrator) canned(ctx context.Context, r Request, forbidden []string, status string) Result {
	text, err := n.Fallback.Generate(ctx, r)
	if err != nil || Leaks(text, forbidden) {
		// last resort: a fixed sentence with no numbers at all
		text = lastResort(r.Lang)
		status = StatusFallback
	}
	return Result{Text: text, Status: status, Source: "canned"}
}

// lastResort is shown only if even the canned template fails: it contains no number.
func lastResort(lang string) string {
	if strings.HasPrefix(lang, "en") {
		return "The adventure goes on. Take your time and look at the choices."
	}
	return "L’aventure continue. Prends ton temps et regarde bien les choix."
}

// Leaks reports whether text reveals an answer token. Numeric tokens must match as whole numbers
// ("42" is found in "42 seeds" but not in "142"); other tokens match as lower-case phrases.
func Leaks(text string, forbidden []string) bool {
	low := strings.ToLower(text)
	for _, tok := range forbidden {
		tok = strings.TrimSpace(strings.ToLower(tok))
		if tok == "" {
			continue
		}
		if isDigits(tok) {
			if regexp.MustCompile(`(^|\D)` + regexp.QuoteMeta(tok) + `(\D|$)`).MatchString(low) {
				return true
			}
		} else if strings.Contains(low, tok) {
			return true
		}
	}
	return false
}

// isDigits reports whether s is a non-empty run of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
