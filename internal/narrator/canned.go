package narrator

import (
	"context"
	_ "embed" // enables the //go:embed directive below
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// kitJSON is the story kit per world (heroes, place, object, titles, scene openings and denouements,
// fr and en), exported from frontend/mock/lib/kit.mjs. Compiled into the binary.
//
//go:embed data/kit.json
var kitJSON []byte

// world is one world's story kit. Each text is a pair [fr, en]; hero is [name, role fr, role en].
type world struct {
	Heroes [][]string `json:"h"`
	Place  []string   `json:"p"`
	Object []string   `json:"o"`
	Titles [][]string `json:"t"`
	Open   [][]string `json:"op"`
	OK     [][]string `json:"ok"`
	Typ    [][]string `json:"dt"`
	Other  [][]string `json:"su"`
}

// kit is parsed once at start.
var kit = func() map[string]world {
	var k map[string]world
	if err := json.Unmarshal(kitJSON, &k); err != nil {
		panic("narrator: bad embedded kit.json: " + err.Error()) // a build-time mistake, not a runtime one
	}
	return k
}()

// worldOf returns the kit of a world, nature when unknown.
func worldOf(id string) world {
	if w, ok := kit[id]; ok {
		return w
	}
	return kit["nature"]
}

// mix is the deterministic hash of the mock (FNV-like, 32 bit): same inputs, same pick, in Go and in JS.
func mix(vals ...int) uint32 {
	x := uint32(2166136261)
	for _, v := range vals {
		x = (x ^ uint32(int32(v))) * 16777619
		x ^= x >> 13
	}
	x = (x ^ (x >> 15)) * 2246822507
	return x ^ (x >> 13)
}

// pickPair chooses one [fr,en] pair deterministically.
func pickPair(arr [][]string, seed uint32, i, salt int) []string {
	return arr[int(mix(int(seed), i, salt))%len(arr)]
}

// lang returns 0 for French, 1 for English.
func lang(l string) int {
	if strings.HasPrefix(strings.ToLower(l), "en") {
		return 1
	}
	return 0
}

// HeroOf returns the session's hero for a world: name, role in the chosen language.
func HeroOf(worldID string, seed uint32, l string) (name, role string) {
	h := worldOf(worldID).Heroes
	hero := h[int(mix(int(seed), 1))%len(h)]
	return hero[0], hero[1+lang(l)]
}

// PlaceOf returns the place phrase of a world ("dans la forêt de Brume").
func PlaceOf(worldID, l string) string { return worldOf(worldID).Place[lang(l)] }

// TitleIndex picks which of the world's three story titles a session uses.
func TitleIndex(seed uint32) int { return int(mix(int(seed), 99)) % 3 }

// TitleOf returns the story title for a world, title index and language.
func TitleOf(worldID string, idx int, l string) string {
	t := worldOf(worldID).Titles
	return t[idx%len(t)][lang(l)]
}

// deAprostrophe turns "de A..." into "d’A..." in French.
var deVowel = regexp.MustCompile(`\bde ([AEIOUÉ])`)

// slotRe finds {name} placeholders.
var slotRe = regexp.MustCompile(`\{(\w+)\}`)

// fill replaces {h} hero, {r} role, {p} place, {o} object, {do} "of object" and any extra slot.
func fill(text string, w world, seed uint32, l int, extra map[string]string) string {
	hero := w.Heroes[int(mix(int(seed), 1))%len(w.Heroes)]
	obj := w.Object[l]
	of := "de " + obj
	if l == 0 && regexp.MustCompile(`(?i)^[aeiouhœéèêàâîôû]`).MatchString(obj) {
		of = "d’" + obj
	}
	slots := map[string]string{"h": hero[0], "r": hero[1+l], "p": w.Place[l], "o": obj, "do": of}
	for k, v := range extra {
		slots[k] = v
	}
	out := slotRe.ReplaceAllStringFunc(text, func(m string) string { return slots[m[1:len(m)-1]] })
	if l == 1 {
		return out
	}
	return deVowel.ReplaceAllString(out, "d’$1")
}

// mathText holds the plain-words statements per operation family, [fr, en] (ported from session.mjs).
var mathText = map[string][][]string{
	"add":     {{"{h} a {a} {o}. {h} en trouve {b} de plus. Combien {do} y a-t-il en tout ?", "{h} has {a} {o}. {h} finds {b} more. How many {o} are there in all?"}, {"La première boîte a {a} {o}. La seconde en a {b}. Combien {do} en tout ?", "The first box has {a} {o}. The second box has {b}. How many {o} in all?"}},
	"sub":     {{"{h} avait {a} {o}. {h} en donne {b}. Combien lui en reste-t-il ?", "{h} had {a} {o}. {h} gives away {b}. How many are left?"}, {"Il y a {a} {o} dans le sac. On en enlève {b}. Combien en reste-t-il ?", "There are {a} {o} in the bag. {b} are taken out. How many are left?"}},
	"missing": {{"{h} a {a} {o}. {h} veut en avoir {c}. Combien lui en manque-t-il ?", "{h} has {a} {o}. {h} wants to have {c}. How many more are needed?"}, {"Il faut {c} {o} en tout. Il y en a déjà {a}. Combien en manque-t-il ?", "{c} {o} are needed in all. There are already {a}. How many are missing?"}},
	"mult":    {{"Il y a {b} rangées de {a} {o}. Combien {do} en tout ?", "There are {b} rows of {a} {o}. How many {o} in all?"}, {"{h} a {b} paniers. Il y a {a} {o} dans chaque panier. Combien {do} en tout ?", "{h} has {b} baskets. Each basket holds {a} {o}. How many {o} in all?"}},
	"div":     {{"On partage {N} {o} entre {k} boîtes. Combien {do} dans chaque boîte ?", "We share {N} {o} between {k} boxes. How many in each box?"}, {"{h} a {N} {o}. {h} les range en {k} tas égaux. Combien {do} dans chaque tas ?", "{h} has {N} {o}. {h} makes {k} equal piles. How many {o} in each pile?"}},
	"divrem":  {{"{h} a {N} {o}. {h} remplit des sacs de {k}. Combien de sacs pleins ? Il en reste combien ?", "{h} has {N} {o}. {h} fills bags of {k}. How many full bags? How many are left over?"}, {"On range {N} {o} par groupes de {k}. Combien de groupes pleins ? Il en reste combien ?", "{N} {o} go into groups of {k}. How many full groups? How many are left over?"}},
	"generic": {{"Voici le défi de {h} : {m}", "Here is {h}’s challenge: {m}"}},
}

// Statement shapes recognised in the displayed statement.
var (
	reAdd     = regexp.MustCompile(`^\d+ \+ \d+ = \?$`)
	reSub     = regexp.MustCompile(`^\d+ [−-] \d+ = \?$`)
	reMissing = regexp.MustCompile(`^\d+ \+ \? = \d+$`)
	reMult    = regexp.MustCompile(`^\d+ × \d+ = \?$`)
	reDiv     = regexp.MustCompile(`^\d+ ÷ \d+ = \?$`)
	reNumber  = regexp.MustCompile(`\d+`)
)

// family names the operation family of a displayed statement ("generic" when it is a sentence).
func family(math string) string {
	m := strings.TrimSpace(math)
	switch {
	case strings.Contains(m, "÷") && (strings.Contains(m, "(avec reste)") || strings.Contains(m, "(with remainder)")):
		return "divrem"
	case reAdd.MatchString(m):
		return "add"
	case reSub.MatchString(m):
		return "sub"
	case reMissing.MatchString(m):
		return "missing"
	case reMult.MatchString(m):
		return "mult"
	case reDiv.MatchString(m):
		return "div"
	}
	return "generic"
}

// slotsFromStatement reads the numbers the child can already see in the statement.
func slotsFromStatement(math, fam string) map[string]string {
	nums := reNumber.FindAllString(math, -1)
	at := func(i int) string {
		if i < len(nums) {
			return nums[i]
		}
		return ""
	}
	switch fam {
	case "add", "sub", "mult":
		return map[string]string{"a": at(0), "b": at(1)}
	case "missing":
		return map[string]string{"a": at(0), "c": at(1)}
	case "div", "divrem":
		return map[string]string{"N": at(0), "k": at(1)}
	}
	return map[string]string{"m": math}
}

// Canned is the offline provider: texts come from the kit, so it works with no network and no budget.
type Canned struct{}

// NewCanned returns the canned provider.
func NewCanned() *Canned { return &Canned{} }

// Name identifies the provider in logs and the narrations table.
func (*Canned) Name() string { return "canned" }

// Generate renders the scene (opening plus plain-words statement) or the denouement for the outcome.
// It reads only the statement the child sees, never an answer.
func (*Canned) Generate(_ context.Context, r Request) (string, error) {
	w := worldOf(r.World)
	l := lang(r.Lang)
	switch r.Phase {
	case PhaseScene:
		fam := family(r.MathText)
		statements := mathText[fam]
		parts := []string{
			fill(pickPair(w.Open, r.Seed, r.Index, 11)[l], w, r.Seed, l, nil),
			fill(pickPair(statements, r.Seed, r.Index, 12)[l], w, r.Seed, l, slotsFromStatement(r.MathText, fam)),
		}
		return strings.Join(parts, " "), nil
	case PhaseDenouement:
		var bank [][]string
		var salt int
		switch r.Outcome {
		case "correct":
			bank, salt = w.OK, 21
		case "erreur_typique":
			bank, salt = w.Typ, 22
		default:
			bank, salt = w.Other, 23
		}
		return fill(pickPair(bank, r.Seed, r.Index, salt)[l], w, r.Seed, l, nil), nil
	}
	return "", fmt.Errorf("unknown narration phase %q", r.Phase)
}

// Words splits a text into SSE chunks of about four words, like the mock (used by the stream handler).
func Words(text string) []string {
	words := strings.Split(text, " ")
	var chunks []string
	for i := 0; i < len(words); i += 4 {
		end := min(i+4, len(words))
		c := strings.Join(words[i:end], " ")
		if end < len(words) {
			c += " "
		}
		chunks = append(chunks, c)
	}
	return chunks
}
