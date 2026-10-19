package app

// Go for Spring people: this file holds the small helpers shared by the platform_*.go files
// (Spring: a package-private @Component with utility methods). Files in one Go package see each
// other's lowercase names freely, there is no import between them.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // embeds the IANA time zone database so LoadLocation works on Windows and scratch images

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/auth"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// plTSLayout is the timestamp text stored in SQLite: UTC ISO-8601 with milliseconds (same as the column defaults).
const plTSLayout = "2006-01-02T15:04:05.000Z"

// plDay is 24 hours (Go has no time.Day constant).
const plDay = 24 * time.Hour

// plMaxChildren is the family limit (contract: at most 8 profiles per parent).
const plMaxChildren = 8

// plWorldIDs are the twenty worlds of the contract (WorldId enum). A new child starts with all of them enabled.
var plWorldIDs = []string{"nature", "espace", "cuisine", "sport", "construction", "medecine", "science", "echecs",
	"ocean", "dinosaures", "animaux", "musique", "transport", "pirates", "marche", "meteo", "robots", "arts", "hiver", "jeux"}

// plPasswordMax caps password length before argon2 runs (QA-BE-001).
const plPasswordMax = 128

// plPinRe is the PIN format: exactly four digits.
var plPinRe = regexp.MustCompile(`^[0-9]{4}$`)

// plTS formats a time for storage.
func plTS(t time.Time) string { return t.UTC().Format(plTSLayout) }

// plParseTS reads a stored timestamp; an empty or bad value gives the zero time.
func plParseTS(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// plNullTime turns a nullable DB text column into *time.Time for the JSON models (nil when NULL).
func plNullTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t := plParseTS(s.String)
	if t.IsZero() {
		return nil
	}
	return &t
}

// plCaller returns the authenticated principal. The Guard already refused anonymous calls on protected
// operations, so this is only a safety net (Spring: SecurityContextHolder.getContext().getAuthentication()).
func plCaller(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.FromContext(ctx)
	if !ok {
		return auth.Principal{}, httpapi.ErrUnauthorized
	}
	return p, nil
}

// plInternalErr logs the real cause and returns the error unchanged; WriteError turns it into a generic 500.
func (s *Server) plInternalErr(ctx context.Context, what string, err error) error {
	s.Log.Error(what, "request_id", httpapi.RequestID(ctx), "err", err)
	return fmt.Errorf("%s: %w", what, err)
}

// plUID converts a uuid.UUID path value to its text.
func plUID(id openapi_types.UUID) string { return uuid.UUID(id).String() }

// plUUID converts stored text to the contract's UUID type (zero UUID when malformed, never panics).
func plUUID(s string) openapi_types.UUID {
	u, _ := uuid.Parse(s)
	return openapi_types.UUID(u)
}

// locale normalises a language to a stored locale: en* gives en-CA, anything else gives fr-CA.
func plLocaleOf(accept *string, fallback string) string {
	if accept == nil || *accept == "" {
		return fallback
	}
	if strings.HasPrefix(strings.ToLower(*accept), "en") {
		return "en-CA"
	}
	return "fr-CA"
}

// plValidLocale tells whether l is one of the two supported locales.
func plValidLocale(l string) bool { return l == "fr-CA" || l == "en-CA" }

// plChild is one row of the children table plus the derived consent flag.
type plChild struct {
	ID, ParentID, DisplayName, Locale, Timezone string
	BirthYear, Grade                            sql.NullInt64
	AvatarID, PinHash, FavoriteWorld            sql.NullString
	Onboarded, Consented                        bool
	CreatedAt                                   string
}

// plChildCols is the SELECT list matching plScanChild (Spring: the column mapping of a RowMapper).
const plChildCols = `c.id, c.parent_id, c.display_name, c.locale, c.timezone, c.birth_year, c.grade, c.avatar_id,
	c.pin_hash, c.favorite_world, c.onboarding_completed, c.created_at,
	EXISTS (SELECT 1 FROM consents k WHERE k.child_id = c.id AND k.revoked_at IS NULL)`

// plRowScanner is satisfied by both *sql.Row and *sql.Rows, so one scan function serves get and list.
type plRowScanner interface{ Scan(dest ...any) error }

func plScanChild(r plRowScanner) (plChild, error) {
	var c plChild
	var onboarded, consented int
	err := r.Scan(&c.ID, &c.ParentID, &c.DisplayName, &c.Locale, &c.Timezone, &c.BirthYear, &c.Grade, &c.AvatarID,
		&c.PinHash, &c.FavoriteWorld, &onboarded, &c.CreatedAt, &consented)
	c.Onboarded, c.Consented = onboarded != 0, consented != 0
	return c, err
}

// toAPI builds the public Child JSON. The PIN hash never leaves: only has_pin.
func (c plChild) toAPI() api.Child {
	created := plParseTS(c.CreatedAt)
	tz := c.Timezone
	out := api.Child{
		Id:                  plUUID(c.ID),
		DisplayName:         c.DisplayName,
		AvatarId:            c.AvatarID.String,
		Grade:               int(c.Grade.Int64),
		HasPin:              c.PinHash.Valid && c.PinHash.String != "",
		ConsentGiven:        c.Consented,
		Timezone:            &tz,
		CreatedAt:           &created,
		OnboardingCompleted: &c.Onboarded,
	}
	if c.BirthYear.Valid {
		by := int(c.BirthYear.Int64)
		out.BirthYear = &by
	}
	if c.FavoriteWorld.Valid {
		w := api.WorldId(c.FavoriteWorld.String)
		out.FavoriteWorld = &w
	}
	return out
}

// plOwnedChild loads a child the plCaller may act on. A parent sees only its own children (else 404, so
// ids of other families are not confirmed); a child token may only touch itself (else 403).
func (s *Server) plOwnedChild(ctx context.Context, p auth.Principal, childID string) (plChild, error) {
	if !p.OwnsChild(childID) {
		return plChild{}, httpapi.ErrForbidden
	}
	row := s.Store.R.QueryRowContext(ctx,
		`SELECT `+plChildCols+` FROM children c WHERE c.id = ? AND c.parent_id = ? AND c.deleted_at IS NULL`, childID, p.ParentID)
	c, err := plScanChild(row)
	if errors.Is(err, sql.ErrNoRows) {
		return plChild{}, httpapi.ErrNotFound
	}
	if err != nil {
		return plChild{}, s.plInternalErr(ctx, "load child", err)
	}
	return c, nil
}

// plField builds one validation error entry.
func plField(name, msg string) httpapi.FieldError {
	return httpapi.FieldError{Field: name, Message: msg}
}

// plConflict is a 409 with a stable code.
func plConflict(code, detail string) *httpapi.APIError {
	return httpapi.NewError(http.StatusConflict, code, detail)
}
