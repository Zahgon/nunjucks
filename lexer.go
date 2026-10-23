// Ported from nunjucks/src/lexer.js of mozilla/nunjucks (BSD-2-Clause, see LICENSE).

package nunjucks

import (
	"regexp"

	"github.com/mozilla/nunjucks/js"
)

const (
	whitespaceChars = " \n\t\r "
	delimChars      = "()[]{}%*-+~/#,:|.<>=!"
	intChars        = "0123456789"
)

// Token types.
const (
	tokenString        = "string"
	tokenWhitespace    = "whitespace"
	tokenData          = "data"
	tokenBlockStart    = "block-start"
	tokenBlockEnd      = "block-end"
	tokenVariableStart = "variable-start"
	tokenVariableEnd   = "variable-end"
	tokenComment       = "comment"
	tokenLeftParen     = "left-paren"
	tokenRightParen    = "right-paren"
	tokenLeftBracket   = "left-bracket"
	tokenRightBracket  = "right-bracket"
	tokenLeftCurly     = "left-curly"
	tokenRightCurly    = "right-curly"
	tokenOperator      = "operator"
	tokenComma         = "comma"
	tokenColon         = "colon"
	tokenTilde         = "tilde"
	tokenPipe          = "pipe"
	tokenInt           = "int"
	tokenFloat         = "float"
	tokenBoolean       = "boolean"
	tokenNone          = "none"
	tokenSymbol        = "symbol"
	tokenRegex         = "regex"
)

// Tags are the template delimiters; empty fields take the defaults.
type Tags struct {
	BlockStart    string
	BlockEnd      string
	VariableStart string
	VariableEnd   string
	CommentStart  string
	CommentEnd    string
}

type token struct {
	typ    string
	value  string
	flags  string // regular expression flags of a regex token
	lineno int
	colno  int
}

type tokenizer struct {
	str           []uint16
	index         int
	lineno        int
	colno         int
	inCode        bool
	tags          Tags
	trimBlocks    bool
	lstripBlocks  bool
	blockStart    []uint16
	blockEnd      []uint16
	variableStart []uint16
	variableEnd   []uint16
	commentStart  []uint16
	commentEnd    []uint16
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func newTokenizer(src string, tags Tags, trimBlocks, lstripBlocks bool) *tokenizer {
	t := &tokenizer{str: js.U16(src), trimBlocks: trimBlocks, lstripBlocks: lstripBlocks}
	t.tags = Tags{
		BlockStart:    or(tags.BlockStart, "{%"),
		BlockEnd:      or(tags.BlockEnd, "%}"),
		VariableStart: or(tags.VariableStart, "{{"),
		VariableEnd:   or(tags.VariableEnd, "}}"),
		CommentStart:  or(tags.CommentStart, "{#"),
		CommentEnd:    or(tags.CommentEnd, "#}"),
	}
	t.blockStart = js.U16(t.tags.BlockStart)
	t.blockEnd = js.U16(t.tags.BlockEnd)
	t.variableStart = js.U16(t.tags.VariableStart)
	t.variableEnd = js.U16(t.tags.VariableEnd)
	t.commentStart = js.U16(t.tags.CommentStart)
	t.commentEnd = js.U16(t.tags.CommentEnd)
	return t
}

var (
	intTokenRe = regexp.MustCompile(`^[-+]?[0-9]+$`)
	whitespace = js.U16(whitespaceChars)
	delims     = js.U16(delimChars)
	intDigits  = js.U16(intChars)
	complexOps = []string{"==", "===", "!=", "!==", "<=", ">=", "//", "**"}
)

func contains(set []uint16, c uint16) bool {
	for _, x := range set {
		if x == c {
			return true
		}
	}
	return false
}

func isComplexOp(s string) bool {
	for _, op := range complexOps {
		if op == s {
			return true
		}
	}
	return false
}

func (t *tokenizer) nextToken() (*token, error) {
	lineno, colno := t.lineno, t.colno
	tok := func(typ string, value []uint16) *token {
		return &token{typ: typ, value: js.FromU16(value), lineno: lineno, colno: colno}
	}
	if t.inCode {
		if t.isFinished() {
			return nil, nil
		}
		cur := t.current()
		if cur == '"' || cur == '\'' {
			return tok(tokenString, t.parseString(cur)), nil
		}
		if ws := t.extract(whitespace); len(ws) > 0 {
			return tok(tokenWhitespace, ws), nil
		}
		if s, ok := t.extractString(t.blockEnd); ok {
			return t.blockEndToken(s, lineno, colno), nil
		}
		if s, ok := t.extractString(append([]uint16{'-'}, t.blockEnd...)); ok {
			return t.blockEndToken(s, lineno, colno), nil
		}
		if s, ok := t.extractString(t.variableEnd); ok {
			t.inCode = false
			return tok(tokenVariableEnd, s), nil
		}
		if s, ok := t.extractString(append([]uint16{'-'}, t.variableEnd...)); ok {
			t.inCode = false
			return tok(tokenVariableEnd, s), nil
		}
		if cur == 'r' && t.charAt(t.index+1) == '/' {
			t.forwardN(2)
			var body []uint16
			for !t.isFinished() {
				if t.current() == '/' && t.previous() != '\\' {
					t.forward()
					break
				}
				body = append(body, t.current())
				t.forward()
			}
			var flags []uint16
			for !t.isFinished() {
				c := t.current()
				if c == 'g' || c == 'i' || c == 'm' || c == 'y' {
					flags = append(flags, c)
					t.forward()
				} else {
					break
				}
			}
			r := tok(tokenRegex, body)
			r.flags = js.FromU16(flags)
			return r, nil
		}
		if contains(delims, cur) {
			t.forward()
			op := []uint16{cur}
			typ := tokenOperator
			if !t.isFinished() && isComplexOp(js.FromU16([]uint16{cur, t.current()})) {
				op = append(op, t.current())
				t.forward()
				if !t.isFinished() && isComplexOp(js.FromU16(append(append([]uint16{}, op...), t.current()))) {
					op = append(op, t.current())
					t.forward()
				}
			}
			switch js.FromU16(op) {
			case "(":
				typ = tokenLeftParen
			case ")":
				typ = tokenRightParen
			case "[":
				typ = tokenLeftBracket
			case "]":
				typ = tokenRightBracket
			case "{":
				typ = tokenLeftCurly
			case "}":
				typ = tokenRightCurly
			case ",":
				typ = tokenComma
			case ":":
				typ = tokenColon
			case "~":
				typ = tokenTilde
			case "|":
				typ = tokenPipe
			}
			return tok(typ, op), nil
		}
		word := t.extractUntil(append(append([]uint16{}, whitespace...), delims...))
		s := js.FromU16(word)
		switch {
		case intTokenRe.MatchString(s):
			if t.current() == '.' && !t.isFinished() {
				t.forward()
				dec := "null"
				if !t.isFinished() {
					dec = js.FromU16(t.extract(intDigits))
				}
				return &token{typ: tokenFloat, value: s + "." + dec, lineno: lineno, colno: colno}, nil
			}
			return tok(tokenInt, word), nil
		case s == "true" || s == "false":
			return tok(tokenBoolean, word), nil
		case s == "none" || s == "null":
			return tok(tokenNone, word), nil
		case s != "":
			return tok(tokenSymbol, word), nil
		}
		return nil, js.NewError("Unexpected value while parsing: " + s)
	}

	beginChars := []uint16{t.blockStart[0], t.variableStart[0], t.commentStart[0], t.commentEnd[0]}
	if t.isFinished() {
		return nil, nil
	}
	if s, ok := t.extractString(append(append([]uint16{}, t.blockStart...), '-')); ok {
		t.inCode = true
		return tok(tokenBlockStart, s), nil
	}
	if s, ok := t.extractString(t.blockStart); ok {
		t.inCode = true
		return tok(tokenBlockStart, s), nil
	}
	if s, ok := t.extractString(append(append([]uint16{}, t.variableStart...), '-')); ok {
		t.inCode = true
		return tok(tokenVariableStart, s), nil
	}
	if s, ok := t.extractString(t.variableStart); ok {
		t.inCode = true
		return tok(tokenVariableStart, s), nil
	}
	var text []uint16
	inComment := false
	if t.matches(t.commentStart) {
		inComment = true
		s, _ := t.extractString(t.commentStart)
		text = append(text, s...)
	}
	finished := false
	for {
		data, ok := t.extractUntilOK(beginChars)
		if !ok {
			finished = true
			break
		}
		text = append(text, data...)
		if (t.matches(t.blockStart) || t.matches(t.variableStart) || t.matches(t.commentStart)) && !inComment {
			if t.lstripBlocks && t.matches(t.blockStart) && t.colno > 0 && t.colno <= len(text) {
				lastLine := text[len(text)-t.colno:]
				if allWhitespace(lastLine) {
					text = text[:len(text)-t.colno]
					if len(text) == 0 {
						return t.nextToken()
					}
				}
			}
			break
		} else if t.matches(t.commentEnd) {
			if !inComment {
				return nil, js.NewError("unexpected end of comment")
			}
			s, _ := t.extractString(t.commentEnd)
			text = append(text, s...)
			break
		} else {
			if !t.isFinished() {
				text = append(text, t.current())
			}
			t.forward()
		}
	}
	if finished && inComment {
		return nil, js.NewError("expected end of comment, got end of file")
	}
	if inComment {
		return tok(tokenComment, text), nil
	}
	return tok(tokenData, text), nil
}

// allWhitespace is the /^\s+$/ test of a non-empty run of code units.
func allWhitespace(u []uint16) bool {
	if len(u) == 0 {
		return false
	}
	for _, c := range u {
		if !js.IsJSWhitespace(c) {
			return false
		}
	}
	return true
}

func (t *tokenizer) blockEndToken(s []uint16, lineno, colno int) *token {
	t.inCode = false
	if t.trimBlocks {
		switch t.current() {
		case '\n':
			t.forward()
		case '\r':
			t.forward()
			if t.current() == '\n' && !t.isFinished() {
				t.forward()
			} else {
				t.index--
				t.colno--
			}
		}
	}
	return &token{typ: tokenBlockEnd, value: js.FromU16(s), lineno: lineno, colno: colno}
}

func (t *tokenizer) parseString(delimiter uint16) []uint16 {
	t.forward()
	var str []uint16
	for !t.isFinished() && t.current() != delimiter {
		cur := t.current()
		if cur == '\\' {
			t.forward()
			switch t.current() {
			case 'n':
				str = append(str, '\n')
			case 't':
				str = append(str, '\t')
			case 'r':
				str = append(str, '\r')
			default:
				if !t.isFinished() {
					str = append(str, t.current())
				}
			}
			t.forward()
		} else {
			str = append(str, cur)
			t.forward()
		}
	}
	t.forward()
	return str
}

func (t *tokenizer) matches(s []uint16) bool {
	if t.index+len(s) > len(t.str) {
		return false
	}
	for i, c := range s {
		if t.str[t.index+i] != c {
			return false
		}
	}
	return true
}

func (t *tokenizer) extractString(s []uint16) ([]uint16, bool) {
	if t.matches(s) {
		t.forwardN(len(s))
		return s, true
	}
	return nil, false
}

// extractUntil extracts code units up to one in set; it returns nil at the end of input.
func (t *tokenizer) extractUntil(set []uint16) []uint16 {
	u, _ := t.extractMatching(true, set)
	return u
}

func (t *tokenizer) extractUntilOK(set []uint16) ([]uint16, bool) {
	return t.extractMatching(true, set)
}

func (t *tokenizer) extract(set []uint16) []uint16 {
	u, _ := t.extractMatching(false, set)
	return u
}

// extractMatching is _extractMatching; ok is false where the JavaScript returns null (input finished).
func (t *tokenizer) extractMatching(breakOnMatch bool, set []uint16) ([]uint16, bool) {
	if t.isFinished() {
		return nil, false
	}
	first := contains(set, t.current())
	if (breakOnMatch && !first) || (!breakOnMatch && first) {
		out := []uint16{t.current()}
		t.forward()
		for !t.isFinished() {
			in := contains(set, t.current())
			if (breakOnMatch && in) || (!breakOnMatch && !in) {
				break
			}
			out = append(out, t.current())
			t.forward()
		}
		return out, true
	}
	return []uint16{}, true
}

// extractRegex matches re (anchored) against the rest of the input and moves past the match.
func (t *tokenizer) extractRegex(re *js.RegExp) []string {
	rest := t.str[t.index:]
	caps := re.Search(rest, 0, false)
	if caps == nil {
		return nil
	}
	out := make([]string, len(caps)/2)
	for i := range out {
		if caps[2*i] >= 0 {
			out[i] = js.FromU16(rest[caps[2*i]:caps[2*i+1]])
		}
	}
	t.forwardN(caps[1])
	return out
}

func (t *tokenizer) isFinished() bool { return t.index >= len(t.str) }

func (t *tokenizer) forwardN(n int) {
	for i := 0; i < n; i++ {
		t.forward()
	}
}

func (t *tokenizer) forward() {
	t.index++
	if t.previous() == '\n' {
		t.lineno++
		t.colno = 0
	} else {
		t.colno++
	}
}

// backN moves the cursor back n code units. Stepping back onto a newline fails as in the original, whose
// back() reads this.src, a property the tokenizer does not have.
func (t *tokenizer) backN(n int) error {
	for i := 0; i < n; i++ {
		if err := t.back(); err != nil {
			return err
		}
	}
	return nil
}

func (t *tokenizer) back() error {
	t.index--
	if t.current() == '\n' && !t.isFinished() {
		t.lineno--
		return js.ReadPropertyError(js.Undefined, "lastIndexOf")
	}
	t.colno--
	return nil
}

// current is the code unit at the cursor, or 0 at the end (where the JavaScript returns "").
func (t *tokenizer) current() uint16 {
	if !t.isFinished() {
		return t.str[t.index]
	}
	return 0
}

func (t *tokenizer) charAt(i int) uint16 {
	if i >= 0 && i < len(t.str) {
		return t.str[i]
	}
	return 0
}

func (t *tokenizer) previous() uint16 { return t.charAt(t.index - 1) }
