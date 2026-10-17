package narrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrBudget is returned when the daily cost cap is reached; the Narrator then uses canned texts.
var ErrBudget = errors.New("narrator: daily cost cap reached")

// AnthropicConfig is read from environment variables by FromEnv (Spring: @ConfigurationProperties).
type AnthropicConfig struct {
	APIKey      string        // ANTHROPIC_API_KEY (required to enable the provider)
	Model       string        // NARRATOR_MODEL (required: no default, the operator chooses)
	BaseURL     string        // ANTHROPIC_BASE_URL (default https://api.anthropic.com), mainly for tests
	Timeout     time.Duration // NARRATOR_TIMEOUT_MS (default 4000): slower than this means canned text
	DailyCapUSD float64       // NARRATOR_DAILY_CAP_USD (default 5)
	PriceInUSD  float64       // NARRATOR_PRICE_IN_PER_MTOK (default 3)
	PriceOutUSD float64       // NARRATOR_PRICE_OUT_PER_MTOK (default 15)
	MaxTokens   int           // NARRATOR_MAX_TOKENS (default 220)
}

// Anthropic calls the Messages API over plain HTTP (no SDK, no extra dependency).
type Anthropic struct {
	cfg    AnthropicConfig
	client *http.Client
	now    func() time.Time // replaceable clock for tests

	mu    sync.Mutex // guards the two fields below
	day   string     // UTC day of the running total
	spent float64    // USD spent today
}

// FromEnv builds the provider only when ANTHROPIC_API_KEY and NARRATOR_MODEL are set; otherwise nil
// (the app then runs on the canned bank). getenv is os.Getenv in production.
func FromEnv(getenv func(string) string, log *slog.Logger) Provider {
	key, model := strings.TrimSpace(getenv("ANTHROPIC_API_KEY")), strings.TrimSpace(getenv("NARRATOR_MODEL"))
	if key == "" {
		return nil
	}
	if model == "" {
		log.Warn("ANTHROPIC_API_KEY is set but NARRATOR_MODEL is not: narrator stays on canned texts")
		return nil
	}
	num := func(name string, def float64) float64 {
		if v, err := strconv.ParseFloat(strings.TrimSpace(getenv(name)), 64); err == nil && v >= 0 {
			return v
		}
		return def
	}
	cfg := AnthropicConfig{
		APIKey: key, Model: model, BaseURL: strings.TrimRight(getenv("ANTHROPIC_BASE_URL"), "/"),
		Timeout:     time.Duration(num("NARRATOR_TIMEOUT_MS", 4000)) * time.Millisecond,
		DailyCapUSD: num("NARRATOR_DAILY_CAP_USD", 5), PriceInUSD: num("NARRATOR_PRICE_IN_PER_MTOK", 3),
		PriceOutUSD: num("NARRATOR_PRICE_OUT_PER_MTOK", 15), MaxTokens: int(num("NARRATOR_MAX_TOKENS", 220)),
	}
	return NewAnthropic(cfg)
}

// NewAnthropic builds the provider from a config.
func NewAnthropic(cfg AnthropicConfig) *Anthropic {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 4 * time.Second
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 220
	}
	return &Anthropic{cfg: cfg, client: &http.Client{}, now: time.Now}
}

// Name identifies the provider.
func (*Anthropic) Name() string { return "anthropic" }

// systemPrompt tells the model its role. It never receives the answer; it must not state numbers
// other than the ones in the payload.
func systemPrompt(lang string) string {
	if strings.HasPrefix(lang, "en") {
		return "You write short story text for a child (age 6-11) doing a maths adventure. " +
			"Write 2 or 3 warm, simple sentences. Never calculate, never state or hint at a result, " +
			"never use a number that is not in the JSON you are given. No lists, no markdown, no emoji. " +
			"Never say the child failed or was wrong. Reply with the story text only."
	}
	return "Tu écris un court texte d’histoire pour un enfant (6 à 11 ans) qui fait une aventure de maths. " +
		"Écris 2 ou 3 phrases simples et chaleureuses. Ne calcule jamais, ne donne ni ne suggère aucun résultat, " +
		"n’utilise aucun nombre absent du JSON fourni. Pas de liste, pas de markdown, pas d’émoji. " +
		"Ne dis jamais que l’enfant a échoué ou s’est trompé. Réponds uniquement avec le texte de l’histoire."
}

// Generate asks the model for the scene or the denouement. It never receives an answer: the payload
// was built by the engine with the answer keys removed.
func (a *Anthropic) Generate(ctx context.Context, r Request) (string, error) {
	if !a.reserve() {
		return "", ErrBudget
	}
	payload, err := json.Marshal(map[string]any{
		"monde": r.World, "phase": r.Phase, "issue": r.Outcome, "langue": r.Lang, "charge": r.Payload,
	})
	if err != nil {
		return "", fmt.Errorf("encode payload: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"model": a.cfg.Model, "max_tokens": a.cfg.MaxTokens, "system": systemPrompt(r.Lang),
		"messages": []map[string]string{{"role": "user", "content": string(payload)}},
	})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout) // slower than the timeout: canned text instead
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", a.cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call anthropic: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read anthropic response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic status %d", resp.StatusCode) // body not logged: it may echo the prompt
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode anthropic response: %w", err)
	}
	a.charge(out.Usage.In, out.Usage.Out)
	var text strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return strings.TrimSpace(text.String()), nil
}

// rollDay resets the running total when the UTC day changes. The caller holds a.mu.
func (a *Anthropic) rollDay() {
	if d := a.now().UTC().Format("2006-01-02"); d != a.day {
		a.day, a.spent = d, 0
	}
}

// reserve reports whether today's spend is still under the cap.
func (a *Anthropic) reserve() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollDay()
	return a.spent < a.cfg.DailyCapUSD
}

// charge adds the cost of one call (token counts x price per million tokens).
func (a *Anthropic) charge(in, out int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollDay()
	a.spent += float64(in)/1e6*a.cfg.PriceInUSD + float64(out)/1e6*a.cfg.PriceOutUSD
}

// SpentToday returns today's estimated spend in USD (for logs and tests).
func (a *Anthropic) SpentToday() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollDay()
	return a.spent
}
