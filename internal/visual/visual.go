// Package visual builds "how to solve it" pictures as data (a kind, integer params, one caption per step).
// The client draws and animates them; no pixels here. Port of frontend/mock/lib/visual.mjs.
// The package is pure: it imports nothing from this repository.
//
// Go for Spring people: think of Build as a stateless @Service method returning a DTO.
package visual

import (
	"fmt"
	"strings"
)

// Step is one caption, shown and spoken one at a time.
type Step struct {
	Caption string `json:"caption"`
}

// MethodVisual is the wire shape of the contract's MethodVisual.
type MethodVisual struct {
	Kind     string         `json:"kind"`
	Params   map[string]int `json:"params"`
	Steps    []Step         `json:"steps"`
	Alt      string         `json:"alt"`
	CacheKey string         `json:"cache_key"`
}

// Kinds of picture.
const (
	AddCarry    = "add_carry"    // params a, b
	SubBorrow   = "sub_borrow"   // params a, b
	EqualGroups = "equal_groups" // params groups, size
	EqualShare  = "equal_share"  // params total, parts
)

// paramOrder is the order of params inside cache_key (JavaScript object order in the mock).
var paramOrder = map[string][2]string{
	AddCarry: {"a", "b"}, SubBorrow: {"a", "b"}, EqualGroups: {"groups", "size"}, EqualShare: {"total", "parts"},
}

// ones is the ones digit of |n|.
func ones(n int) int {
	if n < 0 {
		n = -n
	}
	return n % 10
}

// Build returns the picture for a kind and params in the language "fr" or "en" (anything else is fr).
// The second result is false when the kind is unknown or a param is missing.
func Build(kind string, params map[string]int, lang string) (MethodVisual, bool) {
	order, known := paramOrder[kind]
	if !known {
		return MethodVisual{}, false
	}
	x, okX := params[order[0]]
	y, okY := params[order[1]]
	if !okX || !okY {
		return MethodVisual{}, false
	}
	en := strings.HasPrefix(strings.ToLower(lang), "en")
	l := "fr"
	if en {
		l = "en"
	}
	var caps [3]string
	var alt string
	switch kind {
	case AddCarry:
		ua, ub := ones(x), ones(y)
		carry := ua+ub >= 10
		sum := x + y
		if en {
			caps = [3]string{
				fmt.Sprintf("Put the ones together: %d and %d make %d.", ua, ub, ua+ub),
				pick(carry, "Ten ones make one ten. Move it to the left.", "Nothing to regroup."),
				fmt.Sprintf("In all: %d.", sum),
			}
			alt = fmt.Sprintf("Blocks show %d plus %d. %sThe total is %d.", x, y, pick(carry, "Ten ones become one ten. ", ""), sum)
		} else {
			caps = [3]string{
				fmt.Sprintf("On met les unités ensemble : %d et %d font %d.", ua, ub, ua+ub),
				pick(carry, "Dix unités font une dizaine. On la déplace à gauche.", "Rien à regrouper."),
				fmt.Sprintf("Au total : %d.", sum),
			}
			alt = fmt.Sprintf("Des blocs montrent %d plus %d. %sLe total est %d.", x, y, pick(carry, "Dix unités deviennent une dizaine. ", ""), sum)
		}
	case SubBorrow:
		ua, ub := ones(x), ones(y)
		borrow := ua < ub
		diff := x - y
		if en {
			caps = [3]string{
				fmt.Sprintf("Start with %d.", x),
				pick(borrow, fmt.Sprintf("%d ones are not enough to take away %d. Break one ten into ten ones.", ua, ub), "Enough ones. Nothing to break."),
				fmt.Sprintf("Take away %d. %d are left.", y, diff),
			}
			alt = fmt.Sprintf("Blocks show %d. %sTake away %d: %d are left.", x, pick(borrow, "One ten is broken into ten ones. ", ""), y, diff)
		} else {
			caps = [3]string{
				fmt.Sprintf("On part de %d.", x),
				pick(borrow, fmt.Sprintf("%d unités, ce n’est pas assez pour en enlever %d. On casse une dizaine en dix unités.", ua, ub), "Assez d’unités. Rien à casser."),
				fmt.Sprintf("On enlève %d. Il reste %d.", y, diff),
			}
			alt = fmt.Sprintf("Des blocs montrent %d. %sOn enlève %d : il reste %d.", x, pick(borrow, "Une dizaine est cassée en dix unités. ", ""), y, diff)
		}
	case EqualGroups:
		groups, size := x, y
		total := groups * size
		if en {
			caps = [3]string{fmt.Sprintf("%d groups.", groups), fmt.Sprintf("%d in each group.", size), fmt.Sprintf("%d groups of %d make %d.", groups, size, total)}
			alt = fmt.Sprintf("%d groups of %d dots make %d.", groups, size, total)
		} else {
			caps = [3]string{fmt.Sprintf("%d groupes.", groups), fmt.Sprintf("%d dans chaque groupe.", size), fmt.Sprintf("%d groupes de %d font %d.", groups, size, total)}
			alt = fmt.Sprintf("%d groupes de %d points font %d.", groups, size, total)
		}
	default: // EqualShare
		total, parts := x, y
		if parts <= 0 {
			return MethodVisual{}, false // cannot share into zero parts
		}
		q := total / parts
		if en {
			caps = [3]string{fmt.Sprintf("We have %d.", total), fmt.Sprintf("Share them into %d parts, one at a time.", parts), fmt.Sprintf("Each part gets %d.", q)}
			alt = fmt.Sprintf("%d dots are shared into %d equal parts of %d.", total, parts, q)
		} else {
			caps = [3]string{fmt.Sprintf("On a %d.", total), fmt.Sprintf("On partage en %d parts, un à la fois.", parts), fmt.Sprintf("Chaque part reçoit %d.", q)}
			alt = fmt.Sprintf("%d points sont partagés en %d parts égales de %d.", total, parts, q)
		}
	}
	steps := make([]Step, len(caps))
	for i, c := range caps {
		steps[i] = Step{Caption: c}
	}
	return MethodVisual{
		Kind: kind, Params: map[string]int{order[0]: x, order[1]: y}, Steps: steps, Alt: alt,
		CacheKey: fmt.Sprintf("%s:%d,%d:%s", kind, x, y, l),
	}, true
}

// pick returns a when cond is true, else b (Go has no ternary operator).
func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
