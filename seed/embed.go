// Package seed embeds the small word lists the question validator needs, so the API server (admin publish and
// validate endpoints) uses exactly the same lists as cmd/seed and the tests. The JSON seed files themselves are
// read from disk by package seedload.
//
// Go for Spring people: //go:embed copies the listed files into the executable at build time, like files
// under src/main/resources. It only works for files inside this package's directory, which is why this tiny
// Go package lives next to the seed files.
package seed

import (
	_ "embed" // enables the //go:embed directives below
	"strings"
)

//go:embed schema/denylist.txt
var denylist string

//go:embed schema/names.txt
var names string

// Lines returns the non-empty, non-comment lines of an embedded list.
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// Denylist returns the brand and rude words refused in kid text (unfolded, one entry per line).
func Denylist() []string { return lines(denylist) }

// Names returns the allowed first names and proper nouns (unfolded).
func Names() []string { return lines(names) }
