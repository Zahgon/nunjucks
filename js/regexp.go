package js

import (
	"strconv"
	"strings"
)

// RegExp is a JavaScript regular expression object. Patterns are compiled for a backtracking matcher over
// UTF-16 code units with ECMAScript (non-unicode, Annex B) semantics, so that matches, captures and failures
// equal those of the original JavaScript implementation.
type RegExp struct {
	Source    string
	Flags     string
	LastIndex Value
	global    bool
	ignore    bool
	multiline bool
	sticky    bool
	dotAll    bool
	prog      *reNode
	groups    int
	names     []string
}

// NewRegExp is new RegExp(source, flags).
func NewRegExp(source, flags string) (*RegExp, error) {
	re := &RegExp{Source: source, Flags: flags, LastIndex: 0.0}
	for _, f := range flags {
		var p *bool
		switch f {
		case 'g':
			p = &re.global
		case 'i':
			p = &re.ignore
		case 'm':
			p = &re.multiline
		case 'y':
			p = &re.sticky
		case 's':
			p = &re.dotAll
		}
		if p == nil || *p {
			return nil, NewSyntaxError("Invalid flags supplied to RegExp constructor '%s'", flags)
		}
		*p = true
	}
	re.Flags = canonicalFlags(re)
	ps := &reParser{src: U16(source)}
	ps.countGroups()
	node, err := ps.parseDisjunction()
	if err == nil && ps.pos < len(ps.src) {
		err = errUnmatchedParen
	}
	if err != nil {
		return nil, NewSyntaxError("Invalid regular expression: /%s/%s: %s", escapeSource(source), flags, err.Error())
	}
	if err == nil {
		err = checkNamedRefs(node, ps.names)
	}
	if err != nil {
		return nil, NewSyntaxError("Invalid regular expression: /%s/%s: %s", escapeSource(source), flags, err.Error())
	}
	re.prog = node
	re.groups = ps.ngroups
	re.names = ps.names
	return re, nil
}

func canonicalFlags(re *RegExp) string {
	var b strings.Builder
	for _, f := range []struct {
		on bool
		c  byte
	}{{false, 'd'}, {re.global, 'g'}, {re.ignore, 'i'}, {re.multiline, 'm'}, {re.dotAll, 's'}, {false, 'u'}, {false, 'v'}, {re.sticky, 'y'}} {
		if f.on {
			b.WriteByte(f.c)
		}
	}
	return b.String()
}

// escapeSource is EscapeRegExpPattern: the source as it appears between slashes.
func escapeSource(src string) string {
	if src == "" {
		return "(?:)"
	}
	u := U16(src)
	var out []uint16
	inClass := false
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c == '\\' && i+1 < len(u):
			out = append(out, c, u[i+1])
			i++
			continue
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			out = append(out, '\\', '/')
			continue
		case c == '\n':
			out = append(out, U16(`\n`)...)
			continue
		case c == '\r':
			out = append(out, U16(`\r`)...)
			continue
		case c == 0x2028:
			out = append(out, U16(` `)...)
			continue
		case c == 0x2029:
			out = append(out, U16(` `)...)
			continue
		}
		out = append(out, c)
	}
	return FromU16(out)
}

// String is RegExp.prototype.toString.
func (re *RegExp) String() string { return "/" + escapeSource(re.Source) + "/" + re.Flags }

// Global reports the g flag.
func (re *RegExp) Global() bool { return re.global }

// Sticky reports the y flag.
func (re *RegExp) Sticky() bool { return re.sticky }

// Groups returns the number of capture groups.
func (re *RegExp) Groups() int { return re.groups }

// MatchAt runs the pattern at exactly position pos of u and returns the capture positions (pairs of code unit
// offsets, -1 for an unset group), or nil.
func (re *RegExp) MatchAt(u []uint16, pos int) []int {
	m := &matcher{re: re, in: u, caps: make([]int, 2*(re.groups+1))}
	for i := range m.caps {
		m.caps[i] = -1
	}
	var result []int
	ok := m.match(re.prog, pos, func(end int) bool {
		m.caps[0], m.caps[1] = pos, end
		result = append([]int(nil), m.caps...)
		return true
	})
	if !ok {
		return nil
	}
	return result
}

// Search finds the first match at or after from (only at from when sticky is set).
func (re *RegExp) Search(u []uint16, from int, sticky bool) []int {
	for i := from; i <= len(u); i++ {
		if caps := re.MatchAt(u, i); caps != nil {
			return caps
		}
		if sticky {
			break
		}
	}
	return nil
}

// ---- syntax tree ----

type reKind int

const (
	reEmpty reKind = iota
	reChar
	reAny
	reClass
	reSeq
	reAlt
	reGroup
	reLook
	reBackref
	reStart
	reEnd
	reWordB
	reNotWordB
	reRepeat
)

type reRange struct{ lo, hi uint16 }

type reNode struct {
	kind     reKind
	c        uint16
	ranges   []reRange
	negate   bool
	subs     []*reNode
	sub      *reNode
	index    int
	name     string
	ahead    bool
	negLook  bool
	min, max int
	greedy   bool
	firstCap int
	lastCap  int
}

// ---- parser ----

type reError string

func (e reError) Error() string { return string(e) }

const (
	errUnmatchedParen  reError = "Unmatched ')'"
	errUnterminated    reError = "Unterminated group"
	errNothingToRepeat reError = "Nothing to repeat"
	errClass           reError = "Unterminated character class"
	errRange           reError = "Range out of order in character class"
	errQuantOrder      reError = "numbers out of order in {} quantifier"
	errEndBackslash    reError = "\\ at end of pattern"
	errInvalidGroup    reError = "Invalid group"
	errGroupName       reError = "Invalid capture group name"
	errDupName         reError = "Duplicate capture group name"
	errNamedRef        reError = "Invalid named reference"
	errNamedCapture    reError = "Invalid named capture referenced"
)

// checkNamedRefs fails when a \k<name> back reference names no group.
func checkNamedRefs(n *reNode, names []string) error {
	if n == nil {
		return nil
	}
	if n.kind == reBackref && n.index < 0 {
		found := false
		for _, name := range names {
			if name == n.name {
				found = true
			}
		}
		if !found {
			return errNamedCapture
		}
	}
	for _, s := range n.subs {
		if err := checkNamedRefs(s, names); err != nil {
			return err
		}
	}
	return checkNamedRefs(n.sub, names)
}

type reParser struct {
	src     []uint16
	pos     int
	ngroups int
	total   int
	names   []string
	hasName bool
}

func (p *reParser) countGroups() {
	inClass := false
	for i := 0; i < len(p.src); i++ {
		switch c := p.src[i]; {
		case c == '\\':
			i++
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '(' && !inClass:
			if i+1 < len(p.src) && p.src[i+1] == '?' {
				if i+2 < len(p.src) && p.src[i+2] == '<' && i+3 < len(p.src) && p.src[i+3] != '=' && p.src[i+3] != '!' {
					p.total++
					p.hasName = true
				}
				continue
			}
			p.total++
		}
	}
	p.names = make([]string, p.total+1)
}

func (p *reParser) more() bool     { return p.pos < len(p.src) }
func (p *reParser) peek() uint16   { return p.src[p.pos] }
func (p *reParser) at(c byte) bool { return p.more() && p.src[p.pos] == uint16(c) }

func (p *reParser) parseDisjunction() (*reNode, error) {
	first, err := p.parseAlternative()
	if err != nil {
		return nil, err
	}
	if !p.at('|') {
		return first, nil
	}
	alt := &reNode{kind: reAlt, subs: []*reNode{first}}
	for p.at('|') {
		p.pos++
		n, err := p.parseAlternative()
		if err != nil {
			return nil, err
		}
		alt.subs = append(alt.subs, n)
	}
	return alt, nil
}

func (p *reParser) parseAlternative() (*reNode, error) {
	seq := &reNode{kind: reSeq}
	for p.more() && !p.at('|') && !p.at(')') {
		capBefore := p.ngroups
		atom, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		if atom == nil {
			continue
		}
		atom, err = p.parseQuantifier(atom, capBefore)
		if err != nil {
			return nil, err
		}
		seq.subs = append(seq.subs, atom)
	}
	return seq, nil
}

func (p *reParser) parseQuantifier(atom *reNode, capBefore int) (*reNode, error) {
	if !p.more() {
		return atom, nil
	}
	min, max := -1, -1
	start := p.pos
	switch p.peek() {
	case '*':
		min, max = 0, -1
		p.pos++
	case '+':
		min, max = 1, -1
		p.pos++
	case '?':
		min, max = 0, 1
		p.pos++
	case '{':
		lo, hi, ok := p.parseBraces()
		if !ok {
			p.pos = start
			return atom, nil
		}
		min, max = lo, hi
	default:
		return atom, nil
	}
	if atom.kind == reStart || atom.kind == reEnd || atom.kind == reWordB || atom.kind == reNotWordB ||
		(atom.kind == reLook && !atom.ahead) {
		return nil, errNothingToRepeat
	}
	if max != -1 && min > max {
		return nil, errQuantOrder
	}
	greedy := true
	if p.at('?') {
		greedy = false
		p.pos++
	}
	return &reNode{kind: reRepeat, sub: atom, min: min, max: max, greedy: greedy, firstCap: capBefore + 1, lastCap: p.ngroups}, nil
}

// parseBraces reads {n}, {n,} or {n,m}; ok is false when the brace is a literal.
func (p *reParser) parseBraces() (int, int, bool) {
	i := p.pos + 1
	readNum := func() (int, bool) {
		j := i
		for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
			i++
		}
		if i == j {
			return 0, false
		}
		n, err := strconv.Atoi(FromU16(p.src[j:i]))
		if err != nil {
			n = 1 << 30
		}
		return n, true
	}
	lo, ok := readNum()
	if !ok {
		return 0, 0, false
	}
	hi := lo
	if i < len(p.src) && p.src[i] == ',' {
		i++
		if i < len(p.src) && p.src[i] == '}' {
			hi = -1
		} else {
			hi, ok = readNum()
			if !ok {
				return 0, 0, false
			}
		}
	}
	if i >= len(p.src) || p.src[i] != '}' {
		return 0, 0, false
	}
	p.pos = i + 1
	return lo, hi, true
}

func (p *reParser) parseTerm() (*reNode, error) {
	c := p.peek()
	switch c {
	case '^':
		p.pos++
		return &reNode{kind: reStart}, nil
	case '$':
		p.pos++
		return &reNode{kind: reEnd}, nil
	case '.':
		p.pos++
		return &reNode{kind: reAny}, nil
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '*', '+', '?':
		return nil, errNothingToRepeat
	case '{':
		if _, _, ok := p.parseBraces(); ok {
			return nil, errNothingToRepeat
		}
		p.pos++
		return &reNode{kind: reChar, c: c}, nil
	case '\\':
		return p.parseAtomEscape()
	}
	p.pos++
	return &reNode{kind: reChar, c: c}, nil
}

func (p *reParser) parseGroup() (*reNode, error) {
	p.pos++
	node := &reNode{kind: reGroup, index: -1}
	if p.at('?') {
		p.pos++
		if !p.more() {
			return nil, errInvalidGroup
		}
		switch p.peek() {
		case ':':
			p.pos++
		case '=', '!':
			node = &reNode{kind: reLook, ahead: true, negLook: p.peek() == '!'}
			p.pos++
		case '<':
			p.pos++
			if p.at('=') || p.at('!') {
				node = &reNode{kind: reLook, ahead: false, negLook: p.peek() == '!'}
				p.pos++
				break
			}
			name, err := p.parseGroupName()
			if err != nil {
				return nil, err
			}
			for _, n := range p.names {
				if n == name {
					return nil, errDupName
				}
			}
			p.ngroups++
			node.index = p.ngroups
			node.name = name
			p.names[p.ngroups] = name
		default:
			return nil, errInvalidGroup
		}
	} else {
		p.ngroups++
		node.index = p.ngroups
	}
	body, err := p.parseDisjunction()
	if err != nil {
		return nil, err
	}
	if !p.at(')') {
		return nil, errUnterminated
	}
	p.pos++
	node.sub = body
	return node, nil
}

func (p *reParser) parseGroupName() (string, error) {
	start := p.pos
	for p.more() && p.peek() != '>' {
		c := p.peek()
		ok := c == '$' || c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80 ||
			(p.pos > start && c >= '0' && c <= '9')
		if !ok {
			return "", errGroupName
		}
		p.pos++
	}
	if !p.more() || p.pos == start {
		return "", errGroupName
	}
	name := FromU16(p.src[start:p.pos])
	p.pos++
	return name, nil
}

func (p *reParser) parseAtomEscape() (*reNode, error) {
	p.pos++
	if !p.more() {
		return nil, errEndBackslash
	}
	c := p.peek()
	switch {
	case c == 'b':
		p.pos++
		return &reNode{kind: reWordB}, nil
	case c == 'B':
		p.pos++
		return &reNode{kind: reNotWordB}, nil
	case c >= '1' && c <= '9':
		start := p.pos
		for p.more() && p.peek() >= '0' && p.peek() <= '9' {
			p.pos++
		}
		n, _ := strconv.Atoi(FromU16(p.src[start:p.pos]))
		if n <= p.total {
			return &reNode{kind: reBackref, index: n}, nil
		}
		p.pos = start
		if c >= '8' {
			p.pos++
			return &reNode{kind: reChar, c: c}, nil
		}
		return &reNode{kind: reChar, c: p.legacyOctal()}, nil
	case c == 'k' && p.hasName:
		p.pos++
		if !p.at('<') {
			return nil, errNamedRef
		}
		p.pos++
		name, err := p.parseGroupName()
		if err != nil {
			return nil, errNamedRef
		}
		return &reNode{kind: reBackref, index: -1, name: name}, nil
	}
	if r, ok := p.classEscape(); ok {
		return &reNode{kind: reClass, ranges: r.ranges, negate: r.negate}, nil
	}
	ch, err := p.characterEscape(false)
	if err != nil {
		return nil, err
	}
	return &reNode{kind: reChar, c: ch}, nil
}

func (p *reParser) legacyOctal() uint16 {
	v := 0
	for i := 0; i < 3 && p.more() && p.peek() >= '0' && p.peek() <= '7'; i++ {
		nv := v*8 + int(p.peek()-'0')
		if nv > 0377 {
			break
		}
		v = nv
		p.pos++
	}
	return uint16(v)
}

type classSet struct {
	ranges []reRange
	negate bool
}

var (
	digitRanges = []reRange{{'0', '9'}}
	wordRanges  = []reRange{{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}}
	spaceRanges = []reRange{{9, 13}, {32, 32}, {0xA0, 0xA0}, {0x1680, 0x1680}, {0x2000, 0x200A},
		{0x2028, 0x2029}, {0x202F, 0x202F}, {0x205F, 0x205F}, {0x3000, 0x3000}, {0xFEFF, 0xFEFF}}
)

// classEscape reads \d \D \w \W \s \S at p.pos (after the backslash).
func (p *reParser) classEscape() (classSet, bool) {
	var s classSet
	switch p.peek() {
	case 'd':
		s = classSet{ranges: digitRanges}
	case 'D':
		s = classSet{ranges: digitRanges, negate: true}
	case 'w':
		s = classSet{ranges: wordRanges}
	case 'W':
		s = classSet{ranges: wordRanges, negate: true}
	case 's':
		s = classSet{ranges: spaceRanges}
	case 'S':
		s = classSet{ranges: spaceRanges, negate: true}
	default:
		return s, false
	}
	p.pos++
	return s, true
}

func hexVal(c uint16) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func (p *reParser) hexDigits(n int) (uint16, bool) {
	if p.pos+n > len(p.src) {
		return 0, false
	}
	v := 0
	for i := 0; i < n; i++ {
		h := hexVal(p.src[p.pos+i])
		if h < 0 {
			return 0, false
		}
		v = v*16 + h
	}
	p.pos += n
	return uint16(v), true
}

// characterEscape reads the escape after a backslash that is not a class or back reference escape.
func (p *reParser) characterEscape(inClass bool) (uint16, error) {
	c := p.peek()
	p.pos++
	switch c {
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'v':
		return '\v', nil
	case 'f':
		return '\f', nil
	case '0':
		if p.more() && p.peek() >= '0' && p.peek() <= '7' {
			p.pos--
			return p.legacyOctal(), nil
		}
		return 0, nil
	case 'c':
		if p.more() {
			l := p.peek()
			if (l >= 'a' && l <= 'z') || (l >= 'A' && l <= 'Z') {
				p.pos++
				return l % 32, nil
			}
			if inClass && ((l >= '0' && l <= '9') || l == '_') {
				p.pos++
				return l % 32, nil
			}
		}
		p.pos--
		return '\\', nil
	case 'x':
		if v, ok := p.hexDigits(2); ok {
			return v, nil
		}
		return 'x', nil
	case 'u':
		if v, ok := p.hexDigits(4); ok {
			return v, nil
		}
		return 'u', nil
	}
	if c >= '1' && c <= '7' {
		p.pos--
		return p.legacyOctal(), nil
	}
	return c, nil
}

func (p *reParser) parseClass() (*reNode, error) {
	p.pos++
	node := &reNode{kind: reClass}
	if p.at('^') {
		node.negate = true
		p.pos++
	}
	for {
		if !p.more() {
			return nil, errClass
		}
		if p.at(']') {
			p.pos++
			return node, nil
		}
		lo, loSet, err := p.classAtom()
		if err != nil {
			return nil, err
		}
		if p.at('-') && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			p.pos++
			hi, hiSet, err := p.classAtom()
			if err != nil {
				return nil, err
			}
			if loSet != nil || hiSet != nil {
				node.ranges = appendAtom(node.ranges, lo, loSet)
				node.ranges = append(node.ranges, reRange{'-', '-'})
				node.ranges = appendAtom(node.ranges, hi, hiSet)
				continue
			}
			if lo > hi {
				return nil, errRange
			}
			node.ranges = append(node.ranges, reRange{lo, hi})
			continue
		}
		node.ranges = appendAtom(node.ranges, lo, loSet)
	}
}

func appendAtom(r []reRange, c uint16, set *classSet) []reRange {
	if set == nil {
		return append(r, reRange{c, c})
	}
	if !set.negate {
		return append(r, set.ranges...)
	}
	return append(r, invertRanges(set.ranges)...)
}

func invertRanges(rs []reRange) []reRange {
	var out []reRange
	next := 0
	for _, r := range rs {
		if int(r.lo) > next {
			out = append(out, reRange{uint16(next), r.lo - 1})
		}
		next = int(r.hi) + 1
	}
	if next <= 0xFFFF {
		out = append(out, reRange{uint16(next), 0xFFFF})
	}
	return out
}

func (p *reParser) classAtom() (uint16, *classSet, error) {
	c := p.peek()
	if c != '\\' {
		p.pos++
		return c, nil, nil
	}
	p.pos++
	if !p.more() {
		return 0, nil, errEndBackslash
	}
	if s, ok := p.classEscape(); ok {
		return 0, &s, nil
	}
	switch p.peek() {
	case 'b':
		p.pos++
		return '\b', nil, nil
	case '-':
		p.pos++
		return '-', nil, nil
	}
	if d := p.peek(); d >= '8' && d <= '9' {
		p.pos++
		return d, nil, nil
	}
	ch, err := p.characterEscape(true)
	return ch, nil, err
}

// ---- matcher ----

type matcher struct {
	re   *RegExp
	in   []uint16
	caps []int
}

func isWordChar(c uint16) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isLineTerminator(c uint16) bool { return c == '\n' || c == '\r' || c == 0x2028 || c == 0x2029 }

// canonicalize is the non-unicode Canonicalize of ECMAScript case-insensitive matching.
func canonicalize(c uint16) uint16 {
	u := U16(UpperCase(FromU16([]uint16{c})))
	if len(u) != 1 {
		return c
	}
	if c >= 128 && u[0] < 128 {
		return c
	}
	return u[0]
}

func (m *matcher) classMatch(n *reNode, c uint16) bool {
	in := func(c uint16) bool {
		for _, r := range n.ranges {
			if c >= r.lo && c <= r.hi {
				return true
			}
		}
		return false
	}
	found := in(c)
	if !found && m.re.ignore {
		cc := canonicalize(c)
		for _, r := range n.ranges {
			for x := int(r.lo); x <= int(r.hi); x++ {
				if canonicalize(uint16(x)) == cc {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	return found != n.negate
}

func (m *matcher) wordAt(i int) bool { return i >= 0 && i < len(m.in) && isWordChar(m.in[i]) }

// match tries node at pos and calls k with the end position; it returns k's result on the first success.
func (m *matcher) match(n *reNode, pos int, k func(int) bool) bool {
	switch n.kind {
	case reEmpty:
		return k(pos)
	case reChar:
		if pos < len(m.in) && (m.in[pos] == n.c || (m.re.ignore && canonicalize(m.in[pos]) == canonicalize(n.c))) {
			return k(pos + 1)
		}
		return false
	case reAny:
		if pos < len(m.in) && (m.re.dotAll || !isLineTerminator(m.in[pos])) {
			return k(pos + 1)
		}
		return false
	case reClass:
		if pos < len(m.in) && m.classMatch(n, m.in[pos]) {
			return k(pos + 1)
		}
		return false
	case reSeq:
		return m.matchSeq(n.subs, 0, pos, k)
	case reAlt:
		for _, s := range n.subs {
			if m.match(s, pos, k) {
				return true
			}
		}
		return false
	case reGroup:
		if n.index < 0 {
			return m.match(n.sub, pos, k)
		}
		i := n.index
		oldS, oldE := m.caps[2*i], m.caps[2*i+1]
		if m.match(n.sub, pos, func(end int) bool {
			ps, pe := m.caps[2*i], m.caps[2*i+1]
			m.caps[2*i], m.caps[2*i+1] = pos, end
			if k(end) {
				return true
			}
			m.caps[2*i], m.caps[2*i+1] = ps, pe
			return false
		}) {
			return true
		}
		m.caps[2*i], m.caps[2*i+1] = oldS, oldE
		return false
	case reLook:
		saved := append([]int(nil), m.caps...)
		var ok bool
		if n.ahead {
			ok = m.match(n.sub, pos, func(int) bool { return true })
		} else {
			ok = false
			for start := pos; start >= 0 && !ok; start-- {
				ok = m.match(n.sub, start, func(end int) bool { return end == pos })
			}
		}
		if n.negLook {
			copy(m.caps, saved)
			if ok {
				return false
			}
			return k(pos)
		}
		if !ok {
			copy(m.caps, saved)
			return false
		}
		if k(pos) {
			return true
		}
		copy(m.caps, saved)
		return false
	case reBackref:
		idx := n.index
		if idx < 0 {
			for i, name := range m.re.names {
				if name == n.name && i > 0 {
					idx = i
				}
			}
			if idx < 0 {
				return k(pos)
			}
		}
		s, e := m.caps[2*idx], m.caps[2*idx+1]
		if s < 0 || e < 0 {
			return k(pos)
		}
		l := e - s
		if pos+l > len(m.in) {
			return false
		}
		for j := 0; j < l; j++ {
			a, b := m.in[s+j], m.in[pos+j]
			if a != b && !(m.re.ignore && canonicalize(a) == canonicalize(b)) {
				return false
			}
		}
		return k(pos + l)
	case reStart:
		if pos == 0 || (m.re.multiline && isLineTerminator(m.in[pos-1])) {
			return k(pos)
		}
		return false
	case reEnd:
		if pos == len(m.in) || (m.re.multiline && isLineTerminator(m.in[pos])) {
			return k(pos)
		}
		return false
	case reWordB, reNotWordB:
		b := m.wordAt(pos-1) != m.wordAt(pos)
		if b == (n.kind == reWordB) {
			return k(pos)
		}
		return false
	case reRepeat:
		return m.repeat(n, pos, 0, k)
	}
	return false
}

func (m *matcher) matchSeq(subs []*reNode, i, pos int, k func(int) bool) bool {
	if i == len(subs) {
		return k(pos)
	}
	return m.match(subs[i], pos, func(end int) bool { return m.matchSeq(subs, i+1, end, k) })
}

func (m *matcher) repeat(n *reNode, pos, count int, k func(int) bool) bool {
	if n.max != -1 && count >= n.max {
		return k(pos)
	}
	try := func() bool {
		saved := make([]int, 0, 2*(n.lastCap-n.firstCap+1))
		for c := n.firstCap; c <= n.lastCap; c++ {
			saved = append(saved, m.caps[2*c], m.caps[2*c+1])
			m.caps[2*c], m.caps[2*c+1] = -1, -1
		}
		ok := m.match(n.sub, pos, func(end int) bool {
			if end == pos && count >= n.min {
				return false
			}
			return m.repeat(n, end, count+1, k)
		})
		if !ok {
			for j, c := 0, n.firstCap; c <= n.lastCap; c, j = c+1, j+2 {
				m.caps[2*c], m.caps[2*c+1] = saved[j], saved[j+1]
			}
		}
		return ok
	}
	if count < n.min {
		return try()
	}
	if n.greedy {
		if try() {
			return true
		}
		return k(pos)
	}
	if k(pos) {
		return true
	}
	return try()
}

// ---- RegExp built-ins ----

// Exec is RegExp.prototype.exec: it honours and updates lastIndex for global and sticky patterns and returns
// the match array (with index, input and groups) or null.
func (re *RegExp) Exec(s string) (Value, error) {
	caps, err := re.execRaw(s)
	if err != nil || caps == nil {
		return Null, err
	}
	return re.matchArray(s, caps), nil
}

func (re *RegExp) execRaw(s string) ([]int, error) {
	u := U16(s)
	last := 0
	if re.global || re.sticky {
		li, err := ToIntegerOrInfinity(re.LastIndex)
		if err != nil {
			return nil, err
		}
		if li > float64(len(u)) {
			re.LastIndex = 0.0
			return nil, nil
		}
		if li > 0 {
			last = int(li)
		}
	}
	caps := re.Search(u, last, re.sticky)
	if caps == nil {
		if re.global || re.sticky {
			re.LastIndex = 0.0
		}
		return nil, nil
	}
	if re.global || re.sticky {
		re.LastIndex = float64(caps[1])
	}
	return caps, nil
}

func (re *RegExp) matchArray(s string, caps []int) *Array {
	u := U16(s)
	arr := &Array{}
	for i := 0; i <= re.groups; i++ {
		if caps[2*i] < 0 {
			arr.Items = append(arr.Items, Undefined)
		} else {
			arr.Items = append(arr.Items, FromU16(u[caps[2*i]:caps[2*i+1]]))
		}
	}
	arr.Extra = NewObject()
	arr.Extra.Set("index", float64(caps[0]))
	arr.Extra.Set("input", s)
	groups := Undefined
	if hasNames(re.names) {
		g := NewObject()
		for i, name := range re.names {
			if name != "" {
				g.Set(name, arr.Items[i])
			}
		}
		groups = g
	}
	arr.Extra.Set("groups", groups)
	return arr
}

func hasNames(names []string) bool {
	for _, n := range names {
		if n != "" {
			return true
		}
	}
	return false
}

// Test is RegExp.prototype.test.
func (re *RegExp) Test(s string) (bool, error) {
	caps, err := re.execRaw(s)
	return caps != nil, err
}
