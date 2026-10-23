// Ported from nunjucks/src/runtime.js and the Context class of nunjucks/src/environment.js of mozilla/nunjucks
// (BSD-2-Clause, see LICENSE).

package nunjucks

import (
	"strings"

	"github.com/mozilla/nunjucks/js"
)

// Frame is a scope of template variables. Frames are used both by the compiler (names bound to compiled
// slots) and at run time (variable values).
type Frame struct {
	variables     map[string]js.Value
	parent        *Frame
	topLevel      bool
	isolateWrites bool
}

func newFrame(parent *Frame, isolateWrites bool) *Frame {
	return &Frame{variables: map[string]js.Value{}, parent: parent, isolateWrites: isolateWrites}
}

func (f *Frame) push(isolateWrites bool) *Frame { return newFrame(f, isolateWrites) }

func (f *Frame) pop() *Frame { return f.parent }

// set assigns a plain (undotted) name. With resolveUp the frame that already holds the name is written instead.
func (f *Frame) set(name string, val js.Value, resolveUp bool) {
	if resolveUp {
		if fr := f.resolve(name, true); fr != nil {
			fr.variables[name] = val
			return
		}
	}
	f.variables[name] = val
}

// setPath assigns a dotted name such as loop.index, creating the intermediate objects; assigning into a
// primitive fails as in strict-mode JavaScript.
func (f *Frame) setPath(name string, val js.Value) error {
	parts := strings.Split(name, ".")
	if len(parts) == 1 {
		f.variables[name] = val
		return nil
	}
	cur, ok := f.variables[parts[0]]
	if !ok || !js.Truthy(cur) {
		cur = js.NewObject()
		f.variables[parts[0]] = cur
	}
	for _, id := range parts[1 : len(parts)-1] {
		next := js.GetKey(cur, id)
		if !js.Truthy(next) {
			next = js.NewObject()
			if err := setProperty(cur, id, next); err != nil {
				return err
			}
		}
		cur = next
	}
	return setProperty(cur, parts[len(parts)-1], val)
}

// setProperty is a property assignment in strict-mode code: assigning to a primitive fails.
func setProperty(obj js.Value, key string, val js.Value) error {
	switch obj.(type) {
	case float64, string, bool:
		return js.NewTypeError("Cannot create property '%s' on %s '%s'", key, js.TypeOf(obj), js.ToString(obj))
	}
	return js.Set(obj, key, val)
}

func (f *Frame) own(name string) (js.Value, bool) {
	v, ok := f.variables[name]
	if !ok || js.IsUndefined(v) {
		return nil, false
	}
	return v, true
}

func (f *Frame) lookup(name string) js.Value {
	for fr := f; fr != nil; fr = fr.parent {
		if v, ok := fr.own(name); ok {
			return v
		}
	}
	return js.Undefined
}

func (f *Frame) resolve(name string, forWrite bool) *Frame {
	if _, ok := f.own(name); ok {
		return f
	}
	if forWrite && f.isolateWrites {
		return nil
	}
	for fr := f.parent; fr != nil; fr = fr.parent {
		if _, ok := fr.own(name); ok {
			return fr
		}
	}
	return nil
}

// compileFrame is the compiler's frame: it maps template names to the compiled slots that hold them.
type compileFrame struct {
	names  map[string]*slot
	parent *compileFrame
}

func (f *compileFrame) push() *compileFrame {
	return &compileFrame{names: map[string]*slot{}, parent: f}
}

func (f *compileFrame) set(name string, s *slot) { f.names[name] = s }

func (f *compileFrame) lookup(name string) *slot {
	for fr := f; fr != nil; fr = fr.parent {
		if s, ok := fr.names[name]; ok {
			return s
		}
	}
	return nil
}

// Context holds the variables of one template render and the blocks available to it.
type Context struct {
	env      *Environment
	ctx      *js.Object
	blocks   map[string][]*blockFunc
	exported []string
}

func newContext(ctx *js.Object, blocks []*blockFunc, env *Environment) *Context {
	c := &Context{env: env, ctx: js.NewObject(), blocks: map[string][]*blockFunc{}}
	if ctx != nil {
		for _, k := range ctx.Keys() {
			v, _ := ctx.Get(k)
			c.ctx.Set(k, v)
		}
	}
	for _, b := range blocks {
		c.addBlock(b.name, b)
	}
	return c
}

func (c *Context) lookup(name string) js.Value {
	if js.HasProperty(c.env.globals, name) && !js.HasProperty(c.ctx, name) {
		return js.GetKey(c.env.globals, name)
	}
	return js.GetKey(c.ctx, name)
}

func (c *Context) setVariable(name string, val js.Value) { c.ctx.Set(name, val) }

func (c *Context) addBlock(name string, b *blockFunc) { c.blocks[name] = append(c.blocks[name], b) }

func (c *Context) getBlock(name string) (*blockFunc, error) {
	bs := c.blocks[name]
	if len(bs) == 0 {
		return nil, js.NewError(`unknown block "` + name + `"`)
	}
	return bs[0], nil
}

func (c *Context) getSuper(name string, block *blockFunc, frame *Frame, cb callback) error {
	bs, ok := c.blocks[name]
	idx := -1
	for i, b := range bs {
		if b == block {
			idx = i
			break
		}
	}
	if !ok {
		return js.ReadPropertyError(js.Undefined, "0")
	}
	if idx == -1 || idx+1 >= len(bs) {
		return js.NewError(`no super block available for "` + name + `"`)
	}
	return bs[idx+1].run(c, frame, cb)
}

func (c *Context) addExport(name string) { c.exported = append(c.exported, name) }

func (c *Context) getExported() *js.Object {
	out := js.NewObject()
	for _, name := range c.exported {
		out.Set(name, js.GetKey(c.ctx, name))
	}
	return out
}

// makeKeywordArgs marks a keyword-argument object.
func makeKeywordArgs(obj *js.Object) *js.Object {
	obj.Set("__keywords", true)
	return obj
}

func isKeywordArgs(v js.Value) bool {
	o, ok := v.(*js.Object)
	return ok && o.Has("__keywords")
}

func getKeywordArgs(args []js.Value) *js.Object {
	if len(args) > 0 && isKeywordArgs(args[len(args)-1]) {
		return args[len(args)-1].(*js.Object)
	}
	return js.NewObject()
}

func numArgs(args []js.Value) int {
	if len(args) > 0 && isKeywordArgs(args[len(args)-1]) {
		return len(args) - 1
	}
	return len(args)
}

// makeMacro is runtime.makeMacro: it maps positional and keyword arguments onto the declared names before
// calling fn, whose last argument is the keyword-argument object.
func makeMacro(argNames, kwargNames []string, fn func(args []js.Value) (js.Value, error)) *js.Function {
	return js.NewFunction("macro", func(_ js.Value, macroArgs []js.Value) (js.Value, error) {
		argCount := numArgs(macroArgs)
		kwargs := getKeywordArgs(macroArgs)
		var args []js.Value
		switch {
		case argCount > len(argNames):
			args = append([]js.Value(nil), macroArgs[:len(argNames)]...)
			for i, val := range macroArgs[len(args):argCount] {
				if i < len(kwargNames) {
					kwargs.Set(kwargNames[i], val)
				}
			}
			args = append(args, kwargs)
		case argCount < len(argNames):
			args = append([]js.Value(nil), macroArgs[:argCount]...)
			for _, arg := range argNames[argCount:] {
				args = append(args, js.GetKey(kwargs, arg))
				kwargs.Delete(arg)
			}
			args = append(args, kwargs)
		default:
			args = macroArgs
		}
		return fn(args)
	})
}

// markSafe is runtime.markSafe.
func markSafe(v js.Value) js.Value {
	switch x := v.(type) {
	case string:
		return &js.SafeString{Val: x}
	case *js.Function:
		return js.NewFunction(x.Name, func(this js.Value, args []js.Value) (js.Value, error) {
			r, err := x.Call(this, args)
			if s, ok := r.(string); ok && err == nil {
				return &js.SafeString{Val: s}, nil
			}
			return r, err
		})
	}
	return v
}

// copySafeness is runtime.copySafeness.
func copySafeness(dest js.Value, target js.Value) (js.Value, error) {
	if _, ok := dest.(*js.SafeString); ok {
		s, err := js.ToStringErr(target)
		if err != nil {
			return nil, err
		}
		return &js.SafeString{Val: s}, nil
	}
	return callToString(target, "target")
}

// callToString is the JavaScript expression expr.toString().
func callToString(v js.Value, expr string) (js.Value, error) {
	if o, ok := v.(*js.Object); ok {
		if t, ok := o.Get("toString"); ok {
			return js.Call(t, o, nil, expr+".toString")
		}
	}
	return js.CallMethod(v, "toString", nil, expr)
}

// escapeHTML is lib.escape.
func escapeHTML(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '\\':
			b.WriteString("&#92;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeValue is lib.escape(val.toString()) as the escaping filters and the output code use it.
func escapeValue(v js.Value, expr string) (string, error) {
	s, err := callToString(v, expr)
	if err != nil {
		return "", err
	}
	str, ok := s.(string)
	if !ok {
		return "", js.NotFunctionError("val.replace")
	}
	return escapeWTF(str), nil
}

// escapeWTF escapes a JavaScript string without disturbing unpaired surrogates.
func escapeWTF(s string) string {
	if !strings.ContainsAny(s, "&\"'<>\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '&', '"', '\'', '<', '>', '\\':
			b.WriteString(escapeHTML(string(c)))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// suppressValue is runtime.suppressValue.
func suppressValue(v js.Value, autoescape bool) (js.Value, error) {
	if js.IsNullish(v) {
		v = ""
	}
	if _, safe := v.(*js.SafeString); autoescape && !safe {
		s, err := escapeValue(v, "val")
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	return v, nil
}

// ensureDefined is runtime.ensureDefined.
func ensureDefined(v js.Value, lineno, colno int) (js.Value, error) {
	if js.IsNullish(v) {
		return nil, templateError("attempted to output null or undefined value", lineno+1, colno+1)
	}
	return v, nil
}

// memberLookup is runtime.memberLookup: method values come back bound to the object they were read from.
func memberLookup(obj, key js.Value) (js.Value, error) {
	if js.IsNullish(obj) {
		return js.Undefined, nil
	}
	v, err := js.Get(obj, key)
	if err != nil {
		return nil, err
	}
	if fn, ok := v.(*js.Function); ok {
		wrapper := js.NewFunction(fn.Name, func(_ js.Value, args []js.Value) (js.Value, error) {
			cur, err := js.Get(obj, key)
			if err != nil {
				return nil, err
			}
			return js.Call(cur, obj, args, "obj[val].apply")
		})
		wrapper.Source = "(...args) => obj[val].apply(obj, args)"
		return wrapper, nil
	}
	return v, nil
}

// callWrap is runtime.callWrap.
func callWrap(obj js.Value, name string, args []js.Value) (js.Value, error) {
	if !js.Truthy(obj) {
		return nil, js.NewError("Unable to call `" + name + "`, which is undefined or falsey")
	}
	fn, ok := obj.(*js.Function)
	if !ok {
		return nil, js.NewError("Unable to call `" + name + "`, which is not a function")
	}
	return fn.Call(js.Undefined, args)
}

// inOperator is lib.inOperator.
func inOperator(key, val js.Value) (js.Value, error) {
	switch v := val.(type) {
	case *js.Array:
		for _, it := range v.Items {
			if js.StrictEquals(it, key) {
				return true, nil
			}
		}
		return false, nil
	case string:
		k, err := js.ToStringErr(key)
		if err != nil {
			return nil, err
		}
		return js.IndexOf16(v, k, 0) != -1, nil
	case *js.Object, *js.SafeString:
		k, err := js.ToPropertyKey(key)
		if err != nil {
			return nil, err
		}
		return js.HasProperty(v, k), nil
	}
	return nil, js.NewError(`Cannot use "in" operator to search for "` + js.ToString(key) + `" in unexpected types.`)
}

// fromIterator is runtime.fromIterator: SafeString objects iterate as their code points.
func fromIterator(v js.Value) js.Value {
	if s, ok := v.(*js.SafeString); ok {
		return &js.Array{Items: js.U16Points(s.Val)}
	}
	return v
}

// keysOf is lib.keys: the own enumerable keys in for-in order.
func keysOf(v js.Value) []string {
	if js.IsNullish(v) {
		return nil
	}
	return js.OwnKeys(v)
}
