package content

// Number words in French and English, used by the leak check: an answer written as "quinze" or "fifteen"
// must not appear in the text the child reads before answering, just like the digits "15".

import "strings"

// MaxWordNumber is the largest number NumberWords can spell (plan 3.5: up to 1 000 000).
const MaxWordNumber = 1_000_000

var (
	enUnits = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
		"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
	enTens = []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}

	frUnits = []string{"zero", "un", "deux", "trois", "quatre", "cinq", "six", "sept", "huit", "neuf", "dix",
		"onze", "douze", "treize", "quatorze", "quinze", "seize", "dix-sept", "dix-huit", "dix-neuf"}
	frTens = []string{"", "", "vingt", "trente", "quarante", "cinquante", "soixante"}
)

// NumberWords returns the spellings of n (0 to 1 000 000) in the language ("fr" or "en"). Several variants
// can exist ("quatre-vingts" and "quatre-vingt"). It returns nil outside the range.
func NumberWords(n int, lang string) []string {
	if n < 0 || n > MaxWordNumber {
		return nil
	}
	if lang == "en" {
		return []string{enWords(n)}
	}
	w := frWords(n)
	out := []string{w}
	// the plural "s" of vingt/cent is dropped in some forms: accept the singular spelling too
	if sing := strings.NewReplacer("quatre-vingts", "quatre-vingt", "cents", "cent").Replace(w); sing != w {
		out = append(out, sing)
	}
	return out
}

// enWords spells n in English ("one hundred forty-two").
func enWords(n int) string {
	switch {
	case n < 20:
		return enUnits[n]
	case n < 100:
		if n%10 == 0 {
			return enTens[n/10]
		}
		return enTens[n/10] + "-" + enUnits[n%10]
	case n < 1000:
		s := enUnits[n/100] + " hundred"
		if n%100 != 0 {
			s += " " + enWords(n%100)
		}
		return s
	case n < 1_000_000:
		s := enWords(n/1000) + " thousand"
		if n%1000 != 0 {
			s += " " + enWords(n%1000)
		}
		return s
	}
	return "one million"
}

// frWords spells n in French (Quebec/France spelling: soixante-dix, quatre-vingts).
func frWords(n int) string {
	switch {
	case n < 20:
		return frUnits[n]
	case n < 70:
		t, u := n/10, n%10
		switch u {
		case 0:
			return frTens[t]
		case 1:
			return frTens[t] + " et un"
		}
		return frTens[t] + "-" + frUnits[u]
	case n < 80: // 70 to 79: soixante + 10 to 19
		if n == 71 {
			return "soixante et onze"
		}
		return "soixante-" + frUnits[n-60]
	case n < 100: // 80 to 99: quatre-vingt + 0 to 19
		if n == 80 {
			return "quatre-vingts"
		}
		return "quatre-vingt-" + frUnits[n-80]
	case n < 1000:
		h, r := n/100, n%100
		var s string
		if h == 1 {
			s = "cent"
		} else {
			s = frUnits[h] + " cent"
			if r == 0 {
				s += "s"
			}
		}
		if r != 0 {
			s += " " + frWords(r)
		}
		return s
	case n < 1_000_000:
		th, r := n/1000, n%1000
		var s string
		if th == 1 {
			s = "mille"
		} else {
			s = frWords(th) + " mille"
		}
		if r != 0 {
			s += " " + frWords(r)
		}
		return s
	}
	return "un million"
}

// Fold lowercases text, strips accents and turns hyphens and apostrophes into spaces, so
// "Quatre-Vingt-Dix" and "quatre vingt dix" compare equal.
func Fold(s string) string {
	return foldReplacer.Replace(strings.ToLower(s))
}

// foldReplacer maps accented letters to plain ones and separators to spaces (strings.NewReplacer is safe for concurrent use).
var foldReplacer = strings.NewReplacer(
	"é", "e", "è", "e", "ê", "e", "ë", "e", "à", "a", "â", "a", "ä", "a", "î", "i", "ï", "i", "ô", "o", "ö", "o",
	"ù", "u", "û", "u", "ü", "u", "ç", "c", "œ", "oe", "-", " ", "'", " ", "’", " ",
)
