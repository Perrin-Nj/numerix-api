package content

import (
	"encoding/json"
	"fmt"
	"math/big"
)

// ApplyCalc makes the server, not the author or a model, the authority on the numbers of a draft. When the
// answer carries a `calc` expression, the stored number (numeric value or range exact_for_check) is replaced by
// the exact result of the expression. It returns one sentence per correction and a flag telling that nothing
// could be verified (a numeric or range answer without a usable calc).
func (q *Question) ApplyCalc() (corrections []string, unverified bool) {
	a := &q.Answer
	switch a.Kind {
	case KindNumeric:
		if a.Calc == "" {
			return nil, true
		}
		v, err := Eval(a.Calc)
		if err != nil {
			return []string{fmt.Sprintf("calc %q could not be evaluated: %v", a.Calc, err)}, true
		}
		raw, ok := valueJSON(v)
		if !ok {
			return []string{fmt.Sprintf("calc %q does not give a finite decimal", a.Calc)}, true
		}
		if !jsonEqual(a.Value, raw) {
			corrections = append(corrections, fmt.Sprintf("answer value %s replaced by the result of calc %q: %s", string(a.Value), a.Calc, string(raw)))
			a.Value = raw
		}
	case KindRange:
		if a.Calc == "" {
			return nil, true
		}
		v, err := Eval(a.Calc)
		if err != nil || v.IsPair {
			return []string{fmt.Sprintf("calc %q could not be used for exact_for_check", a.Calc)}, true
		}
		if s := RatString(v.R); s != "" && !isFraction(s) && json.Number(s) != a.ExactForCheck {
			corrections = append(corrections, fmt.Sprintf("exact_for_check %q replaced by the result of calc %q: %s", a.ExactForCheck.String(), a.Calc, s))
			a.ExactForCheck = json.Number(s)
		}
	case KindChoice:
		if len(a.CalcChoice) == 0 {
			unverified = true
		}
	}
	return corrections, unverified
}

// isFraction reports whether a RatString result is a/b rather than a decimal.
func isFraction(s string) bool {
	for _, c := range s {
		if c == '/' {
			return true
		}
	}
	return false
}

// valueJSON renders a calc result as a JSON number or [quotient, remainder].
func valueJSON(v Value) (json.RawMessage, bool) {
	one := func(r *big.Rat) (string, bool) {
		s := RatString(r)
		return s, !isFraction(s)
	}
	if v.IsPair {
		a, ok1 := one(v.Pair[0])
		b, ok2 := one(v.Pair[1])
		if !ok1 || !ok2 {
			return nil, false
		}
		return json.RawMessage("[" + a + "," + b + "]"), true
	}
	s, ok := one(v.R)
	return json.RawMessage(s), ok
}

// jsonEqual compares two JSON numbers (or pairs) by value, not by spelling.
func jsonEqual(a, b json.RawMessage) bool {
	x, ok1 := parseNum(a)
	y, ok2 := parseNum(b)
	return ok1 && ok2 && equalNum(x, y)
}
