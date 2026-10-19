package content

// The `calc` mini-language (plan 3.4): integers and decimals, + - * / ( ), and the functions
// divmod, min, max, abs, round, gcd, pow, sum. Nothing else: no names, no loops, no assignment.
// Everything is computed with exact fractions (math/big.Rat), so 0.1 + 0.2 is exactly 0.3 and
// the server can prove an answer without ever asking a language model.
//
// Go for Spring people: this is a tiny hand-written recursive descent parser (what ANTLR would generate).
// *big.Rat is like Java's BigDecimal but for fractions; its methods write into the receiver, so
// `new(big.Rat).Add(a, b)` means `result = a + b` without changing a or b.

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Value is the result of a calc: a single number, or a (quotient, remainder) pair from divmod.
type Value struct {
	R    *big.Rat    // set for a number
	Pair [2]*big.Rat // set when IsPair
	// IsPair is true for the result of divmod.
	IsPair bool
}

const (
	maxCalcLen   = 200 // longest accepted expression
	maxCalcDepth = 20  // deepest nesting of parentheses and calls
	maxPowExp    = 12  // largest exponent pow accepts (keeps numbers small)
)

// ErrCalc wraps every calc failure so callers can test errors.Is(err, ErrCalc).
var ErrCalc = errors.New("calc")

// Eval computes an expression exactly.
func Eval(expr string) (Value, error) {
	if len(expr) > maxCalcLen {
		return Value{}, fmt.Errorf("%w: expression longer than %d characters", ErrCalc, maxCalcLen)
	}
	p := &calcParser{src: expr}
	v, err := p.expr(0)
	if err != nil {
		return Value{}, err
	}
	p.skip()
	if p.pos != len(p.src) {
		return Value{}, fmt.Errorf("%w: unexpected %q at position %d", ErrCalc, p.src[p.pos:], p.pos)
	}
	return v, nil
}

// calcParser walks the expression text from left to right.
type calcParser struct {
	src string
	pos int
}

// skip moves past spaces.
func (p *calcParser) skip() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

// peek returns the next non-space byte, or 0 at the end.
func (p *calcParser) peek() byte {
	p.skip()
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

// scalar demands a single number (a pair cannot be used inside arithmetic).
func scalar(v Value) (*big.Rat, error) {
	if v.IsPair {
		return nil, fmt.Errorf("%w: divmod gives two numbers and cannot be used inside arithmetic", ErrCalc)
	}
	return v.R, nil
}

// expr := term (('+'|'-') term)*
func (p *calcParser) expr(depth int) (Value, error) {
	if depth > maxCalcDepth {
		return Value{}, fmt.Errorf("%w: nesting too deep", ErrCalc)
	}
	left, err := p.term(depth)
	if err != nil {
		return Value{}, err
	}
	for c := p.peek(); c == '+' || c == '-'; c = p.peek() {
		p.pos++
		right, err := p.term(depth)
		if err != nil {
			return Value{}, err
		}
		a, err := scalar(left)
		if err != nil {
			return Value{}, err
		}
		b, err := scalar(right)
		if err != nil {
			return Value{}, err
		}
		if c == '+' {
			left = Value{R: new(big.Rat).Add(a, b)}
		} else {
			left = Value{R: new(big.Rat).Sub(a, b)}
		}
	}
	return left, nil
}

// term := unary (('*'|'/') unary)*
func (p *calcParser) term(depth int) (Value, error) {
	left, err := p.unary(depth)
	if err != nil {
		return Value{}, err
	}
	for c := p.peek(); c == '*' || c == '/'; c = p.peek() {
		p.pos++
		right, err := p.unary(depth)
		if err != nil {
			return Value{}, err
		}
		a, err := scalar(left)
		if err != nil {
			return Value{}, err
		}
		b, err := scalar(right)
		if err != nil {
			return Value{}, err
		}
		if c == '*' {
			left = Value{R: new(big.Rat).Mul(a, b)}
			continue
		}
		if b.Sign() == 0 {
			return Value{}, fmt.Errorf("%w: division by zero", ErrCalc)
		}
		left = Value{R: new(big.Rat).Quo(a, b)}
	}
	return left, nil
}

// unary := '-' unary | primary
func (p *calcParser) unary(depth int) (Value, error) {
	if p.peek() == '-' {
		p.pos++
		v, err := p.unary(depth)
		if err != nil {
			return Value{}, err
		}
		r, err := scalar(v)
		if err != nil {
			return Value{}, err
		}
		return Value{R: new(big.Rat).Neg(r)}, nil
	}
	return p.primary(depth)
}

// primary := number | '(' expr ')' | name '(' args ')'
func (p *calcParser) primary(depth int) (Value, error) {
	c := p.peek()
	switch {
	case c == '(':
		p.pos++
		v, err := p.expr(depth + 1)
		if err != nil {
			return Value{}, err
		}
		if p.peek() != ')' {
			return Value{}, fmt.Errorf("%w: missing ) at position %d", ErrCalc, p.pos)
		}
		p.pos++
		return v, nil
	case c >= '0' && c <= '9' || c == '.':
		start := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
			p.pos++
		}
		text := p.src[start:p.pos]
		if strings.Count(text, ".") > 1 || text == "." || strings.HasSuffix(text, ".") {
			return Value{}, fmt.Errorf("%w: bad number %q", ErrCalc, text)
		}
		r, ok := new(big.Rat).SetString(text)
		if !ok {
			return Value{}, fmt.Errorf("%w: bad number %q", ErrCalc, text)
		}
		return Value{R: r}, nil
	case c >= 'a' && c <= 'z':
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] >= 'a' && p.src[p.pos] <= 'z' {
			p.pos++
		}
		name := p.src[start:p.pos]
		if p.peek() != '(' {
			return Value{}, fmt.Errorf("%w: unknown name %q (only functions are allowed)", ErrCalc, name)
		}
		p.pos++
		var args []*big.Rat
		if p.peek() != ')' {
			for {
				a, err := p.expr(depth + 1)
				if err != nil {
					return Value{}, err
				}
				r, err := scalar(a)
				if err != nil {
					return Value{}, err
				}
				args = append(args, r)
				if p.peek() == ',' {
					p.pos++
					continue
				}
				break
			}
		}
		if p.peek() != ')' {
			return Value{}, fmt.Errorf("%w: missing ) after %s(", ErrCalc, name)
		}
		p.pos++
		return callFunc(name, args)
	case c == 0:
		return Value{}, fmt.Errorf("%w: expression ends too early", ErrCalc)
	}
	return Value{}, fmt.Errorf("%w: unexpected %q at position %d", ErrCalc, string(c), p.pos)
}

// isInt reports whether r is a whole number.
func isInt(r *big.Rat) bool { return r.IsInt() }

// callFunc runs one of the allowed functions.
func callFunc(name string, a []*big.Rat) (Value, error) {
	need := func(n int) error {
		if len(a) != n {
			return fmt.Errorf("%w: %s needs %d arguments, got %d", ErrCalc, name, n, len(a))
		}
		return nil
	}
	switch name {
	case "divmod":
		if err := need(2); err != nil {
			return Value{}, err
		}
		if !isInt(a[0]) || !isInt(a[1]) || a[0].Sign() < 0 || a[1].Sign() <= 0 {
			return Value{}, fmt.Errorf("%w: divmod needs a whole dividend >= 0 and a whole divisor > 0", ErrCalc)
		}
		q, r := new(big.Int).QuoRem(a[0].Num(), a[1].Num(), new(big.Int))
		return Value{IsPair: true, Pair: [2]*big.Rat{new(big.Rat).SetInt(q), new(big.Rat).SetInt(r)}}, nil
	case "abs":
		if err := need(1); err != nil {
			return Value{}, err
		}
		return Value{R: new(big.Rat).Abs(a[0])}, nil
	case "round":
		if err := need(1); err != nil {
			return Value{}, err
		}
		return Value{R: roundHalfUp(a[0])}, nil
	case "gcd":
		if err := need(2); err != nil {
			return Value{}, err
		}
		if !isInt(a[0]) || !isInt(a[1]) {
			return Value{}, fmt.Errorf("%w: gcd needs whole numbers", ErrCalc)
		}
		g := new(big.Int).GCD(nil, nil, new(big.Int).Abs(a[0].Num()), new(big.Int).Abs(a[1].Num()))
		return Value{R: new(big.Rat).SetInt(g)}, nil
	case "pow":
		if err := need(2); err != nil {
			return Value{}, err
		}
		if !isInt(a[1]) || a[1].Sign() < 0 || a[1].Num().Cmp(big.NewInt(maxPowExp)) > 0 {
			return Value{}, fmt.Errorf("%w: pow exponent must be a whole number from 0 to %d", ErrCalc, maxPowExp)
		}
		n := int(a[1].Num().Int64())
		out := big.NewRat(1, 1)
		for i := 0; i < n; i++ {
			out.Mul(out, a[0])
		}
		return Value{R: out}, nil
	case "min", "max", "sum":
		if len(a) == 0 {
			return Value{}, fmt.Errorf("%w: %s needs at least one argument", ErrCalc, name)
		}
		out := new(big.Rat).Set(a[0])
		for _, x := range a[1:] {
			switch {
			case name == "sum":
				out.Add(out, x)
			case name == "min" && x.Cmp(out) < 0, name == "max" && x.Cmp(out) > 0:
				out.Set(x)
			}
		}
		return Value{R: out}, nil
	}
	return Value{}, fmt.Errorf("%w: unknown function %q", ErrCalc, name)
}

// roundHalfUp rounds to the nearest whole number, halves away from zero (2.5 -> 3, -2.5 -> -3).
func roundHalfUp(r *big.Rat) *big.Rat {
	half := big.NewRat(1, 2)
	x := new(big.Rat).Abs(r)
	x.Add(x, half)
	fl := new(big.Int).Quo(x.Num(), x.Denom()) // floor for a positive number
	out := new(big.Rat).SetInt(fl)
	if r.Sign() < 0 {
		out.Neg(out)
	}
	return out
}

// RatString formats an exact number for messages: "15", "0.3", or "1/3" when it does not terminate.
func RatString(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	if s := r.FloatString(6); ratEqualsDecimal(r, s) {
		s = strings.TrimRight(s, "0")
		return strings.TrimSuffix(s, ".")
	}
	return r.String()
}

// ratEqualsDecimal reports whether the decimal text s is exactly r.
func ratEqualsDecimal(r *big.Rat, s string) bool {
	back, ok := new(big.Rat).SetString(s)
	return ok && back.Cmp(r) == 0
}
