// Package httpapi holds the HTTP layer: handlers and the router.
//
// Go for Spring people:
//   - A "package" is a folder of .go files that share one namespace (Spring: a Java package).
//   - Names that start with a Capital letter are public; lowercase names are private to the package.
//     Go has no "public" or "private" keywords, the first letter decides.
package httpapi

import (
	"encoding/json" // standard library JSON encoder (Spring: Jackson, but built in)
	"net/http"      // standard library web server types (Spring: Servlet API + Spring MVC basics)
)

// healthBody is the JSON shape returned by GET /health.
// A struct is a bag of named fields (Spring: a DTO / record). The `json:"..."` text after each
// field is a "tag": it tells the JSON encoder which key name to use (Spring: @JsonProperty).
type healthBody struct {
	Status string `json:"status"` // always "ok" while the process can answer
}

// Health answers GET /health. Load balancers and the keep-alive job call it.
//
// A handler in Go is just a function with two parameters:
//   - w http.ResponseWriter: where you write the reply (Spring: HttpServletResponse)
//   - r *http.Request: the incoming request (Spring: HttpServletRequest); the * means "pointer",
//     so the function reads the caller's request instead of copying it.
func Health(w http.ResponseWriter, r *http.Request) {
	// Tell the client the body is JSON (Spring: produces = "application/json").
	w.Header().Set("Content-Type", "application/json")

	// Encode the struct straight into the response. NewEncoder(w) wraps the writer;
	// Encode(...) writes the JSON. It returns an error value; Go has no exceptions,
	// so errors are returned and must be checked.
	if err := json.NewEncoder(w).Encode(healthBody{Status: "ok"}); err != nil {
		// Headers are already sent, so all we can do is return an HTTP 500 attempt; real logging comes in phase 2.
		http.Error(w, "encode failed", http.StatusInternalServerError)
	}
}

// NewRouter builds the table of URL -> handler (Spring: the @RequestMapping annotations, gathered in one place).
// Since Go 1.22 a pattern can start with the HTTP method: "GET /health" matches only GET.
func NewRouter() http.Handler {
	mux := http.NewServeMux()             // := declares and assigns in one step; the type (*http.ServeMux) is inferred
	mux.HandleFunc("GET /health", Health) // register Health for GET /health
	return mux                            // *ServeMux satisfies the http.Handler interface, so it can be returned as one
}
