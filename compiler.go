// Ported from nunjucks/src/compiler.js of mozilla/nunjucks (BSD-2-Clause, see LICENSE).
//
// nunjucks compiles a template to JavaScript source and evaluates it. This port compiles the same tree to Go
// closures that keep the observable semantics of that generated code: the JavaScript function scopes of the
// generated code (the root and block functions, macros, captures), the variables they close over (frame,
// lineno, colno, the output buffer), the continuation-passing structure of include, import, extends, block and
// super, and JavaScript operator precedence over the emitted expression text.

package nunjucks

import (
	"strconv"
	"strings"

	"github.com/mozilla/nunjucks/js"
)

// callback is the (err, res) callback of the generated code. Returning an error is a JavaScript throw.
type callback func(err *js.Error, res js.Value) error

type exprFn func(a *act) (js.Value, error)

// stmtFn runs a statement; k is the rest of the enclosing scope, which continuation-passing statements call
// from inside their callbacks.
type stmtFn func(a *act, k func() error) error

// slot is a variable of the generated code (a temporary, a macro argument or a macro).
type slot struct{ name string }

// fnScope is a function of the generated code; declared lists its var declarations.
type fnScope struct {
	declared []string
}

func (s *fnScope) declare(name string) { s.declared = append(s.declared, name) }

// act is the activation of a generated function: its variables, and the enclosing function it closes over.
type act struct {
	parent *act
	vars   map[string]any
}

func newAct(parent *act, scope *fnScope) *act {
	a := &act{parent: parent, vars: map[string]any{}}
	if scope != nil {
		for _, n := range scope.declared {
			a.vars[n] = js.Undefined
		}
	}
	return a
}

func (a *act) find(name string) *act {
	for x := a; x != nil; x = x.parent {
		if _, ok := x.vars[name]; ok {
			return x
		}
	}
	return nil
}

func (a *act) get(name string) any {
	if x := a.find(name); x != nil {
		return x.vars[name]
	}
	return js.Undefined
}

// put assigns a variable where it is declared; an undeclared name is created in this activation.
func (a *act) put(name string, v any) {
	if x := a.find(name); x != nil {
		x.vars[name] = v
		return
	}
	a.vars[name] = v
}

func (a *act) value(name string) js.Value {
	if v, ok := a.get(name).(js.Value); ok && v != nil {
		return v
	}
	return js.Undefined
}

func (a *act) frame() *Frame            { f, _ := a.get("frame").(*Frame); return f }
func (a *act) setFrame(f *Frame)        { a.put("frame", f) }
func (a *act) context() *Context        { c, _ := a.get("context").(*Context); return c }
func (a *act) cb() callback             { c, _ := a.get("cb").(callback); return c }
func (a *act) setPos(lineno, colno int) { a.put("lineno", lineno); a.put("colno", colno) }
func (a *act) pos() (int, int) {
	l, _ := a.get("lineno").(int)
	c, _ := a.get("colno").(int)
	return l, c
}
func (a *act) parentTemplate() *Template { t, _ := a.get("parentTemplate").(*Template); return t }

func (a *act) appendTo(buffer string, v js.Value) error {
	r, err := js.Add(a.value(buffer), v)
	if err != nil {
		return err
	}
	a.put(buffer, r)
	return nil
}

// blockFunc is a compiled b_<name> block function.
type blockFunc struct {
	name string
	run  func(c *Context, frame *Frame, cb callback) error
}

type compiledTemplate struct {
	root   func(c *Context, frame *Frame, cb callback) error
	blocks []*blockFunc
}

type compiler struct {
	env              *Environment
	templateName     js.Value
	lastID           int
	buffer           string
	bufferStack      []string
	inBlock          bool
	throwOnUndefined bool
	fn               *fnScope
	blocks           map[string]*blockFunc
	syntaxErr        error
}

func (c *compiler) fail(msg string, lineno, colno int) error {
	return templateError(msg, lineno+1, colno+1)
}

func (c *compiler) tmpid() string {
	c.lastID++
	return "t_" + strconv.Itoa(c.lastID)
}

// unsupported marks generated code that the original rejects with a JavaScript SyntaxError when it evaluates
// the compiled template.
func (c *compiler) syntaxError(msg string) {
	if c.syntaxErr == nil {
		c.syntaxErr = js.NewSyntaxError("%s", msg)
	}
}

func (c *compiler) pushBuffer() string {
	id := c.tmpid()
	c.bufferStack = append(c.bufferStack, c.buffer)
	c.buffer = id
	c.fn.declare(id)
	return id
}

func (c *compiler) popBuffer() {
	c.buffer = c.bufferStack[len(c.bufferStack)-1]
	c.bufferStack = c.bufferStack[:len(c.bufferStack)-1]
}

func end() error { return nil }

// ---- statements ----

func (c *compiler) compileList(children []*node, frame *compileFrame) (stmtFn, error) {
	var steps []stmtFn
	for _, ch := range children {
		s, err := c.compileStmt(ch, frame)
		if err != nil {
			return nil, err
		}
		if s != nil {
			steps = append(steps, s)
		}
	}
	return chain(steps), nil
}

func chain(steps []stmtFn) stmtFn {
	return func(a *act, k func() error) error {
		var run func(i int) error
		run = func(i int) error {
			if i == len(steps) {
				return k()
			}
			return steps[i](a, func() error { return run(i + 1) })
		}
		return run(0)
	}
}

// simple turns a statement without continuations into a stmtFn.
func simple(f func(a *act) error) stmtFn {
	return func(a *act, k func() error) error {
		if err := f(a); err != nil {
			return err
		}
		return k()
	}
}

func (c *compiler) compileStmt(n *node, frame *compileFrame) (stmtFn, error) {
	if n == nil {
		return nil, js.ReadPropertyError(js.Null, "typename")
	}
	switch n.typ {
	case "NodeList":
		return c.compileList(n.children, frame)
	case "Output":
		return c.compileOutput(n, frame)
	case "If":
		return c.compileIf(n, frame)
	case "IfAsync":
		return c.compileIfAsync(n, frame)
	case "For":
		return c.compileFor(n, frame)
	case "AsyncEach", "AsyncAll":
		return c.compileAsyncLoop(n, frame, n.typ == "AsyncAll")
	case "Set":
		return c.compileSet(n, frame)
	case "Switch":
		return c.compileSwitch(n, frame)
	case "Macro":
		return c.compileMacro(n, frame)
	case "Import":
		return c.compileImport(n, frame)
	case "FromImport":
		return c.compileFromImport(n, frame)
	case "Block":
		return c.compileBlock(n)
	case "Super":
		return c.compileSuper(n, frame)
	case "Extends":
		return c.compileExtends(n, frame)
	case "Include":
		return c.compileInclude(n, frame)
	}
	// Any other node is an expression statement: its value is computed and dropped.
	e, err := c.compileExpr(n, frame)
	if err != nil {
		return nil, err
	}
	return simple(func(a *act) error {
		_, err := e(a)
		return err
	}), nil
}

func (c *compiler) compileOutput(n *node, frame *compileFrame) (stmtFn, error) {
	var steps []func(a *act) error
	buffer := c.buffer
	for _, child := range n.children {
		if child.typ == "TemplateData" {
			s, _ := child.lit.(string)
			if s != "" {
				steps = append(steps, func(a *act) error { return a.appendTo(buffer, s) })
			}
			continue
		}
		e, err := c.compileExpr(child, frame)
		if err != nil {
			return nil, err
		}
		throwOnUndefined := c.throwOnUndefined
		lineno, colno := n.lineno, n.colno
		steps = append(steps, func(a *act) error {
			v, err := e(a)
			if err != nil {
				return err
			}
			if throwOnUndefined {
				if v, err = ensureDefined(v, lineno, colno); err != nil {
					return err
				}
			}
			v, err = suppressValue(v, a.context().env.opts.Autoescape)
			if err != nil {
				return err
			}
			return a.appendTo(buffer, v)
		})
	}
	return simple(func(a *act) error {
		for _, s := range steps {
			if err := s(a); err != nil {
				return err
			}
		}
		return nil
	}), nil
}

func (c *compiler) compileIf(n *node, frame *compileFrame) (stmtFn, error) {
	cond, err := c.compileExpr(n.cond, frame)
	if err != nil {
		return nil, err
	}
	body, err := c.compileStmt(n.body, frame)
	if err != nil {
		return nil, err
	}
	var els stmtFn
	if n.else_ != nil {
		if els, err = c.compileStmt(n.else_, frame); err != nil {
			return nil, err
		}
	}
	return simple(func(a *act) error {
		v, err := cond(a)
		if err != nil {
			return err
		}
		if js.Truthy(v) {
			return runOpt(body, a)
		}
		if els != nil {
			return els(a, end)
		}
		return nil
	}), nil
}

func runOpt(s stmtFn, a *act) error {
	if s == nil {
		return nil
	}
	return s(a, end)
}

func (c *compiler) compileIfAsync(n *node, frame *compileFrame) (stmtFn, error) {
	outer := c.fn
	scope := &fnScope{}
	c.fn = scope
	cond, err := c.compileExpr(n.cond, frame)
	if err != nil {
		return nil, err
	}
	body, err := c.compileStmt(n.body, frame)
	if err != nil {
		return nil, err
	}
	var els stmtFn
	if n.else_ != nil {
		if els, err = c.compileStmt(n.else_, frame); err != nil {
			return nil, err
		}
	}
	c.fn = outer
	return func(a *act, k func() error) error {
		iife := newAct(a, scope)
		iife.vars["cb"] = callback(func(err *js.Error, _ js.Value) error {
			if err != nil {
				return a.cb()(err, js.Undefined)
			}
			return k()
		})
		done := func() error { return iife.cb()(nil, js.Undefined) }
		v, err := cond(iife)
		if err != nil {
			return err
		}
		if js.Truthy(v) {
			if body == nil {
				return done()
			}
			return body(iife, done)
		}
		if els != nil {
			return els(iife, done)
		}
		return done()
	}, nil
}

// loopBindings sets loop.index and the other loop variables for iteration i of length len.
func loopBindings(f *Frame, i, length js.Value) error {
	index, err := js.Add(i, 1.0)
	if err != nil {
		return err
	}
	revindex, err := js.Arith("-", length, i)
	if err != nil {
		return err
	}
	revindex0, err := js.Arith("-", revindex, 1.0)
	if err != nil {
		return err
	}
	last, err := js.Arith("-", length, 1.0)
	if err != nil {
		return err
	}
	bindings := []struct {
		name string
		val  js.Value
	}{
		{"index", index},
		{"index0", i},
		{"revindex", revindex},
		{"revindex0", revindex0},
		{"first", js.StrictEquals(i, 0.0)},
		{"last", js.StrictEquals(i, last)},
		{"length", length},
	}
	for _, b := range bindings {
		if err := f.setPath("loop."+b.name, b.val); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileFor(n *node, frame *compileFrame) (stmtFn, error) {
	c.tmpid()
	lenID := c.tmpid()
	c.tmpid()
	c.fn.declare(lenID)
	frame = frame.push()
	arrExpr, err := c.compileExpr(n.arr, frame)
	if err != nil {
		return nil, err
	}
	if n.name.typ == "Array" {
		var tids []string
		for _, child := range n.name.children {
			tid := c.tmpid()
			c.fn.declare(tid)
			tids = append(tids, tid)
			frame.set(child.symbolName(), &slot{tid})
		}
		arrayBody, err := c.compileStmt(n.body, frame)
		if err != nil {
			return nil, err
		}
		key, val := n.name.children[0], (*node)(nil)
		if len(n.name.children) > 1 {
			val = n.name.children[1]
		}
		k, v := c.tmpid(), c.tmpid()
		c.fn.declare(k)
		c.fn.declare(v)
		frame.set(key.symbolName(), &slot{k})
		valName := "undefined"
		if val != nil {
			valName = val.symbolName()
		}
		frame.set(valName, &slot{v})
		objBody, err := c.compileStmt(n.body, frame)
		if err != nil {
			return nil, err
		}
		var els stmtFn
		if n.else_ != nil {
			if els, err = c.compileStmt(n.else_, frame); err != nil {
				return nil, err
			}
		}
		keyName := key.symbolName()
		return simple(func(a *act) error {
			a.setFrame(a.frame().push(false))
			arr, err := arrExpr(a)
			if err != nil {
				return err
			}
			if js.Truthy(arr) {
				arr = fromIterator(arr)
				if items, ok := arr.(*js.Array); ok {
					length := float64(len(items.Items))
					a.put(lenID, length)
					for i := 0; i < len(items.Items); i++ {
						item := items.Items[i]
						for u, tid := range tids {
							el, err := js.Get(item, float64(u))
							if err != nil {
								return err
							}
							a.put(tid, el)
							a.frame().set("[object Object]", el, false)
						}
						if err := loopBindings(a.frame(), float64(i), length); err != nil {
							return err
						}
						if err := runOpt(arrayBody, a); err != nil {
							return err
						}
					}
				} else {
					keys := keysOf(arr)
					a.put(lenID, float64(len(keys)))
					i := -1
					for _, kk := range forInKeys(arr) {
						i++
						a.put(k, kk)
						vv, err := js.Get(arr, kk)
						if err != nil {
							return err
						}
						a.put(v, vv)
						a.frame().set(keyName, kk, false)
						a.frame().set(valName, vv, false)
						if err := loopBindings(a.frame(), float64(i), float64(len(keys))); err != nil {
							return err
						}
						if err := runOpt(objBody, a); err != nil {
							return err
						}
					}
				}
			}
			if els != nil && !js.Truthy(a.value(lenID)) {
				if err := els(a, end); err != nil {
					return err
				}
			}
			a.setFrame(a.frame().pop())
			return nil
		}), nil
	}
	v := c.tmpid()
	c.fn.declare(v)
	name := n.name.symbolName()
	frame.set(name, &slot{v})
	body, err := c.compileStmt(n.body, frame)
	if err != nil {
		return nil, err
	}
	var els stmtFn
	if n.else_ != nil {
		if els, err = c.compileStmt(n.else_, frame); err != nil {
			return nil, err
		}
	}
	return simple(func(a *act) error {
		a.setFrame(a.frame().push(false))
		arr, err := arrExpr(a)
		if err != nil {
			return err
		}
		if js.Truthy(arr) {
			arr = fromIterator(arr)
			length := js.GetKey(arr, "length")
			a.put(lenID, length)
			for i := 0; ; i++ {
				ok, _ := js.Compare("<", float64(i), js.GetKey(arr, "length"))
				if !ok {
					break
				}
				item, err := js.Get(arr, float64(i))
				if err != nil {
					return err
				}
				a.put(v, item)
				a.frame().set(name, item, false)
				if err := loopBindings(a.frame(), float64(i), length); err != nil {
					return err
				}
				if err := runOpt(body, a); err != nil {
					return err
				}
			}
		}
		if els != nil && !js.Truthy(a.value(lenID)) {
			if err := els(a, end); err != nil {
				return err
			}
		}
		a.setFrame(a.frame().pop())
		return nil
	}), nil
}

// forInKeys is the key order of for (k in obj): own enumerable keys (the prototypes add none).
func forInKeys(v js.Value) []string {
	if js.IsNullish(v) {
		return nil
	}
	if _, ok := v.(float64); ok {
		return nil
	}
	if _, ok := v.(bool); ok {
		return nil
	}
	return js.OwnKeys(v)
}

func (c *compiler) compileAsyncLoop(n *node, frame *compileFrame, parallel bool) (stmtFn, error) {
	iID, lenID, arrID := c.tmpid(), c.tmpid(), c.tmpid()
	c.fn.declare(arrID)
	frame = frame.push()
	arrExpr, err := c.compileExpr(n.arr, frame)
	if err != nil {
		return nil, err
	}
	var names []string
	if n.name.typ == "Array" {
		for _, ch := range n.name.children {
			names = append(names, ch.symbolName())
		}
	} else {
		names = []string{n.name.symbolName()}
	}
	outer := c.fn
	iterScope := &fnScope{}
	c.fn = iterScope
	for _, nm := range names {
		frame.set(nm, &slot{nm})
	}
	var buf string
	if parallel {
		buf = c.pushBuffer()
	}
	body, err := c.compileStmt(n.body, frame)
	if err != nil {
		return nil, err
	}
	if parallel {
		c.popBuffer()
	}
	c.fn = outer
	outID := c.tmpid()
	buffer := c.buffer
	var els stmtFn
	if n.else_ != nil {
		if els, err = c.compileStmt(n.else_, frame); err != nil {
			return nil, err
		}
	}
	dimen := len(names)
	return func(a *act, k func() error) error {
		a.setFrame(a.frame().push(false))
		arrV, err := arrExpr(a)
		if err != nil {
			return err
		}
		arr := fromIterator(arrV)
		a.put(arrID, arr)
		cont := callback(func(e *js.Error, out js.Value) error {
			if e != nil {
				return a.cb()(e, js.Undefined)
			}
			a.vars[outID] = out
			if parallel {
				if err := a.appendTo(buffer, out); err != nil {
					return err
				}
			}
			if els != nil {
				l, err := js.Get(arr, "length")
				if err != nil {
					return err
				}
				if !js.Truthy(l) {
					if err := els(a, end); err != nil {
						return err
					}
				}
			}
			a.setFrame(a.frame().pop())
			return k()
		})
		// iterate calls the loop body function with the positional arguments the runtime passes it.
		iterate := func(args []js.Value) error {
			it := newAct(a, iterScope)
			if parallel {
				it.vars[buf] = ""
			}
			for j, nm := range names {
				it.vars[nm] = js.Arg(args, j)
			}
			i, length, next := js.Arg(args, dimen), js.Arg(args, dimen+1), js.Arg(args, dimen+2)
			it.vars[iID], it.vars[lenID] = i, length
			for _, nm := range names {
				a.frame().set(nm, it.value(nm), false)
			}
			if err := loopBindings(a.frame(), i, length); err != nil {
				return err
			}
			return body(it, func() error {
				nextArgs := []js.Value{i}
				if parallel {
					nextArgs = append(nextArgs, it.value(buf))
				}
				_, err := js.Call(next, js.Undefined, nextArgs, "next")
				return err
			})
		}
		if parallel {
			return asyncAll(arr, dimen, iterate, cont)
		}
		return asyncEach(arr, dimen, iterate, cont)
	}, nil
}

// asyncEach is runtime.asyncEach for synchronous templates.
func asyncEach(arr js.Value, dimen int, iter func([]js.Value) error, cb callback) error {
	if items, ok := arr.(*js.Array); ok {
		length := float64(len(items.Items))
		i := -1
		var next *js.Function
		next = js.NewFunction("next", func(js.Value, []js.Value) (js.Value, error) {
			i++
			if i < len(items.Items) {
				args, err := spreadItem(items.Items[i], dimen, float64(i), length, next)
				if err != nil {
					return nil, err
				}
				return js.Undefined, iter(args)
			}
			return js.Undefined, cb(nil, js.Undefined)
		})
		_, err := next.Call(js.Undefined, nil)
		return err
	}
	keys := keysOf(orEmpty(arr))
	i := -1
	var next *js.Function
	next = js.NewFunction("next", func(js.Value, []js.Value) (js.Value, error) {
		i++
		if i < len(keys) {
			v := js.GetKey(arr, keys[i])
			return js.Undefined, iter([]js.Value{keys[i], v, float64(i), float64(len(keys)), next})
		}
		return js.Undefined, cb(nil, js.Undefined)
	})
	_, err := next.Call(js.Undefined, nil)
	return err
}

func orEmpty(v js.Value) js.Value {
	if !js.Truthy(v) {
		return js.NewObject()
	}
	return v
}

// spreadItem builds the iterator arguments for one array element: up to three destructured values, or the
// element itself extended with the position (which mutates it, as in the original).
func spreadItem(item js.Value, dimen int, i, length float64, next js.Value) ([]js.Value, error) {
	if dimen == 1 {
		return []js.Value{item, i, length, next}, nil
	}
	if dimen <= 3 {
		var out []js.Value
		for k := 0; k < dimen; k++ {
			v, err := js.Get(item, float64(k))
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return append(out, i, length, next), nil
	}
	a, ok := item.(*js.Array)
	if !ok {
		return nil, js.NotFunctionError("item.push")
	}
	a.Items = append(a.Items, i, length, next)
	return append([]js.Value(nil), a.Items...), nil
}

// asyncAll is runtime.asyncAll for synchronous templates.
func asyncAll(arr js.Value, dimen int, fn func([]js.Value) error, cb callback) error {
	finished := 0
	var outputs []js.Value
	length := 0
	done := js.NewFunction("done", func(_ js.Value, args []js.Value) (js.Value, error) {
		finished++
		idx := int(js.Num(js.Arg(args, 0)))
		if idx >= 0 && idx < len(outputs) {
			outputs[idx] = js.Arg(args, 1)
		}
		if finished == length {
			s, err := js.Join(&js.Array{Items: outputs}, "")
			if err != nil {
				return nil, err
			}
			return js.Undefined, cb(nil, s)
		}
		return js.Undefined, nil
	})
	if items, ok := arr.(*js.Array); ok {
		length = len(items.Items)
		outputs = make([]js.Value, length)
		for i := range outputs {
			outputs[i] = js.Undefined
		}
		if length == 0 {
			return cb(nil, "")
		}
		for i := 0; i < len(items.Items); i++ {
			args, err := spreadItem(items.Items[i], dimen, float64(i), float64(length), done)
			if err != nil {
				return err
			}
			if err := fn(args); err != nil {
				return err
			}
		}
		return nil
	}
	keys := keysOf(orEmpty(arr))
	length = len(keys)
	outputs = make([]js.Value, length)
	for i := range outputs {
		outputs[i] = js.Undefined
	}
	if length == 0 {
		return cb(nil, "")
	}
	for i, k := range keys {
		if err := fn([]js.Value{k, js.GetKey(arr, k), float64(i), float64(length), done}); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileSet(n *node, frame *compileFrame) (stmtFn, error) {
	type target struct {
		name string
		id   string
	}
	var targets []target
	for _, t := range n.targets {
		name := t.symbolName()
		id := ""
		if s := frame.lookup(name); s != nil {
			id = s.name
		} else {
			id = c.tmpid()
			c.fn.declare(id)
		}
		targets = append(targets, target{name, id})
	}
	var value exprFn
	var err error
	if n.value != nil {
		value, err = c.compileExpr(n.value, frame)
	} else if n.setBody != nil {
		value, err = c.compileCapture(n.setBody, frame)
	} else {
		err = js.ReadPropertyError(js.Null, "typename")
	}
	if err != nil {
		return nil, err
	}
	for _, t := range n.targets {
		switch name := t.lit.(type) {
		case string:
		case nil:
			return nil, js.ReadPropertyError(js.Undefined, "charAt")
		default:
			if js.IsNull(name) {
				return nil, js.ReadPropertyError(js.Null, "charAt")
			}
			return nil, js.NotFunctionError("name.charAt")
		}
	}
	return simple(func(a *act) error {
		v, err := value(a)
		if err != nil {
			return err
		}
		for _, t := range targets {
			a.put(t.id, v)
		}
		for _, t := range targets {
			f := a.frame()
			f.set(t.name, v, true)
			if f.topLevel {
				a.context().setVariable(t.name, v)
			}
			if !strings.HasPrefix(t.name, "_") && a.frame().topLevel {
				a.context().addExport(t.name)
			}
		}
		return nil
	}), nil
}

func (c *compiler) compileSwitch(n *node, frame *compileFrame) (stmtFn, error) {
	expr, err := c.compileExpr(n.expr, frame)
	if err != nil {
		return nil, err
	}
	type caseFn struct {
		cond exprFn
		body stmtFn
		brk  bool
	}
	var cases []caseFn
	for _, cs := range n.cases {
		cond, err := c.compileExpr(cs.cond, frame)
		if err != nil {
			return nil, err
		}
		body, err := c.compileStmt(cs.body, frame)
		if err != nil {
			return nil, err
		}
		if hasContinuation(cs.body) {
			c.syntaxError("Unexpected token 'case'")
		}
		cases = append(cases, caseFn{cond: cond, body: body, brk: len(cs.body.children) > 0})
	}
	var def stmtFn
	if n.def != nil {
		if def, err = c.compileStmt(n.def, frame); err != nil {
			return nil, err
		}
		if hasContinuation(n.def) {
			c.syntaxError("Unexpected token ')'")
		}
	}
	return simple(func(a *act) error {
		v, err := expr(a)
		if err != nil {
			return err
		}
		start := -1
		for i, cs := range cases {
			cv, err := cs.cond(a)
			if err != nil {
				return err
			}
			if js.StrictEquals(v, cv) {
				start = i
				break
			}
		}
		if start < 0 {
			if def != nil {
				return def(a, end)
			}
			return nil
		}
		for _, cs := range cases[start:] {
			if err := runOpt(cs.body, a); err != nil {
				return err
			}
			if cs.brk {
				return nil
			}
		}
		if def != nil {
			return def(a, end)
		}
		return nil
	}), nil
}

// hasContinuation reports whether a statement list directly holds a statement that turns the rest of its scope
// into a callback.
func hasContinuation(n *node) bool {
	if n == nil {
		return false
	}
	for _, ch := range n.children {
		switch ch.typ {
		case "Include", "Import", "FromImport", "Extends", "Block", "Super", "IfAsync", "AsyncEach", "AsyncAll":
			return true
		case "NodeList":
			if hasContinuation(ch) {
				return true
			}
		}
	}
	return false
}

// compileMacroFn compiles a macro or caller body and returns a constructor that creates the macro function in
// an activation.
func (c *compiler) compileMacroFn(n *node, frame *compileFrame, keepFrame bool) (func(a *act) js.Value, error) {
	var args []*node
	var kwargs *node
	for i, arg := range n.args.children {
		if i == len(n.args.children)-1 && (arg.typ == "Dict" || arg.typ == "KeywordArgs") {
			kwargs = arg
			continue
		}
		if arg.typ != "Symbol" {
			return nil, c.fail("assertType: invalid type: "+arg.typ, arg.lineno, arg.colno)
		}
		args = append(args, arg)
	}
	var argNames, kwargNames []string
	for _, arg := range args {
		argNames = append(argNames, arg.symbolName())
	}
	if kwargs != nil {
		for _, pair := range kwargs.children {
			kwargNames = append(kwargNames, pair.key.symbolName())
		}
	}
	var currFrame *compileFrame
	if keepFrame {
		currFrame = frame.push()
	} else {
		currFrame = &compileFrame{names: map[string]*slot{}}
	}
	outer, outerBuffer, outerStack := c.fn, c.buffer, c.bufferStack
	scope := &fnScope{}
	c.fn = scope
	for _, name := range argNames {
		scope.declare("l_" + name)
		currFrame.set(name, &slot{"l_" + name})
	}
	scope.declare("kwargs")
	scope.declare("callerFrame")
	defaults := make([]exprFn, len(kwargNames))
	if kwargs != nil {
		for i, pair := range kwargs.children {
			d, err := c.compileExpr(pair.value, currFrame)
			if err != nil {
				return nil, err
			}
			defaults[i] = d
		}
	}
	bufferID := c.pushBuffer()
	body, err := c.compileStmt(n.body, currFrame)
	if err != nil {
		return nil, err
	}
	c.fn, c.buffer, c.bufferStack = outer, outerBuffer, outerStack
	return func(a *act) js.Value {
		return makeMacro(argNames, kwargNames, func(callArgs []js.Value) (js.Value, error) {
			m := newAct(a, scope)
			for i, name := range argNames {
				m.vars["l_"+name] = js.Arg(callArgs, i)
			}
			kw := js.Arg(callArgs, len(argNames))
			callerFrame := m.frame()
			m.vars["callerFrame"] = callerFrame
			if keepFrame {
				m.setFrame(callerFrame.push(true))
			} else {
				m.setFrame(newFrame(nil, false))
			}
			if !js.Truthy(kw) {
				kw = js.NewObject()
			}
			m.vars["kwargs"] = kw
			if js.HasOwn(kw, "caller") {
				m.frame().set("caller", js.GetKey(kw, "caller"), false)
			}
			for _, name := range argNames {
				m.frame().set(name, m.value("l_"+name), false)
			}
			for i, name := range kwargNames {
				var v js.Value
				if js.HasOwn(kw, name) {
					v = js.GetKey(kw, name)
				} else {
					d, err := defaults[i](m)
					if err != nil {
						return nil, err
					}
					v = d
				}
				m.frame().set(name, v, false)
			}
			m.vars[bufferID] = ""
			if err := runOpt(body, m); err != nil {
				return nil, err
			}
			if keepFrame {
				m.setFrame(m.frame().pop())
			} else {
				m.setFrame(callerFrame)
			}
			return &js.SafeString{Val: js.ToString(m.value(bufferID))}, nil
		})
	}, nil
}

func (c *compiler) compileMacro(n *node, frame *compileFrame) (stmtFn, error) {
	funcID := "macro_" + c.tmpid()
	makeFn, err := c.compileMacroFn(n, nil, false)
	if err != nil {
		return nil, err
	}
	c.fn.declare(funcID)
	name := n.name.symbolName()
	frame.set(name, &slot{funcID})
	hasParent := frame.parent != nil
	return simple(func(a *act) error {
		fn := makeFn(a)
		a.put(funcID, fn)
		if hasParent {
			a.frame().set(name, fn, false)
			return nil
		}
		if !strings.HasPrefix(name, "_") {
			a.context().addExport(name)
		}
		a.context().setVariable(name, fn)
		return nil
	}), nil
}

func (c *compiler) compileCaller(n *node, frame *compileFrame) (exprFn, error) {
	outer := c.fn
	scope := &fnScope{}
	c.fn = scope
	c.tmpid()
	makeFn, err := c.compileMacroFn(n, frame, true)
	c.fn = outer
	if err != nil {
		return nil, err
	}
	return func(a *act) (js.Value, error) {
		return makeFn(newAct(a, scope)), nil
	}, nil
}

// getTemplateStep is _compileGetTemplate: it evaluates the template expression and loads it.
func (c *compiler) getTemplateStep(n *node, frame *compileFrame, eager, ignoreMissing bool) (func(a *act, then func(t *Template) error) error, string, error) {
	id := c.tmpid()
	parentName := c.templateName
	expr, err := c.compileExpr(n.template, frame)
	if err != nil {
		return nil, "", err
	}
	return func(a *act, then func(t *Template) error) error {
		name, err := expr(a)
		if err != nil {
			return err
		}
		return a.context().env.getTemplate(name, eager, parentName, ignoreMissing, func(e *js.Error, t *Template) error {
			if e != nil {
				return a.cb()(e, js.Undefined)
			}
			return then(t)
		})
	}, id, nil
}

func (c *compiler) compileImport(n *node, frame *compileFrame) (stmtFn, error) {
	target := n.target.symbolName()
	get, id, err := c.getTemplateStep(n, frame, false, false)
	if err != nil {
		return nil, err
	}
	frame.set(target, &slot{id})
	hasParent := frame.parent != nil
	withContext := js.Truthy(n.withContext)
	return func(a *act, k func() error) error {
		return get(a, func(t *Template) error {
			a.vars[id] = t
			done := callback(func(e *js.Error, exported js.Value) error {
				if e != nil {
					return a.cb()(e, js.Undefined)
				}
				a.vars[id] = exported
				if hasParent {
					a.frame().set(target, exported, false)
				} else {
					a.context().setVariable(target, exported)
				}
				return k()
			})
			if withContext {
				return t.getExported(a.context().ctx, a.frame(), done)
			}
			return t.getExported(nil, nil, done)
		})
	}, nil
}

func (c *compiler) compileFromImport(n *node, frame *compileFrame) (stmtFn, error) {
	get, _, err := c.getTemplateStep(n, frame, false, false)
	if err != nil {
		return nil, err
	}
	type imported struct{ name, alias, id string }
	var names []imported
	for _, nn := range n.names.children {
		var name, alias string
		if nn.typ == "Pair" {
			name, alias = nn.key.symbolName(), nn.value.symbolName()
		} else {
			name = nn.symbolName()
			alias = name
		}
		id := c.tmpid()
		c.fn.declare(id)
		frame.set(alias, &slot{id})
		names = append(names, imported{name, alias, id})
	}
	hasParent := frame.parent != nil
	withContext := js.Truthy(n.withContext)
	return func(a *act, k func() error) error {
		return get(a, func(t *Template) error {
			done := callback(func(e *js.Error, exported js.Value) error {
				if e != nil {
					return a.cb()(e, js.Undefined)
				}
				for _, im := range names {
					if !js.HasOwn(exported, im.name) {
						return a.cb()(js.NewError("cannot import '"+im.name+"'"), js.Undefined)
					}
					v := js.GetKey(exported, im.name)
					a.put(im.id, v)
					if hasParent {
						a.frame().set(im.alias, v, false)
					} else {
						a.context().setVariable(im.alias, v)
					}
				}
				return k()
			})
			if withContext {
				return t.getExported(a.context().ctx, a.frame(), done)
			}
			return t.getExported(nil, nil, done)
		})
	}, nil
}

func (c *compiler) compileBlock(n *node) (stmtFn, error) {
	c.tmpid()
	name := n.name.symbolName()
	inBlock := c.inBlock
	buffer := c.buffer
	return func(a *act, k func() error) error {
		done := callback(func(e *js.Error, res js.Value) error {
			if e != nil {
				return a.cb()(e, js.Undefined)
			}
			if err := a.appendTo(buffer, res); err != nil {
				return err
			}
			return k()
		})
		if !inBlock && a.parentTemplate() != nil {
			return done(nil, js.Undefined)
		}
		b, err := a.context().getBlock(name)
		if err != nil {
			return err
		}
		return b.run(a.context(), a.frame(), done)
	}, nil
}

func (c *compiler) compileSuper(n *node, frame *compileFrame) (stmtFn, error) {
	name := n.blockName.symbolName()
	id := n.symbol.symbolName()
	frame.set(id, &slot{id})
	c.tmpid()
	blocks := c.blocks
	return func(a *act, k func() error) error {
		self := blocks[name]
		if self == nil {
			return &js.Error{Name: "ReferenceError", Message: "b_" + name + " is not defined"}
		}
		return a.context().getSuper(name, self, a.frame(), func(e *js.Error, res js.Value) error {
			if e != nil {
				return a.cb()(e, js.Undefined)
			}
			a.vars[id] = markSafe(res)
			return k()
		})
	}, nil
}

func (c *compiler) compileExtends(n *node, frame *compileFrame) (stmtFn, error) {
	c.tmpid()
	get, _, err := c.getTemplateStep(n, frame, true, false)
	if err != nil {
		return nil, err
	}
	return func(a *act, k func() error) error {
		return get(a, func(t *Template) error {
			if a.find("parentTemplate") != nil {
				a.put("parentTemplate", t)
			}
			for _, b := range t.blocks {
				a.context().addBlock(b.name, b)
			}
			return k()
		})
	}, nil
}

func (c *compiler) compileInclude(n *node, frame *compileFrame) (stmtFn, error) {
	c.fn.declare("tasks")
	get, _, err := c.getTemplateStep(n, frame, false, n.ignoreMissing)
	if err != nil {
		return nil, err
	}
	c.tmpid()
	buffer := c.buffer
	return func(a *act, k func() error) error {
		return get(a, func(t *Template) error {
			_, err := t.render(a.context().ctx, a.frame(), func(e *js.Error, res js.Value) error {
				if e != nil {
					return a.cb()(e, js.Undefined)
				}
				if err := a.appendTo(buffer, res); err != nil {
					return err
				}
				return k()
			})
			return err
		})
	}, nil
}

func (c *compiler) compileCapture(n *node, frame *compileFrame) (exprFn, error) {
	outer, outerBuffer := c.fn, c.buffer
	scope := &fnScope{}
	scope.declare("output")
	c.fn = scope
	c.buffer = "output"
	body, err := c.compileStmt(n.body, frame)
	c.fn, c.buffer = outer, outerBuffer
	if err != nil {
		return nil, err
	}
	return func(a *act) (js.Value, error) {
		cap := newAct(a, scope)
		cap.vars["output"] = ""
		if err := runOpt(body, cap); err != nil {
			return nil, err
		}
		return cap.value("output"), nil
	}, nil
}

// ---- root ----

func (c *compiler) compileRoot(root *node) (*compiledTemplate, error) {
	ct := &compiledTemplate{}
	c.blocks = map[string]*blockFunc{}
	rootScope := &fnScope{}
	c.fn = rootScope
	c.buffer = "output"
	for _, n := range []string{"frame", "lineno", "colno", "output", "cb", "context", "parentTemplate"} {
		rootScope.declare(n)
	}
	body, err := c.compileList(root.children, &compileFrame{names: map[string]*slot{}})
	if err != nil {
		return nil, err
	}
	ct.root = func(ctx *Context, frame *Frame, cb callback) error {
		a := newAct(nil, rootScope)
		a.vars["frame"], a.vars["context"], a.vars["cb"] = frame, ctx, cb
		a.vars["lineno"], a.vars["colno"], a.vars["output"] = root.lineno, root.colno, ""
		a.vars["parentTemplate"] = nil
		err := body(a, func() error {
			if pt := a.parentTemplate(); pt != nil {
				return pt.compiled.root(ctx, a.frame(), cb)
			}
			return cb(nil, a.value("output"))
		})
		if err != nil {
			l, col := a.pos()
			return cb(handleError(err, l, col), js.Undefined)
		}
		return nil
	}
	c.inBlock = true
	var names []string
	blockNodes := root.findAll("Block", nil)
	for _, b := range blockNodes {
		name := b.name.symbolName()
		for _, seen := range names {
			if seen == name {
				return nil, js.NewError(`Block "` + name + `" defined more than once.`)
			}
		}
		names = append(names, name)
		bf := &blockFunc{name: name}
		c.blocks[name] = bf
		ct.blocks = append(ct.blocks, bf)
	}
	for i, b := range blockNodes {
		scope := &fnScope{}
		for _, n := range []string{"frame", "lineno", "colno", "output", "cb", "context"} {
			scope.declare(n)
		}
		c.fn = scope
		c.buffer = "output"
		body, err := c.compileStmt(b.body, &compileFrame{names: map[string]*slot{}})
		if err != nil {
			return nil, err
		}
		lineno, colno := b.lineno, b.colno
		ct.blocks[i].run = func(ctx *Context, frame *Frame, cb callback) error {
			a := newAct(nil, scope)
			a.vars["context"], a.vars["cb"] = ctx, cb
			a.vars["lineno"], a.vars["colno"], a.vars["output"] = lineno, colno, ""
			a.vars["frame"] = frame.push(true)
			err := runStmt(body, a, func() error { return cb(nil, a.value("output")) })
			if err != nil {
				l, col := a.pos()
				return cb(handleError(err, l, col), js.Undefined)
			}
			return nil
		}
	}
	if c.syntaxErr != nil {
		return nil, c.syntaxErr
	}
	return ct, nil
}

func runStmt(s stmtFn, a *act, k func() error) error {
	if s == nil {
		return k()
	}
	return s(a, k)
}
