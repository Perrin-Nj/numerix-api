package httpapi

// Cookie access for handlers.
//
// Go for Spring people: generated strict handlers receive only (ctx, typed request) and return a typed
// response, so they cannot touch cookies or headers. Guard stores the raw ResponseWriter and Request in the
// context (WithExchange) and handlers use ReadCookie / SetCookie below. Think of it as RequestContextHolder
// in Spring: a thread-local-like way to reach HttpServletRequest and HttpServletResponse from a service.

import (
	"context"
	"net/http"
)

// exchangeKey is a private type so no other package can collide with our context key.
type exchangeKey struct{}

// exchange pairs the response writer and the request of one HTTP call.
type exchange struct {
	w http.ResponseWriter
	r *http.Request
}

// WithExchange returns a copy of ctx that carries the writer and the request (called by Guard).
func WithExchange(ctx context.Context, w http.ResponseWriter, r *http.Request) context.Context {
	return context.WithValue(ctx, exchangeKey{}, exchange{w: w, r: r})
}

// ReadCookie returns the value of a request cookie, or "" when absent or when there is no HTTP request
// in the context (unit tests that call a handler directly).
func ReadCookie(ctx context.Context, name string) string {
	ex, ok := ctx.Value(exchangeKey{}).(exchange)
	if !ok {
		return ""
	}
	c, err := ex.r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// SetCookie queues a Set-Cookie header on the response. The strict handler writes the body after the
// handler returns, so headers added here are still sent. It does nothing without an HTTP exchange.
func SetCookie(ctx context.Context, c *http.Cookie) {
	if ex, ok := ctx.Value(exchangeKey{}).(exchange); ok {
		http.SetCookie(ex.w, c)
	}
}

// UserAgent returns the User-Agent header of the current request ("" when unknown).
func UserAgent(ctx context.Context) string {
	if ex, ok := ctx.Value(exchangeKey{}).(exchange); ok {
		return ex.r.UserAgent()
	}
	return ""
}
