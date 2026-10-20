package appconfig

// The typed documents, one per namespace, with their safe defaults and validation rules.
//
// Go for Spring people: a struct with `json:"..."` tags is a DTO with @JsonProperty. A default is just a function
// returning a filled struct (Spring: a @ConfigurationProperties class with field initialisers). Validation is a
// plain function returning an error (Spring: a Bean Validation constraint set).

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Bi is a French and English text pair.
type Bi struct {
	FR string `json:"fr"`
	EN string `json:"en"`
}

// Namespace names, in the order the admin app lists them.
const (
	NSPlatform    = "platform"
	NSMusic       = "music"
	NSVoice       = "voice"
	NSScenes      = "scenes"
	NSFeed        = "feed"
	NSMaintenance = "maintenance"
	NSAI          = "ai"
	NSCopy        = "copy"
)

// ---- platform (policy, trial, thresholds, feature switches) ----

// Platform is the policy document read by the game and the billing code.
type Platform struct {
	CardThresholdPct             int             `json:"card_threshold_pct"`
	UnpaidPolicy                 string          `json:"unpaid_policy"`
	TrialDays                    int             `json:"trial_days"`
	DailyTimeLimitMinutesDefault *int            `json:"daily_time_limit_minutes_default"` // null = no default limit
	ReminderHourLocal            int             `json:"reminder_hour_local"`
	ExpiringWarningDays          int             `json:"expiring_warning_days"`
	DefaultLocale                string          `json:"default_locale"`
	Features                     map[string]bool `json:"features"`
	DailyGoalDefault             int             `json:"daily_goal_default"` // chapters a day when the child has no goal of its own
	WelcomeGift                  bool            `json:"welcome_gift"`       // the env FEATURE_WELCOME_GIFT only seeds the default
}

func platformDefault(welcomeGift bool) Platform {
	limit := 60
	return Platform{CardThresholdPct: 70, UnpaidPolicy: "block", TrialDays: 14, DailyTimeLimitMinutesDefault: &limit,
		ReminderHourLocal: 17, ExpiringWarningDays: 7, DefaultLocale: "fr-CA", DailyGoalDefault: 1, WelcomeGift: welcomeGift,
		Features: map[string]bool{"feed": true, "challenges": true, "push": false, "payments": true}}
}

var featureKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func (p Platform) check() error {
	switch {
	case p.CardThresholdPct < 0 || p.CardThresholdPct > 100:
		return errors.New("invalid config card_threshold_pct")
	case p.UnpaidPolicy != "block" && p.UnpaidPolicy != "allow":
		return errors.New("invalid config unpaid_policy")
	case p.TrialDays < 0 || p.TrialDays > 365:
		return errors.New("invalid config trial_days")
	case p.DailyTimeLimitMinutesDefault != nil && (*p.DailyTimeLimitMinutesDefault < 5 || *p.DailyTimeLimitMinutesDefault > 180):
		return errors.New("invalid config daily_time_limit_minutes_default")
	case p.ReminderHourLocal < 0 || p.ReminderHourLocal > 23:
		return errors.New("invalid config reminder_hour_local")
	case p.ExpiringWarningDays < 0 || p.ExpiringWarningDays > 60:
		return errors.New("invalid config expiring_warning_days")
	case p.DefaultLocale != "fr-CA" && p.DefaultLocale != "en-CA":
		return errors.New("invalid config default_locale")
	case p.DailyGoalDefault < 1 || p.DailyGoalDefault > 5:
		return errors.New("invalid config daily_goal_default")
	case len(p.Features) > 20:
		return errors.New("invalid config features")
	}
	for k := range p.Features {
		if !featureKey.MatchString(k) {
			return errors.New("invalid config features")
		}
	}
	return nil
}

// ---- music ----

// MusicTrack is one background loop the player may use.
type MusicTrack struct {
	ID      string `json:"id"`
	Title   Bi     `json:"title"`
	Enabled bool   `json:"enabled"`
}

// Music is the background music policy.
type Music struct {
	Enabled       bool         `json:"enabled"`
	DefaultVolume int          `json:"default_volume"`
	Tracks        []MusicTrack `json:"tracks"`
}

func musicDefault() Music {
	t := func(id, fr, en string) MusicTrack { return MusicTrack{ID: id, Title: Bi{fr, en}, Enabled: true} }
	return Music{Enabled: true, DefaultVolume: 30, Tracks: []MusicTrack{
		t("morning", "Matin", "Morning"), t("clear-air", "Air pur", "Clear air"), t("avec-soin", "Avec soin", "With care"),
		t("enchanted-journey", "Voyage enchanté", "Enchanted journey"), t("facile", "Facile", "Easy"),
		t("reawakening", "Réveil", "Reawakening"), t("water-lily", "Nénuphar", "Water lily")}}
}

var slugID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (m Music) check() error {
	if m.DefaultVolume < 0 || m.DefaultVolume > 100 {
		return errors.New("default_volume must be 0 to 100")
	}
	if len(m.Tracks) > 50 {
		return errors.New("at most 50 tracks")
	}
	seen := map[string]bool{}
	for _, t := range m.Tracks {
		if !slugID.MatchString(t.ID) || seen[t.ID] {
			return fmt.Errorf("track id %q is invalid or repeated", t.ID)
		}
		seen[t.ID] = true
		if err := bi("track title", t.Title, 80, false); err != nil {
			return err
		}
	}
	return nil
}

// ---- voice ----

// VoiceClip is one recorded line.
type VoiceClip struct {
	LineID  string `json:"line_id"`
	Locale  string `json:"locale"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// Voice is the voice policy and the admin-managed clip manifest.
type Voice struct {
	HumanEnabledDefault  bool        `json:"human_enabled_default"`
	ComputerVoiceAllowed bool        `json:"computer_voice_allowed"` // false hides the parent opt-in for the computer voice
	Clips                []VoiceClip `json:"clips"`
}

func voiceDefault() Voice { return Voice{HumanEnabledDefault: true, Clips: []VoiceClip{}} }

var lineID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,80}$`)

func (v Voice) check() error {
	if len(v.Clips) > 2000 {
		return errors.New("at most 2000 clips")
	}
	seen := map[string]bool{}
	for _, c := range v.Clips {
		key := c.LineID + "|" + c.Locale
		switch {
		case !lineID.MatchString(c.LineID):
			return fmt.Errorf("clip line_id %q is invalid", c.LineID)
		case c.Locale != "fr-CA" && c.Locale != "en-CA":
			return fmt.Errorf("clip %s: locale must be fr-CA or en-CA", c.LineID)
		case seen[key]:
			return fmt.Errorf("clip %s %s is repeated", c.LineID, c.Locale)
		case !safeURL(c.URL):
			return fmt.Errorf("clip %s: url must start with / or https://", c.LineID)
		}
		seen[key] = true
	}
	return nil
}

// safeURL accepts a same-site path or an https URL, no spaces or control characters (the value ends up in <audio src>).
func safeURL(u string) bool {
	if len(u) == 0 || len(u) > 300 || strings.ContainsFunc(u, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return false
	}
	return (strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//")) || strings.HasPrefix(u, "https://")
}

// ---- scenes ----

// SceneFamily switches one consequence-scene family and some of its variants.
type SceneFamily struct {
	Enabled          bool     `json:"enabled"`
	DisabledVariants []string `json:"disabled_variants"`
}

// Scenes is the consequence-scene policy.
type Scenes struct {
	PhysicsEnabled bool                   `json:"physics_enabled"` // false = the client plays its plain animation, no simulation
	Families       map[string]SceneFamily `json:"families"`
}

// SceneFamilies are the families the server knows (internal/app/scene.go sceneFamily, plus "time").
var SceneFamilies = []string{"number", "add_sub", "mult_div", "fractions", "geometry", "measure", "data", "time"}

func scenesDefault() Scenes {
	s := Scenes{PhysicsEnabled: true, Families: map[string]SceneFamily{}}
	for _, f := range SceneFamilies {
		s.Families[f] = SceneFamily{Enabled: true, DisabledVariants: []string{}}
	}
	return s
}

var variantID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

func (s Scenes) check() error {
	for name, f := range s.Families {
		known := false
		for _, k := range SceneFamilies {
			known = known || k == name
		}
		if !known {
			return fmt.Errorf("unknown scene family %q", name)
		}
		if len(f.DisabledVariants) > 100 {
			return fmt.Errorf("family %s: at most 100 disabled variants", name)
		}
		for _, v := range f.DisabledVariants {
			if !variantID.MatchString(v) {
				return fmt.Errorf("family %s: variant id %q is invalid", name, v)
			}
		}
	}
	return nil
}

// ---- feed ----

// Feed is the Discover feed and board policy.
type Feed struct {
	DiscoverEnabled    bool     `json:"discover_enabled"`
	HiddenItemIDs      []string `json:"hidden_item_ids"` // Discover items (slug ids) hidden from every child
	MaxGradeStretch    int      `json:"max_grade_stretch"`
	FamilyBoardVisible bool     `json:"family_board_visible"`
}

func feedDefault() Feed {
	return Feed{DiscoverEnabled: true, HiddenItemIDs: []string{}, MaxGradeStretch: 1, FamilyBoardVisible: true}
}

func (f Feed) check() error {
	if f.MaxGradeStretch < 0 || f.MaxGradeStretch > 2 {
		return errors.New("max_grade_stretch must be 0 to 2")
	}
	if len(f.HiddenItemIDs) > 500 {
		return errors.New("at most 500 hidden items")
	}
	for _, id := range f.HiddenItemIDs {
		if !slugID.MatchString(id) {
			return fmt.Errorf("hidden item id %q is invalid", id)
		}
	}
	return nil
}

// ---- maintenance ----

// Maintenance is the banner and the optional play block.
type Maintenance struct {
	Enabled   bool   `json:"enabled"`
	Level     string `json:"level"` // info, warning or critical
	Message   Bi     `json:"message"`
	BlockPlay bool   `json:"block_play"` // true: starting a game session answers 503 "maintenance"
}

func maintenanceDefault() Maintenance { return Maintenance{Level: "info"} }

func (m Maintenance) check() error {
	if m.Level != "info" && m.Level != "warning" && m.Level != "critical" {
		return errors.New("level must be info, warning or critical")
	}
	return bi("message", m.Message, 280, m.Enabled) // a shown banner needs both languages
}

// ---- ai ----

// AI switches the model-backed helpers. Env-only secrets (ANTHROPIC_API_KEY) are not here.
type AI struct {
	DraftEnabled    bool `json:"draft_enabled"`    // admin question AI drafts
	NarratorEnabled bool `json:"narrator_enabled"` // false: story text always comes from the canned bank
}

func aiDefault() AI { return AI{DraftEnabled: true, NarratorEnabled: true} }

// ---- copy (texts children and parents see) ----

// Copy holds admin-editable kid-visible sentences.
type Copy struct {
	HomeTip     Bi `json:"home_tip"`
	FeedEmpty   Bi `json:"feed_empty"`
	SessionDone Bi `json:"session_done"`
}

func copyDefault() Copy {
	return Copy{
		HomeTip:     Bi{"Un petit chapitre par jour fait grandir ton cerveau.", "One small chapter a day grows your brain."},
		FeedEmpty:   Bi{"Reviens plus tard, de nouvelles découvertes arrivent.", "Come back later, new discoveries are coming."},
		SessionDone: Bi{"Bravo, chapitre terminé !", "Well done, chapter finished!"},
	}
}

func (c Copy) check() error {
	for name, b := range map[string]Bi{"home_tip": c.HomeTip, "feed_empty": c.FeedEmpty, "session_done": c.SessionDone} {
		if err := bi(name, b, 200, true); err != nil {
			return err
		}
	}
	return nil
}

// bi checks a text pair: bounded length, no control characters, both languages when required.
func bi(name string, b Bi, max int, required bool) error {
	for lang, s := range map[string]string{"fr": b.FR, "en": b.EN} {
		if utf8.RuneCountInString(s) > max || strings.ContainsFunc(s, unicode.IsControl) {
			return fmt.Errorf("%s.%s must be at most %d characters without control characters", name, lang, max)
		}
		if required && strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s.%s is required", name, lang)
		}
	}
	return nil
}
