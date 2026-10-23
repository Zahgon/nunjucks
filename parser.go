// Ported from nunjucks/src/parser.js of mozilla/nunjucks (BSD-2-Clause, see LICENSE), including the slice
// syntax that nunjucks/src/jinja-compat.js adds to the parser.

package nunjucks

import (
	"strings"

	"github.com/mozilla/nunjucks/js"
)

type parser struct {
	tokens                *tokenizer
	peeked                *token
	breakOnBlocks         []string
	dropLeadingWhitespace bool
	jinjaCompat           bool
}

// parseTemplate parses template source into its Root node.
func parseTemplate(src string, opts *Options, jinjaCompat bool) (*node, error) {
	p := &parser{tokens: newTokenizer(src, opts.Tags, opts.TrimBlocks, opts.LstripBlocks), jinjaCompat: jinjaCompat}
	children, err := p.parseNodes()
	if err != nil {
		return nil, err
	}
	return newList("Root", 0, 0, children), nil
}

// nullTypeError is the TypeError of reading a property of a missing (null) token.
func nullTypeError(prop string) error { return js.ReadPropertyError(js.Null, prop) }

func (p *parser) nextToken(withWhitespace bool) (*token, error) {
	if p.peeked != nil {
		if !withWhitespace && p.peeked.typ == tokenWhitespace {
			p.peeked = nil
		} else {
			tok := p.peeked
			p.peeked = nil
			return tok, nil
		}
	}
	tok, err := p.tokens.nextToken()
	if err != nil {
		return nil, err
	}
	if !withWhitespace {
		for tok != nil && tok.typ == tokenWhitespace {
			if tok, err = p.tokens.nextToken(); err != nil {
				return nil, err
			}
		}
	}
	return tok, nil
}

func (p *parser) next() (*token, error) { return p.nextToken(false) }

func (p *parser) peekToken() (*token, error) {
	if p.peeked == nil {
		tok, err := p.next()
		if err != nil {
			return nil, err
		}
		p.peeked = tok
	}
	return p.peeked, nil
}

// peekType returns the type of the next token; a missing token is the original's TypeError.
func (p *parser) peekType() (string, error) {
	tok, err := p.peekToken()
	if err != nil {
		return "", err
	}
	if tok == nil {
		return "", nullTypeError("type")
	}
	return tok.typ, nil
}

func (p *parser) pushToken(tok *token) error {
	if p.peeked != nil {
		return js.NewError("pushToken: can only push one token on between reads")
	}
	p.peeked = tok
	return nil
}

// errAt is a parser error positioned at tok (unpositioned when tok is nil).
func (p *parser) errAt(msg string, tok *token) error {
	if tok == nil {
		return templateError(msg, 0, 0)
	}
	return templateError(msg, tok.lineno+1, tok.colno+1)
}

// errPeek is a parser error positioned at the next token.
func (p *parser) errPeek(msg string) error {
	tok, err := p.peekToken()
	if err != nil {
		return err
	}
	return p.errAt(msg, tok)
}

func (p *parser) skip(typ string) (bool, error) {
	tok, err := p.next()
	if err != nil {
		return false, err
	}
	if tok == nil || tok.typ != typ {
		return false, p.pushToken(tok)
	}
	return true, nil
}

func (p *parser) expect(typ string) (*token, error) {
	tok, err := p.next()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, nullTypeError("type")
	}
	if tok.typ != typ {
		return nil, p.errAt("expected "+typ+", got "+tok.typ, tok)
	}
	return tok, nil
}

func (p *parser) skipValue(typ, val string) (bool, error) {
	tok, err := p.next()
	if err != nil {
		return false, err
	}
	if tok == nil || tok.typ != typ || tok.value != val {
		return false, p.pushToken(tok)
	}
	return true, nil
}

func (p *parser) skipSymbol(val string) (bool, error) { return p.skipValue(tokenSymbol, val) }

func (p *parser) advanceAfterBlockEnd(name string) (*token, error) {
	if name == "" {
		tok, err := p.peekToken()
		if err != nil {
			return nil, err
		}
		if tok == nil {
			return nil, p.errPeek("unexpected end of file")
		}
		if tok.typ != tokenSymbol {
			return nil, p.errPeek("advanceAfterBlockEnd: expected symbol token or explicit name to be passed")
		}
		t, err := p.next()
		if err != nil {
			return nil, err
		}
		name = t.value
	}
	tok, err := p.next()
	if err != nil {
		return nil, err
	}
	if tok != nil && tok.typ == tokenBlockEnd {
		if strings.HasPrefix(tok.value, "-") {
			p.dropLeadingWhitespace = true
		}
		return tok, nil
	}
	return nil, p.errPeek("expected block end in " + name + " statement")
}

func (p *parser) advanceAfterVariableEnd() error {
	tok, err := p.next()
	if err != nil {
		return err
	}
	if tok != nil && tok.typ == tokenVariableEnd {
		u := js.U16(tok.value)
		i := len(u) - len(js.U16(p.tokens.tags.VariableEnd)) - 1
		p.dropLeadingWhitespace = i >= 0 && i < len(u) && u[i] == '-'
		return nil
	}
	if err := p.pushToken(tok); err != nil {
		return err
	}
	return p.errPeek("expected variable end")
}

func (p *parser) parseFor() (*node, error) {
	forTok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	var n *node
	var endBlock string
	if ok, err := p.skipSymbol("for"); err != nil {
		return nil, err
	} else if ok {
		n, endBlock = newNode("For", forTok.lineno, forTok.colno), "endfor"
	} else if ok, err := p.skipSymbol("asyncEach"); err != nil {
		return nil, err
	} else if ok {
		n, endBlock = newNode("AsyncEach", forTok.lineno, forTok.colno), "endeach"
	} else if ok, err := p.skipSymbol("asyncAll"); err != nil {
		return nil, err
	} else if ok {
		n, endBlock = newNode("AsyncAll", forTok.lineno, forTok.colno), "endall"
	} else {
		return nil, p.errAt("parseFor: expected for{Async}", forTok)
	}
	if n.name, err = p.parsePrimary(false); err != nil {
		return nil, err
	}
	if n.name.typ != "Symbol" {
		return nil, p.errPeek("parseFor: variable name expected for loop")
	}
	typ, err := p.peekType()
	if err != nil {
		return nil, err
	}
	if typ == tokenComma {
		key := n.name
		n.name = newList("Array", key.lineno, key.colno, []*node{key})
		for {
			ok, err := p.skip(tokenComma)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			prim, err := p.parsePrimary(false)
			if err != nil {
				return nil, err
			}
			n.name.children = append(n.name.children, prim)
		}
	}
	if ok, err := p.skipSymbol("in"); err != nil {
		return nil, err
	} else if !ok {
		return nil, p.errAt(`parseFor: expected "in" keyword for loop`, forTok)
	}
	if n.arr, err = p.parseExpression(); err != nil {
		return nil, err
	}
	if _, err := p.advanceAfterBlockEnd(forTok.value); err != nil {
		return nil, err
	}
	if n.body, err = p.parseUntilBlocks(endBlock, "else"); err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("else"); err != nil {
		return nil, err
	} else if ok {
		if _, err := p.advanceAfterBlockEnd("else"); err != nil {
			return nil, err
		}
		if n.else_, err = p.parseUntilBlocks(endBlock); err != nil {
			return nil, err
		}
	}
	if _, err := p.advanceAfterBlockEnd(""); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) parseMacro() (*node, error) {
	macroTok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("macro"); err != nil {
		return nil, err
	} else if !ok {
		return nil, p.errPeek("expected macro")
	}
	name, err := p.parsePrimary(true)
	if err != nil {
		return nil, err
	}
	args, err := p.parseSignature(false, false)
	if err != nil {
		return nil, err
	}
	n := newNode("Macro", macroTok.lineno, macroTok.colno)
	n.name, n.args = name, args
	if _, err := p.advanceAfterBlockEnd(macroTok.value); err != nil {
		return nil, err
	}
	if n.body, err = p.parseUntilBlocks("endmacro"); err != nil {
		return nil, err
	}
	if _, err := p.advanceAfterBlockEnd(""); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) parseCall() (*node, error) {
	callTok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("call"); err != nil {
		return nil, err
	} else if !ok {
		return nil, p.errPeek("expected call")
	}
	callerArgs, err := p.parseSignature(true, false)
	if err != nil {
		return nil, err
	}
	if callerArgs == nil {
		callerArgs = newList("NodeList", 0, 0, nil)
	}
	macroCall, err := p.parsePrimary(false)
	if err != nil {
		return nil, err
	}
	if _, err := p.advanceAfterBlockEnd(callTok.value); err != nil {
		return nil, err
	}
	body, err := p.parseUntilBlocks("endcall")
	if err != nil {
		return nil, err
	}
	if _, err := p.advanceAfterBlockEnd(""); err != nil {
		return nil, err
	}
	callerName := newLiteral("Symbol", callTok.lineno, callTok.colno, "caller")
	callerNode := newNode("Caller", callTok.lineno, callTok.colno)
	callerNode.name, callerNode.args, callerNode.body = callerName, callerArgs, body
	if macroCall.typ != "FunCall" && macroCall.typ != "Filter" {
		return nil, js.ReadPropertyError(js.Undefined, "children")
	}
	args := macroCall.args.children
	if len(args) == 0 || args[len(args)-1].typ != "KeywordArgs" {
		args = append(args, newList("KeywordArgs", 0, 0, nil))
	}
	kwargs := args[len(args)-1]
	pair := newNode("Pair", callTok.lineno, callTok.colno)
	pair.key, pair.value = callerName, callerNode
	kwargs.children = append(kwargs.children, pair)
	macroCall.args.children = args
	return newList("Output", callTok.lineno, callTok.colno, []*node{macroCall}), nil
}

// parseWithContext returns null, true or false.
func (p *parser) parseWithContext() (js.Value, error) {
	tok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	var withContext js.Value = js.Null
	if ok, err := p.skipSymbol("with"); err != nil {
		return nil, err
	} else if ok {
		withContext = true
	} else if ok, err := p.skipSymbol("without"); err != nil {
		return nil, err
	} else if ok {
		withContext = false
	}
	if !js.IsNull(withContext) {
		if ok, err := p.skipSymbol("context"); err != nil {
			return nil, err
		} else if !ok {
			return nil, p.errAt("parseFrom: expected context after with/without", tok)
		}
	}
	return withContext, nil
}

func (p *parser) parseImport() (*node, error) {
	importTok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("import"); err != nil {
		return nil, err
	} else if !ok {
		return nil, p.errAt("parseImport: expected import", importTok)
	}
	n := newNode("Import", importTok.lineno, importTok.colno)
	if n.template, err = p.parseExpression(); err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("as"); err != nil {
		return nil, err
	} else if !ok {
		return nil, p.errAt(`parseImport: expected "as" keyword`, importTok)
	}
	if n.target, err = p.parseExpression(); err != nil {
		return nil, err
	}
	if n.withContext, err = p.parseWithContext(); err != nil {
		return nil, err
	}
	if _, err := p.advanceAfterBlockEnd(importTok.value); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) parseFrom() (*node, error) {
	fromTok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("from"); err != nil {
		return nil, err
	} else if !ok {
		return nil, p.errPeek("parseFrom: expected from")
	}
	template, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("import"); err != nil {
		return nil, err
	} else if !ok {
		return nil, p.errAt("parseFrom: expected import", fromTok)
	}
	names := newList("NodeList", 0, 0, nil)
	var withContext js.Value = js.Undefined
	for {
		nextTok, err := p.peekToken()
		if err != nil {
			return nil, err
		}
		if nextTok == nil {
			return nil, nullTypeError("type")
		}
		if nextTok.typ == tokenBlockEnd {
			if len(names.children) == 0 {
				return nil, p.errAt("parseFrom: Expected at least one import name", fromTok)
			}
			if strings.HasPrefix(nextTok.value, "-") {
				p.dropLeadingWhitespace = true
			}
			if _, err := p.next(); err != nil {
				return nil, err
			}
			break
		}
		if len(names.children) > 0 {
			if ok, err := p.skip(tokenComma); err != nil {
				return nil, err
			} else if !ok {
				return nil, p.errAt("parseFrom: expected comma", fromTok)
			}
		}
		name, err := p.parsePrimary(false)
		if err != nil {
			return nil, err
		}
		nameStr, ok := name.lit.(string)
		if !ok {
			return nil, js.NotFunctionError("name.value.charAt")
		}
		if strings.HasPrefix(nameStr, "_") {
			return nil, p.errAt("parseFrom: names starting with an underscore cannot be imported", &token{lineno: name.lineno, colno: name.colno})
		}
		if ok, err := p.skipSymbol("as"); err != nil {
			return nil, err
		} else if ok {
			alias, err := p.parsePrimary(false)
			if err != nil {
				return nil, err
			}
			pair := newNode("Pair", name.lineno, name.colno)
			pair.key, pair.value = name, alias
			names.children = append(names.children, pair)
		} else {
			names.children = append(names.children, name)
		}
		if withContext, err = p.parseWithContext(); err != nil {
			return nil, err
		}
	}
	n := newNode("FromImport", fromTok.lineno, fromTok.colno)
	n.template, n.names, n.withContext = template, names, withContext
	return n, nil
}

func (p *parser) parseBlock() (*node, error) {
	tag, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("block"); err != nil {
		return nil, err
	} else if !ok {
		return nil, p.errAt("parseBlock: expected block", tag)
	}
	n := newNode("Block", tag.lineno, tag.colno)
	if n.name, err = p.parsePrimary(false); err != nil {
		return nil, err
	}
	if n.name.typ != "Symbol" {
		return nil, p.errAt("parseBlock: variable name expected", tag)
	}
	if _, err := p.advanceAfterBlockEnd(tag.value); err != nil {
		return nil, err
	}
	if n.body, err = p.parseUntilBlocks("endblock"); err != nil {
		return nil, err
	}
	if _, err := p.skipSymbol("endblock"); err != nil {
		return nil, err
	}
	if _, err := p.skipSymbol(n.name.symbolName()); err != nil {
		return nil, err
	}
	tok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, p.errPeek("parseBlock: expected endblock, got end of file")
	}
	if _, err := p.advanceAfterBlockEnd(tok.value); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) parseTemplateRef(typ, tagName string) (*node, error) {
	tag, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol(tagName); err != nil {
		return nil, err
	} else if !ok {
		return nil, p.errPeek("parseTemplateRef: expected " + tagName)
	}
	n := newNode(typ, tag.lineno, tag.colno)
	if n.template, err = p.parseExpression(); err != nil {
		return nil, err
	}
	if typ == "Include" {
		ignore, err := p.skipSymbol("ignore")
		if err != nil {
			return nil, err
		}
		if ignore {
			missing, err := p.skipSymbol("missing")
			if err != nil {
				return nil, err
			}
			n.ignoreMissing = missing
		}
	}
	if _, err := p.advanceAfterBlockEnd(tag.value); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) parseIf() (*node, error) {
	tag, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	var n *node
	for _, kw := range []string{"if", "elif", "elseif"} {
		ok, err := p.skipSymbol(kw)
		if err != nil {
			return nil, err
		}
		if ok {
			n = newNode("If", tag.lineno, tag.colno)
			break
		}
	}
	if n == nil {
		ok, err := p.skipSymbol("ifAsync")
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, p.errAt("parseIf: expected if, elif, or elseif", tag)
		}
		n = newNode("IfAsync", tag.lineno, tag.colno)
	}
	if n.cond, err = p.parseExpression(); err != nil {
		return nil, err
	}
	if _, err := p.advanceAfterBlockEnd(tag.value); err != nil {
		return nil, err
	}
	if n.body, err = p.parseUntilBlocks("elif", "elseif", "else", "endif"); err != nil {
		return nil, err
	}
	tok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	value := ""
	if tok != nil {
		value = tok.value
	}
	switch value {
	case "elseif", "elif":
		if n.else_, err = p.parseIf(); err != nil {
			return nil, err
		}
	case "else":
		if _, err := p.advanceAfterBlockEnd(""); err != nil {
			return nil, err
		}
		if n.else_, err = p.parseUntilBlocks("endif"); err != nil {
			return nil, err
		}
		if _, err := p.advanceAfterBlockEnd(""); err != nil {
			return nil, err
		}
	case "endif":
		n.else_ = nil
		if _, err := p.advanceAfterBlockEnd(""); err != nil {
			return nil, err
		}
	default:
		return nil, p.errPeek("parseIf: expected elif, else, or endif, got end of file")
	}
	return n, nil
}

func (p *parser) parseSet() (*node, error) {
	tag, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("set"); err != nil {
		return nil, err
	} else if !ok {
		return nil, p.errAt("parseSet: expected set", tag)
	}
	n := newNode("Set", tag.lineno, tag.colno)
	n.targets = []*node{}
	for {
		target, err := p.parsePrimary(false)
		if err != nil {
			return nil, err
		}
		n.targets = append(n.targets, target)
		ok, err := p.skip(tokenComma)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
	}
	isAssign, err := p.skipValue(tokenOperator, "=")
	if err != nil {
		return nil, err
	}
	if !isAssign {
		ok, err := p.skip(tokenBlockEnd)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, p.errAt("parseSet: expected = or block end in set tag", tag)
		}
		body, err := p.parseUntilBlocks("endset")
		if err != nil {
			return nil, err
		}
		capture := newNode("Capture", tag.lineno, tag.colno)
		capture.body = body
		n.setBody = capture
		n.value = nil
		if _, err := p.advanceAfterBlockEnd(""); err != nil {
			return nil, err
		}
		return n, nil
	}
	if n.value, err = p.parseExpression(); err != nil {
		return nil, err
	}
	if _, err := p.advanceAfterBlockEnd(tag.value); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) parseSwitch() (*node, error) {
	tag, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	matched := false
	for _, kw := range []string{"switch", "case", "default"} {
		ok, err := p.skipSymbol(kw)
		if err != nil {
			return nil, err
		}
		if ok {
			matched = true
			break
		}
	}
	if !matched {
		return nil, p.errAt(`parseSwitch: expected "switch," "case" or "default"`, tag)
	}
	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := p.advanceAfterBlockEnd("switch"); err != nil {
		return nil, err
	}
	if _, err := p.parseUntilBlocks("case", "default", "endswitch"); err != nil {
		return nil, err
	}
	var tok *token
	var cases []*node
	var def *node
	for {
		if _, err := p.skipSymbol("case"); err != nil {
			return nil, err
		}
		cond, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.advanceAfterBlockEnd("switch"); err != nil {
			return nil, err
		}
		body, err := p.parseUntilBlocks("case", "default", "endswitch")
		if err != nil {
			return nil, err
		}
		c := newNode("Case", 0, 0)
		c.cond, c.body = cond, body
		cases = append(cases, c)
		if tok, err = p.peekToken(); err != nil {
			return nil, err
		}
		if tok == nil || tok.value != "case" {
			break
		}
	}
	if tok == nil {
		return nil, nullTypeError("value")
	}
	switch tok.value {
	case "default":
		if _, err := p.advanceAfterBlockEnd(""); err != nil {
			return nil, err
		}
		if def, err = p.parseUntilBlocks("endswitch"); err != nil {
			return nil, err
		}
		if _, err := p.advanceAfterBlockEnd(""); err != nil {
			return nil, err
		}
	case "endswitch":
		if _, err := p.advanceAfterBlockEnd(""); err != nil {
			return nil, err
		}
	default:
		return nil, p.errPeek(`parseSwitch: expected "case," "default" or "endswitch," got EOF.`)
	}
	n := newNode("Switch", tag.lineno, tag.colno)
	n.expr, n.cases, n.def = expr, cases, def
	return n, nil
}

func (p *parser) parseStatement() (*node, error) {
	tok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, nullTypeError("type")
	}
	if tok.typ != tokenSymbol {
		return nil, p.errAt("tag name expected", tok)
	}
	for _, b := range p.breakOnBlocks {
		if b == tok.value {
			return nil, nil
		}
	}
	switch tok.value {
	case "raw":
		return p.parseRaw("raw")
	case "verbatim":
		return p.parseRaw("verbatim")
	case "if", "ifAsync":
		return p.parseIf()
	case "for", "asyncEach", "asyncAll":
		return p.parseFor()
	case "block":
		return p.parseBlock()
	case "extends":
		return p.parseTemplateRef("Extends", "extends")
	case "include":
		return p.parseTemplateRef("Include", "include")
	case "set":
		return p.parseSet()
	case "macro":
		return p.parseMacro()
	case "call":
		return p.parseCall()
	case "import":
		return p.parseImport()
	case "from":
		return p.parseFrom()
	case "filter":
		return p.parseFilterStatement()
	case "switch":
		return p.parseSwitch()
	}
	return nil, p.errAt("unknown block tag: "+tok.value, tok)
}

func (p *parser) parseRaw(tagName string) (*node, error) {
	endTagName := "end" + tagName
	re, err := js.NewRegExp(`([\s\S]*?){%\s*(`+tagName+`|`+endTagName+`)\s*(?=%})%}`, "")
	if err != nil {
		return nil, err
	}
	rawLevel := 1
	var str strings.Builder
	begun, err := p.advanceAfterBlockEnd("")
	if err != nil {
		return nil, err
	}
	for {
		matches := p.tokens.extractRegex(re)
		if matches == nil || rawLevel <= 0 {
			break
		}
		all, pre, blockName := matches[0], matches[1], matches[2]
		if blockName == tagName {
			rawLevel++
		} else if blockName == endTagName {
			rawLevel--
		}
		if rawLevel == 0 {
			str.WriteString(pre)
			if err := p.tokens.backN(js.Len16(all) - js.Len16(pre)); err != nil {
				return nil, err
			}
		} else {
			str.WriteString(all)
		}
	}
	data := newLiteral("TemplateData", begun.lineno, begun.colno, str.String())
	return newList("Output", begun.lineno, begun.colno, []*node{data}), nil
}

func (p *parser) parsePostfix(n *node) (*node, error) {
	tok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	for tok != nil {
		switch {
		case tok.typ == tokenLeftParen:
			args, err := p.parseSignature(false, false)
			if err != nil {
				return nil, err
			}
			call := newNode("FunCall", tok.lineno, tok.colno)
			call.name, call.args = n, args
			n = call
		case tok.typ == tokenLeftBracket:
			lookup, err := p.parseAggregateNode()
			if err != nil {
				return nil, err
			}
			if len(lookup.children) > 1 {
				return nil, p.errPeek("invalid index")
			}
			lv := newNode("LookupVal", tok.lineno, tok.colno)
			lv.target = n
			if len(lookup.children) == 1 {
				lv.val = lookup.children[0]
			}
			n = lv
		case tok.typ == tokenOperator && tok.value == ".":
			if _, err := p.next(); err != nil {
				return nil, err
			}
			val, err := p.next()
			if err != nil {
				return nil, err
			}
			if val == nil {
				return nil, nullTypeError("type")
			}
			if val.typ != tokenSymbol {
				return nil, p.errAt("expected name as lookup value, got "+val.value, val)
			}
			lv := newNode("LookupVal", tok.lineno, tok.colno)
			lv.target = n
			lv.val = newLiteral("Literal", val.lineno, val.colno, val.value)
			n = lv
		default:
			return n, nil
		}
		if tok, err = p.peekToken(); err != nil {
			return nil, err
		}
	}
	return n, nil
}

func (p *parser) parseExpression() (*node, error) { return p.parseInlineIf() }

func (p *parser) parseInlineIf() (*node, error) {
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("if"); err != nil {
		return nil, err
	} else if ok {
		cond, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		body := n
		n = newNode("InlineIf", n.lineno, n.colno)
		n.body, n.cond = body, cond
		if ok, err := p.skipSymbol("else"); err != nil {
			return nil, err
		} else if ok {
			if n.else_, err = p.parseOr(); err != nil {
				return nil, err
			}
		}
	}
	return n, nil
}

// parseBinary parses a left-associative chain of operator tokens of type typ and value val.
func (p *parser) parseBinary(nodeType, typ, val string, operand func() (*node, error)) (*node, error) {
	n, err := operand()
	if err != nil {
		return nil, err
	}
	for {
		ok, err := p.skipValue(typ, val)
		if err != nil {
			return nil, err
		}
		if !ok {
			return n, nil
		}
		right, err := operand()
		if err != nil {
			return nil, err
		}
		n = newBinOp(nodeType, n.lineno, n.colno, n, right)
	}
}

func (p *parser) parseOr() (*node, error) {
	return p.parseBinary("Or", tokenSymbol, "or", p.parseAnd)
}

func (p *parser) parseAnd() (*node, error) {
	return p.parseBinary("And", tokenSymbol, "and", p.parseNot)
}

func (p *parser) parseNot() (*node, error) {
	tok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("not"); err != nil {
		return nil, err
	} else if ok {
		target, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return newUnary("Not", tok.lineno, tok.colno, target), nil
	}
	return p.parseIn()
}

func (p *parser) parseIn() (*node, error) {
	n, err := p.parseIs()
	if err != nil {
		return nil, err
	}
	for {
		tok, err := p.next()
		if err != nil {
			return nil, err
		}
		if tok == nil {
			break
		}
		invert := tok.typ == tokenSymbol && tok.value == "not"
		if !invert {
			if err := p.pushToken(tok); err != nil {
				return nil, err
			}
		}
		ok, err := p.skipSymbol("in")
		if err != nil {
			return nil, err
		}
		if ok {
			right, err := p.parseIs()
			if err != nil {
				return nil, err
			}
			n = newBinOp("In", n.lineno, n.colno, n, right)
			if invert {
				n = newUnary("Not", n.lineno, n.colno, n)
			}
			continue
		}
		if invert {
			if err := p.pushToken(tok); err != nil {
				return nil, err
			}
		}
		break
	}
	return n, nil
}

func (p *parser) parseIs() (*node, error) {
	n, err := p.parseCompare()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("is"); err != nil {
		return nil, err
	} else if ok {
		not, err := p.skipSymbol("not")
		if err != nil {
			return nil, err
		}
		right, err := p.parseCompare()
		if err != nil {
			return nil, err
		}
		n = newBinOp("Is", n.lineno, n.colno, n, right)
		if not {
			n = newUnary("Not", n.lineno, n.colno, n)
		}
	}
	return n, nil
}

var compareOps = map[string]bool{"==": true, "===": true, "!=": true, "!==": true, "<": true, ">": true, "<=": true, ">=": true}

func (p *parser) parseCompare() (*node, error) {
	expr, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	var ops []*node
	for {
		tok, err := p.next()
		if err != nil {
			return nil, err
		}
		if tok == nil {
			break
		}
		if tok.typ != tokenRegex && compareOps[tok.value] {
			operand, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			op := newNode("CompareOperand", tok.lineno, tok.colno)
			op.expr, op.opType = operand, tok.value
			ops = append(ops, op)
			continue
		}
		if err := p.pushToken(tok); err != nil {
			return nil, err
		}
		break
	}
	if len(ops) == 0 {
		return expr, nil
	}
	n := newNode("Compare", ops[0].lineno, ops[0].colno)
	n.expr, n.ops = expr, ops
	return n, nil
}

func (p *parser) parseConcat() (*node, error) {
	return p.parseBinary("Concat", tokenTilde, "~", p.parseAdd)
}

func (p *parser) parseAdd() (*node, error) {
	return p.parseBinary("Add", tokenOperator, "+", p.parseSub)
}

func (p *parser) parseSub() (*node, error) {
	return p.parseBinary("Sub", tokenOperator, "-", p.parseMul)
}

func (p *parser) parseMul() (*node, error) {
	return p.parseBinary("Mul", tokenOperator, "*", p.parseDiv)
}

func (p *parser) parseDiv() (*node, error) {
	return p.parseBinary("Div", tokenOperator, "/", p.parseFloorDiv)
}

func (p *parser) parseFloorDiv() (*node, error) {
	return p.parseBinary("FloorDiv", tokenOperator, "//", p.parseMod)
}

func (p *parser) parseMod() (*node, error) {
	return p.parseBinary("Mod", tokenOperator, "%", p.parsePow)
}

func (p *parser) parsePow() (*node, error) {
	return p.parseBinary("Pow", tokenOperator, "**", func() (*node, error) { return p.parseUnary(false) })
}

func (p *parser) parseUnary(noFilters bool) (*node, error) {
	tok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	var n *node
	if ok, err := p.skipValue(tokenOperator, "-"); err != nil {
		return nil, err
	} else if ok {
		target, err := p.parseUnary(true)
		if err != nil {
			return nil, err
		}
		n = newUnary("Neg", tok.lineno, tok.colno, target)
	} else if ok, err := p.skipValue(tokenOperator, "+"); err != nil {
		return nil, err
	} else if ok {
		target, err := p.parseUnary(true)
		if err != nil {
			return nil, err
		}
		n = newUnary("Pos", tok.lineno, tok.colno, target)
	} else if n, err = p.parsePrimary(false); err != nil {
		return nil, err
	}
	if !noFilters {
		return p.parseFilter(n)
	}
	return n, nil
}

func (p *parser) parsePrimary(noPostfix bool) (*node, error) {
	tok, err := p.next()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, p.errPeek("expected expression, got end of file")
	}
	var val js.Value
	switch tok.typ {
	case tokenString:
		val = tok.value
	case tokenInt:
		val, _ = js.ParseInt(tok.value, 10.0)
	case tokenFloat:
		val, _ = js.ParseFloat(tok.value)
	case tokenBoolean:
		val = tok.value == "true"
	case tokenNone:
		val = js.Null
	case tokenRegex:
		re, err := js.NewRegExp(tok.value, tok.flags)
		if err != nil {
			return nil, err
		}
		val = re
	}
	var n *node
	switch {
	case val != nil:
		n = newLiteral("Literal", tok.lineno, tok.colno, val)
	case tok.typ == tokenSymbol:
		n = newLiteral("Symbol", tok.lineno, tok.colno, tok.value)
	default:
		if err := p.pushToken(tok); err != nil {
			return nil, err
		}
		if n, err = p.parseAggregateNode(); err != nil {
			return nil, err
		}
	}
	if !noPostfix {
		if n, err = p.parsePostfix(n); err != nil {
			return nil, err
		}
	}
	if n == nil {
		return nil, p.errAt("unexpected token: "+tok.value, tok)
	}
	return n, nil
}

func (p *parser) parseFilterName() (*node, error) {
	tok, err := p.expect(tokenSymbol)
	if err != nil {
		return nil, err
	}
	name := tok.value
	for {
		ok, err := p.skipValue(tokenOperator, ".")
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		part, err := p.expect(tokenSymbol)
		if err != nil {
			return nil, err
		}
		name += "." + part.value
	}
	return newLiteral("Symbol", tok.lineno, tok.colno, name), nil
}

func (p *parser) parseFilterArgs(n *node) ([]*node, error) {
	typ, err := p.peekType()
	if err != nil {
		return nil, err
	}
	if typ != tokenLeftParen {
		return nil, nil
	}
	call, err := p.parsePostfix(n)
	if err != nil {
		return nil, err
	}
	if call.args == nil {
		return nil, js.ReadPropertyError(js.Undefined, "children")
	}
	return call.args.children, nil
}

func (p *parser) parseFilter(n *node) (*node, error) {
	for {
		ok, err := p.skip(tokenPipe)
		if err != nil {
			return nil, err
		}
		if !ok {
			return n, nil
		}
		name, err := p.parseFilterName()
		if err != nil {
			return nil, err
		}
		args, err := p.parseFilterArgs(n)
		if err != nil {
			return nil, err
		}
		f := newNode("Filter", name.lineno, name.colno)
		f.name = name
		f.args = newList("NodeList", name.lineno, name.colno, append([]*node{n}, args...))
		n = f
	}
}

func (p *parser) parseFilterStatement() (*node, error) {
	filterTok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if ok, err := p.skipSymbol("filter"); err != nil {
		return nil, err
	} else if !ok {
		return nil, p.errPeek("parseFilterStatement: expected filter")
	}
	name, err := p.parseFilterName()
	if err != nil {
		return nil, err
	}
	args, err := p.parseFilterArgs(name)
	if err != nil {
		return nil, err
	}
	if _, err := p.advanceAfterBlockEnd(filterTok.value); err != nil {
		return nil, err
	}
	body, err := p.parseUntilBlocks("endfilter")
	if err != nil {
		return nil, err
	}
	capture := newNode("Capture", name.lineno, name.colno)
	capture.body = body
	if _, err := p.advanceAfterBlockEnd(""); err != nil {
		return nil, err
	}
	f := newNode("Filter", name.lineno, name.colno)
	f.name = name
	f.args = newList("NodeList", name.lineno, name.colno, append([]*node{capture}, args...))
	return newList("Output", name.lineno, name.colno, []*node{f}), nil
}

// parseAggregateNode is parseAggregate as installed: the Jinja compatibility layer adds slices.
func (p *parser) parseAggregateNode() (*node, error) {
	if !p.jinjaCompat {
		return p.parseAggregate()
	}
	origIndex, origLine, origCol := p.tokens.index-1, p.tokens.lineno, p.tokens.colno-1
	n, err := p.parseAggregate()
	if err == nil {
		return n, nil
	}
	errIndex, errLine, errCol := p.tokens.index, p.tokens.lineno, p.tokens.colno
	rethrow := func() error {
		p.tokens.index, p.tokens.lineno, p.tokens.colno = errIndex, errLine, errCol
		return err
	}
	p.tokens.index, p.tokens.lineno, p.tokens.colno = origIndex, origLine, origCol
	p.peeked = nil
	tok, perr := p.peekToken()
	if perr != nil {
		return nil, perr
	}
	if tok == nil {
		return nil, nullTypeError("type")
	}
	if tok.typ != tokenLeftBracket {
		return nil, rethrow()
	}
	if _, err := p.next(); err != nil {
		return nil, err
	}
	slice := newNode("Slice", tok.lineno, tok.colno)
	slice.start = newLiteral("Literal", tok.lineno, tok.colno, js.Null)
	slice.stop = newLiteral("Literal", tok.lineno, tok.colno, js.Null)
	slice.step = newLiteral("Literal", tok.lineno, tok.colno, 1.0)
	fields := []**node{&slice.start, &slice.stop, &slice.step}
	isSlice := false
	for i := 0; i <= len(fields); i++ {
		if ok, err := p.skip(tokenRightBracket); err != nil {
			return nil, err
		} else if ok {
			break
		}
		if i == len(fields) {
			if isSlice {
				return nil, p.errAt("parseSlice: too many slice components", tok)
			}
			break
		}
		if ok, err := p.skip(tokenColon); err != nil {
			return nil, err
		} else if ok {
			isSlice = true
			continue
		}
		if *fields[i], err = p.parseExpression(); err != nil {
			return nil, err
		}
		colon, err := p.skip(tokenColon)
		if err != nil {
			return nil, err
		}
		isSlice = colon || isSlice
	}
	if !isSlice {
		return nil, rethrow()
	}
	return newList("Array", tok.lineno, tok.colno, []*node{slice}), nil
}

func (p *parser) parseAggregate() (*node, error) {
	tok, err := p.next()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, nullTypeError("type")
	}
	var n *node
	switch tok.typ {
	case tokenLeftParen:
		n = newList("Group", tok.lineno, tok.colno, nil)
	case tokenLeftBracket:
		n = newList("Array", tok.lineno, tok.colno, nil)
	case tokenLeftCurly:
		n = newList("Dict", tok.lineno, tok.colno, nil)
	default:
		return nil, nil
	}
	for {
		typ, err := p.peekType()
		if err != nil {
			return nil, err
		}
		if typ == tokenRightParen || typ == tokenRightBracket || typ == tokenRightCurly {
			if _, err := p.next(); err != nil {
				return nil, err
			}
			break
		}
		if len(n.children) > 0 {
			if ok, err := p.skip(tokenComma); err != nil {
				return nil, err
			} else if !ok {
				return nil, p.errAt("parseAggregate: expected comma after expression", tok)
			}
		}
		if n.typ == "Dict" {
			key, err := p.parsePrimary(false)
			if err != nil {
				return nil, err
			}
			if ok, err := p.skip(tokenColon); err != nil {
				return nil, err
			} else if !ok {
				return nil, p.errAt("parseAggregate: expected colon after dict key", tok)
			}
			value, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			pair := newNode("Pair", key.lineno, key.colno)
			pair.key, pair.value = key, value
			n.children = append(n.children, pair)
		} else {
			expr, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			n.children = append(n.children, expr)
		}
	}
	return n, nil
}

func (p *parser) parseSignature(tolerant, noParens bool) (*node, error) {
	tok, err := p.peekToken()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, nullTypeError("type")
	}
	if !noParens && tok.typ != tokenLeftParen {
		if tolerant {
			return nil, nil
		}
		return nil, p.errAt("expected arguments", tok)
	}
	if tok.typ == tokenLeftParen {
		if tok, err = p.next(); err != nil {
			return nil, err
		}
	}
	args := newList("NodeList", tok.lineno, tok.colno, nil)
	kwargs := newList("KeywordArgs", tok.lineno, tok.colno, nil)
	checkComma := false
	for {
		if tok, err = p.peekToken(); err != nil {
			return nil, err
		}
		if tok == nil {
			return nil, nullTypeError("type")
		}
		if !noParens && tok.typ == tokenRightParen {
			if _, err := p.next(); err != nil {
				return nil, err
			}
			break
		} else if noParens && tok.typ == tokenBlockEnd {
			break
		}
		if checkComma {
			if ok, err := p.skip(tokenComma); err != nil {
				return nil, err
			} else if !ok {
				return nil, p.errAt("parseSignature: expected comma after expression", tok)
			}
		}
		arg, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if ok, err := p.skipValue(tokenOperator, "="); err != nil {
			return nil, err
		} else if ok {
			value, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			pair := newNode("Pair", arg.lineno, arg.colno)
			pair.key, pair.value = arg, value
			kwargs.children = append(kwargs.children, pair)
		} else {
			args.children = append(args.children, arg)
		}
		checkComma = true
	}
	if len(kwargs.children) > 0 {
		args.children = append(args.children, kwargs)
	}
	return args, nil
}

func (p *parser) parseUntilBlocks(names ...string) (*node, error) {
	prev := p.breakOnBlocks
	p.breakOnBlocks = names
	children, err := p.parseNodes()
	p.breakOnBlocks = prev
	if err != nil {
		return nil, err
	}
	return newList("NodeList", 0, 0, children), nil
}

func (p *parser) parseNodes() ([]*node, error) {
	var buf []*node
	for {
		tok, err := p.next()
		if err != nil {
			return nil, err
		}
		if tok == nil {
			break
		}
		switch tok.typ {
		case tokenData:
			data := tok.value
			next, err := p.peekToken()
			if err != nil {
				return nil, err
			}
			if p.dropLeadingWhitespace {
				data = js.TrimJS(data, true, false)
				p.dropLeadingWhitespace = false
			}
			if next != nil && p.trimsBefore(next) {
				data = js.TrimJS(data, false, true)
			}
			out := newList("Output", tok.lineno, tok.colno,
				[]*node{newLiteral("TemplateData", tok.lineno, tok.colno, data)})
			buf = append(buf, out)
		case tokenBlockStart:
			p.dropLeadingWhitespace = false
			n, err := p.parseStatement()
			if err != nil {
				return nil, err
			}
			if n == nil {
				return buf, nil
			}
			buf = append(buf, n)
		case tokenVariableStart:
			e, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			p.dropLeadingWhitespace = false
			if err := p.advanceAfterVariableEnd(); err != nil {
				return nil, err
			}
			buf = append(buf, newList("Output", tok.lineno, tok.colno, []*node{e}))
		case tokenComment:
			u := js.U16(tok.value)
			i := len(u) - len(js.U16(p.tokens.tags.CommentEnd)) - 1
			p.dropLeadingWhitespace = i >= 0 && i < len(u) && u[i] == '-'
		default:
			return nil, p.errAt("Unexpected token at top-level: "+tok.typ, tok)
		}
	}
	return buf, nil
}

// trimsBefore reports whether the start tag next asks for the whitespace before it to be removed.
func (p *parser) trimsBefore(next *token) bool {
	u := js.U16(next.value)
	at := func(i int) bool { return i >= 0 && i < len(u) && u[i] == '-' }
	switch next.typ {
	case tokenBlockStart:
		return at(len(u) - 1)
	case tokenVariableStart:
		return at(len(js.U16(p.tokens.tags.VariableStart)))
	case tokenComment:
		return at(len(js.U16(p.tokens.tags.CommentStart)))
	}
	return false
}
