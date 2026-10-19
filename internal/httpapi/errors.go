// Package httpapi is the HTTP layer: error format, middleware, rate limits and the router assembly.
//
// Go for Spring people: middleware here are plain functions func(http.Handler) http.Handler
// (Spring: servlet Filters / HandlerInterceptors). Each one wraps the next handler and runs code before/after it.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Perrin-Nj/numerix-api/api"
)

// FieldError names one invalid field (contract: Problem.errors[]).
type FieldError struct {
	Field   string
	Message string
}

// APIError is an error that knows its HTTP answer. Handlers return it (or wrap it with %w) and the
// single WriteError function turns it into problem+json (Spring: @ControllerAdvice + @ExceptionHandler).
type APIError struct {
	Status     int
	Code       string // stable machine code the UI switches on, e.g. consent_missing
	Detail     string // optional human sentence; never a stack trace, never personal data
	RetryAfter int    // seconds; sent on 429 and 503
	Fields     []FieldError
}

// Error makes *APIError satisfy the built-in error interface.
func (e *APIError) Error() string { return e.Code }

// NewError builds an APIError.
func NewError(status int, code, detail string) *APIError {
	return &APIError{Status: status, Code: code, Detail: detail}
}

// Common errors, ready to return.
var (
	ErrNotImplemented = NewError(http.StatusNotImplemented, "not_implemented", "This feature is not available yet.")
	ErrUnauthorized   = NewError(http.StatusUnauthorized, "unauthorized", "")
	ErrForbidden      = NewError(http.StatusForbidden, "forbidden", "")
	ErrNotFound       = NewError(http.StatusNotFound, "not_found", "")
)

// RateLimited builds a 429 with the retry delay.
func RateLimited(retryAfterSeconds int) *APIError {
	return &APIError{Status: http.StatusTooManyRequests, Code: "rate_limited", RetryAfter: max(1, retryAfterSeconds)}
}

// Validation builds a 422 validation_failed with field errors.
func Validation(fields ...FieldError) *APIError {
	return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Fields: fields}
}

// titles holds the short, kind titles per code in French and English (contract section 4).
var titles = map[string][2]string{ // [fr, en]
	"unauthorized":           {"Ta session est terminée", "Your session has ended"},
	"forbidden":              {"Accès refusé", "Access denied"},
	"not_found":              {"Introuvable", "Not found"},
	"validation_failed":      {"Vérifie les informations", "Please check the information"},
	"rate_limited":           {"Doucement", "Slow down a little"},
	"internal_error":         {"Quelque chose s’est mal passé", "Something went wrong"},
	"not_implemented":        {"Pas encore disponible", "Not available yet"},
	"overloaded":             {"Beaucoup de monde en ce moment", "We are very busy right now"},
	"service_unavailable":    {"Service indisponible", "Service unavailable"},
	"not_configured":         {"Pas configuré", "Not configured"},
	"version_conflict":       {"Version dépassée", "Version out of date"},
	"transition_not_allowed": {"Changement d’état impossible", "State change not allowed"},
	"needs_reviewer":         {"Un autre relecteur est requis", "Another reviewer is needed"},
}

// titleFor picks the title for a code; unknown codes get a generic title for their status class.
func titleFor(code string, status int, french bool) string {
	t, ok := titles[code]
	if !ok && status >= 500 {
		t = titles["internal_error"]
	} else if !ok {
		t = titles["forbidden"]
	}
	if french {
		return t[0]
	}
	return t[1]
}

// WriteError sends err as application/problem+json (RFC 9457). Anything that is not an *APIError
// becomes a generic 500: the real cause is logged by the caller, never shown to the client.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *APIError
	if !errors.As(err, &ae) {
		ae = NewError(http.StatusInternalServerError, "internal_error", "")
	}
	french := !strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "en") // default fr-CA
	p := api.Problem{
		Type:   "https://numerix.app/problems/" + strings.ReplaceAll(ae.Code, "_", "-"),
		Title:  titleFor(ae.Code, ae.Status, french),
		Status: ae.Status,
		Code:   &ae.Code,
	}
	if ae.Detail != "" {
		p.Detail = &ae.Detail
	}
	if ae.RetryAfter > 0 {
		p.RetryAfterSeconds = &ae.RetryAfter
		w.Header().Set("Retry-After", strconv.Itoa(ae.RetryAfter)) // contract rule 6: header AND body
	}
	if len(ae.Fields) > 0 {
		fields := make([]struct {
			Field   *string `json:"field,omitempty"`
			Message *string `json:"message,omitempty"`
		}, len(ae.Fields))
		for i := range ae.Fields {
			fields[i].Field, fields[i].Message = &ae.Fields[i].Field, &ae.Fields[i].Message
		}
		p.Errors = &fields
	}
	if ae.Status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(ae.Status)
	_ = json.NewEncoder(w).Encode(p) // headers are sent; an encode failure here cannot be reported
}
