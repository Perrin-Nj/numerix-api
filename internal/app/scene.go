package app

// Scene keys: a hint that tells the client which consequence scene to play (R3 plan, B1).
//
// Go for Spring people: a package-level func with a lower-case name is "package private" (no public modifier
// needed); a switch on a string needs no break, each case ends by itself.

import (
	"strings"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
)

// maxVariantHistory is how many recent scene ids a session keeps (oldest dropped first).
const maxVariantHistory = 30

// maxVariantIDLen is the longest scene id the server accepts (matches maxLength in the contract).
const maxVariantIDLen = 64

// sceneFamily maps a skill id to the topic family of its consequence scenes.
// shortcut: by id prefix, no registry lookup; "time" has no skill prefix yet, so it is never returned.
func sceneFamily(skillID string) string {
	switch {
	case strings.HasPrefix(skillID, "ARI-SEN-B"):
		return "fractions"
	case strings.HasPrefix(skillID, "ARI-SEN-"):
		return "number"
	case strings.HasPrefix(skillID, "ARI-SOP-"), strings.HasPrefix(skillID, "ARI-OPE-C"):
		return "add_sub"
	case strings.HasPrefix(skillID, "ARI-OPE-A"):
		// A01 to A05 are add and subtract facts, A06 and up multiply and divide
		if n := skillID[len("ARI-OPE-A"):]; len(n) >= 2 && n[:2] >= "06" {
			return "mult_div"
		}
		return "add_sub"
	case strings.HasPrefix(skillID, "GEO-"):
		return "geometry"
	case strings.HasPrefix(skillID, "MES-"):
		return "measure"
	case strings.HasPrefix(skillID, "STA-"), strings.HasPrefix(skillID, "PRO-"):
		return "data"
	}
	return "number" // unknown skill: the neutral family, never an empty key
}

// sceneKeyFor is the family, plus ":" and the misconception id when the answer was a typical error.
func sceneKeyFor(skillID, outcome, misconception string) string {
	k := sceneFamily(skillID)
	if outcome == string(engine.ErreurTypique) && misconception != "" {
		k += ":" + misconception
	}
	return k
}

// cleanVariants checks the client's history (422 field message on abuse) and returns it as is.
func cleanVariants(in []string) ([]string, string) {
	if len(in) > maxVariantHistory {
		return nil, "30 ids at most"
	}
	for _, id := range in {
		if id == "" || len(id) > maxVariantIDLen {
			return nil, "each id is 1 to 64 characters"
		}
	}
	return in, ""
}

// sceneKeyPtr gives the contract's optional string.
func sceneKeyPtr(k string) *api.SceneKey { return &k }
