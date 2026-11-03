package approval

import (
	"fmt"
	"os"
	"strings"
)

// Shell parsing (design Q7): the reviewer NEVER sees the raw command
// string - parse-divergence is its own attack class. It sees the
// shell-parsed, statically-expanded form: quotes resolved, ~ and
// known vars expanded, command substitutions $(...) and backticks
// left VISIBLE and surfaced as their own risk signal. Expansion is
// static: nothing here ever runs the command to learn its form.

// Segment is one shell command after the control-operator split
// (| ; && || & ( ) newlines). Words are the parsed, expanded tokens.
// Op records how the segment chains to the next ("" = last).
type Segment struct {
	Words        []string
	Op           string
	Substitution bool // a $(...) or `...` rides in this segment's words
}

// Parsed is the reviewer-visible command: the canonical rebuilt form
// plus its segments. ParseError non-nil means the text never parsed
// clean (an unclosed quote stays literal) - the caller treats that as
// would-interrupt, never as safe.
type Parsed struct {
	Canonical string
	Segments  []Segment
	ParseErr  error
}

// envLookup resolves one variable name for expansion. The shell tool
// runs commands under the process environment, so "known vars" are
// exactly the ones the environment answers; unknown names stay
// visible (they may be the command's business - do not invent a
// value for them and never pass through the caller's shell semantics).
func envLookup(name string) (string, bool) { return os.LookupEnv(name) }

// Parse splits and expands one shell command. Control operators,
// quotes and substitutions follow POSIX sh rules - the same rules the
// RunShell tool's `sh -c` applies - so what the reviewer reads is what
// the shell would execute.
func Parse(command string) Parsed {
	tz := &tokenizer{out: make([]token, 0, 16)}
	if err := tz.run(command); err != nil {
		e := fmt.Errorf("shell parse: %w", err)
		// Fail visible: hand back the raw text as one segment so a
		// caller that wants to show something can, and let the
		// error drive the would-interrupt decision.
		return Parsed{
			Canonical: command,
			Segments:  []Segment{{Words: []string{command}}},
			ParseErr:  e,
		}
	}
	p := assemble(tz.out)
	p.Canonical = stringJoin(p.Segments)
	return p
}

// assemble folds the tokenizer's token stream into segments.
func assemble(toks []token) Parsed {
	p := Parsed{Segments: make([]Segment, 0, 4)}
	cur := Segment{}
	for _, t := range toks {
		if t.op {
			if len(cur.Words) > 0 || cur.Op != "" {
				p.Segments = append(p.Segments, cur)
			}
			// an operator with no preceding words: preserve its
			// shape only if it chains an earlier segment
			if len(p.Segments) > 0 {
				// trailing op becomes the PREVIOUS segment's chain
				p.Segments[len(p.Segments)-1].Op = t.text
			}
			cur = Segment{}
			continue
		}
		cur.Words = append(cur.Words, t.text)
		if t.subst {
			cur.Substitution = true
		}
	}
	if len(cur.Words) > 0 {
		p.Segments = append(p.Segments, cur)
	}
	return p
}

// stringJoin rebuilds the canonical reviewed form: words spaced,
// segments joined by their chain operators. This is what leaves the
// machine (plus redaction upstream of it in the reviewer client).
func stringJoin(segs []Segment) string {
	var sb strings.Builder
	for i, s := range segs {
		if i > 0 {
			op := segs[i-1].Op
			if op == "" {
				op = ";"
			}
			sb.WriteString(" " + op + " ")
		}
		for j, w := range s.Words {
			if j > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(w)
		}
	}
	return sb.String()
}

type token struct {
	text  string
	op    bool // a control operator (| ; && || & ( ))
	subst bool // carries an unexpanded $(...) or `...`
}

type tokenizer struct {
	out []token
}

func (tz *tokenizer) run(input string) error {
	cur := &token{}
	var words []token
	flush := func() {
		if cur.text != "" {
			words = append(words, *cur)
			*cur = token{}
		}
	}
	consumeWord := func() {
		flush()
	}
	const (
		modeNormal = iota
		modeSingle // inside '...'
		modeDouble // inside "..."
	)
	mode := modeNormal
	rs := []rune(input)
	i := 0
	emitOp := func(op string) error {
		consumeWord()
		tz.out = append(tz.out, words...)
		words = nil
		tz.out = append(tz.out, token{op: true, text: op})
		return nil
	}
	for i < len(rs) {
		r := rs[i]
		switch {
		case r == '\\' && mode != modeSingle && i+1 < len(rs):
			// a backslash escapes the next char (outside single
			// quotes); the pair becomes part of the word literal
			cur.text += string(rs[i+1])
			i += 2
			continue
		case r == '\'':
			if mode == modeDouble {
				cur.text += "'"
			} else if mode == modeSingle {
				mode = modeNormal
			} else {
				mode = modeSingle
			}
			i++
		case r == '"':
			if mode == modeSingle {
				cur.text += "\""
			} else if mode == modeDouble {
				mode = modeNormal
			} else {
				mode = modeDouble
			}
			i++
		case mode == modeNormal && (r == '|' || r == ';' || r == '&'):
			// doubled forms first: && || ; | &
			if i+1 < len(rs) && rs[i+1] == r && (r == '|' || r == '&') {
				if err := emitOp(string(r) + string(rs[i+1])); err != nil {
					return err
				}
				i += 2
				continue
			}
			if err := emitOp(string(r)); err != nil {
				return err
			}
			i++
		case mode == modeNormal && (r == '(' || r == ')'):
			if err := emitOp(string(r)); err != nil {
				return err
			}
			i++
		case r == '\n' && mode == modeNormal:
			consumeWord()
			// newline splits like ';' between words
			if len(words) > 0 || cur.text != "" {
				tz.out = append(tz.out, words...)
				words = nil
				tz.out = append(tz.out, token{op: true, text: ";"})
			}
			i++
		case r == ' ' || r == '\t' || (r == '\r'):
			if mode == modeNormal {
				consumeWord()
			} else {
				cur.text += string(r)
			}
			i++
		case r == '~' && mode == modeNormal && cur.text == "":
			// tilde expansion at a word start: `~` or `~/...` expands
			// to the home directory; a user-tilde (~name) and ~- stay
			// literal (POSIX leaves them to the shell's own rule).
			if i+1 < len(rs) && isUserWord(rs[i+1]) {
				cur.text += "~"
				i++
				continue
			}
			home, ok := os.UserHomeDir()
			if ok != nil || home == "" {
				cur.text += "~"
			} else {
				cur.text += home
			}
			i++
		case r == '$' && mode != modeSingle:
			next := ""
			if i+1 < len(rs) {
				next = string(rs[i+1])
			}
			switch {
			case next == "(" || next == "{":
				// keep visible; the substitution is its own signal
				sub, ni, err := scanSub(rs, i, mode)
				if err != nil {
					// a $( that never closes stays literal text to
					// word end - visible, and the detector treats
					// non-termination as substitution-shaped
					cur.text += string(rs[i:])
					cur.subst = true
					i = len(rs)
					continue
				}
				cur.text += sub
				cur.subst = true
				i = ni
			case i+1 < len(rs) && isNameStart(rs[i+1]):
				// $VAR: expand when the environment knows it
				j := i + 1
				for j < len(rs) && isNameRune(rs[j]) {
					j++
				}
				name := string(rs[i+1 : j])
				if v, ok := envLookup(name); ok {
					cur.text += v
				} else {
					cur.text += "$" + name
				}
				i = j
			default:
				// positional/other ($?, $#, $@...): keep visible
				cur.text += "$"
				i++
				if i < len(rs) && rs[i] != '"' && rs[i] != ' ' {
					cur.text += string(rs[i])
					i++
				}
			}
		case r == '`' && mode != modeSingle:
			// backtick substitution: keep visible to its closing pair
			end := -1
			for j := i + 1; j < len(rs); j++ {
				if rs[j] == '`' {
					end = j
					break
				}
			}
			if end < 0 {
				cur.text += string(rs[i:])
				cur.subst = true
				i = len(rs)
				continue
			}
			cur.text += string(rs[i : end+1])
			cur.subst = true
			i = end + 1
		default:
			cur.text += string(r)
			i++
		}
	}
	if mode != modeNormal {
		return fmt.Errorf("unterminated %s quote", map[int]string{modeSingle: "single", modeDouble: "double"}[mode])
	}
	consumeWord()
	tz.out = append(tz.out, words...)
	return nil
}

func isUserWord(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
}

func isNameStart(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_'
}

func isNameRune(r rune) bool {
	return isNameStart(r) || r >= '0' && r <= '9'
}

// scanSub copies an unexpanded $() ${} span (or its backticked
// double) into out and returns the index past it. Balanced-paren
// counting for $(); a ${} ends at its brace.
func scanSub(rs []rune, i int, mode int) (string, int, error) {
	if rs[i+1] == '{' {
		for j := i + 2; j < len(rs); j++ {
			if rs[j] == '}' {
				return string(rs[i : j+1]), j + 1, nil
			}
		}
		return "", 0, fmt.Errorf("unterminated ${")
	}
	// $( depth counting
	depth := 0
	for j := i + 1; j < len(rs); j++ {
		switch rs[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return string(rs[i : j+1]), j + 1, nil
			}
		}
	}
	return "", 0, fmt.Errorf("unterminated $( (")
}
