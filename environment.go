// Ported from nunjucks/src/environment.js, nunjucks/src/loader.js and nunjucks/src/node-loaders.js of
// mozilla/nunjucks (BSD-2-Clause, see LICENSE).

package nunjucks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mozilla/nunjucks/js"
)

// Options configures an Environment. Autoescape is on in DefaultOptions, as in nunjucks.
type Options struct {
	Autoescape       bool
	ThrowOnUndefined bool
	TrimBlocks       bool
	LstripBlocks     bool
	// Dev keeps template errors as they are raised instead of rewrapping them on the way out of each template.
	Dev  bool
	Tags Tags
}

// DefaultOptions returns the nunjucks defaults.
func DefaultOptions() Options { return Options{Autoescape: true} }

// Environment holds the loaders, filters, tests and globals templates are rendered with.
type Environment struct {
	opts        Options
	loaders     []*FileSystemLoader
	globals     *js.Object
	filters     map[string]filterFunc
	tests       map[string]testFunc
	jinjaCompat bool
	sym         int
}

// ErrAsyncCallback reports a render that the original only finishes asynchronously, after its synchronous
// render has already returned: an included template that fails to compile.
var ErrAsyncCallback = errors.New("nunjucks: an included template failed to compile after the render returned")

// NewEnvironment returns an environment that loads templates with loader (nil: the "views" directory).
func NewEnvironment(loader *FileSystemLoader, opts Options) *Environment {
	if loader == nil {
		loader = NewFileSystemLoader([]string{"views"})
	}
	env := &Environment{opts: opts, loaders: []*FileSystemLoader{loader}}
	loader.cache = map[string]*Template{}
	env.globals = newGlobals()
	env.filters = builtinFilters()
	env.tests = builtinTests()
	return env
}

// InstallJinjaCompat turns on the Jinja compatibility layer: True/False/None, Python list and dict methods,
// and slices.
func (env *Environment) InstallJinjaCompat() { env.jinjaCompat = true }

func (env *Environment) getFilter(name string) (filterFunc, error) {
	f, ok := env.filters[name]
	if !ok {
		return nil, js.NewError("filter not found: " + name)
	}
	return f, nil
}

func (env *Environment) getTest(name string) (testFunc, error) {
	t, ok := env.tests[name]
	if !ok {
		return nil, js.NewError("test not found: " + name)
	}
	return t, nil
}

func (env *Environment) prettify(path string, err error) *js.Error {
	if !env.opts.Dev {
		return prettifyError(path, err)
	}
	e := asJSError(err)
	if !e.Template {
		e = wrapError(e, 0, 0)
	}
	update(e, path)
	return e
}

func (env *Environment) resolveTemplate(l *FileSystemLoader, parentName js.Value, filename string) string {
	if p, ok := parentName.(string); ok && p != "" && l.isRelative(filename) {
		return l.resolve(p, filename)
	}
	return filename
}

// getTemplate loads (or takes from the cache) the template nameV and passes it to cb.
func (env *Environment) getTemplate(nameV js.Value, eager bool, parentName js.Value, ignoreMissing bool,
	cb func(*js.Error, *Template) error) error {
	if js.Truthy(nameV) {
		raw, err := js.Get(nameV, "raw")
		if err != nil {
			return err
		}
		if js.Truthy(raw) {
			nameV = raw
		}
	}
	name, ok := nameV.(string)
	if !ok {
		return js.NewError("template names must be a string: " + js.ToString(nameV))
	}
	for _, l := range env.loaders {
		if t := l.cache[env.resolveTemplate(l, parentName, name)]; t != nil {
			if eager {
				if err := t.compile(); err != nil {
					return err
				}
			}
			return cb(nil, t)
		}
	}
	var info *source
	var loader *FileSystemLoader
	for _, l := range env.loaders {
		name = env.resolveTemplate(l, parentName, name)
		src, err := l.getSource(name)
		if err != nil {
			return err
		}
		if src != nil {
			info, loader = src, l
			break
		}
	}
	if info == nil {
		if !ignoreMissing {
			return cb(js.NewError("template not found: "+name), nil)
		}
		return cb(nil, noopTemplate(env))
	}
	t, err := newTemplate(info.src, env, info.path, eager)
	if err != nil {
		return err
	}
	loader.cache[name] = t
	return cb(nil, t)
}

// Render renders the template called name with the context ctx.
func (env *Environment) Render(name string, ctx *js.Object) (string, error) {
	var out js.Value = js.Null
	err := env.getTemplate(name, false, js.Null, false, func(e *js.Error, t *Template) error {
		if e != nil {
			return e
		}
		res, err := t.render(ctx, nil, nil)
		out = res
		return err
	})
	if err != nil {
		return "", err
	}
	return js.ToString(out), nil
}

// RenderString renders template source that has no path.
func (env *Environment) RenderString(src string, ctx *js.Object) (string, error) {
	t, err := newTemplate(src, env, "", false)
	if err != nil {
		return "", err
	}
	res, err := t.render(ctx, nil, nil)
	if err != nil {
		return "", err
	}
	return js.ToString(res), nil
}

// Template is a template source and, once compiled, its root and block functions.
type Template struct {
	env      *Environment
	src      string
	path     string
	noop     bool
	compiled *compiledTemplate
	blocks   []*blockFunc
}

func newTemplate(src string, env *Environment, path string, eager bool) (*Template, error) {
	t := &Template{env: env, src: src, path: path}
	if eager {
		if err := t.doCompile(); err != nil {
			return nil, env.prettify(t.path, err)
		}
	}
	return t, nil
}

// noopTemplate is the empty template of {% include ... ignore missing %}.
func noopTemplate(env *Environment) *Template {
	t := &Template{env: env, noop: true}
	t.compiled = &compiledTemplate{root: func(_ *Context, _ *Frame, cb callback) error {
		if err := cb(nil, ""); err != nil {
			return cb(handleError(err, 0, 0), js.Undefined)
		}
		return nil
	}}
	return t
}

func (t *Template) compile() error {
	if t.compiled == nil {
		return t.doCompile()
	}
	return nil
}

func (t *Template) doCompile() error {
	root, err := parseTemplate(t.src, &t.env.opts, t.env.jinjaCompat)
	if err != nil {
		return err
	}
	root = t.env.transform(root)
	var name js.Value = js.Undefined
	if t.path != "" {
		name = t.path
	}
	c := &compiler{env: t.env, templateName: name, throwOnUndefined: t.env.opts.ThrowOnUndefined}
	ct, err := c.compileRoot(root)
	if err != nil {
		return err
	}
	t.compiled = ct
	t.blocks = ct.blocks
	return nil
}

// render is Template.render: with a callback (an include) the result goes to cb, otherwise it is returned.
func (t *Template) render(ctx *js.Object, parentFrame *Frame, cb callback) (js.Value, error) {
	if err := t.compile(); err != nil {
		perr := t.env.prettify(t.path, err)
		if cb != nil {
			return nil, ErrAsyncCallback
		}
		return nil, perr
	}
	context := newContext(ctx, t.blocks, t.env)
	var frame *Frame
	if parentFrame != nil {
		frame = parentFrame.push(true)
	} else {
		frame = newFrame(nil, false)
	}
	frame.topLevel = true
	var syncResult js.Value = js.Null
	didError := false
	err := t.compiled.root(context, frame, func(e *js.Error, res js.Value) error {
		if didError && cb != nil && !js.IsUndefined(res) {
			return nil
		}
		if e != nil {
			e = t.env.prettify(t.path, e)
			didError = true
		}
		if cb != nil {
			return cb(e, res)
		}
		if e != nil {
			return e
		}
		syncResult = res
		return nil
	})
	return syncResult, err
}

// getExported runs the template to collect its top-level exports (import and from-import).
func (t *Template) getExported(ctx *js.Object, parentFrame *Frame, cb callback) error {
	if err := t.compile(); err != nil {
		return cb(asJSError(err), js.Undefined)
	}
	var frame *Frame
	if parentFrame != nil {
		frame = parentFrame.push(false)
	} else {
		frame = newFrame(nil, false)
	}
	frame.topLevel = true
	context := newContext(ctx, t.blocks, t.env)
	return t.compiled.root(context, frame, func(e *js.Error, _ js.Value) error {
		if e != nil {
			return cb(e, js.Null)
		}
		return cb(nil, context.getExported())
	})
}

// FileSystemLoader loads templates from search directories.
type FileSystemLoader struct {
	searchPaths []string
	cache       map[string]*Template
}

type source struct {
	src  string
	path string
}

// NewFileSystemLoader returns a loader over the search paths (none: the current directory).
func NewFileSystemLoader(searchPaths []string) *FileSystemLoader {
	l := &FileSystemLoader{cache: map[string]*Template{}}
	if len(searchPaths) == 0 {
		l.searchPaths = []string{"."}
		return l
	}
	for _, p := range searchPaths {
		l.searchPaths = append(l.searchPaths, normalizePath(p))
	}
	return l
}

// normalizePath is Node's path.normalize.
func normalizePath(p string) string {
	if p == "" {
		return "."
	}
	trailing := strings.HasSuffix(p, "/")
	p = filepath.Clean(p)
	if trailing && p != "/" {
		p += "/"
	}
	return p
}

// resolvePath is Node's path.resolve(base, name).
func resolvePath(base, name string) string {
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	if !filepath.IsAbs(base) {
		cwd, err := os.Getwd()
		if err == nil {
			base = filepath.Join(cwd, base)
		}
	}
	return filepath.Clean(filepath.Join(base, name))
}

func (l *FileSystemLoader) isRelative(name string) bool {
	return strings.HasPrefix(name, "./") || strings.HasPrefix(name, "../")
}

func (l *FileSystemLoader) resolve(from, to string) string {
	return resolvePath(filepath.Dir(from), to)
}

func (l *FileSystemLoader) getSource(name string) (*source, error) {
	full := ""
	for _, sp := range l.searchPaths {
		base := resolvePath(sp, "")
		p := resolvePath(sp, name)
		if strings.HasPrefix(p, base) {
			if _, err := os.Stat(p); err == nil {
				full = p
				break
			}
		}
	}
	if full == "" {
		return nil, nil
	}
	if fi, err := os.Stat(full); err == nil && fi.IsDir() {
		return nil, &js.Error{Name: "Error", Message: "EISDIR: illegal operation on a directory, read"}
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, js.NewError(err.Error())
	}
	return &source{src: js.FromUTF8(data), path: full}, nil
}
