// Command nunjucks-render renders one nunjucks template.
//
//	nunjucks-render [flags] TEMPLATE
//	  -p, --path DIR          add a template search directory (repeatable; default: the current directory)
//	  -c, --context FILE      read the render context from a JSON file holding an object ("-" reads stdin)
//	  -s, --string            TEMPLATE is template source instead of a template name
//	      --no-autoescape     turn autoescaping off (it is on by default)
//	      --throw-on-undefined, --trim-blocks, --lstrip-blocks
//	      --tags JSON         custom delimiters: blockStart, blockEnd, variableStart, variableEnd,
//	                          commentStart and/or commentEnd
//	      --jinja-compat      install the Jinja compatibility layer before rendering
//	      --dev               keep template errors as raised instead of rewrapping them per template
//
// The rendered text goes to stdout (exit status 0). A render or template error is printed to stderr as the
// error's name and message (exit status 1); a usage error prints one "nunjucks-render: " line (exit status 2).
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mozilla/nunjucks"
	"github.com/mozilla/nunjucks/js"
)

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

type options struct {
	paths       []string
	context     string
	hasContext  bool
	isString    bool
	tags        string
	hasTags     bool
	jinjaCompat bool
	opts        nunjucks.Options
	template    string
}

func parseArgs(argv []string) (*options, error) {
	o := &options{opts: nunjucks.DefaultOptions()}
	var positional []string
	value := func(i int, flag string) (string, error) {
		if i+1 >= len(argv) {
			return "", &usageError{"flag " + flag + " needs a value"}
		}
		return argv[i+1], nil
	}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			positional = append(positional, argv[i+1:]...)
			break
		}
		switch a {
		case "-p", "--path", "-c", "--context", "--tags":
			v, err := value(i, a)
			if err != nil {
				return nil, err
			}
			i++
			switch a {
			case "-p", "--path":
				o.paths = append(o.paths, v)
			case "-c", "--context":
				o.context, o.hasContext = v, true
			default:
				o.tags, o.hasTags = v, true
			}
		case "-s", "--string":
			o.isString = true
		case "--no-autoescape":
			o.opts.Autoescape = false
		case "--throw-on-undefined":
			o.opts.ThrowOnUndefined = true
		case "--trim-blocks":
			o.opts.TrimBlocks = true
		case "--lstrip-blocks":
			o.opts.LstripBlocks = true
		case "--jinja-compat":
			o.jinjaCompat = true
		case "--dev":
			o.opts.Dev = true
		default:
			if len(a) > 1 && a[0] == '-' {
				return nil, &usageError{"unknown flag " + a}
			}
			positional = append(positional, a)
		}
	}
	if len(positional) != 1 {
		return nil, &usageError{"expected exactly one TEMPLATE argument"}
	}
	o.template = positional[0]
	return o, nil
}

func readContext(o *options, stdin io.Reader) (*js.Object, error) {
	if !o.hasContext {
		return js.NewObject(), nil
	}
	var data []byte
	var err error
	if o.context == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(o.context)
	}
	if err != nil {
		return nil, &usageError{"cannot read context " + o.context}
	}
	v, err := js.ParseJSON(js.FromUTF8(data))
	if err != nil {
		return nil, &usageError{"context is not valid JSON"}
	}
	obj, ok := v.(*js.Object)
	if !ok {
		return nil, &usageError{"context must be a JSON object"}
	}
	return obj, nil
}

func readTags(o *options) (nunjucks.Tags, error) {
	var tags nunjucks.Tags
	if !o.hasTags {
		return tags, nil
	}
	v, err := js.ParseJSON(o.tags)
	if err != nil {
		return tags, &usageError{"--tags is not valid JSON"}
	}
	obj, ok := v.(*js.Object)
	if !ok {
		return tags, &usageError{"--tags must be a JSON object"}
	}
	str := func(k string) string {
		v, _ := obj.Get(k)
		if !js.Truthy(v) {
			return ""
		}
		return js.ToString(v)
	}
	tags.BlockStart, tags.BlockEnd = str("blockStart"), str("blockEnd")
	tags.VariableStart, tags.VariableEnd = str("variableStart"), str("variableEnd")
	tags.CommentStart, tags.CommentEnd = str("commentStart"), str("commentEnd")
	return tags, nil
}

func run(argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	o, err := parseArgs(argv)
	var ctx *js.Object
	if err == nil {
		ctx, err = readContext(o, stdin)
	}
	if err == nil {
		o.opts.Tags, err = readTags(o)
	}
	if err != nil {
		fmt.Fprintf(stderr, "nunjucks-render: %s\n", err)
		return 2
	}
	env := nunjucks.NewEnvironment(nunjucks.NewFileSystemLoader(o.paths), o.opts)
	if o.jinjaCompat {
		env.InstallJinjaCompat()
	}
	var out string
	if o.isString {
		out, err = env.RenderString(o.template, ctx)
	} else {
		out, err = env.Render(o.template, ctx)
	}
	if err != nil {
		var jsErr *js.Error
		if errors.As(err, &jsErr) {
			fmt.Fprintln(stderr, js.ToUTF8(jsErr.String()))
		} else {
			fmt.Fprintln(stderr, err)
		}
		return 1
	}
	if _, err := io.WriteString(stdout, js.ToUTF8(out)); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
