// Ported from the expression part of nunjucks/src/compiler.js of mozilla/nunjucks (BSD-2-Clause, see LICENSE).

package nunjucks

import (
	"math"
	"strings"

	"github.com/mozilla/nunjucks/js"
)

// nunjucks writes operators into the generated JavaScript without parentheses, so the operator nesting a
// template gets is the one JavaScript's precedence gives the emitted text, not the parse tree's. An expression
// is therefore first flattened into the operands and operators it emits, then regrouped with JavaScript
// precedence.

type item struct {
	op     string // operator; empty for an operand
	prefix bool
	fn     exprFn
}

var precedence = map[string]int{
	"*": 13, "/": 13, "%": 13,
	"+": 12, "-": 12,
	"<": 10, ">": 10, "<=": 10, ">=": 10,
	"==": 9, "!=": 9, "===": 9, "!==": 9,
	"&&": 5, "||": 4,
}

var binaryOps = map[string]string{
	"Or": "||", "And": "&&", "Add": "+", "Sub": "-", "Mul": "*", "Div": "/", "Mod": "%",
}

func (c *compiler) emit(n *node, frame *compileFrame) ([]item, error) {
	if n == nil {
		return nil, js.ReadPropertyError(js.Null, "typename")
	}
	if op, ok := binaryOps[n.typ]; ok {
		l, err := c.emit(n.left, frame)
		if err != nil {
			return nil, err
		}
		r, err := c.emit(n.right, frame)
		if err != nil {
			return nil, err
		}
		return append(append(l, item{op: op}), r...), nil
	}
	switch n.typ {
	case "Concat":
		l, err := c.emit(n.left, frame)
		if err != nil {
			return nil, err
		}
		r, err := c.emit(n.right, frame)
		if err != nil {
			return nil, err
		}
		empty := func(*act) (js.Value, error) { return "", nil }
		l = append(l, item{op: "+"}, item{fn: empty}, item{op: "+"})
		return append(l, r...), nil
	case "Compare":
		items, err := c.emit(n.expr, frame)
		if err != nil {
			return nil, err
		}
		for _, op := range n.ops {
			r, err := c.emit(op.expr, frame)
			if err != nil {
				return nil, err
			}
			items = append(append(items, item{op: op.opType}), r...)
		}
		return items, nil
	case "Not", "Neg", "Pos":
		op := map[string]string{"Not": "!", "Neg": "-", "Pos": "+"}[n.typ]
		t, err := c.emit(n.target, frame)
		if err != nil {
			return nil, err
		}
		if len(t) > 0 && t[0].prefix && t[0].op == op && op != "!" {
			c.syntaxError("Invalid left-hand side expression in prefix operation")
		}
		return append([]item{{op: op, prefix: true}}, t...), nil
	case "Is":
		test, err := c.compileIsCall(n, frame)
		if err != nil {
			return nil, err
		}
		yes := func(*act) (js.Value, error) { return true, nil }
		return []item{{fn: test}, {op: "==="}, {fn: yes}}, nil
	}
	fn, err := c.compileAtom(n, frame)
	if err != nil {
		return nil, err
	}
	return []item{{fn: fn}}, nil
}

// compileExpr compiles an expression to a closure.
func (c *compiler) compileExpr(n *node, frame *compileFrame) (exprFn, error) {
	items, err := c.emit(n, frame)
	if err != nil {
		return nil, err
	}
	b := &builder{items: items}
	return b.parse(0), nil
}

type builder struct {
	items []item
	pos   int
}

func (b *builder) parse(minPrec int) exprFn {
	left := b.unary()
	for b.pos < len(b.items) {
		it := b.items[b.pos]
		p := precedence[it.op]
		if it.op == "" || it.prefix || p < minPrec {
			break
		}
		b.pos++
		right := b.parse(p + 1)
		left = binary(it.op, left, right)
	}
	return left
}

func (b *builder) unary() exprFn {
	it := b.items[b.pos]
	b.pos++
	if !it.prefix {
		return it.fn
	}
	operand := b.unary()
	switch it.op {
	case "!":
		return func(a *act) (js.Value, error) {
			v, err := operand(a)
			if err != nil {
				return nil, err
			}
			return !js.Truthy(v), nil
		}
	case "-":
		return func(a *act) (js.Value, error) {
			v, err := operand(a)
			if err != nil {
				return nil, err
			}
			n, err := js.ToNumber(v)
			if err != nil {
				return nil, err
			}
			return -n, nil
		}
	}
	return func(a *act) (js.Value, error) {
		v, err := operand(a)
		if err != nil {
			return nil, err
		}
		return js.ToNumber(v)
	}
}

func binary(op string, left, right exprFn) exprFn {
	switch op {
	case "&&", "||":
		return func(a *act) (js.Value, error) {
			l, err := left(a)
			if err != nil {
				return nil, err
			}
			if js.Truthy(l) == (op == "||") {
				return l, nil
			}
			return right(a)
		}
	}
	return func(a *act) (js.Value, error) {
		l, err := left(a)
		if err != nil {
			return nil, err
		}
		r, err := right(a)
		if err != nil {
			return nil, err
		}
		switch op {
		case "+":
			return js.Add(l, r)
		case "-", "*", "/", "%":
			return js.Arith(op, l, r)
		case "==", "!=":
			eq, err := js.LooseEquals(l, r)
			return eq == (op == "=="), err
		case "===":
			return js.StrictEquals(l, r), nil
		case "!==":
			return !js.StrictEquals(l, r), nil
		}
		return js.Compare(op, l, r)
	}
}

func (c *compiler) compileAtom(n *node, frame *compileFrame) (exprFn, error) {
	switch n.typ {
	case "Literal", "TemplateData":
		return compileLiteral(n.lit), nil
	case "Symbol":
		return c.compileSymbol(n, frame), nil
	case "Group":
		return c.compileGroup(n, frame)
	case "Array":
		return c.compileArray(n, frame)
	case "Dict", "KeywordArgs":
		return c.compileDict(n, frame)
	case "LookupVal":
		return c.compileLookupVal(n, frame)
	case "FunCall":
		return c.compileFunCall(n, frame)
	case "Filter":
		return c.compileFilter(n, frame)
	case "InlineIf":
		return c.compileInlineIf(n, frame)
	case "In":
		l, err := c.compileExpr(n.left, frame)
		if err != nil {
			return nil, err
		}
		r, err := c.compileExpr(n.right, frame)
		if err != nil {
			return nil, err
		}
		return func(a *act) (js.Value, error) {
			lv, err := l(a)
			if err != nil {
				return nil, err
			}
			rv, err := r(a)
			if err != nil {
				return nil, err
			}
			return inOperator(lv, rv)
		}, nil
	case "FloorDiv", "Pow":
		l, err := c.compileExpr(n.left, frame)
		if err != nil {
			return nil, err
		}
		r, err := c.compileExpr(n.right, frame)
		if err != nil {
			return nil, err
		}
		floor := n.typ == "FloorDiv"
		return func(a *act) (js.Value, error) {
			lv, err := l(a)
			if err != nil {
				return nil, err
			}
			rv, err := r(a)
			if err != nil {
				return nil, err
			}
			if floor {
				q, err := js.Arith("/", lv, rv)
				if err != nil {
					return nil, err
				}
				return math.Floor(q.(float64)), nil
			}
			x, err := js.ToNumber(lv)
			if err != nil {
				return nil, err
			}
			y, err := js.ToNumber(rv)
			if err != nil {
				return nil, err
			}
			return js.Pow(x, y), nil
		}, nil
	case "Caller":
		return c.compileCaller(n, frame)
	case "Capture":
		return c.compileCapture(n, frame)
	case "NodeList":
		if len(n.children) == 1 {
			return c.compileExpr(n.children[0], frame)
		}
		c.syntaxError("Unexpected identifier")
		return func(*act) (js.Value, error) { return js.Undefined, nil }, nil
	}
	return nil, c.fail("compile: Cannot compile node: "+n.typ, n.lineno, n.colno)
}

func compileLiteral(v js.Value) exprFn {
	if re, ok := v.(*js.RegExp); ok {
		return func(*act) (js.Value, error) { return js.NewRegExp(re.Source, re.Flags) }
	}
	return func(*act) (js.Value, error) { return v, nil }
}

func (c *compiler) compileSymbol(n *node, frame *compileFrame) exprFn {
	name := n.symbolName()
	if s := frame.lookup(name); s != nil {
		id := s.name
		return func(a *act) (js.Value, error) {
			if a.find(id) == nil {
				return nil, &js.Error{Name: "ReferenceError", Message: id + " is not defined"}
			}
			return a.value(id), nil
		}
	}
	compat := c.env.jinjaCompat
	return func(a *act) (js.Value, error) {
		v := a.frame().lookup(name)
		if js.IsUndefined(v) {
			v = a.context().lookup(name)
		}
		if compat && js.IsUndefined(v) {
			switch name {
			case "True":
				return true, nil
			case "False":
				return false, nil
			case "None":
				return js.Null, nil
			}
		}
		return v, nil
	}
}

// compileElements compiles the elements of an array, group or argument list. A slice (Jinja compatibility)
// emits its three parts as three elements.
func (c *compiler) compileElements(children []*node, frame *compileFrame) ([]exprFn, error) {
	var out []exprFn
	for _, ch := range children {
		if ch.typ == "Slice" {
			for _, part := range []*node{ch.start, ch.stop, ch.step} {
				e, err := c.compileExpr(part, frame)
				if err != nil {
					return nil, err
				}
				out = append(out, e)
			}
			continue
		}
		e, err := c.compileExpr(ch, frame)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func evalAll(a *act, fns []exprFn) ([]js.Value, error) {
	out := make([]js.Value, 0, len(fns))
	for _, f := range fns {
		v, err := f(a)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (c *compiler) compileGroup(n *node, frame *compileFrame) (exprFn, error) {
	if len(n.children) == 0 {
		c.syntaxError("Unexpected token ')'")
	}
	fns, err := c.compileElements(n.children, frame)
	if err != nil {
		return nil, err
	}
	return func(a *act) (js.Value, error) {
		vals, err := evalAll(a, fns)
		if err != nil {
			return nil, err
		}
		if len(vals) == 0 {
			return js.Undefined, nil
		}
		return vals[len(vals)-1], nil
	}, nil
}

func (c *compiler) compileArray(n *node, frame *compileFrame) (exprFn, error) {
	fns, err := c.compileElements(n.children, frame)
	if err != nil {
		return nil, err
	}
	return func(a *act) (js.Value, error) {
		vals, err := evalAll(a, fns)
		if err != nil {
			return nil, err
		}
		return &js.Array{Items: vals}, nil
	}, nil
}

func (c *compiler) compileDict(n *node, frame *compileFrame) (exprFn, error) {
	type pair struct {
		key string
		val exprFn
	}
	var pairs []pair
	for _, p := range n.children {
		key := p.key
		var k string
		switch {
		case key.typ == "Symbol":
			k = key.symbolName()
		case key.typ == "Literal":
			s, ok := key.lit.(string)
			if !ok {
				return nil, c.fail("compilePair: Dict keys must be strings or names", key.lineno, key.colno)
			}
			k = s
		default:
			return nil, c.fail("compilePair: Dict keys must be strings or names", key.lineno, key.colno)
		}
		v, err := c.compileExpr(p.value, frame)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, pair{k, v})
	}
	keywords := n.typ == "KeywordArgs"
	return func(a *act) (js.Value, error) {
		obj := js.NewObject()
		for _, p := range pairs {
			v, err := p.val(a)
			if err != nil {
				return nil, err
			}
			obj.Set(p.key, v)
		}
		if keywords {
			return makeKeywordArgs(obj), nil
		}
		return obj, nil
	}, nil
}

func (c *compiler) compileLookupVal(n *node, frame *compileFrame) (exprFn, error) {
	target, err := c.compileExpr(n.target, frame)
	if err != nil {
		return nil, err
	}
	if n.val == nil {
		return nil, js.ReadPropertyError(js.Null, "typename")
	}
	compat := c.env.jinjaCompat
	if n.val.typ == "Slice" {
		parts, err := c.compileElements([]*node{n.val}, frame)
		if err != nil {
			return nil, err
		}
		return func(a *act) (js.Value, error) {
			obj, err := target(a)
			if err != nil {
				return nil, err
			}
			vals, err := evalAll(a, parts)
			if err != nil {
				return nil, err
			}
			if compat {
				return sliceLookup(obj, vals[0], vals[1], vals[2])
			}
			return memberLookup(obj, vals[0])
		}, nil
	}
	val, err := c.compileExpr(n.val, frame)
	if err != nil {
		return nil, err
	}
	return func(a *act) (js.Value, error) {
		obj, err := target(a)
		if err != nil {
			return nil, err
		}
		key, err := val(a)
		if err != nil {
			return nil, err
		}
		if compat {
			return compatMemberLookup(obj, key)
		}
		return memberLookup(obj, key)
	}, nil
}

// nodeName is _getNodeName: the description of a callee used in call errors.
func (c *compiler) nodeName(n *node) (string, error) {
	switch n.typ {
	case "Symbol":
		return n.symbolName(), nil
	case "FunCall":
		inner, err := c.nodeName(n.name)
		return "the return value of (" + inner + ")", err
	case "LookupVal":
		t, err := c.nodeName(n.target)
		if err != nil {
			return "", err
		}
		if n.val == nil {
			return "", js.ReadPropertyError(js.Null, "typename")
		}
		v, err := c.nodeName(n.val)
		return t + `["` + v + `"]`, err
	case "Literal":
		if js.IsNull(n.lit) {
			return "", js.ReadPropertyError(js.Null, "toString")
		}
		return js.ToString(n.lit), nil
	}
	return "--expression--", nil
}

func (c *compiler) compileFunCall(n *node, frame *compileFrame) (exprFn, error) {
	callee, err := c.compileExpr(n.name, frame)
	if err != nil {
		return nil, err
	}
	name, err := c.nodeName(n.name)
	if err != nil {
		return nil, err
	}
	name, ok := jsStringLiteral(strings.ReplaceAll(name, `"`, `\"`))
	if !ok {
		c.syntaxError("Invalid or unexpected token")
	}
	args, err := c.compileElements(n.args.children, frame)
	if err != nil {
		return nil, err
	}
	lineno, colno := n.lineno, n.colno
	return func(a *act) (js.Value, error) {
		a.setPos(lineno, colno)
		fn, err := callee(a)
		if err != nil {
			return nil, err
		}
		vals, err := evalAll(a, args)
		if err != nil {
			return nil, err
		}
		return callWrap(fn, name, vals)
	}, nil
}

func (c *compiler) compileFilter(n *node, frame *compileFrame) (exprFn, error) {
	if n.name.typ != "Symbol" {
		return nil, c.fail("assertType: invalid type: "+n.name.typ, n.name.lineno, n.name.colno)
	}
	name := n.name.symbolName()
	args, err := c.compileElements(n.args.children, frame)
	if err != nil {
		return nil, err
	}
	return func(a *act) (js.Value, error) {
		ctx := a.context()
		f, err := ctx.env.getFilter(name)
		if err != nil {
			return nil, err
		}
		vals, err := evalAll(a, args)
		if err != nil {
			return nil, err
		}
		return f(ctx, vals)
	}, nil
}

func (c *compiler) compileIsCall(n *node, frame *compileFrame) (exprFn, error) {
	right := n.right
	var name string
	var argNodes []*node
	if (right.typ == "FunCall" || right.typ == "Filter") && right.name != nil {
		name = right.name.symbolName()
		argNodes = right.args.children
	} else {
		name = js.ToString(right.lit)
		if right.lit == nil {
			name = "undefined"
		}
	}
	left, err := c.compileExpr(n.left, frame)
	if err != nil {
		return nil, err
	}
	if len(argNodes) > 1 {
		c.syntaxError("Unexpected identifier")
	}
	args, err := c.compileElements(argNodes, frame)
	if err != nil {
		return nil, err
	}
	return func(a *act) (js.Value, error) {
		ctx := a.context()
		t, err := ctx.env.getTest(name)
		if err != nil {
			return nil, err
		}
		lv, err := left(a)
		if err != nil {
			return nil, err
		}
		vals, err := evalAll(a, args)
		if err != nil {
			return nil, err
		}
		return t(ctx, append([]js.Value{lv}, vals...))
	}, nil
}

func (c *compiler) compileInlineIf(n *node, frame *compileFrame) (exprFn, error) {
	cond, err := c.compileExpr(n.cond, frame)
	if err != nil {
		return nil, err
	}
	body, err := c.compileExpr(n.body, frame)
	if err != nil {
		return nil, err
	}
	var els exprFn
	if n.else_ != nil {
		if els, err = c.compileExpr(n.else_, frame); err != nil {
			return nil, err
		}
	}
	return func(a *act) (js.Value, error) {
		v, err := cond(a)
		if err != nil {
			return nil, err
		}
		if js.Truthy(v) {
			return body(a)
		}
		if els == nil {
			return "", nil
		}
		return els(a)
	}, nil
}

// jsStringLiteral is the value of the JavaScript string literal with body s, as the generated code embeds a
// callee's description; ok is false when the literal would not parse.
func jsStringLiteral(s string) (string, bool) {
	u := js.U16(s)
	var out []uint16
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c == '\n' || c == '\r' || c == '"' {
			return "", false
		}
		if c != '\\' {
			out = append(out, c)
			continue
		}
		i++
		if i >= len(u) {
			return "", false
		}
		e := u[i]
		simple := map[uint16]uint16{'n': '\n', 't': '\t', 'r': '\r', 'b': '\b', 'f': '\f', 'v': '\v'}
		switch {
		case simple[e] != 0:
			out = append(out, simple[e])
		case e == '0' && (i+1 >= len(u) || u[i+1] < '0' || u[i+1] > '9'):
			out = append(out, 0)
		case e == 'x' || e == 'u':
			n := 2
			if e == 'u' {
				n = 4
			}
			if i+n >= len(u) {
				return "", false
			}
			v := 0
			for _, h := range u[i+1 : i+1+n] {
				d := strings.IndexRune("0123456789abcdef", rune(h|0x20))
				if d < 0 {
					return "", false
				}
				v = v*16 + d
			}
			out = append(out, uint16(v))
			i += n
		case e == '\r' || e == '\n' || e == 0x2028 || e == 0x2029:
			if e == '\r' && i+1 < len(u) && u[i+1] == '\n' {
				i++
			}
		case e >= '0' && e <= '7':
			v := int(e - '0')
			for k := 0; k < 2 && i+1 < len(u) && u[i+1] >= '0' && u[i+1] <= '7' && v*8+int(u[i+1]-'0') <= 0377; k++ {
				i++
				v = v*8 + int(u[i]-'0')
			}
			out = append(out, uint16(v))
		default:
			out = append(out, e)
		}
	}
	return js.FromU16(out), true
}
